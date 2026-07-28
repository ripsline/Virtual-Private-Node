// internal/installer/lnd_test.go

package installer

import (
	"strings"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/paths"
)

// The generated lnd.conf binds by the literal loopback
// addresses defined once in paths — the same constants every
// client dials — and carries no host name anywhere. On this
// node, which disables IPv6, the name localhost can resolve to
// an unusable IPv6 address, so it must never appear.
func TestBuildLNDConfigBindsByAddress(t *testing.T) {
	// Tor-only: every listener on the loopback constants.
	cfg := config.Default()
	torOnly := BuildLNDConfig(cfg, "", "")
	for _, want := range []string{
		"listen=" + paths.LNDP2PBind,
		"restlisten=" + paths.LNDRESTEndpoint,
		"rpclisten=" + paths.LNDGRPCEndpoint,
	} {
		if !strings.Contains(torOnly, want) {
			t.Errorf("tor-only config missing %q", want)
		}
	}

	// Hybrid: P2P and REST bind all interfaces as computed;
	// gRPC stays on the loopback constant.
	hybrid := config.Default()
	hybrid.P2PMode = "hybrid"
	hybridConf := BuildLNDConfig(hybrid, "203.0.113.7", "")
	for _, want := range []string{
		"listen=0.0.0.0:9735",
		"restlisten=0.0.0.0:8080",
		"rpclisten=" + paths.LNDGRPCEndpoint,
		"externalhosts=203.0.113.7:9735",
		"tlsextraip=203.0.113.7",
	} {
		if !strings.Contains(hybridConf, want) {
			t.Errorf("hybrid config missing %q", want)
		}
	}

	// No host name in either variant, with or without a REST
	// onion for tlsextradomain.
	withOnion := BuildLNDConfig(cfg, "",
		"exampleonionaddress.onion")
	for name, conf := range map[string]string{
		"tor-only": torOnly,
		"hybrid":   hybridConf,
		"onion":    withOnion,
	} {
		if strings.Contains(conf, "localhost") {
			t.Errorf("%s config contains the name localhost", name)
		}
	}
	if !strings.Contains(withOnion,
		"tlsextradomain=exampleonionaddress.onion") {
		t.Error("onion config missing tlsextradomain")
	}
}

// The LND unit template: one source for both variants, so they
// can never drift apart in anything but the unlock flag.
func TestLNDServiceUnit(t *testing.T) {
	plain := lndServiceUnit("bitcoin", false)
	unlock := lndServiceUnit("bitcoin", true)

	unlockFlag := "--wallet-unlock-password-file=" +
		paths.LNDWalletPassword
	if strings.Contains(plain, unlockFlag) {
		t.Error("plain unit carries the unlock flag")
	}
	if got := strings.Count(unlock, unlockFlag); got != 1 {
		t.Errorf("unlock unit has %d unlock flags, want 1", got)
	}

	// The only difference between the variants is the flag.
	if strings.Replace(unlock, " "+unlockFlag, "", 1) != plain {
		t.Error("variants differ beyond the unlock flag")
	}

	for _, unit := range []string{plain, unlock} {
		for _, want := range []string{
			"User=bitcoin",
			"Group=bitcoin",
			"ExecStart=/usr/local/bin/lnd " +
				"--configfile=/etc/lnd/lnd.conf",
			"Restart=on-failure",
			"TimeoutStopSec=300",
			"WantedBy=multi-user.target",
		} {
			if !strings.Contains(unit, want) {
				t.Errorf("unit lacks %q", want)
			}
		}
	}

	// The unlock flag rides the ExecStart line, not a line of
	// its own — systemd would ignore a bare flag line.
	for _, line := range strings.Split(unlock, "\n") {
		if strings.Contains(line, unlockFlag) &&
			!strings.HasPrefix(line, "ExecStart=") {
			t.Errorf("unlock flag off the ExecStart line: %q", line)
		}
	}
}
