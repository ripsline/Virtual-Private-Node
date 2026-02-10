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

// downloadLND fetches the LND tarball and manifest.
func downloadLND(version string) error {
    filename := fmt.Sprintf("lnd-linux-amd64-v%s.tar.gz", version)
    url := fmt.Sprintf("https://github.com/lightningnetwork/lnd/releases/download/v%s/%s",
        version, filename)
    manifestURL := fmt.Sprintf("https://github.com/lightningnetwork/lnd/releases/download/v%s/manifest-v%s.txt",
        version, version)

    if err := download(url, "/tmp/"+filename); err != nil {
        return err
    }
    // Manifest is best-effort
    download(manifestURL, "/tmp/manifest.txt")
    return nil
}

// verifyLND checks the manifest checksum if available.
func verifyLND(version string) error {
    if _, err := os.Stat("/tmp/manifest.txt"); err != nil {
        return nil // no manifest, skip verification
    }
    cmd := exec.Command("sha256sum", "--ignore-missing", "--check", "manifest.txt")
    cmd.Dir = "/tmp"
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("checksum failed: %s: %s", err, output)
    }
    return nil
}

// extractAndInstallLND extracts and installs lnd and lncli binaries.
func extractAndInstallLND(version string) error {
    filename := fmt.Sprintf("lnd-linux-amd64-v%s.tar.gz", version)

    cmd := exec.Command("tar", "-xzf", "/tmp/"+filename, "-C", "/tmp")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("extract failed: %s: %s", err, output)
    }

    extractDir := fmt.Sprintf("/tmp/lnd-linux-amd64-v%s", version)
    for _, bin := range []string{"lnd", "lncli"} {
        src := fmt.Sprintf("%s/%s", extractDir, bin)
        cmd = exec.Command("install", "-m", "0755", "-o", "root", "-g", "root",
            src, "/usr/local/bin/")
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("install %s: %s: %s", bin, err, output)
        }
    }

    os.Remove("/tmp/" + filename)
    os.Remove("/tmp/manifest.txt")
    os.RemoveAll(extractDir)
    return nil
}

func writeLNDConfig(cfg *installConfig) error {
    restOnion := strings.TrimSpace(readFileOrDefault("/var/lib/tor/lnd-rest/hostname", ""))

    listenLine := "listen=localhost:9735"
    externalLine := ""
    if cfg.p2pMode == "hybrid" && cfg.publicIPv4 != "" {
        listenLine = "listen=0.0.0.0:9735"
        externalLine = fmt.Sprintf("externalhosts=%s:9735", cfg.publicIPv4)
    }

    tlsExtraDomain := ""
    if restOnion != "" {
        tlsExtraDomain = fmt.Sprintf("tlsextradomain=%s", restOnion)
    }

    cookiePath := fmt.Sprintf("/var/lib/bitcoin/%s", cfg.network.CookiePath)

    content := fmt.Sprintf(`# Virtual Private Node — LND Configuration
#
# Network: %s
# P2P:     %s

[Application Options]
lnddir=/var/lib/lnd
%s
rpclisten=localhost:10009
restlisten=localhost:8080
debuglevel=info
%s
%s

[Bitcoin]
bitcoin.active=true
%s
bitcoin.node=bitcoind

[Bitcoind]
bitcoind.dir=/var/lib/bitcoin
bitcoind.config=/etc/bitcoin/bitcoin.conf
bitcoind.rpccookie=%s
bitcoind.rpchost=127.0.0.1:%d
bitcoind.zmqpubrawblock=tcp://127.0.0.1:%d
bitcoind.zmqpubrawtx=tcp://127.0.0.1:%d

[Tor]
tor.active=true
tor.socks=127.0.0.1:9050
tor.control=127.0.0.1:9051
tor.targetipaddress=127.0.0.1
tor.v3=true
tor.streamisolation=true
`,
        cfg.network.Name, cfg.p2pMode,
        listenLine, externalLine, tlsExtraDomain,
        cfg.network.LNDBitcoinFlag,
        cookiePath, cfg.network.RPCPort,
        cfg.network.ZMQBlockPort, cfg.network.ZMQTxPort,
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

func startLND() error {
    for _, args := range [][]string{
        {"systemctl", "daemon-reload"},
        {"systemctl", "enable", "lnd"},
        {"systemctl", "start", "lnd"},
    } {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }
    return nil
}

func setupAutoUnlock(password string) error {
    passwordFile := "/var/lib/lnd/wallet_password"
    if err := os.WriteFile(passwordFile, []byte(password), 0400); err != nil {
        return err
    }
    cmd := exec.Command("chown", systemUser+":"+systemUser, passwordFile)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }

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

    for _, args := range [][]string{
        {"systemctl", "daemon-reload"},
        {"systemctl", "restart", "lnd"},
    } {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }
    return nil
}

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