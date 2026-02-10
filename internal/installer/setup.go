package installer

import (
    "bufio"
    "fmt"
    "os"
    "os/exec"
    "strings"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
    "github.com/ripsline/virtual-private-node/internal/config"
)

const (
    bitcoinVersion = "29.2"
    lndVersion     = "0.20.0-beta"
    systemUser     = "bitcoin"
    appVersion     = "0.1.0"
)

type installConfig struct {
    network    *NetworkConfig
    components string
    pruneSize  int
    p2pMode    string
    publicIPv4 string
    sshPort    int
}

func NeedsInstall() bool {
    _, err := os.Stat("/etc/rlvpn/config.json")
    return err != nil
}

// ── Install progress TUI ─────────────────────────────────

type stepStatus int

const (
    stepPending stepStatus = iota
    stepRunning
    stepDone
    stepFailed
)

type installStep struct {
    name   string
    fn     func() error
    status stepStatus
    err    error
}

type stepDoneMsg struct {
    index int
    err   error
}

type installModel struct {
    steps   []installStep
    current int
    done    bool
    failed  bool
    version string
    width   int
    height  int
}

var (
    progTitleStyle = lipgloss.NewStyle().
            Bold(true).
            Foreground(lipgloss.Color("0")).
            Background(lipgloss.Color("15")).
            Padding(0, 2)

    progBoxStyle = lipgloss.NewStyle().
            Border(lipgloss.RoundedBorder()).
            BorderForeground(lipgloss.Color("245")).
            Padding(1, 2)

    progDoneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
    progRunStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
    progPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
    progFailStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
    progDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
    progGoodStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
)

const progWidth = 75

func (m installModel) Init() tea.Cmd {
    return m.runStep(0)
}

func (m installModel) runStep(index int) tea.Cmd {
    return func() tea.Msg {
        if index >= len(m.steps) {
            return stepDoneMsg{index: index}
        }
        err := m.steps[index].fn()
        return stepDoneMsg{index: index, err: err}
    }
}

func (m installModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        return m, nil

    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c":
            return m, tea.Quit
        case "enter":
            if m.done {
                return m, tea.Quit
            }
        case "q":
            if m.done {
                return m, tea.Quit
            }
        }

    case stepDoneMsg:
        if msg.index < len(m.steps) {
            if msg.err != nil {
                m.steps[msg.index].status = stepFailed
                m.steps[msg.index].err = msg.err
                m.failed = true
                m.done = true
                return m, nil
            }
            m.steps[msg.index].status = stepDone

            next := msg.index + 1
            if next < len(m.steps) {
                m.current = next
                m.steps[next].status = stepRunning
                return m, m.runStep(next)
            }

            m.done = true
            return m, nil
        }
    }
    return m, nil
}

func (m installModel) View() string {
    if m.width == 0 {
        return "Loading..."
    }

    boxWidth := iMinInt(m.width-4, progWidth)

    title := progTitleStyle.Width(boxWidth).Align(lipgloss.Center).
        Render(fmt.Sprintf(" Virtual Private Node v%s ", m.version))

    var lines []string
    for i, s := range m.steps {
        var style lipgloss.Style
        var indicator string

        switch s.status {
        case stepDone:
            style = progDoneStyle
            indicator = "✓"
        case stepRunning:
            style = progRunStyle
            indicator = "⟳"
        case stepFailed:
            style = progFailStyle
            indicator = "✗"
        default:
            style = progPendingStyle
            indicator = "○"
        }

        lines = append(lines,
            style.Render(fmt.Sprintf("  %s [%d/%d] %s",
                indicator, i+1, len(m.steps), s.name)))

        if s.status == stepFailed && s.err != nil {
            lines = append(lines,
                progFailStyle.Render(fmt.Sprintf("      Error: %v", s.err)))
        }
    }

    content := strings.Join(lines, "\n")
    box := progBoxStyle.Width(boxWidth).Render(content)

    var footer string
    if m.done && !m.failed {
        footer = progGoodStyle.Render("  ✓ Installation complete — press Enter to continue  ")
    } else if m.failed {
        footer = progFailStyle.Render("  Installation failed. Press q to exit.  ")
    } else {
        footer = progDimStyle.Render("  Installing... please wait  ")
    }

    full := lipgloss.JoinVertical(lipgloss.Center,
        "", title, "", box, "", footer)

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Center, full)
}

func runInstallTUI(steps []installStep) error {
    if len(steps) == 0 {
        return nil
    }
    steps[0].status = stepRunning

    m := installModel{
        steps:   steps,
        current: 0,
        version: appVersion,
    }

    p := tea.NewProgram(m, tea.WithAltScreen())
    result, err := p.Run()
    if err != nil {
        return fmt.Errorf("install TUI error: %w", err)
    }

    final := result.(installModel)
    if final.failed {
        for _, s := range final.steps {
            if s.status == stepFailed {
                return fmt.Errorf("%s failed: %w", s.name, s.err)
            }
        }
    }
    return nil
}

// ── Info box (centered message, wait for Enter) ──────────

var setupBoxStyle = lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("245")).
    Padding(1, 3)

var setupTitleStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("15")).Bold(true)

var setupTextStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("250"))

var setupWarnStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("196")).Bold(true)

var setupDimStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("243"))

type infoBoxModel struct {
    content string
    width   int
    height  int
}

func (m infoBoxModel) Init() tea.Cmd { return nil }

func (m infoBoxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
    case tea.KeyMsg:
        if msg.String() == "enter" || msg.String() == "ctrl+c" {
            return m, tea.Quit
        }
    }
    return m, nil
}

func (m infoBoxModel) View() string {
    if m.width == 0 {
        return "Loading..."
    }
    maxW := iMinInt(m.width-8, 70)
    box := setupBoxStyle.Width(maxW).Render(m.content)
    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Center, box)
}

func showInfoBox(content string) {
    m := infoBoxModel{content: content}
    p := tea.NewProgram(m, tea.WithAltScreen())
    p.Run()
}

// ── Main install flow ────────────────────────────────────

func Run() error {
    if err := checkOS(); err != nil {
        return err
    }

    cfg, err := RunTUI(appVersion)
    if err != nil {
        return err
    }
    if cfg == nil {
        fmt.Println("\n  Installation cancelled.")
        return nil
    }

    steps := buildSteps(cfg)
    if err := runInstallTUI(steps); err != nil {
        return err
    }

    if cfg.components == "bitcoin+lnd" {
        if err := walletCreationPhase(cfg); err != nil {
            return err
        }
    }

    if err := setupShellEnvironment(cfg); err != nil {
        fmt.Printf("  Warning: shell setup failed: %v\n", err)
    }

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

    return nil
}

// buildSteps creates granular install steps. Download, verify,
// and extract are split into separate visible steps so progress
// is clear without stdout leaks.
func buildSteps(cfg *installConfig) []installStep {
    steps := []installStep{
        {name: "Creating system user", fn: func() error { return createSystemUser(systemUser) }},
        {name: "Creating directories", fn: func() error { return createDirs(systemUser, cfg) }},
        {name: "Disabling IPv6", fn: disableIPv6},
        {name: "Configuring firewall", fn: func() error { return configureFirewall(cfg) }},
        {name: "Installing Tor", fn: installTor},
        {name: "Configuring Tor", fn: func() error { return writeTorConfig(cfg) }},
        {name: "Adding user to debian-tor group", fn: func() error { return addUserToTorGroup(systemUser) }},
        {name: "Starting Tor", fn: restartTor},
        {name: "Downloading Bitcoin Core " + bitcoinVersion, fn: func() error { return downloadBitcoin(bitcoinVersion) }},
        {name: "Verifying Bitcoin Core", fn: func() error { return verifyBitcoin(bitcoinVersion) }},
        {name: "Installing Bitcoin Core", fn: func() error { return extractAndInstallBitcoin(bitcoinVersion) }},
        {name: "Configuring Bitcoin Core", fn: func() error { return writeBitcoinConfig(cfg) }},
        {name: "Creating bitcoind service", fn: func() error { return writeBitcoindService(systemUser) }},
        {name: "Starting Bitcoin Core", fn: startBitcoind},
    }

    if cfg.components == "bitcoin+lnd" {
        steps = append(steps,
            installStep{name: "Downloading LND " + lndVersion, fn: func() error { return downloadLND(lndVersion) }},
            installStep{name: "Verifying LND", fn: func() error { return verifyLND(lndVersion) }},
            installStep{name: "Installing LND", fn: func() error { return extractAndInstallLND(lndVersion) }},
            installStep{name: "Configuring LND", fn: func() error { return writeLNDConfig(cfg) }},
            installStep{name: "Creating LND service", fn: func() error { return writeLNDServiceInitial(systemUser) }},
            installStep{name: "Starting LND", fn: startLND},
        )
    }

    return steps
}

// ── Wallet creation ──────────────────────────────────────

func walletCreationPhase(cfg *installConfig) error {
    walletInfo := setupTitleStyle.Render("Create Your LND Wallet") + "\n\n" +
        setupTextStyle.Render("LND will ask you to:") + "\n\n" +
        setupTextStyle.Render("  1. Enter a wallet password (min 8 characters)") + "\n" +
        setupTextStyle.Render("  2. Confirm the password") + "\n" +
        setupTextStyle.Render("  3. 'n' to create a new seed") + "\n" +
        setupTextStyle.Render("  4. Optionally set a cipher seed passphrase") + "\n" +
        setupTextStyle.Render("     (press Enter to skip)") + "\n" +
        setupTextStyle.Render("  5. Write down your 24-word seed phrase") + "\n\n" +
        setupWarnStyle.Render("WARNING: Your seed phrase is the ONLY way to recover funds.") + "\n" +
        setupWarnStyle.Render("WARNING: No one can help you if you lose it.") + "\n\n" +
        setupDimStyle.Render("Press Enter to continue...")

    showInfoBox(walletInfo)

    // Clear screen and show header before lncli takes over
    fmt.Print("\033[2J\033[H")
    fmt.Println()
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println("    LND Wallet Creation")
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println()

    fmt.Println("  Waiting for LND to be ready...")
    if err := waitForLND(); err != nil {
        return fmt.Errorf("LND not ready: %w", err)
    }
    fmt.Println("  ✓ LND is ready")
    fmt.Println()

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

    // Seed confirmation
    seedConfirm := setupTitleStyle.Render("Seed Phrase Confirmation") + "\n\n" +
        setupWarnStyle.Render("Have you written down your 24-word seed phrase?") + "\n\n" +
        setupTextStyle.Render("Your seed phrase was displayed above by LND.") + "\n" +
        setupTextStyle.Render("Make sure you have saved it in a secure location.") + "\n" +
        setupTextStyle.Render("You will NOT be able to see it again.") + "\n\n" +
        setupDimStyle.Render("Press Enter to confirm you have saved your seed...")

    showInfoBox(seedConfirm)

    // Auto-unlock
    unlockInfo := setupTitleStyle.Render("Auto-Unlock Configuration") + "\n\n" +
        setupTextStyle.Render("Your wallet password will be stored on disk so LND") + "\n" +
        setupTextStyle.Render("can start automatically after a server reboot.") + "\n\n" +
        setupTextStyle.Render("Without auto-unlock, you would need to SSH in and") + "\n" +
        setupTextStyle.Render("manually unlock the wallet after every restart.") + "\n\n" +
        setupDimStyle.Render("Press Enter to continue...")

    showInfoBox(unlockInfo)

    // Clear screen for password prompt
    fmt.Print("\033[2J\033[H")
    fmt.Println()
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println("    Auto-Unlock Password")
    fmt.Println("  ═══════════════════════════════════════════")
    fmt.Println()
    fmt.Print("  Re-enter your wallet password for auto-unlock: ")
    password := readPassword()
    fmt.Println()

    if password == "" {
        fmt.Println("  No password entered. Skipping auto-unlock.")
        return nil
    }

    if err := setupAutoUnlock(password); err != nil {
        fmt.Printf("  Warning: auto-unlock setup failed: %v\n", err)
    } else {
        fmt.Println("  ✓ Auto-unlock configured")
    }

    return nil
}

// ── Helpers ──────────────────────────────────────────────

func readLine(reader *bufio.Reader) string {
    line, _ := reader.ReadString('\n')
    return strings.TrimSpace(line)
}

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

func detectPublicIP() string {
    cmd := exec.Command("curl", "-4", "-s", "--max-time", "5", "ifconfig.me")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return ""
    }
    ip := strings.TrimSpace(string(output))
    if len(strings.Split(ip, ".")) != 4 {
        return ""
    }
    return ip
}

func readFileOrDefault(path, def string) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return def
    }
    return string(data)
}

func setupShellEnvironment(cfg *installConfig) error {
    networkFlag := ""
    if cfg.network.Name != "mainnet" {
        networkFlag = fmt.Sprintf("\nexport LNCLI_NETWORK=%s", cfg.network.LNCLINetwork)
    }

    lndBlock := ""
    if cfg.components == "bitcoin+lnd" {
        lndBlock = fmt.Sprintf(`
export LNCLI_LNDDIR=/var/lib/lnd%s
export LNCLI_MACAROONPATH=/var/lib/lnd/data/chain/bitcoin/%s/admin.macaroon
export LNCLI_TLSCERTPATH=/var/lib/lnd/tls.cert
`, networkFlag, cfg.network.LNCLINetwork)
    }

    content := fmt.Sprintf(`
# ── Virtual Private Node ──────────────────────
bitcoin-cli() {
    sudo -u bitcoin /usr/local/bin/bitcoin-cli \
        -datadir=/var/lib/bitcoin \
        -conf=/etc/bitcoin/bitcoin.conf \
        "$@"
}
export -f bitcoin-cli
%s
lncli() {
    sudo -u bitcoin /usr/local/bin/lncli "$@"
}
export -f lncli
`, lndBlock)

    f, err := os.OpenFile("/home/ripsline/.bashrc",
        os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
    if err != nil {
        return err
    }
    defer f.Close()
    _, err = f.WriteString(content)
    return err
}

func minInt(a, b int) int {
    if a < b {
        return a
    }
    return b
}