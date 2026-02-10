// Package welcome displays the post-install dashboard shown
// on every SSH login as the ripsline user. Provides three tabs:
//   - Dashboard: service health, system resources, sync status
//   - Pairing: Zeus and Sparrow wallet connection overview
//   - Logs: journalctl output for tor, bitcoind, lnd
//
// Press esc to quit and drop to a bash shell.
// Press backspace to go back from any subview.
package welcome

import (
    "encoding/hex"
    "fmt"
    "os"
    "os/exec"
    "strconv"
    "strings"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
    qrcode "github.com/skip2/go-qrcode"

    "github.com/ripsline/virtual-private-node/internal/config"
)

// ── Styles ───────────────────────────────────────────────

var (
    wTitleStyle = lipgloss.NewStyle().
            Bold(true).
            Foreground(lipgloss.Color("0")).
            Background(lipgloss.Color("15")).
            Padding(0, 2)

    wActiveTabStyle = lipgloss.NewStyle().
            Bold(true).
            Foreground(lipgloss.Color("0")).
            Background(lipgloss.Color("15")).
            Padding(0, 2)

    wInactiveTabStyle = lipgloss.NewStyle().
                Foreground(lipgloss.Color("250")).
                Background(lipgloss.Color("236")).
                Padding(0, 2)

    wHeaderStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("15")).
            Bold(true)

    // Short labels — no fixed width, tight spacing
    wLabelStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("245"))

    wValueStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("15"))

    wGoodStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("15")).
            Bold(true)

    wWarnStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("245"))

    // Yellow highlight for selected items
    wSelectedStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("220")).
            Bold(true)

    wGreenDotStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("10"))

    wRedDotStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("9"))

    wLightningStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("135")).
            Bold(true)

    wDimStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("243"))

    wBorderStyle = lipgloss.NewStyle().
            Border(lipgloss.RoundedBorder()).
            BorderForeground(lipgloss.Color("245"))

    wFooterStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("243"))

    wMonoStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("15"))

    // Bright red for warnings
    wWarningStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("196")).
            Bold(true)

    wActionStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("220")).
            Bold(true)
)

// Fixed width for consistent layout
const wContentWidth = 76

// ── Enums ────────────────────────────────────────────────

type tab int

const (
    tabDashboard tab = iota
    tabPairing
    tabLogs
)

type logSource int

const (
    logTor logSource = iota
    logBitcoin
    logLND
)

type subview int

const (
    subviewNone subview = iota
    subviewZeus
    subviewSparrow
    subviewMacaroon
    subviewQR
)

// ── Model ────────────────────────────────────────────────

type Model struct {
    cfg       *config.AppConfig
    version   string
    activeTab tab
    logSource logSource
    logLines  []string // full log buffer
    logOffset int      // scroll offset (0 = bottom/newest)
    subview   subview
    width     int
    height    int
}

func NewModel(cfg *config.AppConfig, version string) Model {
    return Model{
        cfg:       cfg,
        version:   version,
        activeTab: tabDashboard,
        logSource: logBitcoin,
        subview:   subviewNone,
    }
}

func Show(cfg *config.AppConfig, version string) {
    m := NewModel(cfg, version)
    p := tea.NewProgram(m, tea.WithAltScreen())
    p.Run()
}

func (m Model) Init() tea.Cmd { return nil }

// boxHeight returns the fixed inner height for content boxes.
// All tabs use the same box size.
func (m Model) boxHeight() int {
    // terminal height minus title(1) + gap(1) + tabs(1) + gap(1) +
    // border(2) + padding(2) + footer(1) + gaps(3) = ~12 lines overhead
    h := m.height - 12
    if h < 10 {
        h = 10
    }
    if h > 30 {
        h = 30
    }
    return h
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        return m, nil

    case tea.KeyMsg:
        // Universal: esc always quits to shell
        if msg.String() == "escape" || msg.String() == "ctrl+c" {
            return m, tea.Quit
        }

        // Subview navigation
        if m.subview != subviewNone {
            switch msg.String() {
            case "backspace":
                // QR goes back to Zeus, macaroon goes back to Zeus,
                // Zeus/Sparrow go back to pairing overview
                switch m.subview {
                case subviewQR, subviewMacaroon:
                    m.subview = subviewZeus
                default:
                    m.subview = subviewNone
                }
                return m, nil
            case "m":
                if m.subview == subviewZeus && m.cfg.HasLND() {
                    m.subview = subviewMacaroon
                    return m, nil
                }
            case "r":
                if m.subview == subviewZeus && m.cfg.HasLND() {
                    m.subview = subviewQR
                    return m, nil
                }
            }
            return m, nil
        }

        // Main screen keys
        switch msg.String() {
        case "tab", "right":
            if m.activeTab == tabLogs {
                m.activeTab = tabDashboard
            } else {
                m.activeTab++
            }
            return m, nil

        case "shift+tab", "left":
            if m.activeTab == tabDashboard {
                m.activeTab = tabLogs
            } else {
                m.activeTab--
            }
            return m, nil

        case "1":
            m.activeTab = tabDashboard
        case "2":
            m.activeTab = tabPairing
        case "3":
            m.activeTab = tabLogs

        case "z":
            if m.activeTab == tabPairing && m.cfg.HasLND() {
                m.subview = subviewZeus
                return m, nil
            }
        case "s":
            if m.activeTab == tabPairing {
                m.subview = subviewSparrow
                return m, nil
            }

        // Log controls
        case "t":
            if m.activeTab == tabLogs {
                m.logSource = logTor
                m.logLines = fetchLogLines("tor", 200)
                m.logOffset = 0
            }
        case "b":
            if m.activeTab == tabLogs {
                m.logSource = logBitcoin
                m.logLines = fetchLogLines("bitcoind", 200)
                m.logOffset = 0
            }
        case "l":
            if m.activeTab == tabLogs && m.cfg.HasLND() {
                m.logSource = logLND
                m.logLines = fetchLogLines("lnd", 200)
                m.logOffset = 0
            }

        // Scroll logs
        case "up", "k":
            if m.activeTab == tabLogs {
                maxOffset := len(m.logLines) - m.logsVisible()
                if maxOffset < 0 {
                    maxOffset = 0
                }
                if m.logOffset < maxOffset {
                    m.logOffset++
                }
            }
        case "down", "j":
            if m.activeTab == tabLogs {
                if m.logOffset > 0 {
                    m.logOffset--
                }
            }

        case "r":
            if m.activeTab == tabLogs {
                switch m.logSource {
                case logTor:
                    m.logLines = fetchLogLines("tor", 200)
                case logBitcoin:
                    m.logLines = fetchLogLines("bitcoind", 200)
                case logLND:
                    m.logLines = fetchLogLines("lnd", 200)
                }
                m.logOffset = 0
            }
        }
    }
    return m, nil
}

// logsVisible returns how many log lines fit in the box.
func (m Model) logsVisible() int {
    // box height minus source tabs(1) and gap(1)
    v := m.boxHeight() - 2
    if v < 5 {
        v = 5
    }
    return v
}

func (m Model) View() string {
    if m.width == 0 {
        return "Loading..."
    }

    // Handle subviews
    switch m.subview {
    case subviewZeus:
        return m.renderZeusScreen()
    case subviewSparrow:
        return m.renderSparrowScreen()
    case subviewMacaroon:
        return m.renderMacaroonView()
    case subviewQR:
        return m.renderQRScreen()
    }

    boxWidth := wMinInt(m.width-4, wContentWidth)

    // Render tab content
    var content string
    switch m.activeTab {
    case tabDashboard:
        content = m.renderDashboard(boxWidth)
    case tabPairing:
        content = m.renderPairing(boxWidth)
    case tabLogs:
        content = m.renderLogs(boxWidth)
    }

    title := wTitleStyle.Width(boxWidth).Align(lipgloss.Center).
        Render(" Virtual Private Node v" + m.version + " ")
    tabs := m.renderTabs(boxWidth)
    footer := m.renderFooter()

    body := lipgloss.JoinVertical(lipgloss.Center,
        "", title, "", tabs, "", content,
    )

    bodyHeight := lipgloss.Height(body)
    gap := m.height - bodyHeight - 2
    if gap < 0 {
        gap = 0
    }

    full := lipgloss.JoinVertical(lipgloss.Center,
        body,
        strings.Repeat("\n", gap),
        footer,
    )

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Top, full)
}

// ── Tab bar ──────────────────────────────────────────────

func (m Model) renderTabs(totalWidth int) string {
    tabs := []struct {
        name string
        id   tab
    }{
        {"Dashboard", tabDashboard},
        {"Pairing", tabPairing},
        {"Logs", tabLogs},
    }

    tabWidth := totalWidth / len(tabs)
    var rendered []string
    for _, t := range tabs {
        if t.id == m.activeTab {
            rendered = append(rendered,
                wActiveTabStyle.Width(tabWidth).Align(lipgloss.Center).Render(t.name))
        } else {
            rendered = append(rendered,
                wInactiveTabStyle.Width(tabWidth).Align(lipgloss.Center).Render(t.name))
        }
    }
    return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m Model) renderFooter() string {
    var hint string
    switch m.activeTab {
    case tabDashboard:
        hint = "← → switch tabs • esc quit to shell"
    case tabPairing:
        if m.cfg.HasLND() {
            hint = "z zeus • s sparrow • ← → tabs • esc quit"
        } else {
            hint = "s sparrow • ← → tabs • esc quit"
        }
    case tabLogs:
        if m.cfg.HasLND() {
            hint = "t tor • b bitcoin • l lnd • ↑↓ scroll • r refresh • esc quit"
        } else {
            hint = "t tor • b bitcoin • ↑↓ scroll • r refresh • esc quit"
        }
    }
    return wFooterStyle.Render("  " + hint + "  ")
}

// ── Dashboard tab ────────────────────────────────────────

func (m Model) renderDashboard(boxWidth int) string {
    var sections []string

    sections = append(sections, wHeaderStyle.Render("Services"))
    sections = append(sections, "")
    sections = append(sections, renderServiceRow("tor"))
    sections = append(sections, renderServiceRow("bitcoind"))
    if m.cfg.HasLND() {
        sections = append(sections, renderServiceRow("lnd"))
    }

    sections = append(sections, "")
    sections = append(sections, wHeaderStyle.Render("System"))
    sections = append(sections, "")
    sections = append(sections, renderSystemStats()...)

    sections = append(sections, "")
    sections = append(sections, wHeaderStyle.Render("Blockchain"))
    sections = append(sections, "")
    sections = append(sections, m.renderBlockchainInfo()...)

    content := lipgloss.JoinVertical(lipgloss.Left, sections...)

    // Pad to fixed height
    contentHeight := lipgloss.Height(content)
    target := m.boxHeight()
    if contentHeight < target {
        content += strings.Repeat("\n", target-contentHeight)
    }

    return wBorderStyle.Width(boxWidth).Padding(1, 2).Render(content)
}

func renderServiceRow(name string) string {
    cmd := exec.Command("systemctl", "is-active", "--quiet", name)
    if cmd.Run() == nil {
        return "  " + wGreenDotStyle.Render("●") + " " + wValueStyle.Render(name)
    }
    return "  " + wRedDotStyle.Render("●") + " " + wDimStyle.Render(name)
}

func renderSystemStats() []string {
    var rows []string

    total, used, pct := diskUsage("/")
    rows = append(rows, "  "+wLabelStyle.Render("Disk: ")+
        wValueStyle.Render(fmt.Sprintf("%s / %s (%s)", used, total, pct)))

    ramTotal, ramUsed, ramPct := memUsage()
    rows = append(rows, "  "+wLabelStyle.Render("RAM:  ")+
        wValueStyle.Render(fmt.Sprintf("%s / %s (%s)", ramUsed, ramTotal, ramPct)))

    btcSize := dirSize("/var/lib/bitcoin")
    rows = append(rows, "  "+wLabelStyle.Render("Bitcoin data: ")+
        wValueStyle.Render(btcSize))

    lndSize := dirSize("/var/lib/lnd")
    if lndSize != "N/A" {
        rows = append(rows, "  "+wLabelStyle.Render("LND data: ")+
            wValueStyle.Render(lndSize))
    }

    return rows
}

func (m Model) renderBlockchainInfo() []string {
    var rows []string

    cmd := exec.Command("sudo", "-u", "bitcoin", "bitcoin-cli",
        "-datadir=/var/lib/bitcoin",
        "-conf=/etc/bitcoin/bitcoin.conf",
        "getblockchaininfo")
    output, err := cmd.CombinedOutput()
    if err != nil {
        rows = append(rows, "  "+wWarnStyle.Render("Bitcoin Core not responding"))
        return rows
    }

    info := string(output)
    blocks := extractJSON(info, "blocks")
    headers := extractJSON(info, "headers")
    ibd := strings.Contains(info, `"initialblockdownload": true`)

    if ibd {
        rows = append(rows, "  "+wLabelStyle.Render("Sync: ")+wWarnStyle.Render("⟳ syncing"))
    } else {
        rows = append(rows, "  "+wLabelStyle.Render("Sync: ")+wGoodStyle.Render("✓ synced"))
    }

    rows = append(rows, "  "+wLabelStyle.Render("Height: ")+
        wValueStyle.Render(blocks+" / "+headers))

    progress := extractJSON(info, "verificationprogress")
    if progress != "" {
        pct, err := strconv.ParseFloat(progress, 64)
        if err == nil {
            rows = append(rows, "  "+wLabelStyle.Render("Progress: ")+
                wValueStyle.Render(fmt.Sprintf("%.2f%%", pct*100)))
        }
    }

    rows = append(rows, "  "+wLabelStyle.Render("Network: ")+
        wValueStyle.Render(m.cfg.Network))
    rows = append(rows, "  "+wLabelStyle.Render("Prune: ")+
        wValueStyle.Render(fmt.Sprintf("%d GB", m.cfg.PruneSize)))

    return rows
}

// ── Pairing tab (side-by-side overview) ──────────────────

func (m Model) renderPairing(boxWidth int) string {
    innerWidth := boxWidth - 6 // account for border + padding
    halfWidth := (innerWidth - 3) / 2

    // Zeus column
    var zeusLines []string
    if m.cfg.HasLND() {
        restOnion := readOnion("/var/lib/tor/lnd-rest/hostname")
        status := wGreenDotStyle.Render("●") + " ready"
        if restOnion == "" {
            status = wRedDotStyle.Render("●") + " waiting"
        }
        zeusLines = append(zeusLines,
            wLightningStyle.Render("⚡ Zeus Wallet"),
            "",
            wDimStyle.Render("LND REST over Tor"),
            "",
            status,
            "",
            wActionStyle.Render("Press [z] for setup"),
        )
    } else {
        zeusLines = append(zeusLines,
            wDimStyle.Render("Zeus Wallet"),
            "",
            wDimStyle.Render("LND not installed"),
        )
    }

    zeusContent := lipgloss.JoinVertical(lipgloss.Left, zeusLines...)
    zeusBox := wBorderStyle.Width(halfWidth).Padding(1, 2).Render(zeusContent)

    // Sparrow column
    btcRPC := readOnion("/var/lib/tor/bitcoin-rpc/hostname")
    sparrowStatus := wGreenDotStyle.Render("●") + " ready"
    if btcRPC == "" {
        sparrowStatus = wRedDotStyle.Render("●") + " waiting"
    }

    sparrowLines := []string{
        wHeaderStyle.Render("Sparrow Wallet"),
        "",
        wDimStyle.Render("Bitcoin Core RPC / Tor"),
        "",
        sparrowStatus,
        "",
        wActionStyle.Render("Press [s] for setup"),
    }

    sparrowContent := lipgloss.JoinVertical(lipgloss.Left, sparrowLines...)
    sparrowBox := wBorderStyle.Width(halfWidth).Padding(1, 2).Render(sparrowContent)

    paired := lipgloss.JoinHorizontal(lipgloss.Top, zeusBox, " ", sparrowBox)

    // Pad to fixed height
    pairedHeight := lipgloss.Height(paired)
    target := m.boxHeight() + 4 // +4 for outer border+padding
    if pairedHeight < target {
        paired += strings.Repeat("\n", target-pairedHeight)
    }

    return paired
}

// ── Zeus full pairing screen ─────────────────────────────

func (m Model) renderZeusScreen() string {
    boxWidth := wMinInt(m.width-4, wContentWidth)

    var lines []string
    lines = append(lines, wLightningStyle.Render("⚡ Zeus Wallet — LND REST over Tor"))
    lines = append(lines, "")

    restOnion := readOnion("/var/lib/tor/lnd-rest/hostname")
    if restOnion == "" {
        lines = append(lines, wWarnStyle.Render("LND REST onion not available. Wait for Tor."))
    } else {
        lines = append(lines, wHeaderStyle.Render("Connection Details"))
        lines = append(lines, "")
        lines = append(lines, wLabelStyle.Render("Type: ")+wMonoStyle.Render("LND (REST)"))
        lines = append(lines, wLabelStyle.Render("Port: ")+wMonoStyle.Render("8080"))
        lines = append(lines, "")
        lines = append(lines, wLabelStyle.Render("Host:"))
        lines = append(lines, "  "+wMonoStyle.Render(restOnion))
        lines = append(lines, "")

        mac := readMacaroonHex(m.cfg)
        if mac != "" {
            preview := mac
            if len(preview) > 40 {
                preview = preview[:40] + "..."
            }
            lines = append(lines, wLabelStyle.Render("Macaroon (hex):"))
            lines = append(lines, "  "+wMonoStyle.Render(preview))
            lines = append(lines, "")
            lines = append(lines, wActionStyle.Render("[m] full macaroon    [r] QR code"))
        } else {
            lines = append(lines, wWarningStyle.Render("Macaroon not available. Create wallet first."))
        }
    }

    lines = append(lines, "")
    lines = append(lines, wDimStyle.Render("Steps:"))
    lines = append(lines, wDimStyle.Render("1. Install Zeus, enable Tor in settings"))
    lines = append(lines, wDimStyle.Render("2. Scan QR or add node manually"))
    lines = append(lines, wDimStyle.Render("3. Paste host, port, and macaroon"))

    content := lipgloss.JoinVertical(lipgloss.Left, lines...)
    box := wBorderStyle.Width(boxWidth).Padding(1, 2).Render(content)

    title := wTitleStyle.Width(boxWidth).Align(lipgloss.Center).
        Render(" Zeus Wallet Setup ")
    footer := wFooterStyle.Render("  m macaroon • r QR code • backspace back • esc quit  ")

    full := lipgloss.JoinVertical(lipgloss.Center,
        "", title, "", box, "", footer)

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Top, full)
}

// ── Sparrow full pairing screen ──────────────────────────

func (m Model) renderSparrowScreen() string {
    boxWidth := wMinInt(m.width-4, wContentWidth)

    var lines []string
    lines = append(lines, wHeaderStyle.Render("Sparrow Wallet — Bitcoin Core RPC over Tor"))
    lines = append(lines, "")

    lines = append(lines, wWarningStyle.Render(
        "WARNING: Cookie changes on restart. Reconnect Sparrow after any restart."))
    lines = append(lines, "")

    btcRPC := readOnion("/var/lib/tor/bitcoin-rpc/hostname")
    if btcRPC == "" {
        lines = append(lines, wWarnStyle.Render("Bitcoin RPC onion not available."))
    } else {
        port := "8332"
        if !m.cfg.IsMainnet() {
            port = "48332"
        }
        cookieValue := readCookieValue(m.cfg)

        lines = append(lines, wHeaderStyle.Render("Connection Details"))
        lines = append(lines, "")
        lines = append(lines, wLabelStyle.Render("Port: ")+wMonoStyle.Render(port))
        lines = append(lines, wLabelStyle.Render("User: ")+wMonoStyle.Render("__cookie__"))
        lines = append(lines, "")
        lines = append(lines, wLabelStyle.Render("URL:"))
        lines = append(lines, "  "+wMonoStyle.Render(btcRPC))
        lines = append(lines, "")

        if cookieValue != "" {
            lines = append(lines, wLabelStyle.Render("Password:"))
            lines = append(lines, "  "+wMonoStyle.Render(cookieValue))
        } else {
            lines = append(lines, wLabelStyle.Render("Password: ")+
                wWarnStyle.Render("not available — is bitcoind running?"))
        }
    }

    lines = append(lines, "")
    lines = append(lines, wDimStyle.Render("Steps:"))
    lines = append(lines, wDimStyle.Render("1. In Sparrow: File → Preferences → Server"))
    lines = append(lines, wDimStyle.Render("2. Select Bitcoin Core tab"))
    lines = append(lines, wDimStyle.Render("3. Enter URL, port, user, and password"))
    lines = append(lines, wDimStyle.Render("4. Select Test Connection"))
    lines = append(lines, wDimStyle.Render("5. Sparrow needs Tor on your local machine"))
    lines = append(lines, wDimStyle.Render("   SOCKS5 proxy: localhost:9050"))

    content := lipgloss.JoinVertical(lipgloss.Left, lines...)
    box := wBorderStyle.Width(boxWidth).Padding(1, 2).Render(content)

    title := wTitleStyle.Width(boxWidth).Align(lipgloss.Center).
        Render(" Sparrow Wallet Setup ")
    footer := wFooterStyle.Render("  backspace back • esc quit  ")

    full := lipgloss.JoinVertical(lipgloss.Center,
        "", title, "", box, "", footer)

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Top, full)
}

// ── Macaroon full view ───────────────────────────────────

func (m Model) renderMacaroonView() string {
    mac := readMacaroonHex(m.cfg)
    if mac == "" {
        mac = "Macaroon not available."
    }

    title := wLightningStyle.Render("⚡ Admin Macaroon (hex)")
    hint := wDimStyle.Render("Select and copy the text below. Press backspace to go back.")

    content := lipgloss.JoinVertical(lipgloss.Left,
        "", title, "", hint, "", mac, "")

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Center, content)
}

// ── QR code screen ───────────────────────────────────────

func (m Model) renderQRScreen() string {
    restOnion := readOnion("/var/lib/tor/lnd-rest/hostname")
    mac := readMacaroonHex(m.cfg)

    if restOnion == "" || mac == "" {
        content := wWarnStyle.Render("QR code not available — missing onion address or macaroon.")
        return lipgloss.Place(m.width, m.height,
            lipgloss.Center, lipgloss.Center, content)
    }

    lndconnectURI := fmt.Sprintf("lndconnect://%s:8080?macaroon=%s",
        restOnion, hexToBase64URL(mac))

    qr := renderQRCode(lndconnectURI)

    var lines []string
    lines = append(lines, wLightningStyle.Render("⚡ Zeus QR Code"))
    lines = append(lines, "")
    lines = append(lines, wDimStyle.Render("You may need to zoom out to see the full QR code."))
    lines = append(lines, wDimStyle.Render("macOS: Cmd+Minus  |  Linux: Ctrl+Minus"))
    lines = append(lines, "")

    if qr != "" {
        lines = append(lines, qr)
    } else {
        lines = append(lines, wWarnStyle.Render("Could not generate QR code."))
    }

    lines = append(lines, "")
    lines = append(lines, wFooterStyle.Render("backspace back • esc quit"))

    content := lipgloss.JoinVertical(lipgloss.Left, lines...)

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Top, content)
}

// ── Logs tab ─────────────────────────────────────────────

func (m Model) renderLogs(boxWidth int) string {
    var sources []string
    torS := wDimStyle
    btcS := wDimStyle
    lndS := wDimStyle

    switch m.logSource {
    case logTor:
        torS = wActiveTabStyle
    case logBitcoin:
        btcS = wActiveTabStyle
    case logLND:
        lndS = wActiveTabStyle
    }

    sources = append(sources, torS.Render(" [t] Tor "))
    sources = append(sources, btcS.Render(" [b] Bitcoin "))
    if m.cfg.HasLND() {
        sources = append(sources, lndS.Render(" [l] LND "))
    }

    sourceTabs := lipgloss.JoinHorizontal(lipgloss.Top, sources...)

    // Fetch logs if buffer is empty
    logLines := m.logLines
    if len(logLines) == 0 {
        switch m.logSource {
        case logTor:
            logLines = fetchLogLines("tor", 200)
        case logBitcoin:
            logLines = fetchLogLines("bitcoind", 200)
        case logLND:
            logLines = fetchLogLines("lnd", 200)
        }
    }

    // Calculate visible window with scroll offset
    visible := m.logsVisible()
    totalLines := len(logLines)

    // offset 0 = show newest (bottom), higher offset = scroll up
    start := totalLines - visible - m.logOffset
    if start < 0 {
        start = 0
    }
    end := start + visible
    if end > totalLines {
        end = totalLines
    }

    var displayLines []string
    if totalLines == 0 {
        displayLines = []string{wDimStyle.Render("No logs available. Press r to refresh.")}
    } else {
        for _, line := range logLines[start:end] {
            displayLines = append(displayLines, wDimStyle.Render(line))
        }
    }

    // Scroll indicator
    scrollHint := ""
    if m.logOffset > 0 {
        scrollHint = wDimStyle.Render(fmt.Sprintf(" ↑ %d more lines above", start))
    }

    logContent := strings.Join(displayLines, "\n")

    var contentParts []string
    contentParts = append(contentParts, sourceTabs)
    if scrollHint != "" {
        contentParts = append(contentParts, scrollHint)
    }
    contentParts = append(contentParts, "")
    contentParts = append(contentParts, logContent)

    content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

    // Pad to fixed height
    contentHeight := lipgloss.Height(content)
    target := m.boxHeight()
    if contentHeight < target {
        content += strings.Repeat("\n", target-contentHeight)
    }

    return wBorderStyle.Width(boxWidth).Padding(1, 2).Render(content)
}

// ── QR rendering ─────────────────────────────────────────

func renderQRCode(data string) string {
    qr, err := qrcode.New(data, qrcode.Low)
    if err != nil {
        return ""
    }

    bitmap := qr.Bitmap()
    rows := len(bitmap)
    cols := len(bitmap[0])

    var b strings.Builder
    for y := 0; y < rows; y += 2 {
        for x := 0; x < cols; x++ {
            top := bitmap[y][x]
            bottom := false
            if y+1 < rows {
                bottom = bitmap[y+1][x]
            }
            switch {
            case top && bottom:
                b.WriteString("█")
            case top && !bottom:
                b.WriteString("▀")
            case !top && bottom:
                b.WriteString("▄")
            default:
                b.WriteString(" ")
            }
        }
        if y+2 < rows {
            b.WriteString("\n")
        }
    }
    return b.String()
}

func hexToBase64URL(hexStr string) string {
    data, err := hex.DecodeString(hexStr)
    if err != nil {
        return ""
    }

    const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
    result := make([]byte, 0, (len(data)*4/3)+4)
    padding := (3 - len(data)%3) % 3
    padded := make([]byte, len(data)+padding)
    copy(padded, data)

    for i := 0; i < len(padded); i += 3 {
        n := uint(padded[i])<<16 | uint(padded[i+1])<<8 | uint(padded[i+2])
        result = append(result, chars[(n>>18)&63])
        result = append(result, chars[(n>>12)&63])
        result = append(result, chars[(n>>6)&63])
        result = append(result, chars[n&63])
    }

    if padding > 0 {
        result = result[:len(result)-padding]
    }

    s := string(result)
    s = strings.ReplaceAll(s, "+", "-")
    s = strings.ReplaceAll(s, "/", "_")
    return s
}

// ── Helpers ──────────────────────────────────────────────

func readOnion(path string) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(data))
}

func readMacaroonHex(cfg *config.AppConfig) string {
    network := cfg.Network
    if cfg.IsMainnet() {
        network = "mainnet"
    }
    path := fmt.Sprintf("/var/lib/lnd/data/chain/bitcoin/%s/admin.macaroon", network)
    data, err := os.ReadFile(path)
    if err != nil {
        return ""
    }
    return hex.EncodeToString(data)
}

func readCookieValue(cfg *config.AppConfig) string {
    cookiePath := "/var/lib/bitcoin/.cookie"
    if !cfg.IsMainnet() {
        cookiePath = fmt.Sprintf("/var/lib/bitcoin/%s/.cookie", cfg.Network)
    }
    data, err := os.ReadFile(cookiePath)
    if err != nil {
        return ""
    }
    parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
    if len(parts) != 2 {
        return ""
    }
    return parts[1]
}

func diskUsage(path string) (string, string, string) {
    cmd := exec.Command("df", "-h", "--output=size,used,pcent", path)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "N/A", "N/A", "N/A"
    }
    lines := strings.Split(strings.TrimSpace(string(output)), "\n")
    if len(lines) < 2 {
        return "N/A", "N/A", "N/A"
    }
    fields := strings.Fields(lines[1])
    if len(fields) < 3 {
        return "N/A", "N/A", "N/A"
    }
    return fields[0], fields[1], fields[2]
}

func memUsage() (string, string, string) {
    data, err := os.ReadFile("/proc/meminfo")
    if err != nil {
        return "N/A", "N/A", "N/A"
    }
    var total, available int
    for _, line := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(line, "MemTotal:") {
            fmt.Sscanf(line, "MemTotal: %d kB", &total)
        }
        if strings.HasPrefix(line, "MemAvailable:") {
            fmt.Sscanf(line, "MemAvailable: %d kB", &available)
        }
    }
    if total == 0 {
        return "N/A", "N/A", "N/A"
    }
    used := total - available
    pct := float64(used) / float64(total) * 100
    return formatKB(total), formatKB(used), fmt.Sprintf("%.0f%%", pct)
}

func dirSize(path string) string {
    cmd := exec.Command("du", "-sh", path)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "N/A"
    }
    fields := strings.Fields(string(output))
    if len(fields) < 1 {
        return "N/A"
    }
    return fields[0]
}

func formatKB(kb int) string {
    if kb >= 1048576 {
        return fmt.Sprintf("%.1f GB", float64(kb)/1048576.0)
    }
    return fmt.Sprintf("%.0f MB", float64(kb)/1024.0)
}

// fetchLogLines fetches journal lines and returns them as a slice.
// No --plain flag as it causes exit code 1 on some Debian installs.
func fetchLogLines(service string, count int) []string {
    cmd := exec.Command("journalctl", "-u", service,
        "-n", fmt.Sprintf("%d", count),
        "--no-pager")
    output, err := cmd.CombinedOutput()
    if err != nil && len(output) == 0 {
        return []string{"Could not fetch logs: " + err.Error()}
    }
    text := strings.TrimSpace(string(output))
    if text == "" {
        return []string{"No logs available."}
    }
    return strings.Split(text, "\n")
}

func extractJSON(json, key string) string {
    search := fmt.Sprintf(`"%s":`, key)
    idx := strings.Index(json, search)
    if idx == -1 {
        search = fmt.Sprintf(`"%s" :`, key)
        idx = strings.Index(json, search)
        if idx == -1 {
            return ""
        }
    }
    rest := json[idx+len(search):]
    rest = strings.TrimSpace(rest)
    if strings.HasPrefix(rest, `"`) {
        end := strings.Index(rest[1:], `"`)
        if end == -1 {
            return ""
        }
        return rest[1 : end+1]
    }
    end := strings.IndexAny(rest, ",}\n")
    if end == -1 {
        return strings.TrimSpace(rest)
    }
    return strings.TrimSpace(rest[:end])
}

func wMinInt(a, b int) int {
    if a < b {
        return a
    }
    return b
}