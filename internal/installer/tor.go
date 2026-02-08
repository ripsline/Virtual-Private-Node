package installer

import (
    "fmt"
    "os"
    "os/exec"
)

// installTor installs the Tor package from Debian's repositories.
func installTor() error {
    cmd := exec.Command("apt-get", "install", "-y", "-qq", "tor")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }
    return nil
}

// writeTorConfig writes the torrc with hidden services appropriate
// for the installed components. Bitcoin-only gets RPC and P2P
// hidden services. Bitcoin+LND adds gRPC and REST hidden services.
func writeTorConfig(cfg *installConfig) error {
    content := `# Virtual Private Node — Tor Configuration
SOCKSPort 9050
`

    // Control port is needed for LND to manage its P2P onion service
    if cfg.components == "bitcoin+lnd" {
        content += `
# Control port for LND P2P onion management
ControlPort 9051
CookieAuthentication 1
CookieAuthFileGroupReadable 1
`
    }

    // Bitcoin hidden services — always created
    content += fmt.Sprintf(`
# Bitcoin Core RPC (for wallet connections like Sparrow)
HiddenServiceDir /var/lib/tor/bitcoin-rpc/
HiddenServicePort %d 127.0.0.1:%d

# Bitcoin Core P2P (static onion address for peers)
HiddenServiceDir /var/lib/tor/bitcoin-p2p/
HiddenServicePort %d 127.0.0.1:%d
`, cfg.network.RPCPort, cfg.network.RPCPort,
        cfg.network.P2PPort, cfg.network.P2PPort)

    // LND hidden services — only if LND is installed
    if cfg.components == "bitcoin+lnd" {
        content += `
# LND gRPC (wallet connections over Tor)
HiddenServiceDir /var/lib/tor/lnd-grpc/
HiddenServicePort 10009 127.0.0.1:10009

# LND REST (wallet connections over Tor)
HiddenServiceDir /var/lib/tor/lnd-rest/
HiddenServicePort 8080 127.0.0.1:8080
`
    }

    return os.WriteFile("/etc/tor/torrc", []byte(content), 0644)
}

// addUserToTorGroup allows the system user to read the Tor
// control auth cookie for LND's onion service management.
func addUserToTorGroup(username string) error {
    cmd := exec.Command("usermod", "-aG", "debian-tor", username)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }
    return nil
}

// restartTor enables and restarts the Tor service.
// This must happen after writing torrc so the hidden
// service directories and keys are created.
func restartTor() error {
    commands := [][]string{
        {"systemctl", "enable", "tor"},
        {"systemctl", "restart", "tor"},
    }

    for _, args := range commands {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }

    return nil
}