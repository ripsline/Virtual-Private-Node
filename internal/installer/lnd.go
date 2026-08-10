// internal/installer/lnd.go

package installer

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/helper"
	"github.com/virtualprivatenode/vpn/internal/logger"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

func downloadLND(version, workDir string) error {
	filename := fmt.Sprintf("lnd-linux-amd64-v%s.tar.gz", version)
	url := fmt.Sprintf(
		"https://github.com/lightningnetwork/lnd/releases/download/v%s/%s",
		version, filename)
	manifestURL := fmt.Sprintf(
		"https://github.com/lightningnetwork/lnd/releases/download/v%s/manifest-v%s.txt",
		version, version)
	if err := system.DownloadRequireTor(
		url, filepath.Join(workDir, filename)); err != nil {
		return err
	}
	if err := system.DownloadRequireTor(
		manifestURL,
		filepath.Join(workDir, "manifest.txt")); err != nil {
		return fmt.Errorf("download LND manifest: %w", err)
	}
	return nil
}

func extractAndInstallLND(version, workDir string) error {
	filename := fmt.Sprintf("lnd-linux-amd64-v%s.tar.gz", version)
	if err := system.Run("tar", "-xzf",
		filepath.Join(workDir, filename),
		"-C", workDir); err != nil {
		return err
	}
	extractDir := filepath.Join(workDir,
		fmt.Sprintf("lnd-linux-amd64-v%s", version))
	for _, bin := range []string{"lnd", "lncli"} {
		src := filepath.Join(extractDir, bin)
		if err := system.SudoRun("install", "-m", "0755",
			"-o", "root", "-g", "root",
			src, "/usr/local/bin/"); err != nil {
			return err
		}
	}
	return nil
}

func createLNDDirs(username string) error {
	dirs := []struct {
		path  string
		owner string
		mode  os.FileMode
	}{
		{paths.LNDDir, "root:" + username, 0750},
		{paths.LNDDataDir, username + ":" + username, 0750},
	}
	for _, d := range dirs {
		if err := system.SudoRun("mkdir", "-p", d.path); err != nil {
			return err
		}
		if err := system.SudoRun("chown", d.owner, d.path); err != nil {
			return err
		}
		if err := system.SudoRun("chmod", fmt.Sprintf("%o", d.mode), d.path); err != nil {
			return err
		}
	}
	return nil
}

// BuildLNDConfig generates lnd.conf content. Pure logic — no
// side effects. LND binds by the literal loopback addresses
// defined once in paths — the same constants every client
// dials — so the two ends of each connection cannot disagree,
// and the name localhost appears nowhere in the file (on this
// node, which disables IPv6, that name can resolve to an
// unusable IPv6 address).
func BuildLNDConfig(
	cfg *config.AppConfig, publicIPv4, restOnion,
	bitcoindRPCUser, bitcoindRPCPass string,
) string {
	net := cfg.NetworkConfig()

	listenLine := "listen=" + paths.LNDP2PBind
	restListenLine := "restlisten=" + paths.LNDRESTEndpoint
	externalLine := ""
	tlsExtraIP := ""
	if cfg.P2PMode == "hybrid" && publicIPv4 != "" {
		listenLine = "listen=0.0.0.0:9735"
		restListenLine = "restlisten=0.0.0.0:8080"
		externalLine = fmt.Sprintf("externalhosts=%s:9735", publicIPv4)
		tlsExtraIP = fmt.Sprintf("tlsextraip=%s", publicIPv4)
	}

	tlsExtraDomain := ""
	if restOnion != "" {
		tlsExtraDomain = fmt.Sprintf("tlsextradomain=%s", restOnion)
	}

	return fmt.Sprintf(`# Virtual Private Node — LND
[Application Options]
lnddir=/var/lib/lnd
%s
rpclisten=%s
%s
debuglevel=info
%s
%s
%s

# Let LND own its TLS cert lifecycle. tlsautorefresh
# regenerates the cert when its parameters change
# (e.g. tlsextraip is added during a P2P upgrade) or
# when it's near expiry. tlsdisableautofill keeps the
# cert deterministic — it contains only what we set
# explicitly here, not autodetected interface IPs.
# This is the same pattern used by Raspiblitz.
tlsautorefresh=1
tlsdisableautofill=1

# Accept keysend (spontaneous) and AMP (multi-path)
# payments. Many Lightning apps depend on keysend.
accept-keysend=true
accept-amp=true

# Auto-delete canceled invoices to prevent database
# bloat on long-running nodes.
gc-canceled-invoices-on-the-fly=true

# Allow routing payments that arrive and depart on the
# same channel. Required for circular rebalancing.
allow-circular-route=true

[Bitcoin]
%s
bitcoin.node=bitcoind

[Bitcoind]
bitcoind.rpcuser=%s
bitcoind.rpcpass=%s
bitcoind.rpchost=127.0.0.1:%d
bitcoind.zmqpubrawblock=tcp://127.0.0.1:%d
bitcoind.zmqpubrawtx=tcp://127.0.0.1:%d

[Tor]
tor.active=true
tor.socks=127.0.0.1:9050
tor.control=127.0.0.1:9051
tor.targetipaddress=127.0.0.1
tor.v3=true
tor.streamisolation=true

[protocol]
# Taproot channels: smaller, cheaper cooperative closes
# with better on-chain privacy (MuSig2 key spend).
protocol.simple-taproot-chans=true
# Accept channels larger than 0.16 BTC.
protocol.wumbo-channels=true
# Channels referenced by alias instead of on-chain UTXO
# for better privacy in gossip.
protocol.option-scid-alias=true

[db]
# Compact the bolt database on startup to reclaim disk
# space from deleted records. Runs at most once per week.
db.bolt.auto-compact=true
db.bolt.auto-compact-min-age=168h

[healthcheck]
# Graceful shutdown if disk space falls below 5%%. On a
# 90GB SSD this triggers at ~4.5GB free — enough headroom
# for bolt compaction while avoiding false shutdowns.
healthcheck.diskspace.diskrequired=0.05
healthcheck.diskspace.attempts=2
healthcheck.diskspace.interval=12h
`, listenLine, paths.LNDGRPCEndpoint, restListenLine, externalLine,
		tlsExtraDomain, tlsExtraIP,
		net.LNDBitcoinFlag, bitcoindRPCUser, bitcoindRPCPass,
		net.RPCPort, net.ZMQBlockPort, net.ZMQTxPort)
}

func writeLNDConfig(cfg *config.AppConfig, publicIPv4 string) error {
	password, err := readLNDBitcoindRPCPassword()
	if err != nil {
		return err
	}
	return writeLNDConfigWithRPCPassword(cfg, publicIPv4, password)
}

func writeLNDConfigWithRPCPassword(
	cfg *config.AppConfig, publicIPv4, password string,
) error {
	if password == "" {
		return fmt.Errorf("refusing to write lnd.conf with an empty " +
			"bitcoind RPC password")
	}
	restOnion, err := readRequiredLNDRESTOnion()
	if err != nil {
		return err
	}
	content := BuildLNDConfig(cfg, publicIPv4, restOnion,
		LNDBitcoindRPCUser, password)
	if err := system.SudoWriteFile(paths.LNDConf, []byte(content), 0640); err != nil {
		return err
	}
	return system.SudoRun("chown", "root:"+lndUser, paths.LNDConf)
}

// readRequiredLNDRESTOnion enforces the dependency between Tor's
// hidden-service identity and LND's TLS configuration. This node
// requests v3 onions, whose host label is 56 lowercase base32
// characters. Missing, unreadable, or malformed state is never
// converted into an lnd.conf without tlsextradomain.
func readRequiredLNDRESTOnion() (string, error) {
	data, err := os.ReadFile(paths.TorLNDRESTHostname)
	if err != nil {
		return "", fmt.Errorf("read LND REST onion hostname: %w", err)
	}
	onion := strings.TrimSpace(string(data))
	if err := validateLNDRESTOnion(onion); err != nil {
		return "", err
	}
	return onion, nil
}

func validateLNDRESTOnion(onion string) error {
	const suffix = ".onion"
	if len(onion) != 56+len(suffix) ||
		!strings.HasSuffix(onion, suffix) {
		return fmt.Errorf("invalid LND REST onion hostname %q", onion)
	}
	for _, c := range strings.TrimSuffix(onion, suffix) {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return fmt.Errorf("invalid LND REST onion hostname %q", onion)
		}
	}
	return nil
}

// verifyLNDTLSOnionSAN waits for LND's startup-time TLS
// generation/refresh and proves that the certificate it wrote
// contains the exact REST onion DNS SAN configured above.
func verifyLNDTLSOnionSAN() error {
	onion, err := readRequiredLNDRESTOnion()
	if err != nil {
		return err
	}
	var lastErr error
	for i := 0; i < 60; i++ {
		certPEM, readErr := os.ReadFile(paths.LNDTLSCert)
		if readErr == nil {
			lastErr = verifyCertificateDNSName(certPEM, onion)
			if lastErr == nil {
				return nil
			}
		} else {
			lastErr = readErr
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf(
		"LND TLS certificate does not contain REST onion %s: %w",
		onion, lastErr)
}

func verifyCertificateDNSName(certPEM []byte, name string) error {
	foundCertificate := false
	for len(certPEM) > 0 {
		var block *pem.Block
		block, certPEM = pem.Decode(certPEM)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		foundCertificate = true
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse LND TLS certificate: %w", err)
		}
		for _, dnsName := range cert.DNSNames {
			if dnsName == name {
				return nil
			}
		}
	}
	if foundCertificate {
		return fmt.Errorf("DNS SAN %q is missing", name)
	}
	return fmt.Errorf("no certificate PEM block found")
}

// readLNDBitcoindRPCPassword preserves LND's independent RPC
// credential when a later operation rewrites lnd.conf (for
// example a P2P-mode change). Duplicate or empty entries fail
// closed rather than choosing an ambiguous credential.
func readLNDBitcoindRPCPassword() (string, error) {
	data, err := os.ReadFile(paths.LNDConf)
	if err != nil {
		return "", fmt.Errorf("read LND bitcoind RPC credential: %w", err)
	}
	return parseLNDBitcoindRPCPassword(string(data))
}

func parseLNDBitcoindRPCPassword(content string) (string, error) {
	var password string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "bitcoind.rpcpass=") {
			continue
		}
		value := strings.TrimPrefix(line, "bitcoind.rpcpass=")
		if value == "" || password != "" {
			return "", fmt.Errorf(
				"lnd.conf has an empty or duplicate bitcoind.rpcpass")
		}
		password = value
	}
	if password == "" {
		return "", fmt.Errorf("lnd.conf has no bitcoind.rpcpass")
	}
	return password, nil
}

// lndServiceUnit renders the LND systemd unit. withUnlock adds
// LND's --wallet-unlock-password-file flag, pointing at the
// root-staged password file, so the wallet unlocks without an
// operator on every service start. Everything else about the
// two variants is identical by construction — they come from
// this one template. Pure — unit-tested.
func lndServiceUnit(username string, withUnlock bool) string {
	unlockFlag := ""
	if withUnlock {
		unlockFlag = " --wallet-unlock-password-file=" +
			paths.LNDWalletPassword
	}
	return fmt.Sprintf(`[Unit]
Description=LND Lightning Network Daemon
After=bitcoind.service tor.service
Wants=bitcoind.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=debian-tor
UMask=0077
ExecStart=/usr/local/bin/lnd --configfile=/etc/lnd/lnd.conf%s
Restart=on-failure
RestartSec=30
TimeoutStopSec=300
PrivateTmp=true
ProtectSystem=full
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, username, username, unlockFlag)
}

// writeLNDService writes the LND unit in the requested variant.
func writeLNDService(username string, withUnlock bool) error {
	return system.SudoWriteFile(paths.LNDService,
		[]byte(lndServiceUnit(username, withUnlock)), 0644)
}

// writeLNDServiceFromConfig writes the LND unit that matches the
// node's desired state: the unlock variant when the config says
// auto-unlock is enabled AND the wallet password file is actually
// present, the plain variant otherwise. Requiring the file too is deliberate: a
// unit pointing at a missing password file would keep LND from
// starting at all.
func writeLNDServiceFromConfig(cfg *config.AppConfig, username string) error {
	withUnlock := cfg.AutoUnlock && walletPasswordFileExists()
	if cfg.AutoUnlock && !withUnlock {
		logger.Install(
			"auto_unlock is enabled in the config but %s is "+
				"missing — writing the LND unit without the unlock "+
				"flag; re-enable auto-unlock from the node console",
			paths.LNDWalletPassword)
	}
	return writeLNDService(username, withUnlock)
}

func walletPasswordFileExists() bool {
	_, err := os.Stat(paths.LNDWalletPassword)
	return err == nil
}

// startLND enables and starts LND. `systemctl restart` rather
// than `start`, deliberately: start is a no-op on a service that
// is already running during an interrupted-install resume. Restart makes the
// unit and config already written by this lifecycle the ones actually in
// effect; on a fresh pass the two commands are equivalent.
func startLND() error {
	if err := system.SudoRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := system.SudoRun("systemctl", "enable", "lnd"); err != nil {
		return err
	}
	return system.SudoRun("systemctl", "restart", "lnd")
}

func setupAutoUnlock(password string) error {
	// Write the password to a secure temp file, then move it
	// into place with the service user's ownership (this runs
	// as root). os.CreateTemp uses O_EXCL to prevent symlink
	// attacks.
	tmpFile, err := os.CreateTemp("", "vpn-wallet-pw-")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPw := tmpFile.Name()
	if _, err := tmpFile.Write([]byte(password)); err != nil {
		tmpFile.Close()
		os.Remove(tmpPw)
		return err
	}
	tmpFile.Close()
	defer os.Remove(tmpPw)

	passwordFile := paths.LNDWalletPassword
	tmpDest := filepath.Join(filepath.Dir(passwordFile), ".wallet_password.tmp")
	if err := system.SudoRun("install", "-m", "0400",
		"-o", lndUser, "-g", lndUser, tmpPw, tmpDest); err != nil {
		system.SudoRunSilent("rm", "-f", tmpDest)
		logger.System("auto-unlock: install wallet password: %v", err)
		return fmt.Errorf("install wallet password: %w", err)
	}
	if err := system.SudoRun("mv", tmpDest, passwordFile); err != nil {
		system.SudoRunSilent("rm", "-f", tmpDest)
		logger.System("auto-unlock: move wallet password: %v", err)
		return fmt.Errorf("move wallet password: %w", err)
	}

	if err := writeLNDService(lndUser, true); err != nil {
		return err
	}
	if err := system.SudoRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return system.SudoRun("systemctl", "restart", "lnd")
}

// disableAutoUnlock rewrites the LND systemd service back to
// its initial (no auto-unlock) form, restarts LND, and only
// THEN removes the wallet password file. After this returns
// successfully, LND requires manual unlock (e.g. `lncli
// unlock`) on next startup.
//
// The password file is removed LAST, deliberately. The old
// order (remove first) meant a failure partway left the
// security-relevant half already done while the operation
// reported failure and the app still believed auto-unlock was
// enabled — the state on disk, the service, and the config all
// disagreed. With removal last, any failure leaves the file in
// place and the operation honestly failed; a retry converges.
func disableAutoUnlock() error {
	if err := writeLNDService(lndUser, false); err != nil {
		return fmt.Errorf("rewrite service: %w", err)
	}
	if err := system.SudoRun(
		"systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := system.SudoRun(
		"systemctl", "restart", "lnd"); err != nil {
		return fmt.Errorf("restart lnd: %w", err)
	}
	// SudoRunSilent because the file may not exist if called
	// from an inconsistent state — that's fine, we just want
	// it gone.
	system.SudoRunSilent(
		"rm", "-f", paths.LNDWalletPassword)
	return nil
}

func waitForLND() error {
	for i := 0; i < 60; i++ {
		client := buildLNDClient()
		resp, err := client.Get(
			"https://" + paths.LNDRESTEndpoint + "/v1/state")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("LND did not respond after 120 seconds")
}

// ── Exported wrappers for the welcome package ───────────
// These wrap the unexported helpers so the welcome
// package can call them from screens without leaking
// the rest of the installer package.

// WaitForLND blocks until LND's REST API responds, or
// returns an error after 120 seconds. Safe to call as a
// tea.Cmd from a screen.
func WaitForLND() error {
	return waitForLND()
}

// SetupAutoUnlock enables wallet auto-unlock. As root
// (installer, helper) it performs the operation directly; from
// the unprivileged TUI it requests the helper's typed
// stage-wallet-password operation — the password travels over
// the local root-owned socket and is written root-side to a
// file the admin user can never read.
func SetupAutoUnlock(password string) error {
	if os.Geteuid() == 0 {
		return setupAutoUnlock(password)
	}
	return helper.Call(helper.VerbStageWalletPassword,
		helper.StageWalletPasswordParams{Password: password}, nil)
}

// DisableAutoUnlock disables wallet auto-unlock (service
// rewritten and restarted first; password file removed last —
// see disableAutoUnlock). Root performs it directly; the TUI
// requests the helper's typed operation.
func DisableAutoUnlock() error {
	if os.Geteuid() == 0 {
		return disableAutoUnlock()
	}
	return helper.Call(helper.VerbRemoveWalletPassword, nil, nil)
}

// lndTLSCertBytes returns LND's TLS certificate for client
// use: read directly where permitted (root; some setups leave
// it world-readable), else from the staging board copy.
func lndTLSCertBytes() ([]byte, error) {
	if data, err := os.ReadFile(paths.LNDTLSCert); err == nil {
		return data, nil
	}
	return helper.ReadBoard(paths.StateLNDTLSCert)
}

func buildLNDClient() *http.Client {
	tlsConfig := &tls.Config{}
	certData, err := lndTLSCertBytes()
	if err != nil {
		logger.System("LND REST client: %v", err)
	} else {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(certData) {
			tlsConfig.RootCAs = pool
		}
	}
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   5 * time.Second,
	}
}
