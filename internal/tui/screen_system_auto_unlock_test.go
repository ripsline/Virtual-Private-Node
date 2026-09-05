package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/installer"
	"github.com/virtualprivatenode/vpn/internal/lndrpc"
)

func autoUnlockTestContext(enabled bool) *ScreenContext {
	cfg := config.Default()
	cfg.AutoUnlock = enabled
	return &ScreenContext{
		Cfg:            cfg,
		State:          &RuntimeState{WalletKnown: true, WalletExists: true},
		ContentFocused: true,
	}
}

func TestAutoUnlockVerificationFailureReturnsToExistingForm(t *testing.T) {
	s := NewAutoUnlockScreen(autoUnlockTestContext(false))
	s.pw1.SetValue("not retained")
	s.pw2.SetValue("not retained")
	_, _ = s.HandleMsg(autoUnlockSetupDoneMsg{result: installer.AutoUnlockResult{
		Outcome: installer.AutoUnlockVerificationFailed,
	}})
	if s.state != auState_form || s.pw1.Value() != "" || s.pw2.Value() != "" {
		t.Fatalf("retry form state=%v pw1=%q pw2=%q", s.state, s.pw1.Value(), s.pw2.Value())
	}
	view := s.View(82, 34)
	for _, want := range []string{
		"Configure Auto-Unlock", "VPN could not verify that password.",
		"LND is locked.", "Skip", "Confirm",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("retry view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Try Again") || strings.Contains(view, "Leave LND Locked") {
		t.Fatal("retry flow introduced unapproved replacement buttons")
	}
}

func TestAutoUnlockTimeoutCopyIsExplicitAndInconclusive(t *testing.T) {
	s := NewAutoUnlockScreen(autoUnlockTestContext(false))
	_, _ = s.HandleMsg(autoUnlockSetupDoneMsg{result: installer.AutoUnlockResult{
		Outcome: installer.AutoUnlockVerificationTimedOut,
	}})
	view := s.View(82, 34)
	for _, want := range []string{
		"did not become ready within 120 seconds",
		"could not determine whether the",
		"password was correct",
		"returned to the locked state",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("timeout view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "password was accepted") {
		t.Fatal("timeout copy makes an unproved password claim")
	}
}

func TestAutoUnlockRepairRequiredUsesApprovedMessage(t *testing.T) {
	s := NewAutoUnlockScreen(autoUnlockTestContext(false))
	_, _ = s.HandleMsg(autoUnlockSetupDoneMsg{result: installer.AutoUnlockResult{
		Outcome:    installer.AutoUnlockRepairRequired,
		FailedStep: "restore normal restart policy",
	}})
	view := s.View(82, 34)
	for _, want := range []string{
		"Repair Required",
		"VPN could not prove the auto-unlock state due to a system failure.",
		"Do not assume",
		"LND is online or that auto-unlock is correctly configured.",
		"Failed step: restore normal restart policy",
		"Done",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("repair view missing %q:\n%s", want, view)
		}
	}
}

func TestAutoUnlockHelperResponseFailureRequiresRepairWithoutRawError(t *testing.T) {
	s := NewAutoUnlockScreen(autoUnlockTestContext(false))
	_, _ = s.HandleMsg(autoUnlockSetupDoneMsg{
		err: errors.New("injected socket detail"),
	})
	view := s.View(82, 34)
	for _, want := range []string{"Repair Required", "Failed step: complete privileged operation"} {
		if !strings.Contains(view, want) {
			t.Fatalf("helper failure view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "injected socket detail") {
		t.Fatal("raw helper error leaked into the user-facing repair message")
	}
}

func TestDisableRollbackReportsStillEnabled(t *testing.T) {
	ctx := autoUnlockTestContext(true)
	s := NewAutoUnlockScreen(ctx)
	_, _ = s.HandleMsg(autoUnlockDisableDoneMsg{result: installer.AutoUnlockResult{
		Outcome: installer.AutoUnlockStillEnabled,
	}})
	if !ctx.Cfg.AutoUnlock || s.state != auState_doneOK {
		t.Fatalf("rollback state cfg=%v screen=%v", ctx.Cfg.AutoUnlock, s.state)
	}
	view := s.View(82, 34)
	for _, want := range []string{
		"Auto-Unlock Still Enabled", "previous enabled state was restored",
		"LND is online", "Done",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("rollback view missing %q:\n%s", want, view)
		}
	}
}

func TestSuccessfulDisableOffersReenableOnSameScreen(t *testing.T) {
	ctx := autoUnlockTestContext(true)
	s := NewAutoUnlockScreen(ctx)
	_, _ = s.HandleMsg(autoUnlockDisableDoneMsg{result: installer.AutoUnlockResult{
		Outcome: installer.AutoUnlockDisabled,
	}})
	view := s.View(82, 34)
	for _, want := range []string{"Auto-Unlock Disabled", "Done", "Re-enable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("disabled view missing %q:\n%s", want, view)
		}
	}
	_, _ = s.HandleKey("right", tea.KeyPressMsg{})
	_, _ = s.HandleKey("enter", tea.KeyPressMsg{})
	if s.mode != autoUnlockEnable || s.state != auState_form ||
		s.focusZone != auZoneInput1 {
		t.Fatalf("re-enable did not return to enable form: mode=%v state=%v focus=%v",
			s.mode, s.state, s.focusZone)
	}
}

func TestSystemServiceRowShowsLockedLND(t *testing.T) {
	ctx := autoUnlockTestContext(false)
	ctx.Status = &statusMsg{
		services:       map[string]bool{"lnd": true},
		lndWalletState: lndrpc.WalletStateLocked,
	}
	view := NewSystemHomeScreen(ctx).View(82, 34)
	if !strings.Contains(view, "lnd") || !strings.Contains(view, "locked") {
		t.Fatalf("LND service row does not expose locked state:\n%s", view)
	}
}
