package installer

import (
    "fmt"
    "os"
    "os/exec"
)

// installBitcoin downloads, verifies, and installs Bitcoin Core.
func installBitcoin(version string) error {
    filename := fmt.Sprintf("bitcoin-%s-x86_64-linux-gnu.tar.gz", version)
    url := fmt.Sprintf("https://bitcoincore.org/bin/bitcoin-core-%s/%s", version, filename)
    shaURL := fmt.Sprintf("https://bitcoincore.org/bin/bitcoin-core-%s/SHA256SUMS", version)

    fmt.Println("    Downloading...")
    if err := download(url, "/tmp/"+filename); err != nil {
        return err
    }
    if err := download(shaURL, "/tmp/SHA256SUMS"); err != nil {
        return err
    }

    fmt.Println("    Verifying checksum...")
    cmd := exec.Command("sha256sum", "--ignore-missing", "--check", "SHA256SUMS")
    cmd.Dir = "/tmp"
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("checksum verification failed: %s: %s", err, output)
    }

    fmt.Println("    Extracting...")
    cmd = exec.Command("tar", "-xzf", "/tmp/"+filename, "-C", "/tmp")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("extract failed: %s: %s", err, output)
    }

    // Install all binaries to /usr/local/bin/
    extractDir := fmt.Sprintf("/tmp/bitcoin-%s/bin", version)
    entries, err := os.ReadDir(extractDir)
    if err != nil {
        return fmt.Errorf("read extracted dir: %w", err)
    }

    for _, entry := range entries {
        src := fmt.Sprintf("%s/%s", extractDir, entry.Name())
        cmd = exec.Command("install", "-m", "0755", "-o", "root", "-g", "root",
            src, "/usr/local/bin/")
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("install %s: %s: %s", entry.Name(), err, output)
        }
    }

    // Clean up
    os.Remove("/tmp/" + filename)
    os.Remove("/tmp/SHA256SUMS")
    os.RemoveAll(fmt.Sprintf("/tmp/bitcoin-%s", version))

    return nil
}

// writeBitcoinConfig writes bitcoin.conf based on the user's
// network and prune size choices.
func writeBitcoinConfig(cfg *installConfig) error {
    // Prune value in MB (config is in GB)
    pruneMB := cfg.pruneSize * 1000

    // Base config — applies to all networks
    content := fmt.Sprintf(`# Virtual Private Node — Bitcoin Core Configuration
#
# Network: %s
# Prune:   %d GB

# ── Global ────────────────────────────────────
server=1
%s
prune=%d
dbcache=512
maxmempool=300
disablewallet=1

# Tor — route all connections through Tor
proxy=127.0.0.1:9050
listen=1
listenonion=1
`, cfg.network.Name, cfg.pruneSize, cfg.network.BitcoinFlag, pruneMB)

    // Network-specific section
    if cfg.network.Name == "testnet4" {
        content += fmt.Sprintf(`
# ── Testnet4 ──────────────────────────────────
[testnet4]
bind=127.0.0.1
rpcbind=127.0.0.1
rpcport=%d
rpcallowip=127.0.0.1

zmqpubrawblock=tcp://127.0.0.1:%d
zmqpubrawtx=tcp://127.0.0.1:%d
`, cfg.network.RPCPort, cfg.network.ZMQBlockPort, cfg.network.ZMQTxPort)
    } else {
        content += fmt.Sprintf(`
# ── Mainnet ───────────────────────────────────
bind=127.0.0.1
rpcbind=127.0.0.1
rpcport=%d
rpcallowip=127.0.0.1

zmqpubrawblock=tcp://127.0.0.1:%d
zmqpubrawtx=tcp://127.0.0.1:%d
`, cfg.network.RPCPort, cfg.network.ZMQBlockPort, cfg.network.ZMQTxPort)
    }

    if err := os.WriteFile("/etc/bitcoin/bitcoin.conf", []byte(content), 0640); err != nil {
        return err
    }

    // Set ownership so the bitcoin user can read it
    cmd := exec.Command("chown", "root:"+systemUser, "/etc/bitcoin/bitcoin.conf")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("%s: %s", err, output)
    }

    return nil
}

// writeBitcoindService creates the systemd service file for bitcoind.
func writeBitcoindService(username string) error {
    content := fmt.Sprintf(`[Unit]
Description=Bitcoin Core
After=network-online.target tor.service
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
ExecStart=/usr/local/bin/bitcoind -conf=/etc/bitcoin/bitcoin.conf -datadir=/var/lib/bitcoin
Restart=on-failure
RestartSec=30
TimeoutStopSec=600
PrivateTmp=true
ProtectSystem=full
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, username, username)

    return os.WriteFile("/etc/systemd/system/bitcoind.service", []byte(content), 0644)
}

// startBitcoind enables and starts the bitcoind systemd service.
func startBitcoind() error {
    commands := [][]string{
        {"systemctl", "daemon-reload"},
        {"systemctl", "enable", "bitcoind"},
        {"systemctl", "start", "bitcoind"},
    }

    for _, args := range commands {
        cmd := exec.Command(args[0], args[1:]...)
        if output, err := cmd.CombinedOutput(); err != nil {
            return fmt.Errorf("%v: %s: %s", args, err, output)
        }
    }

    return nil
}

// download fetches a URL to a local path using wget or curl.
func download(url, dest string) error {
    var cmd *exec.Cmd
    if _, err := exec.LookPath("wget"); err == nil {
        cmd = exec.Command("wget", "-q", "-O", dest, url)
    } else {
        cmd = exec.Command("curl", "-sL", "-o", dest, url)
    }

    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("download %s: %s: %s", url, err, output)
    }

    return nil
}