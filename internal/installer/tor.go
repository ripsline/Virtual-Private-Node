// internal/installer/tor.go

package installer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

func installTor() error {
	return system.SudoRun("apt-get", "install", "-y", "-qq", "tor", "torsocks")
}

// BuildTorConfig generates the complete torrc content from config state.
// Pure logic — no side effects.
// Note: HiddenServiceDir paths are hardcoded strings because they are
// torrc config content read by Tor, not Go logic paths.
func BuildTorConfig(cfg *config.AppConfig) (string, error) {
	net, err := cfg.NetworkConfig()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Virtual Private Node — Tor Configuration\n")
	b.WriteString("SOCKSPort 9050\n")

	// Control port: always emitted. Two consumers — the install-path
	// Tor routing gate (torgate.go reads bootstrap progress here,
	// unconditionally) and LND's P2P onion management. Loopback-only,
	// cookie-authenticated; emitting it without LND adds no exposure.
	b.WriteString("\n# Control port (install routing gate + LND onion management)\n")
	b.WriteString("ControlPort 9051\n")
	b.WriteString("CookieAuthentication 1\n")
	b.WriteString("CookieAuthFileGroupReadable 1\n")

	b.WriteString(fmt.Sprintf(`
# Bitcoin Core P2P (static onion address for peers)
HiddenServiceDir /var/lib/tor/bitcoin-p2p/
HiddenServicePort %d 127.0.0.1:%d
`, net.P2PPort, net.P2PPort))

	if cfg.HasLND() {
		b.WriteString(`
# LND gRPC (wallet connections over Tor)
HiddenServiceDir /var/lib/tor/lnd-grpc/
HiddenServicePort 10009 127.0.0.1:10009

# LND REST (wallet connections over Tor)
HiddenServiceDir /var/lib/tor/lnd-rest/
HiddenServicePort 8080 127.0.0.1:8080
`)
	}

	if cfg.SyncthingEnabled {
		b.WriteString(`
# Syncthing web UI (Tor only, HTTP)
HiddenServiceDir /var/lib/tor/syncthing/
HiddenServicePort 8384 127.0.0.1:8384
`)
		// Sync protocol (port 22000) goes over clearnet.
		// No hidden service needed — Syncthing uses mutual TLS
		// with explicit device approval for authentication.
	}

	return b.String(), nil
}

// RebuildTorConfig writes the torrc to disk.
func RebuildTorConfig(cfg *config.AppConfig) error {
	content, err := BuildTorConfig(cfg)
	if err != nil {
		return err
	}
	if err := system.SudoWriteFile(
		paths.Torrc, []byte(content), 0640); err != nil {
		return err
	}
	return system.SudoRun("chown", "root:debian-tor", paths.Torrc)
}

var (
	torBinaryPresentForAddon = func() bool {
		_, err := exec.LookPath("tor")
		return err == nil
	}
	torServiceEnabledForAddon = func() bool {
		return system.RunSilent(
			"systemctl", "is-enabled", "--quiet", "tor") == nil
	}
	torServiceActiveForAddon = func() bool {
		return system.IsServiceActive("tor")
	}
	readTorConfigForAddon      = os.ReadFile
	readSyncthingOnionForAddon = os.ReadFile
	sleepForTorAddon           = time.Sleep
	runTorServiceAction        = func(action string) error {
		return system.SudoRun("systemctl", action, "tor")
	}
)

// enableAndRestartTor belongs to initial installation. That operation owns
// establishing Tor's boot persistence as part of the base node.
func enableAndRestartTor() error {
	if err := runTorServiceAction("enable"); err != nil {
		return err
	}
	return restartTor()
}

// restartTor reloads the project-owned canonical torrc without changing the
// unit's enablement policy. Post-install add-ons may call this only after
// proving the required base Tor state through the prerequisite below.
func restartTor() error {
	return runTorServiceAction("restart")
}

// verifySyncthingTorPrerequisite refuses add-on mutation unless Tor is the
// installed, enabled, active base service and its project-owned torrc still
// matches the authoritative configuration. Syncthing does not silently repair
// a disabled service or overwrite unexplained base-config divergence.
func verifySyncthingTorPrerequisite(cfg *config.AppConfig) error {
	if !torBinaryPresentForAddon() {
		return fmt.Errorf("Tor is not installed — refusing Syncthing installation")
	}
	if !torServiceEnabledForAddon() {
		return fmt.Errorf("Tor is not enabled — refusing Syncthing installation")
	}
	if !torServiceActiveForAddon() {
		return fmt.Errorf("Tor is not active — refusing Syncthing installation")
	}
	current, err := readTorConfigForAddon(paths.Torrc)
	if err != nil {
		return fmt.Errorf("read current Tor configuration: %w", err)
	}
	expected, err := BuildTorConfig(cfg)
	if err != nil {
		return fmt.Errorf("build expected Tor configuration: %w", err)
	}
	if !bytes.Equal(current, []byte(expected)) {
		return fmt.Errorf(
			"Tor configuration does not match the expected base node — " +
				"refusing Syncthing installation")
	}
	return nil
}

// VerifySyncthingInstallPrerequisites is the root helper's before-mutation
// gate. UFW and Tor must already be healthy base-node facilities; the optional
// add-on never installs, enables, or globally repairs either one.
func VerifySyncthingInstallPrerequisites(cfg *config.AppConfig) error {
	if err := requireActiveUFW(); err != nil {
		return err
	}
	return verifySyncthingTorPrerequisite(cfg)
}

func validV3OnionHostname(hostname string) bool {
	const suffix = ".onion"
	if len(hostname) != 56+len(suffix) ||
		!strings.HasSuffix(hostname, suffix) {
		return false
	}
	for _, c := range strings.TrimSuffix(hostname, suffix) {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

func waitForSyncthingOnion() error {
	var lastErr error
	for i := 0; i < 60; i++ {
		data, err := readSyncthingOnionForAddon(
			paths.TorSyncthingHostname)
		if err == nil {
			hostname := strings.TrimSpace(string(data))
			if validV3OnionHostname(hostname) {
				return nil
			}
			lastErr = fmt.Errorf(
				"invalid Syncthing onion hostname %q", hostname)
		} else {
			lastErr = err
		}
		sleepForTorAddon(time.Second)
	}
	return fmt.Errorf(
		"Syncthing onion hostname was not created after Tor restart: %w",
		lastErr)
}

// restartTorForSyncthing applies the already-written canonical configuration,
// proves Tor returned active, and waits for the new hidden-service identity.
// It deliberately does not enable Tor.
func restartTorForSyncthing() error {
	if err := restartTor(); err != nil {
		return err
	}
	if !torServiceActiveForAddon() {
		return fmt.Errorf("Tor is not active after Syncthing configuration restart")
	}
	return waitForSyncthingOnion()
}
