// internal/paths/paths.go

// Package paths centralizes all filesystem paths used by vpn.
// Every hardcoded path in the project should be defined here.
package paths

import "fmt"

// ── Configuration ────────────────────────────────────────

const (
	ConfigDir  = "/etc/vpn"
	ConfigFile = "/etc/vpn/config.json"

	// InstallStateFile is the per-step install ledger: which
	// install steps have completed, keyed by stable step key
	// (installer/ledger.go). A SEPARATE file from config.json
	// so a config load failure cannot erase install history,
	// and so its ownership flips to root with root-dispatched
	// install without touching config's story.
	InstallStateFile = "/etc/vpn/install-state.json"

	// PasswordPendingMarker exists while an unattended install
	// has applied a generated admin password that was never
	// displayed (the identity step applies early; the print
	// happens only at the end of a completed run — a failure in
	// between would otherwise strand a credential nobody has
	// seen). Written by the identity step on the unattended
	// path; cleared when the password is finally printed, or
	// when the operator sets a password of their own from the
	// node console. Holds no secret — its presence is the fact.
	PasswordPendingMarker = "/etc/vpn/password-pending"

	BitcoinConf = "/etc/bitcoin/bitcoin.conf"
	BitcoinDir  = "/etc/bitcoin"

	// StateDir is the staging board: root-written files that
	// carry privileged facts (staged credentials) to the
	// unprivileged admin user. The directory is root:vpn
	// 0750; each file root:vpn 0640. Root (the installer and
	// the helper) writes; the admin user reads. Every file is
	// re-written by whatever operation changes the fact it
	// carries — a reader that finds a file missing or
	// unreadable reports the feature unavailable and logs
	// why, never guesses.
	//
	// Only facts consumed at MACHINE cadence (per RPC call,
	// per dial) live here — credentials, which fail closed at
	// the moment of use when stale, so a stale copy cannot
	// hide. Display facts consumed at HUMAN cadence (onion
	// addresses, the Syncthing device ID, the SSH
	// password-auth answer) have no copy at all: a stale copy
	// of those would render confidently wrong with no failure
	// to catch it, so the TUI reads them live through the
	// helper's read-only verbs instead. Every board file
	// declares how it stays fresh in the helper's freshness
	// table (internal/helperd/matrix.go), and a unit test
	// fails on any file without a declaration.
	//
	// The board lives under /var/lib/vpn — NOT under /etc/vpn
	// — deliberately: /etc/vpn is owned by the admin user (the
	// TUI writes config.json there), and a directory owner can
	// replace a subdirectory with a symlink. Root-side board
	// writes under an admin-owned parent would hand a
	// compromised admin account a "make root chown an
	// arbitrary directory" primitive. Every ancestor of the
	// board is root-owned, so that class cannot arise.
	VarLibVPN = "/var/lib/vpn"
	StateDir  = VarLibVPN + "/state"

	// ServiceLayoutMarker records that this installation began
	// with the dedicated bitcoin/lnd service-identity layout.
	// It lives below a root-owned /var/lib parent, not the
	// admin-owned /etc/vpn directory, so vpn cannot forge or
	// replace the migration authorization marker.
	ServiceLayoutMarker = VarLibVPN + "/service-layout-v1"

	// Staging board files. One fact per file.
	StateBitcoindRPCPass = StateDir + "/bitcoind-rpc.pass"
	StateLNDTLSCert      = StateDir + "/lnd-tls.cert"
	StateLNDMacaroon     = StateDir + "/lnd-admin.macaroon"
	StateSyncthingAPIKey = StateDir + "/syncthing-api-key"

	LNDConf = "/etc/lnd/lnd.conf"
	LNDDir  = "/etc/lnd"

	SyncthingDir = "/etc/syncthing"
)

// ── Loopback endpoints ───────────────────────────────────

// Each loopback endpoint is defined exactly once, and BOTH
// ends of every connection use the same constant: lnd.conf
// binds LND to these values, and every client dials them —
// so the two ends cannot silently disagree.
//
// The values are literal IPv4 addresses, never the name
// localhost. The installer disables IPv6 at the kernel, but
// Debian's /etc/hosts still maps localhost to ::1, and that
// file is not ours to correct (cloud provider tooling may
// regenerate it). Reaching loopback by name can therefore
// resolve to an IPv6 address the box cannot use; on a node
// that disables IPv6, loopback is always dialed by address.
const (
	// LNDGRPCEndpoint is LND's gRPC server. Dialed by the
	// console's gRPC client, the wallet-creation lncli
	// invocation, and the shell's lncli wrapper.
	LNDGRPCEndpoint = "127.0.0.1:10009"

	// LNDRESTEndpoint is LND's REST server. Dialed by the
	// installer's readiness probe; the Tor hidden service
	// forwards the REST onion here.
	LNDRESTEndpoint = "127.0.0.1:8080"

	// LNDP2PBind is LND's peer listener in tor-only mode.
	// Hybrid mode binds all interfaces instead, computed
	// where the config is written.
	LNDP2PBind = "127.0.0.1:9735"
)

// ── Data ─────────────────────────────────────────────────

const (
	BitcoinDataDir   = "/var/lib/bitcoin"
	LNDDataDir       = "/var/lib/lnd"
	SyncthingDataDir = "/var/lib/syncthing"

	// ExportDir is the root-controlled boundary for artifacts that
	// one service deliberately publishes to another. It is separate
	// from every daemon's private state.
	ExportDir = VarLibVPN + "/exports"

	// ExportReadyMarkerName is the installer-owned marker convention
	// for project exports served from read-only Syncthing folders.
	// Each export places its own marker inside its folder root.
	ExportReadyMarkerName = ".vpn-export-ready"

	// LNDBackupStage is private publisher staging. Syncthing cannot
	// enter it. LNDBackupExport is the send-only folder registered
	// with Syncthing. Both live beneath ExportDir so publication can
	// use a same-filesystem atomic rename without traversing
	// Syncthing's private state.
	LNDBackupStage        = ExportDir + "/lnd-backup-stage"
	LNDBackupExport       = ExportDir + "/lnd-backup"
	LNDBackupExportMarker = LNDBackupExport + "/" + ExportReadyMarkerName
)

// ── LND files ────────────────────────────────────────────

const (
	LNDTLSCert        = "/var/lib/lnd/tls.cert"
	LNDTLSKey         = "/var/lib/lnd/tls.key"
	LNDWalletPassword = "/var/lib/lnd/wallet_password"
)

// LNDMacaroon returns the path to the admin macaroon for a given network.
func LNDMacaroon(network string) string {
	return fmt.Sprintf("/var/lib/lnd/data/chain/bitcoin/%s/admin.macaroon", network)
}

// ChannelBackup returns the path to the channel backup for a given network.
func ChannelBackup(network string) string {
	return fmt.Sprintf("/var/lib/lnd/data/chain/bitcoin/%s/channel.backup", network)
}

// ── Tor ──────────────────────────────────────────────────

const (
	Torrc                = "/etc/tor/torrc"
	TorBitcoinP2P        = "/var/lib/tor/bitcoin-p2p"
	TorLNDGRPC           = "/var/lib/tor/lnd-grpc"
	TorLNDREST           = "/var/lib/tor/lnd-rest"
	TorLNDRESTHostname   = "/var/lib/tor/lnd-rest/hostname"
	TorSyncthing         = "/var/lib/tor/syncthing"
	TorSyncthingHostname = "/var/lib/tor/syncthing/hostname"
	TorSyncthingSync     = "/var/lib/tor/syncthing-sync"
)

// ── Systemd ──────────────────────────────────────────────

const (
	BitcoindService     = "/etc/systemd/system/bitcoind.service"
	LNDService          = "/etc/systemd/system/lnd.service"
	SyncthingService    = "/etc/systemd/system/syncthing.service"
	BackupWatchPath     = "/etc/systemd/system/lnd-backup-watch.path"
	BackupExportService = "/etc/systemd/system/lnd-backup-export.service"

	// The LND TLS certificate watch. LND rewrites tls.cert on
	// its own (tlsautorefresh in lnd.conf regenerates it at
	// any startup whose parameters changed or whose cert
	// nears expiry) — no TUI-requested operation is
	// involved, so no operation can re-stage the TUI's
	// copy. This path unit closes that gap at the source: a
	// rewrite of the certificate triggers the stage service,
	// which refreshes the staged copy within seconds.
	LNDCertWatchPath        = "/etc/systemd/system/vpn-lnd-cert-watch.path"
	LNDCertStageService     = "/etc/systemd/system/vpn-lnd-cert-stage.service"
	LNDCertWatchPathName    = "vpn-lnd-cert-watch.path"
	LNDCertStageServiceName = "vpn-lnd-cert-stage.service"

	// The root helper's socket-activated units. The socket
	// node's ownership and mode (root:vpn 0660, created by
	// systemd before the helper ever runs) ARE the
	// authentication for privileged operations; the service
	// is started by traffic and exits when idle.
	HelperSocket         = "/run/vpn-helperd.sock"
	HelperSocketUnit     = "/etc/systemd/system/vpn-helperd.socket"
	HelperServiceUnit    = "/etc/systemd/system/vpn-helperd.service"
	HelperSocketUnitName = "vpn-helperd.socket"
)

// ── Logs ─────────────────────────────────────────────────

const (
	LogFile = "/var/log/vpn.log"
)

// ── System ───────────────────────────────────────────────

const (
	OSRelease   = "/etc/os-release"
	SudoersFile = "/etc/sudoers"
	SudoersDir  = "/etc/sudoers.d"

	SyncthingConfigXML = "/etc/syncthing/config.xml"
	UFWDefault         = "/etc/default/ufw"
	SSHDConfig         = "/etc/ssh/sshd_config"
	// SSHDDropIn uses a 00- prefix so it is parsed before
	// other drop-ins (notably 50-cloud-init.conf which
	// declares PasswordAuthentication yes on cloud
	// images). sshd's first-match-wins semantics mean
	// loading first = winning.
	SSHDDropIn = "/etc/ssh/sshd_config.d/00-vpn-hardening.conf"

	// OldSSHDDropIn is the drop-in filename from before the
	// rlvpn → vpn rename. On a migrated box the stale file
	// would sort BEFORE SSHDDropIn (r < v) and win every
	// contested directive under first-match-wins, so the
	// install SSH step deletes it — the ONLY old-name
	// artifact the installer removes (ruling xv: everything
	// else old survives until the operator's verified
	// teardown). Ordering is binding: observe → write new →
	// delete old → validate → restart, because a
	// TUI-disabled PasswordAuthentication lives in THIS
	// file until the observed value is carried into the
	// new one.
	OldSSHDDropIn = "/etc/ssh/sshd_config.d/00-rlvpn-hardening.conf"

	Fail2banJail       = "/etc/fail2ban/jail.local"
	AutoUpgrades       = "/etc/apt/apt.conf.d/20auto-upgrades"
	UnattendedUpgrades = "/etc/apt/apt.conf.d/50unattended-upgrades"
	DisableIPv6Conf    = "/etc/sysctl.d/99-disable-ipv6.conf"
)

// ── User ─────────────────────────────────────────────────

const (
	// AdminUser is the node's admin login — same name as the
	// binary, one name to know (ruling vi: clean break from
	// the old ripsline user; migrated boxes retire the old
	// user via MIGRATION.md's operator-run teardown, never
	// via this binary).
	AdminUser          = "vpn"
	AdminHome          = "/home/" + AdminUser
	AdminBashrc        = AdminHome + "/.bashrc"
	AdminBashProfile   = AdminHome + "/.bash_profile"
	AuthorizedKeysFile = AdminHome + "/.ssh/authorized_keys"

	// AdminSudoers is where older builds granted the admin
	// user NOPASSWD sudo. The install now DELETES this file
	// and writes no replacement: the admin user has no sudo
	// rights at all. Privileged operations go through the
	// root helper's socket instead (vpn helperd), which
	// serves a fixed menu of typed operations — not a shell.
	AdminSudoers = "/etc/sudoers.d/" + AdminUser

	// BinaryPath is where the installer places the running
	// binary (and where self-update installs new ones).
	BinaryPath = "/usr/local/bin/vpn"
)

// ── Cache ────────────────────────────────────────────────

const (
	VersionCacheDir  = AdminHome + "/.cache/vpn"
	VersionCacheFile = VersionCacheDir + "/latest-version"
)
