// internal/installer/sshd.go

package installer

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/virtualprivatenode/vpn/internal/helper"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

// BuildSSHHardeningConfig generates the complete contents
// of /etc/ssh/sshd_config.d/00-vpn-hardening.conf from the
// requested closed-set state. Pure function — no side effects. (The 00-
// prefix is load-bearing: sshd applies the first match
// per directive, so this drop-in must sort before a
// provider's 50-cloud-init.conf to win contested
// directives. paths.SSHDDropIn is the single source of
// truth for the name.)
//
// The installer writes the initial drop-in with the
// PasswordAuthentication value EXPLICIT-from-observed
// (ruling xvi(a); see sshstep.go) — "no flip at install"
// holds by identity, not omission. Once the TUI's SSH
// Password Auth screen runs, this function takes over and
// the drop-in stays the authoritative source for password
// auth state.
func BuildSSHHardeningConfig(disabled bool) string {
	passwordAuth := "yes"
	if disabled {
		passwordAuth = "no"
	}
	return buildHardeningDropIn(passwordAuth)
}

// buildHardeningDropIn renders the drop-in body.
// passwordAuth is "yes", "no", or "" — empty OMITS the
// directive entirely (the install step's degraded mode when
// the sshd observation failed: asserting nothing beats
// asserting a guess, ruling xvi(a)). Pure — unit-tested.
func buildHardeningDropIn(passwordAuth string) string {
	base := `# Virtual Private Node — SSH hardening
# Managed by the vpn TUI. Do not edit by hand.
PermitRootLogin no
PubkeyAuthentication yes
ChallengeResponseAuthentication no
KbdInteractiveAuthentication no
X11Forwarding no
`
	if passwordAuth == "" {
		return base
	}
	return base + "PasswordAuthentication " + passwordAuth + "\n"
}

// RebuildSSHHardeningConfig writes the drop-in to disk and
// restarts sshd. Idempotent.
//
// Two lockout guards live here, at the write boundary, so
// every current and future caller inherits them:
//
//  1. Policy: refuses to write a config that disables
//     password authentication while zero SSH keys exist —
//     that would leave the system with zero auth methods.
//  2. Syntax: after the new drop-in is swapped in, the
//     merged sshd configuration is validated (sshd -t)
//     BEFORE any restart. An invalid config would stop
//     sshd from starting at all — a total lockout. On
//     validation failure the previous drop-in content is
//     restored and sshd keeps running its current config.
func RebuildSSHHardeningConfig(disabled bool) error {
	if disabled {
		keys, err := ListAuthorizedKeys()
		if err != nil {
			return fmt.Errorf(
				"check authorized_keys: %w", err)
		}
		if len(keys) == 0 {
			return errors.New(
				"refusing to disable password " +
					"authentication with no SSH keys " +
					"configured — add a key first")
		}
	}

	// Capture the previous drop-in so an invalid merged
	// config can be rolled back before any restart. A
	// missing file is fine (first write); any other read
	// failure aborts, because proceeding would mean a
	// validation failure could not be rolled back.
	prev, prevErr := system.SudoReadFile(paths.SSHDDropIn)
	prevExists := true
	if prevErr != nil {
		if os.IsNotExist(prevErr) ||
			strings.Contains(prevErr.Error(),
				"No such file") {
			prevExists = false
		} else {
			return fmt.Errorf(
				"read current sshd drop-in: %w", prevErr)
		}
	}

	content := BuildSSHHardeningConfig(disabled)
	if err := system.SudoWriteFile(
		paths.SSHDDropIn, []byte(content), 0644); err != nil {
		return fmt.Errorf(
			"write sshd drop-in: %w", err)
	}

	// Validate the merged config (all of sshd_config plus
	// every drop-in) before restarting.
	if out, err := system.SudoRunCombinedOutput(
		"sshd", "-t"); err != nil {
		detail := strings.TrimSpace(out)
		if restoreErr := restorePreviousDropIn(
			prev, prevExists); restoreErr != nil {
			return fmt.Errorf(
				"sshd rejected the new config (%s) and "+
					"restoring the previous drop-in also "+
					"failed (%v) — sshd was NOT restarted "+
					"and keeps running its current config; "+
					"inspect %s before restarting sshd",
				detail, restoreErr, paths.SSHDDropIn)
		}
		return fmt.Errorf(
			"sshd rejected the new config, previous "+
				"drop-in restored, sshd not restarted: %s",
			detail)
	}

	// No staged copy to refresh afterwards: the effective
	// password-auth answer is read live (sshd -T through the
	// helper's read-ssh-auth operation) by every unprivileged
	// consumer, so the write cannot leave a stale copy behind
	// — there is none.
	return restartSSHD()
}

// restorePreviousDropIn puts the drop-in back to its
// pre-rebuild state: the captured content, or absent if the
// file did not exist before.
func restorePreviousDropIn(prev []byte, existed bool) error {
	if !existed {
		return system.SudoRun("rm", "-f", paths.SSHDDropIn)
	}
	return system.SudoWriteFile(
		paths.SSHDDropIn, prev, 0644)
}

// As root the rebuild runs directly; from the unprivileged TUI
// it is requested from the helper as the typed
// rebuild-ssh-config operation — the drop-in template, the
// zero-auth lockout guard, and the validate-and-restore
// sequence all live on the root side either way (the write
// boundary), so every caller passes through them.
func SetSSHPasswordAuth(disabled bool) error {
	if os.Geteuid() == 0 {
		return RebuildSSHHardeningConfig(disabled)
	}
	return helper.Call(helper.VerbRebuildSSHConfig,
		helper.RebuildSSHConfigParams{
			PasswordAuthDisabled: disabled,
		}, nil)
}

// EffectiveSSHPasswordAuth reports whether sshd's effective
// configuration permits password authentication for the admin
// user.
//
// Both paths ask sshd itself, at the moment of the question:
//
//   - As root (installer, helper): directly — see
//     queryEffectiveSSHPasswordAuth.
//   - Unprivileged (the TUI): through the helper's read-only
//     read-ssh-auth operation, which runs the same query on
//     the root side. No copy of the answer is kept anywhere:
//     sshd's config can change outside this app (a hand edit,
//     provider tooling), a stale recorded "yes" would permit
//     removing the last SSH key on a box where password login
//     is off — a lockout — and a fact with no failure moment
//     cannot be repaired at one. Reading live removes the
//     stale input entirely.
//
// Callers MUST treat an error as "password auth unavailable"
// and refuse the risky action — never as unknown-so-proceed.
// An unreachable helper therefore biases toward over-refusal,
// the safe direction.
func EffectiveSSHPasswordAuth() (bool, error) {
	if os.Geteuid() == 0 {
		return queryEffectiveSSHPasswordAuth()
	}
	var res helper.SSHAuthResult
	if err := helper.Call(
		helper.VerbReadSSHAuth, nil, &res); err != nil {
		return false, err
	}
	return res.PasswordAuthEnabled, nil
}

// queryEffectiveSSHPasswordAuth asks sshd itself:
//
//	sshd -T -C user=<admin>,host=localhost,addr=127.0.0.1
//
// -T prints the fully resolved configuration; -C supplies
// the simulated connection so Match blocks targeting the
// admin user or their group are honored. Reading any single
// file — including our own drop-in — would answer with one
// file's vote, not the first-match-wins election result
// across sshd_config and every drop-in (e.g. a provider's
// 50-cloud-init.conf).
//
// -T, not -G: upstream defines -C as the connection spec
// "for the -T extended test mode", and Debian 13's OpenSSH
// (10.0p1, frozen for the release's life) rejects -C with
// -G outright. -G only gained -C support in later OpenSSH.
// -T also runs config-validity and host-key sanity checks,
// so it ERRORS on a broken sshd setup — which is aligned:
// callers refuse on error, and a box with a broken sshd is
// exactly where the last key must not be removed.
//
// The host/addr values are synthetic: a future connection's
// real source address is unknowable, so a Match rule scoped
// to source ADDRESSES is evaluated against 127.0.0.1. That
// residual biases toward over-refusal; user/group-targeted
// rules (the hardening pattern that occurs in practice)
// resolve exactly. Root only — sshd -T reads host keys.
func queryEffectiveSSHPasswordAuth() (bool, error) {
	out, err := system.SudoRunOutput("sshd", "-T",
		"-C", "user="+paths.AdminUser+
			",host=localhost,addr=127.0.0.1")
	if err != nil {
		return false, fmt.Errorf(
			"query effective sshd config: %w", err)
	}
	return parsePasswordAuth(out)
}

// parsePasswordAuth extracts the passwordauthentication
// value from sshd -T output (one "keyword value" pair per
// line, keywords normalized to lowercase by sshd). Pure
// function so it is testable without sshd present.
func parsePasswordAuth(sshdOutput string) (bool, error) {
	for _, line := range strings.Split(sshdOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(
			fields[0], "passwordauthentication") {
			return strings.EqualFold(
				fields[1], "yes"), nil
		}
	}
	return false, errors.New(
		"passwordauthentication not present in " +
			"sshd -T output")
}

func restartSSHD() error {
	// Try the systemd service named "sshd" first; some
	// distros use "ssh" as the unit name.
	if err := system.SudoRun(
		"systemctl", "restart", "sshd"); err == nil {
		return nil
	}
	return system.SudoRun("systemctl", "restart", "ssh")
}
