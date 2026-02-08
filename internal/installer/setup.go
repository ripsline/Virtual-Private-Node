package installer

import (
    "bufio"
    "fmt"
    "os"
    "os/exec"
    "strings"

    "github.com/ripsline/virtual-private-node/internal/config"
)

const (
    bitcoinVersion = "29.2"
    lndVersion     = "0.20.0-beta"
    systemUser     = "bitcoin"
)

// installConfig holds all choices made during the interactive
// setup phase. These are gathered before any installation begins.
type installConfig struct {
    network    *NetworkConfig
    components string // "bitcoin" or "bitcoin+lnd"
    pruneSize  int    // GB
    p2pMode    string // "tor" or "hybrid"
    publicIPv4 string
    sshPort    int
}

// NeedsInstall returns true if the node has not been set up yet.
// Checks for the config file written at the end of installation.
func NeedsInstall() bool {
    _, err := os.Stat("/etc/rlvpn/config.json")
    return err != nil
}

// Run is the main installation entry point. It gathers config
// from the user, installs components, and saves the config.
func Run() error {
    // Verify we're on Debian
    if err := checkOS(); err != nil {
        return err
    }

    reader := bufio.NewReader(os.Stdin)
    cfg := gatherConfig(reader)

    // Build the step list based on component choices
    steps := buildSteps(cfg)

    total := len(steps)
    for i, step := range steps {
        fmt.Printf("\n  [%d/%d] %s...\n", i+1, total, step.name)
        if err := step.fn(); err != nil {
            return fmt.Errorf("%s failed: %w", step.name, err)
        }
        fmt.Printf("  ✓ %s\n", step.name)
    }

    // LND wallet creation — separate phase with clear messaging
    if cfg.components == "bitcoin+lnd" {
        if err := walletCreationPhase(cfg, reader); err != nil {
            return err
        }
    }

    // Save the persistent config
    appCfg := &config.AppConfig{
        Network:    cfg.network.Name,
        Components: cfg.components,
        PruneSize:  cfg.pruneSize,
        P2PMode:    cfg.p2pMode,
        SSHPort:    cfg.sshPort,
    }
    if err := config.Save(appCfg); err != nil {
        return fmt.Errorf("save config: %w", err)
    }

    // Print completion message
    printComplete(cfg)

    return nil
}

// step is a named installation step.
type step struct {
    name string
    fn   func() error
}

// buildSteps returns the ordered list of installation steps
// based on the user's component choices.
func buildSteps(cfg *installConfig) []step {
    steps := []step{
        {"Creating system user", func() error { return createSystemUser(systemUser) }},
        {"Creating directories", func() error { return createDirs(systemUser, cfg) }},
        {"Disabling IPv6", disableIPv6},
        {"Configuring firewall", func() error { return configureFirewall(cfg) }},
        {"Installing Tor", installTor},
        {"Configuring Tor", func() error { return writeTorConfig(cfg) }},
        {"Adding user to debian-tor group", func() error { return addUserToTorGroup(systemUser) }},
        {"Starting Tor", restartTor},
        {"Installing Bitcoin Core " + bitcoinVersion, func() error { return installBitcoin(bitcoinVersion) }},
        {"Configuring Bitcoin Core", func() error { return writeBitcoinConfig(cfg) }},
        {"Creating bitcoind service", func() error { return writeBitcoindService(systemUser) }},
        {"Starting Bitcoin Core", startBitcoind},
    }

    if cfg.components == "bitcoin+lnd" {
        steps = append(steps,
            step{"Installing LND " + lndVersion, func() error { return installLND(lndVersion) }},
            step{"Configuring LND", func() error { return writeLNDConfig(cfg) }},
            step{"Creating LND service", func() error { return writeLNDServiceInitial(systemUser) }},
            step{"Starting LND", startLND},
        )
    }

    return steps
}

// walletCreationPhase handles the interactive wallet creation.
// This is intentionally separated from the automated steps
// so the user understands they are creating their own wallet.
func walletCreationPhase(cfg *installConfig, reader *bufio.Reader) error {
    fmt.Println()
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println()
    fmt.Println("  Automated setup complete.")
    fmt.Println()
    fmt.Println("  Next: Create your LND wallet.")
    fmt.Println()
    fmt.Println("  LND will ask you to:")
    fmt.Println("    1. Enter a wallet password (min 8 characters)")
    fmt.Println("    2. Confirm the password")
    fmt.Println("    3. Optionally set a cipher seed passphrase")
    fmt.Println("       (press Enter to skip)")
    fmt.Println("    4. Write down your 24-word seed phrase")
    fmt.Println()
    fmt.Println("  ⚠️  Your seed phrase is the ONLY way to recover funds.")
    fmt.Println("  ⚠️  No one can help you if you lose it.")
    fmt.Println()
    fmt.Print("  Press Enter to continue...")
    reader.ReadString('\n')

    // Wait for LND REST to be ready
    fmt.Println()
    fmt.Println("  Waiting for LND to be ready...")
    if err := waitForLND(); err != nil {
        return fmt.Errorf("LND not ready: %w", err)
    }
    fmt.Println("  ✓ LND is ready")
    fmt.Println()

    // Hand terminal to lncli create
    lncliArgs := []string{
        "-u", systemUser, "lncli",
        "--lnddir=/var/lib/lnd",
        "--network=" + cfg.network.LNCLINetwork,
        "create",
    }
    cmd := exec.Command("sudo", lncliArgs...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("lncli create failed: %w", err)
    }

    fmt.Println()
    fmt.Println("  ✓ Wallet created")

    // Auto-unlock option
    fmt.Println()
    fmt.Println("  ? Auto-unlock LND wallet on reboot?")
    fmt.Println("    This stores your wallet password on disk so LND")
    fmt.Println("    can start without manual intervention after reboot.")
    fmt.Println()
    fmt.Println("    1) Yes (recommended for always-on nodes)")
    fmt.Println("    2) No (you must unlock manually after every restart)")
    fmt.Print("    Select [1/2]: ")
    choice := readLine(reader)

    if choice != "2" {
        fmt.Println()
        fmt.Print("  ? Re-enter your wallet password for auto-unlock: ")
        password := readPassword()
        fmt.Println()

        if err := setupAutoUnlock(password); err != nil {
            fmt.Printf("  Warning: auto-unlock setup failed: %v\n", err)
            fmt.Println("  You can set this up manually later.")
        } else {
            fmt.Println("  ✓ Auto-unlock configured")
        }
    }

    return nil
}

// gatherConfig asks the user all configuration questions
// before any installation begins.
func gatherConfig(reader *bufio.Reader) *installConfig {
    cfg := &installConfig{
        sshPort: 22,
    }

    // Network
    fmt.Println("  ? Network:")
    fmt.Println("    1) Mainnet (real bitcoin)")
    fmt.Println("    2) Testnet4 (test bitcoin)")
    fmt.Print("    Select [1/2]: ")
    choice := readLine(reader)
    if choice == "1" {
        cfg.network = Mainnet()
    } else {
        cfg.network = Testnet4()
    }

    // Components
    fmt.Println()
    fmt.Println("  ? Components:")
    fmt.Println("    1) Bitcoin Core only (pruned node over Tor)")
    fmt.Println("    2) Bitcoin Core + LND (Lightning node)")
    fmt.Print("    Select [1/2]: ")
    choice = readLine(reader)
    if choice == "1" {
        cfg.components = "bitcoin"
    } else {
        cfg.components = "bitcoin+lnd"
    }

    // Prune size
    fmt.Println()
    fmt.Println("  ? Prune size (blockchain storage):")
    fmt.Println("    1) 10 GB (minimum)")
    fmt.Println("    2) 25 GB (recommended)")
    fmt.Println("    3) 50 GB (more history — requires larger SSD)")
    fmt.Print("    Select [1/2/3]: ")
    choice = readLine(reader)
    switch choice {
    case "1":
        cfg.pruneSize = 10
    case "3":
        cfg.pruneSize = 50
        fmt.Println("    ⚠️  Make sure your VPS has at least 60 GB of disk space.")
    default:
        cfg.pruneSize = 25
    }

    // P2P mode — only relevant if LND is installed
    if cfg.components == "bitcoin+lnd" {
        fmt.Println()
        fmt.Println("  ? LND P2P exposure:")
        fmt.Println("    1) Tor only (maximum privacy)")
        fmt.Println("    2) Hybrid (Tor + clearnet — better routing)")
        fmt.Print("    Select [1/2]: ")
        choice = readLine(reader)
        if choice == "2" {
            cfg.p2pMode = "hybrid"
            cfg.publicIPv4 = detectPublicIP()
            if cfg.publicIPv4 != "" {
                fmt.Printf("\n    Detected public IPv4: %s\n", cfg.publicIPv4)
                fmt.Print("    Use this IP? [Y/n]: ")
                confirm := readLine(reader)
                if strings.ToLower(confirm) == "n" {
                    fmt.Print("    Enter IPv4 manually: ")
                    cfg.publicIPv4 = readLine(reader)
                }
            } else {
                fmt.Print("\n    Could not detect public IP. Enter IPv4: ")
                cfg.publicIPv4 = readLine(reader)
            }
            if cfg.publicIPv4 == "" {
                fmt.Println("    No IP entered — defaulting to Tor only.")
                cfg.p2pMode = "tor"
            }
        } else {
            cfg.p2pMode = "tor"
        }
    } else {
        cfg.p2pMode = "tor"
    }

    // SSH port
    fmt.Println()
    fmt.Println("  ? SSH port:")
    fmt.Println("    1) 22 (default)")
    fmt.Println("    2) Custom")
    fmt.Print("    Select [1/2]: ")
    choice = readLine(reader)
    if choice == "2" {
        fmt.Print("    Enter SSH port: ")
        portStr := readLine(reader)
        if portStr != "" {
            fmt.Sscanf(portStr, "%d", &cfg.sshPort)
        }
    }

    // Confirmation
    fmt.Println()
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println("    Installation Summary")
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Printf("    Network:      %s\n", cfg.network.Name)
    if cfg.components == "bitcoin+lnd" {
        fmt.Printf("    Components:   Bitcoin Core %s + LND %s\n", bitcoinVersion, lndVersion)
    } else {
        fmt.Printf("    Components:   Bitcoin Core %s\n", bitcoinVersion)
    }
    fmt.Printf("    Prune:        %d GB\n", cfg.pruneSize)
    if cfg.components == "bitcoin+lnd" {
        if cfg.p2pMode == "hybrid" {
            fmt.Printf("    P2P mode:     Hybrid (Tor + %s)\n", cfg.publicIPv4)
        } else {
            fmt.Println("    P2P mode:     Tor only")
        }
    }
    fmt.Printf("    SSH port:     %d\n", cfg.sshPort)
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Print("\n  ? Proceed with installation? [y/N]: ")
    confirm := readLine(reader)
    if strings.ToLower(confirm) != "y" {
        fmt.Println("\n  Installation cancelled.")
        os.Exit(0)
    }

    return cfg
}

// printComplete shows the post-installation summary.
func printComplete(cfg *installConfig) {
    fmt.Println()
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println("    Installation Complete!")
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println()
    fmt.Println("  Bitcoin Core is syncing. This takes a few hours.")
    fmt.Println()

    // Print onion addresses
    btcRPCOnion := readFileOrDefault("/var/lib/tor/bitcoin-rpc/hostname", "")
    btcP2POnion := readFileOrDefault("/var/lib/tor/bitcoin-p2p/hostname", "")

    if btcP2POnion != "" {
        fmt.Printf("  Bitcoin P2P:   %s:%d\n",
            strings.TrimSpace(btcP2POnion), cfg.network.P2PPort)
    }
    if btcRPCOnion != "" {
        fmt.Printf("  Bitcoin RPC:   %s:%d\n",
            strings.TrimSpace(btcRPCOnion), cfg.network.RPCPort)
    }

    if cfg.components == "bitcoin+lnd" {
        grpcOnion := readFileOrDefault("/var/lib/tor/lnd-grpc/hostname", "")
        restOnion := readFileOrDefault("/var/lib/tor/lnd-rest/hostname", "")

        if grpcOnion != "" {
            fmt.Printf("  LND gRPC:      %s:10009\n", strings.TrimSpace(grpcOnion))
        }
        if restOnion != "" {
            fmt.Printf("  LND REST:      %s:8080\n", strings.TrimSpace(restOnion))
        }
    }

    fmt.Println()
    fmt.Println("  Your node is ready. Log out and SSH back in to see")
    fmt.Println("  the welcome message with useful commands.")
    fmt.Println()
}

// readLine reads a single line from the reader and trims whitespace.
func readLine(reader *bufio.Reader) string {
    line, _ := reader.ReadString('\n')
    return strings.TrimSpace(line)
}

// readPassword reads a password from stdin with echo disabled.
func readPassword() string {
    sttyOff := exec.Command("stty", "-echo")
    sttyOff.Stdin = os.Stdin
    sttyOff.Run()

    reader := bufio.NewReader(os.Stdin)
    password, _ := reader.ReadString('\n')

    sttyOn := exec.Command("stty", "echo")
    sttyOn.Stdin = os.Stdin
    sttyOn.Run()

    return strings.TrimSpace(password)
}

// detectPublicIP tries to determine the server's public IPv4.
func detectPublicIP() string {
    cmd := exec.Command("curl", "-4", "-s", "--max-time", "5", "ifconfig.me")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return ""
    }
    ip := strings.TrimSpace(string(output))
    parts := strings.Split(ip, ".")
    if len(parts) != 4 {
        return ""
    }
    return ip
}

// readFileOrDefault reads a file or returns a default value.
func readFileOrDefault(path, def string) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return def
    }
    return string(data)
}