// Package installer — tui.go
//
// Interactive TUI for gathering installation configuration.
// Uses bubbletea for terminal UI with arrow key navigation
// and lipgloss for styling. Black/white brand with yellow
// cursor highlight and red warnings.
package installer

import (
    "fmt"
    "strings"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

// ── Styles ───────────────────────────────────────────────

var (
    tuiTitleStyle = lipgloss.NewStyle().
            Bold(true).
            Foreground(lipgloss.Color("0")).
            Background(lipgloss.Color("15")).
            Padding(0, 2)

    tuiSectionStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("15")).
            Bold(true)

    // Yellow highlight for the selected/cursor option
    tuiSelectedStyle = lipgloss.NewStyle().
                Foreground(lipgloss.Color("220")).
                Bold(true)

    tuiUnselectedStyle = lipgloss.NewStyle().
                Foreground(lipgloss.Color("250"))

    tuiDimStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("243"))

    // Bright red for warnings
    tuiWarningStyle = lipgloss.NewStyle().
            Foreground(lipgloss.Color("196")).
            Bold(true)

    tuiBoxStyle = lipgloss.NewStyle().
            Border(lipgloss.RoundedBorder()).
            BorderForeground(lipgloss.Color("245")).
            Padding(1, 2)

    tuiSummaryKeyStyle = lipgloss.NewStyle().
                Foreground(lipgloss.Color("245")).
                Width(16).
                Align(lipgloss.Right)

    tuiSummaryValStyle = lipgloss.NewStyle().
                Foreground(lipgloss.Color("15")).
                Bold(true)
)

// ── Questions ────────────────────────────────────────────

type question struct {
    title   string
    options []option
}

type option struct {
    label string
    desc  string
    value string
    warn  string
}

func buildQuestions() []question {
    return []question{
        {
            title: "Network",
            options: []option{
                {label: "Mainnet", desc: "Real bitcoin — use with caution", value: "mainnet"},
                {label: "Testnet4", desc: "Test bitcoin — safe for experimenting", value: "testnet4"},
            },
        },
        {
            title: "Components",
            options: []option{
                {label: "Bitcoin Core only", desc: "Pruned node routed through Tor", value: "bitcoin"},
                {label: "Bitcoin Core + LND", desc: "Full Lightning node with Tor hidden services", value: "bitcoin+lnd"},
            },
        },
        {
            title: "Blockchain Storage (Pruned)",
            options: []option{
                {label: "10 GB", desc: "Minimum — works but tight", value: "10"},
                {label: "25 GB", desc: "Recommended", value: "25"},
                {label: "50 GB", desc: "More block history", value: "50",
                    warn: "Make sure your VPS has at least 60 GB of disk space"},
            },
        },
    }
}

func p2pQuestion() question {
    return question{
        title: "LND P2P Mode",
        options: []option{
            {label: "Tor only", desc: "Maximum privacy — all connections through Tor", value: "tor"},
            {label: "Hybrid", desc: "Tor + clearnet — better routing performance", value: "hybrid"},
        },
    }
}

func sshQuestion() question {
    return question{
        title: "SSH Port",
        options: []option{
            {label: "22", desc: "Default SSH port", value: "22"},
            {label: "Custom", desc: "Enter a custom port after selection", value: "custom"},
        },
    }
}

// ── TUI Model ────────────────────────────────────────────

type tuiPhase int

const (
    phaseQuestions tuiPhase = iota
    phaseSummary
    phaseConfirmed
    phaseCancelled
)

type tuiModel struct {
    questions []question
    current   int
    cursors   []int
    answers   []string
    phase     tuiPhase
    width     int
    height    int
}

type tuiResult struct {
    network    string
    components string
    pruneSize  string
    p2pMode    string
    sshPort    string
}

const tuiContentWidth = 60

func newTuiModel() tuiModel {
    questions := buildQuestions()
    questions = append(questions, sshQuestion())
    return tuiModel{
        questions: questions,
        cursors:   make([]int, len(questions)),
        answers:   make([]string, len(questions)),
    }
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        return m, nil

    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c", "escape":
            m.phase = phaseCancelled
            return m, tea.Quit
        case "up", "k":
            if m.phase == phaseQuestions && m.cursors[m.current] > 0 {
                m.cursors[m.current]--
            }
        case "down", "j":
            if m.phase == phaseQuestions {
                max := len(m.questions[m.current].options) - 1
                if m.cursors[m.current] < max {
                    m.cursors[m.current]++
                }
            }
        case "enter":
            return m.handleEnter()
        case "backspace":
            if m.phase == phaseQuestions && m.current > 0 {
                m.current--
            } else if m.phase == phaseSummary {
                m.phase = phaseQuestions
                m.current = len(m.questions) - 1
            }
        }
    }
    return m, nil
}

func (m tuiModel) handleEnter() (tea.Model, tea.Cmd) {
    if m.phase == phaseSummary {
        m.phase = phaseConfirmed
        return m, tea.Quit
    }
    if m.phase != phaseQuestions {
        return m, nil
    }

    q := m.questions[m.current]
    selected := q.options[m.cursors[m.current]]
    m.answers[m.current] = selected.value

    if m.current == 1 {
        m = m.handleComponentChoice()
    }

    if m.current < len(m.questions)-1 {
        m.current++
    } else {
        m.phase = phaseSummary
    }
    return m, nil
}

func (m tuiModel) handleComponentChoice() tuiModel {
    hasP2P := false
    for _, q := range m.questions {
        if q.title == "LND P2P Mode" {
            hasP2P = true
            break
        }
    }

    if m.answers[1] == "bitcoin+lnd" && !hasP2P {
        p2p := p2pQuestion()
        newQ := make([]question, 0, len(m.questions)+1)
        newQ = append(newQ, m.questions[:3]...)
        newQ = append(newQ, p2p)
        newQ = append(newQ, m.questions[3:]...)
        m.questions = newQ
        newC := make([]int, len(m.questions))
        copy(newC, m.cursors)
        m.cursors = newC
        newA := make([]string, len(m.questions))
        copy(newA, m.answers)
        m.answers = newA
    } else if m.answers[1] == "bitcoin" && hasP2P {
        for i, q := range m.questions {
            if q.title == "LND P2P Mode" {
                m.questions = append(m.questions[:i], m.questions[i+1:]...)
                m.cursors = append(m.cursors[:i], m.cursors[i+1:]...)
                m.answers = append(m.answers[:i], m.answers[i+1:]...)
                break
            }
        }
    }
    return m
}

func (m tuiModel) View() string {
    if m.width == 0 || m.height == 0 {
        return "Loading..."
    }

    var b strings.Builder

    title := tuiTitleStyle.Width(tuiContentWidth).Align(lipgloss.Center).
        Render(" Virtual Private Node ")
    b.WriteString(title)
    b.WriteString("\n\n")

    switch m.phase {
    case phaseQuestions:
        b.WriteString(m.renderQuestion())
    case phaseSummary:
        b.WriteString(m.renderSummary())
    }

    b.WriteString("\n")
    if m.phase == phaseQuestions {
        b.WriteString(tuiDimStyle.Render("↑↓ navigate • enter select • backspace back • esc quit"))
    } else {
        b.WriteString(tuiDimStyle.Render("enter confirm • backspace edit • esc cancel"))
    }

    return lipgloss.Place(m.width, m.height,
        lipgloss.Center, lipgloss.Center,
        b.String(),
    )
}

func (m tuiModel) renderQuestion() string {
    var b strings.Builder

    b.WriteString(tuiDimStyle.Render(fmt.Sprintf(
        "Question %d of %d", m.current+1, len(m.questions))))
    b.WriteString("\n\n")

    q := m.questions[m.current]
    b.WriteString(tuiSectionStyle.Render(q.title))
    b.WriteString("\n\n")

    for i, opt := range q.options {
        cursor := "  "
        style := tuiUnselectedStyle
        if i == m.cursors[m.current] {
            cursor = "▸ "
            style = tuiSelectedStyle
        }
        b.WriteString(style.Render(cursor+opt.label) + tuiDimStyle.Render(" — "+opt.desc))
        b.WriteString("\n")
        if i == m.cursors[m.current] && opt.warn != "" {
            b.WriteString("  " + tuiWarningStyle.Render("WARNING: "+opt.warn))
            b.WriteString("\n")
        }
    }

    if m.current > 0 {
        b.WriteString("\n" + tuiDimStyle.Render("─────────────────────────────") + "\n")
        for i := 0; i < m.current; i++ {
            b.WriteString(tuiDimStyle.Render(m.questions[i].title+":") + " " + m.answers[i] + "\n")
        }
    }

    return b.String()
}

func (m tuiModel) renderSummary() string {
    var b strings.Builder

    b.WriteString(tuiSectionStyle.Render("Installation Summary"))
    b.WriteString("\n\n")

    r := m.getResult()
    rows := []struct{ key, val string }{
        {"Network", r.network},
        {"Components", r.components},
        {"Prune", r.pruneSize + " GB"},
        {"SSH Port", r.sshPort},
    }

    if r.components == "bitcoin+lnd" {
        mode := "Tor only"
        if r.p2pMode == "hybrid" {
            mode = "Hybrid (Tor + clearnet)"
        }
        rows = append(rows[:3], append([]struct{ key, val string }{{"P2P Mode", mode}}, rows[3:]...)...)
    }

    var content strings.Builder
    for _, row := range rows {
        content.WriteString(tuiSummaryKeyStyle.Render(row.key+":") +
            tuiSummaryValStyle.Render(" "+row.val) + "\n")
    }

    b.WriteString(tuiBoxStyle.Render(content.String()))
    b.WriteString("\n\n")
    b.WriteString(tuiSelectedStyle.Render("Press Enter to install"))

    return b.String()
}

func (m tuiModel) getResult() tuiResult {
    r := tuiResult{network: "testnet4", components: "bitcoin+lnd", pruneSize: "25", p2pMode: "tor", sshPort: "22"}
    for i, q := range m.questions {
        if i >= len(m.answers) || m.answers[i] == "" {
            continue
        }
        switch q.title {
        case "Network":
            r.network = m.answers[i]
        case "Components":
            r.components = m.answers[i]
        case "Blockchain Storage (Pruned)":
            r.pruneSize = m.answers[i]
        case "LND P2P Mode":
            r.p2pMode = m.answers[i]
        case "SSH Port":
            r.sshPort = m.answers[i]
        }
    }
    return r
}

func RunTUI() (*installConfig, error) {
    m := newTuiModel()
    p := tea.NewProgram(m, tea.WithAltScreen())
    result, err := p.Run()
    if err != nil {
        return nil, fmt.Errorf("TUI error: %w", err)
    }

    final := result.(tuiModel)
    if final.phase == phaseCancelled {
        return nil, nil
    }

    r := final.getResult()
    cfg := &installConfig{
        network:    NetworkConfigFromName(r.network),
        components: r.components,
        p2pMode:    r.p2pMode,
        sshPort:    22,
    }
    fmt.Sscanf(r.pruneSize, "%d", &cfg.pruneSize)
    if r.sshPort != "custom" {
        fmt.Sscanf(r.sshPort, "%d", &cfg.sshPort)
    }

    if cfg.p2pMode == "hybrid" {
        cfg.publicIPv4 = detectPublicIP()
        if cfg.publicIPv4 == "" {
            cfg.p2pMode = "tor"
        }
    }

    return cfg, nil
}