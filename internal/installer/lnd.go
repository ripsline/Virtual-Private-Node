package installer

import (
    "crypto/tls"
    "fmt"
    "net/http"
    "os"
    "os/exec"
    "strings"
    "time"
)

// installLND downloads, verifies, and installs LND.
func installLND(version string) error {
    filename := fmt.Sprintf("lnd-linux-amd64-v%s.tar.gz", version)
    url := fmt.Sprintf("https://github.com/lightningnetwork/lnd/releases/download/v%s/%s",
        version, filename)
    manifestURL := fmt.Sprintf("https://github.com/lightningnetwork/lnd/releases/download/v%s/manifest-v%s.txt",
        version, version)

    fmt.Println("    Downloading...")
    if err := download(url, "/tmp/"+filename); err != nil {
        return err
    }

    // Manifest verification is best-effort
    if err := download(manifestURL, "/tmp/manifest.txt"); err != nil {
        fmt.Println("    Warning: could not download manifest for verification")
    } else {
        fmt.Println("    Verifying checksum...")
        cmd := exec.Command("sha256sum", "--ignore-missing", "--check", "manifest.txt")
        cmd.Dir = "/tmp"
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("checksum verification failed: %s: %s", err, output)
        }
    }

    fmt.Println("    Extracting...")
    cmd := exec.Command("tar", "-xzf", "/tmp/"+filename, "-C", "/tmp")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("extract failed: %s: %s", err, output)
    }

    // Install lnd and lncli binaries
    extractDir := fmt.Sprintf("/tmp/lnd-linux-amd64-v%s", version)
    for _, bin := range []string{"lnd", "lncli"} {
        src := fmt.Sprintf("%s/%s", extractDir, bin)
        cmd = exec.Command("install", "-m", "0755", "-o", "root", "-g", "root",
            src, "/usr/local/bin/")
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("install %s: %s: %s", bin, err, output)
        }
    }

    // Clean up
    os.Remove("/tmp/" + filename)
    os.Remove("/tmp/manifest.txt")
    os.RemoveAll(extractDir)

    return nil
}

// writeLNDConfig writes lnd.conf based on the user's network and P2P choices.
func writeLNDConfig(cfg *installConfig) error {
    restOnion := strings.TrimSpace(readFileOrDefault("/var/lib/tor/lnd-rest/hostname", ""))

    // P2P listening
    listenLine := "listen=localhost:9735"
    externalLine := ""
    if cfg.p2pMode == "hybrid" {
        listenLine = "listen=0.0.0.0:9735"
        if cfg.publicIPv4 != "" {
            externalLine = fmt.Sprintf("externalhosts=%s:9735", cfg.publicIPv4)
        }
    }

    // TLS extra domain for Tor wallet connections
    tlsExtraDomain := ""
    if restOnion != "" {
        tlsExtraDomain = fmt.Sprintf("tlsextradomain=%s", restOnion)
    }

    // Cookie path depends on network
    cookiePath := fmt.Sprintf("/var/lib/bitcoin/%s", cfg.network.CookiePath)

    content := fmt.Sprintf(`# Virtual Private Node — LND Configuration
#
# Network: %s
# P2P:     %s

# ── Application ───────────────────────────────
[Application Options]
lnddir=/var/lib/lnd
%s
rpclisten=localhost:10009
restlisten=localhost:8080
debuglevel=info
%s
%s

# ── Bitcoin ───────────────────────────────────
[Bitcoin]
bitcoin.active=true
%s
bitcoin.node=bitcoind

# ── Bitcoind ──────────────────────────────────
[Bitcoind]
bitcoind.dir=/var/lib/bitcoin
bitcoind.config=/etc/bitcoin/bitcoin.conf
bitcoind.rpccookie=%s
bitcoind.rpchost=127.0.0.1:%d
bitcoind.zmqpubrawblock=tcp://127.0.0.1:%d
bitcoind.zmqpubrawtx=tcp://127.0.0.1:%d

# ── Tor ───────────────────────────────────────
[Tor]
tor.active=true
tor.socks=127.0.0.1:9050
tor.control=127.0.0.1:9051
tor.targetipaddress=127.0.0.1
tor.v3=true
tor.streamisolation=true
`,
        cfg.network.Name,
        cfg.p2pMode,
        listenLine,
        externalLine,
        tlsExtraDomain,
        cfg.network.LNDBitcoinFlag,
        cookiePath,
        cfg.network.RPCPort,
        cfg.network.ZMQBlockPort,
        cfg.network.ZMQTxPort,
    )

    if err := os.WriteFile("/etc/lnd/lnd.conf", []byte(content), 0640); err != nil {
        return err
    }

    cmd := exec.Command("chown", "root:"+systemUser, "/etc/lnd/lnd.conf")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }

    return nil
}

// writeLNDServiceInitial creates the systemd service file for LND
// WITHOUT auto-unlock. Auto-unlock is added later if the user opts in.
func writeLNDServiceInitial(username string) error {
    content := fmt.Sprintf(`[Unit]
Description=LND Lightning Network Daemon
After=bitcoind.service tor.service
Wants=bitcoind.service

[Service]
Type=simple
User=%s
Group=%s
ExecStart=/usr/local/bin/lnd --configfile=/etc/lnd/lnd.conf
Restart=on-failure
RestartSec=30
TimeoutStopSec=300
PrivateTmp=true
ProtectSystem=full
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, username, username)

    return os.WriteFile("/etc/systemd/system/lnd.service", []byte(content), 0644)
}

// startLND enables and starts the LND systemd service.
func startLND() error {
    commands := [][]string{
        {"systemctl", "daemon-reload"},
        {"systemctl", "enable", "lnd"},
        {"systemctl", "start", "lnd"},
    }

    for _, args := range commands {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }

    return nil
}

// setupAutoUnlock writes the password file and rewrites the
// systemd service to include --wallet-unlock-password-file.
func setupAutoUnlock(password string) error {
    // Write password file
    passwordFile := "/var/lib/lnd/wallet_password"
    if err := os.WriteFile(passwordFile, []byte(password), 0400); err != nil {
        return err
    }

    cmd := exec.Command("chown", systemUser+":"+systemUser, passwordFile)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }

    // Rewrite service file with auto-unlock flag
    content := fmt.Sprintf(`[Unit]
Description=LND Lightning Network Daemon
After=bitcoind.service tor.service
Wants=bitcoind.service

[Service]
Type=simple
User=%s
Group=%s
ExecStart=/usr/local/bin/lnd --configfile=/etc/lnd/lnd.conf --wallet-unlock-password-file=/var/lib/lnd/wallet_password
Restart=on-failure
RestartSec=30
TimeoutStopSec=300
PrivateTmp=true
ProtectSystem=full
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, systemUser, systemUser)

    if err := os.WriteFile("/etc/systemd/system/lnd.service", []byte(content), 0644); err != nil {
        return err
    }

    // Reload and restart LND with auto-unlock
    commands := [][]string{
        {"systemctl", "daemon-reload"},
        {"systemctl", "restart", "lnd"},
    }
    for _, args := range commands {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }

    return nil
}

// waitForLND polls LND's REST endpoint until it responds.
// LND needs a few seconds after starting before it can accept
// wallet creation commands.
func waitForLND() error {
    client := &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
        Timeout: 5 * time.Second,
    }

    for i := 0; i < 60; i++ {
        resp, err := client.Get("https://localhost:8080/v1/state")
        if err == nil {
            resp.Body.Close()
            return nil
        }
        time.Sleep(2 * time.Second)
    }

    return fmt.Errorf("LND did not respond after 120 seconds")
}