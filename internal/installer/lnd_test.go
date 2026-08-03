// internal/installer/lnd_test.go

package installer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

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
	torOnly := BuildLNDConfig(cfg, "", "", "lnd", "secret")
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
	hybridConf := BuildLNDConfig(
		hybrid, "203.0.113.7", "", "lnd", "secret")
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
		"exampleonionaddress.onion", "lnd", "secret")
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

func TestValidateLNDRESTOnion(t *testing.T) {
	onion := strings.Repeat("a", 56) + ".onion"
	if err := validateLNDRESTOnion(onion); err != nil {
		t.Fatalf("valid v3 onion rejected: %v", err)
	}
	for _, invalid := range []string{
		"", "short.onion", strings.Repeat("A", 56) + ".onion",
		strings.Repeat("0", 56) + ".onion", strings.Repeat("a", 56),
	} {
		if err := validateLNDRESTOnion(invalid); err == nil {
			t.Errorf("invalid onion accepted: %q", invalid)
		}
	}
}

func TestVerifyCertificateDNSName(t *testing.T) {
	onion := strings.Repeat("b", 56) + ".onion"
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "LND"},
		DNSNames:     []string{onion},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(
		rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: der,
	})
	if err := verifyCertificateDNSName(certPEM, onion); err != nil {
		t.Fatalf("certificate with onion SAN rejected: %v", err)
	}
	if err := verifyCertificateDNSName(
		certPEM, strings.Repeat("c", 56)+".onion"); err == nil {
		t.Error("certificate missing the requested onion SAN was accepted")
	}
	if err := verifyCertificateDNSName([]byte("not pem"), onion); err == nil {
		t.Error("malformed certificate was accepted")
	}
}

func TestBuildLNDConfigUsesIndependentBitcoindRPCIdentity(t *testing.T) {
	content := BuildLNDConfig(
		config.Default(), "", "", "lnd", "secret")
	for _, want := range []string{
		"bitcoind.rpcuser=lnd",
		"bitcoind.rpcpass=secret",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("LND config missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"bitcoind.rpccookie=",
		"bitcoind.dir=",
		"bitcoind.config=",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("LND config still carries %q", forbidden)
		}
	}
}

func TestParseLNDBitcoindRPCPassword(t *testing.T) {
	password, err := parseLNDBitcoindRPCPassword(
		"[Bitcoind]\nbitcoind.rpcuser=lnd\n" +
			"bitcoind.rpcpass=secret\n")
	if err != nil || password != "secret" {
		t.Fatalf("parse password: got %q, %v", password, err)
	}
	for name, content := range map[string]string{
		"missing": "bitcoind.rpcuser=lnd\n",
		"empty":   "bitcoind.rpcpass=\n",
		"duplicate": "bitcoind.rpcpass=one\n" +
			"bitcoind.rpcpass=two\n",
	} {
		if _, err := parseLNDBitcoindRPCPassword(content); err == nil {
			t.Errorf("%s password accepted", name)
		}
	}
}

// The LND unit template: one source for both variants, so they
// can never drift apart in anything but the unlock flag.
func TestLNDServiceUnit(t *testing.T) {
	plain := lndServiceUnit("lnd", false)
	unlock := lndServiceUnit("lnd", true)

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
			"User=lnd",
			"Group=lnd",
			"SupplementaryGroups=debian-tor",
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
		if strings.Contains(unit, backupGroup) {
			t.Error("normal lnd unit has channel-backup export access")
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
