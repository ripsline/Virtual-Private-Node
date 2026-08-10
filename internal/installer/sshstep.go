// internal/installer/sshstep.go

package installer

// The install-path SSH hardening step observes effective state, writes the
// current project drop-in, validates the merged configuration, and restarts
// sshd. Prior rlvpn drop-ins are lifecycle conflicts and never reach this step;
// v0.7.0 does not delete or migrate them.
//
// Directive election (ruling xvi(a)): the new drop-in writes
// PasswordAuthentication EXPLICITLY with the value observed
// seconds before, in the same process. Explicit-from-observed is
// STRONGER than the script's omission: our 00- file owns the
// setting from here on, so later provider/cloud-init drift
// cannot silently re-enable password auth. Only if the
// observation itself fails does the step degrade to the script's
// omission semantics, with a logged warning — preflight already
// proved sshd observable, so this failing mid-install means the
// environment regressed, and asserting nothing beats asserting a
// guess.

import (
	"fmt"
	"os"
	"strings"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/logger"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

// installSSHHardening is the ssh.harden step.
func installSSHHardening(cfg *config.AppConfig) error {
	// 1. Observe — seconds before the write, same process.
	passwordAuth := ""
	obs, err := ObserveSSHState()
	if err != nil {
		logger.Install(
			"WARNING: sshd observation failed at the SSH step "+
				"(%v) — writing the drop-in WITHOUT a "+
				"PasswordAuthentication directive (script "+
				"omission semantics); password auth state is "+
				"whatever the rest of the sshd config elects", err)
	} else {
		if obs.PasswordAuth {
			passwordAuth = "yes"
		} else {
			passwordAuth = "no"
		}
		// Reality wins over the in-memory observation copied at preflight if
		// sshd changed during the run.
		disabled := !obs.PasswordAuth
		if cfg.SSHPasswordAuthDisabled != disabled {
			logger.Install(
				"config said ssh_password_auth_disabled=%v but "+
					"sshd's effective state is %v — config corrected "+
					"from observation",
				cfg.SSHPasswordAuthDisabled, disabled)
			cfg.SSHPasswordAuthDisabled = disabled
		}
	}

	// Capture the current project file so validation failure can restore it.
	prevNew, prevNewExists, err := readDropIn(paths.SSHDDropIn)
	if err != nil {
		return fmt.Errorf("read current sshd drop-in: %w", err)
	}
	// 2. Write the current drop-in.
	content := buildHardeningDropIn(passwordAuth)
	if err := system.SudoWriteFile(
		paths.SSHDDropIn, []byte(content), 0644); err != nil {
		return fmt.Errorf("write sshd drop-in: %w", err)
	}

	// 3. Validate the merged config before any restart.
	if out, err := system.SudoRunCombinedOutput(
		"sshd", "-t"); err != nil {
		detail := strings.TrimSpace(out)
		restoreErr := restoreDropIn(prevNew, prevNewExists)
		if restoreErr != nil {
			return fmt.Errorf(
				"sshd rejected the new config (%s) and restoring "+
					"the previous drop-ins also failed (%v) — sshd "+
					"was NOT restarted and keeps running its current "+
					"config; inspect %s before restarting sshd",
				detail, restoreErr, paths.SSHDDropIn)
		}
		return fmt.Errorf(
			"sshd rejected the new config, previous drop-ins "+
				"restored, sshd not restarted: %s", detail)
	}

	// 4. Restart.
	if err := restartSSHD(); err != nil {
		return err
	}
	if passwordAuth == "" {
		logger.Install("SSH hardening applied (root login " +
			"disabled; PasswordAuthentication directive omitted — " +
			"degraded mode)")
	} else {
		logger.Install("SSH hardening applied (root login "+
			"disabled; PasswordAuthentication %s, from observation)",
			passwordAuth)
	}
	return nil
}

// readDropIn reads a drop-in, distinguishing absent (fine) from
// unreadable (abort — an unreadable prior state could not be
// restored after a failed validation).
func readDropIn(path string) ([]byte, bool, error) {
	data, err := system.SudoReadFile(path)
	if err != nil {
		if os.IsNotExist(err) ||
			strings.Contains(err.Error(), "No such file") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// restoreDropIn puts the current project drop-in back to its captured state.
func restoreDropIn(prevNew []byte, newExisted bool) error {
	if newExisted {
		return system.SudoWriteFile(paths.SSHDDropIn, prevNew, 0644)
	}
	return system.SudoRun("rm", "-f", paths.SSHDDropIn)
}
