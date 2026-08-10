// internal/installer/certwatch.go

package installer

// The LND TLS certificate watch. lnd.conf sets tlsautorefresh,
// which lets LND regenerate its own certificate at ANY startup
// whose parameters changed or whose certificate nears expiry —
// a crash restart, a reboot, or a typed configuration change.
// None of those moments is an operation the TUI
// requested, so no operation can refresh the TUI's staged
// copy of the certificate; without a watcher, the copy goes
// stale the moment LND rewrites the file, and the TUI's
// next connection fails until its self-heal notices. These two
// units close that gap at the source: systemd watches the
// certificate file itself and re-stages the copy within
// seconds of a rewrite — hours before a human shows up to
// read it. Same pattern as the channel-backup watcher
// (lnd-backup-watch.path).

import (
	"fmt"

	"github.com/virtualprivatenode/vpn/internal/logger"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

// lndCertWatchUnits renders the path unit and its oneshot
// service. Pure — unit-tested.
func lndCertWatchUnits() (pathUnit, serviceUnit string) {
	pathUnit = fmt.Sprintf(`[Unit]
Description=Watch the LND TLS certificate for the node console

[Path]
PathChanged=%s
Unit=%s

[Install]
WantedBy=multi-user.target
`, paths.LNDTLSCert, paths.LNDCertStageServiceName)

	serviceUnit = fmt.Sprintf(`[Unit]
Description=Refresh the staged copy of the LND TLS certificate

[Service]
Type=oneshot
ExecStart=%s stage-lnd-cert
SyslogIdentifier=vpn-cert-stage
`, paths.BinaryPath)
	return pathUnit, serviceUnit
}

// installLNDCertWatch writes both units, reloads systemd, and
// enables and starts the path unit, verifying it is active.
// Idempotent for a recognized interrupted base install.
func installLNDCertWatch() error {
	pathUnit, serviceUnit := lndCertWatchUnits()
	if err := system.SudoWriteFile(paths.LNDCertWatchPath,
		[]byte(pathUnit), 0644); err != nil {
		return err
	}
	if err := system.SudoWriteFile(paths.LNDCertStageService,
		[]byte(serviceUnit), 0644); err != nil {
		return err
	}
	if err := system.SudoRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := system.SudoRun("systemctl", "enable", "--now",
		paths.LNDCertWatchPathName); err != nil {
		return err
	}
	// Postcondition: the watch is really armed.
	if !system.IsServiceActive(paths.LNDCertWatchPathName) {
		return fmt.Errorf("%s is not active after enable",
			paths.LNDCertWatchPathName)
	}
	logger.Install("LND TLS certificate watch enabled (%s)",
		paths.LNDCertWatchPathName)
	return nil
}
