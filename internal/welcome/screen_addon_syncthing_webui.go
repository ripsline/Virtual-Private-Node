package welcome

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/virtualprivatenode/vpn/internal/theme"
)

// ── SyncthingWebUIScreen ───────────────────────────────
// Shows Syncthing Web UI connection info: URL, user,
// password with show/hide toggle. Two buttons: Full URL
// and Show/Hide Password.
//
// The web UI's onion address is live-read at screen entry
// (no stored copy exists anywhere) and held only for this
// screen's lifetime.

type SyncthingWebUIScreen struct {
	ctx         *ScreenContext
	btnIdx      int // 0=Full URL, 1=Show/Hide Password
	showSecrets bool
	syncOnion   string
	fetched     bool // the live read answered
	fetchErr    bool // ...with an error (already logged)
}

func NewSyncthingWebUIScreen(
	ctx *ScreenContext,
) *SyncthingWebUIScreen {
	return &SyncthingWebUIScreen{
		ctx: ctx,
	}
}

// ── Screen interface ────────────────────────────────────

func (s *SyncthingWebUIScreen) Init() tea.Cmd {
	return fetchNodeAddressesCmd(tabSyncthingWebUI)
}

func (s *SyncthingWebUIScreen) HandleKey(
	keyStr string, msg tea.KeyPressMsg,
) (Screen, tea.Cmd) {
	switch keyStr {
	case "ctrl+c":
		return s, tea.Quit
	case "left":
		if s.btnIdx > 0 {
			s.btnIdx--
			return s, nil
		}
		return s, emitFocusSidebar
	case "right":
		if s.btnIdx < 1 {
			s.btnIdx++
		}
		return s, nil
	case "up", "shift+tab":
		if s.ctx.HasTabs {
			return s, emitFocusTabBar
		}
		return s, nil
	case "down", "tab":
		return s, nil
	case "backspace":
		return s, emitFocusParent
	case "enter":
		return s.handleEnter()
	}
	return s, nil
}

func (s *SyncthingWebUIScreen) handleEnter() (
	Screen, tea.Cmd,
) {
	switch s.btnIdx {
	case 0: // Full URL
		syncOnion := s.syncOnion
		if syncOnion != "" {
			url := "http://" + syncOnion + ":8384"
			return s, func() tea.Msg {
				return showFullURLMsg{URL: url}
			}
		}
	case 1: // Show/Hide Password
		s.showSecrets = !s.showSecrets
	}
	return s, nil
}

func (s *SyncthingWebUIScreen) HandleMsg(
	msg tea.Msg,
) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tabActivatedMsg:
		// Re-entering the tab re-asks: screen entry is the
		// cadence at which live-read facts are read.
		return s, fetchNodeAddressesCmd(tabSyncthingWebUI)
	case nodeAddressesMsg:
		s.syncOnion = msg.addrs.SyncthingOnion
		s.fetched = true
		s.fetchErr = msg.err != nil
	}
	return s, nil
}

func (s *SyncthingWebUIScreen) View(
	w, h int,
) string {
	p := newPane(w)
	p.title(theme.Header, "↻ Syncthing Web UI")

	syncOnion := s.syncOnion
	if syncOnion == "" {
		switch {
		case !s.fetched:
			p.dim("Reading the web UI's Tor address...")
		case s.fetchErr:
			p.warn("Cannot read the web UI's Tor address — " +
				"check: journalctl -u vpn-helperd")
		default:
			p.warn("Tor address not available yet.")
		}
		return p.renderWithBottomButtons(
			[]string{"Waiting..."}, 0, false, h)
	}

	url := "http://" + syncOnion + ":8384"
	if len(url) > w-4 {
		url = url[:w-7] + "..."
	}

	p.labelLine("URL:")
	if s.showSecrets {
		p.mono(url)
	}
	p.blank()
	p.monoField("User: ", "admin")

	if s.ctx.Cfg.SyncthingPassword != "" {
		if s.showSecrets {
			p.monoField("Pass: ",
				s.ctx.Cfg.SyncthingPassword)
		} else {
			p.line(" " +
				theme.Label.Render("Pass: ") +
				theme.Dim.Render("••••••••"))
		}
	}

	showLabel := "Show Password"
	if s.showSecrets {
		showLabel = "Hide Password"
	}

	return p.renderWithBottomButtons(
		[]string{"Full URL", showLabel},
		s.btnIdx, s.ctx.ContentFocused, h)
}

func (s *SyncthingWebUIScreen) HelpBindings() []key.Binding {
	return tabButtonBindings(s.ctx.HasTabs)
}
