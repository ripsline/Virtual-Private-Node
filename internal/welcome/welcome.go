// Package welcome displays the post-install message shown
// on every SSH login as the ripsline user.
package welcome

import (
    "fmt"
    "os"
    "os/exec"
    "strings"

    "github.com/ripsline/virtual-private-node/internal/config"
)

// Show prints the welcome message with network-appropriate commands.
func Show(cfg *config.AppConfig, version string) {
    fmt.Println()
    fmt.Println("  ╔══════════════════════════════════════════╗")
    fmt.Printf("  ║  Virtual Private Node v%-17s ║\n", version)
    fmt.Printf("  ║  Network: %-31s ║\n", cfg.Network)
    fmt.Println("  ╚══════════════════════════════════════════╝")
    fmt.Println()

    // Show service status (quick check, non-blocking)
    printServiceStatus(cfg)

    fmt.Println()
    fmt.Println("  ── Node Commands ──────────────────────────")
    fmt.Println()
    printBitcoinCommands(cfg)

    if cfg.HasLND() {
        fmt.Println()
        printLNDCommands(cfg)
    }

    fmt.Println()
    fmt.Println("  ── Service Management ─────────────────────")
    fmt.Println()
    fmt.Println("    sudo systemctl status bitcoind")
    fmt.Println("    sudo systemctl restart bitcoind")
    fmt.Println("    sudo journalctl -u bitcoind -f")

    if cfg.HasLND() {
        fmt.Println()
        fmt.Println("    sudo systemctl status lnd")
        fmt.Println("    sudo systemctl restart lnd")
        fmt.Println("    sudo journalctl -u lnd -f")
    }

    fmt.Println()
    fmt.Println("  ── Tor Hidden Services ───────────────────")
    fmt.Println()
    printOnionAddresses(cfg)

    fmt.Println()
}

// printServiceStatus shows a quick status line for each service.
func printServiceStatus(cfg *config.AppConfig) {
    btcStatus := serviceStatus("bitcoind")
    fmt.Printf("    bitcoind:  %s\n", btcStatus)

    if cfg.HasLND() {
        lndStatus := serviceStatus("lnd")
        fmt.Printf("    lnd:       %s\n", lndStatus)
    }

    torStatus := serviceStatus("tor")
    fmt.Printf("    tor:       %s\n", torStatus)
}

// serviceStatus returns a one-word status for a systemd service.
func serviceStatus(name string) string {
    cmd := exec.Command("systemctl", "is-active", "--quiet", name)
    if cmd.Run() == nil {
        return "✓ running"
    }
    return "✗ stopped"
}

// printBitcoinCommands prints bitcoin-cli commands with the correct flags.
func printBitcoinCommands(cfg *config.AppConfig) {
    fmt.Println("    # Check sync status")
    fmt.Printf("    bitcoin-cli -datadir=/var/lib/bitcoin -conf=/etc/bitcoin/bitcoin.conf getblockchaininfo\n")
    fmt.Println()
    fmt.Println("    # Peer info")
    fmt.Printf("    bitcoin-cli -datadir=/var/lib/bitcoin -conf=/etc/bitcoin/bitcoin.conf getpeerinfo\n")
    fmt.Println()
    fmt.Println("    # Network info")
    fmt.Printf("    bitcoin-cli -datadir=/var/lib/bitcoin -conf=/etc/bitcoin/bitcoin.conf getnetworkinfo\n")
}

// printLNDCommands prints lncli commands with the correct flags.
func printLNDCommands(cfg *config.AppConfig) {
    networkFlag := ""
    if !cfg.IsMainnet() {
        networkFlag = " --network=" + cfg.Network
    }

    fmt.Println("    # Node info")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s getinfo\n", networkFlag)
    fmt.Println()
    fmt.Println("    # Wallet balance")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s walletbalance\n", networkFlag)
    fmt.Println()
    fmt.Println("    # New receiving address")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s newaddress p2wkh\n", networkFlag)
    fmt.Println()
    fmt.Println("    # List channels")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s listchannels\n", networkFlag)
    fmt.Println()
    fmt.Println("    # Create invoice")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s addinvoice --amt=<sats> --memo=\"<description>\"\n", networkFlag)
    fmt.Println()
    fmt.Println("    # Pay invoice")
    fmt.Printf("    lncli --lnddir=/var/lib/lnd%s payinvoice <bolt11>\n", networkFlag)
}

// printOnionAddresses reads and displays Tor hidden service addresses.
func printOnionAddresses(cfg *config.AppConfig) {
    btcRPC := readOnion("/var/lib/tor/bitcoin-rpc/hostname")
    btcP2P := readOnion("/var/lib/tor/bitcoin-p2p/hostname")

    if btcP2P != "" {
        fmt.Printf("    Bitcoin P2P:   %s\n", btcP2P)
    }
    if btcRPC != "" {
        fmt.Printf("    Bitcoin RPC:   %s\n", btcRPC)
    }

    if cfg.HasLND() {
        grpc := readOnion("/var/lib/tor/lnd-grpc/hostname")
        rest := readOnion("/var/lib/tor/lnd-rest/hostname")

        if grpc != "" {
            fmt.Printf("    LND gRPC:      %s:10009\n", grpc)
        }
        if rest != "" {
            fmt.Printf("    LND REST:      %s:8080\n", rest)
        }
    }
}

// readOnion reads a Tor hidden service hostname file.
func readOnion(path string) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(data))
}