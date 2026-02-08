package installer

import (
    "fmt"
    "os"
    "os/exec"
    "os/user"
    "strings"
)

// checkOS verifies we're running on Debian.
func checkOS() error {
    data, err := os.ReadFile("/etc/os-release")
    if err != nil {
        return fmt.Errorf("cannot read /etc/os-release — is this Linux?")
    }

    if !strings.Contains(string(data), "ID=debian") {
        return fmt.Errorf("unsupported OS — Virtual Private Node requires Debian 12+")
    }

    return nil
}

// createSystemUser creates the system user that runs bitcoind and lnd.
// This is a non-login system user separate from the ripsline admin user.
func createSystemUser(username string) error {
    if _, err := user.Lookup(username); err == nil {
        fmt.Printf("    User '%s' already exists, skipping\n", username)
        return nil
    }

    cmd := exec.Command("adduser",
        "--system", "--group",
        "--home", "/var/lib/bitcoin",
        "--shell", "/usr/sbin/nologin",
        username)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }

    return nil
}

// createDirs creates the FHS-compliant directory structure.
// Config files go in /etc, data goes in /var/lib.
func createDirs(username string, cfg *installConfig) error {
    dirs := []struct {
        path  string
        owner string
        mode  os.FileMode
    }{
        {"/etc/bitcoin", "root:" + username, 0750},
        {"/var/lib/bitcoin", username + ":" + username, 0750},
    }

    // LND directories only if LND is being installed
    if cfg.components == "bitcoin+lnd" {
        dirs = append(dirs,
            struct {
                path  string
                owner string
                mode  os.FileMode
            }{"/etc/lnd", "root:" + username, 0750},
            struct {
                path  string
                owner string
                mode  os.FileMode
            }{"/var/lib/lnd", username + ":" + username, 0750},
        )
    }

    for _, d := range dirs {
        if err := os.MkdirAll(d.path, d.mode); err != nil {
            return fmt.Errorf("mkdir %s: %w", d.path, err)
        }
        cmd := exec.Command("chown", d.owner, d.path)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("chown %s: %s: %s", d.path, err, output)
        }
        if err := os.Chmod(d.path, d.mode); err != nil {
            return fmt.Errorf("chmod %s: %w", d.path, err)
        }
    }

    return nil
}

// disableIPv6 prevents IPv6 traffic that could bypass Tor.
func disableIPv6() error {
    content := `# Virtual Private Node — disable IPv6 to prevent Tor bypass
net.ipv6.conf.all.disable_ipv6 = 1
net.ipv6.conf.default.disable_ipv6 = 1
net.ipv6.conf.lo.disable_ipv6 = 1
`
    if err := os.WriteFile("/etc/sysctl.d/99-disable-ipv6.conf", []byte(content), 0644); err != nil {
        return err
    }

    cmd := exec.Command("sysctl", "--system")
    cmd.Stdout = nil // suppress verbose output
    cmd.Stderr = nil
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("sysctl --system: %w", err)
    }

    return nil
}

// configureFirewall sets up UFW with minimal open ports.
// Only SSH is always open. Port 9735 opens only for LND hybrid mode.
func configureFirewall(cfg *installConfig) error {
    // Install UFW if missing
    cmd := exec.Command("apt-get", "install", "-y", "-qq", "ufw")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("install ufw: %s: %s", err, output)
    }

    // Disable IPv6 in UFW
    ufwDefault, err := os.ReadFile("/etc/default/ufw")
    if err == nil {
        content := strings.ReplaceAll(string(ufwDefault), "IPV6=yes", "IPV6=no")
        os.WriteFile("/etc/default/ufw", []byte(content), 0644)
    }

    commands := [][]string{
        {"ufw", "default", "deny", "incoming"},
        {"ufw", "default", "allow", "outgoing"},
        {"ufw", "allow", fmt.Sprintf("%d/tcp", cfg.sshPort)},
    }

    // Only open 9735 for LND hybrid mode
    if cfg.components == "bitcoin+lnd" && cfg.p2pMode == "hybrid" {
        commands = append(commands, []string{"ufw", "allow", "9735/tcp"})
    }

    commands = append(commands, []string{"ufw", "--force", "enable"})

    for _, args := range commands {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }

    return nil
}