// internal/installer/tor_test.go

package installer

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/virtualprivatenode/vpn/internal/config"
)

func mustBuildTorConfig(t *testing.T, cfg *config.AppConfig) string {
	t.Helper()
	content, err := BuildTorConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestTorConfigWithLND(t *testing.T) {
	cfg := config.Default()
	content := mustBuildTorConfig(t, cfg)

	required := []string{
		"SOCKSPort 9050",
		"ControlPort 9051",
		"CookieAuthentication 1",
		"CookieAuthFileGroupReadable 1",
		"bitcoin-p2p",
		"lnd-grpc",
		"lnd-rest",
		"HiddenServicePort 10009",
		"HiddenServicePort 8080",
	}
	for _, req := range required {
		if !strings.Contains(content, req) {
			t.Errorf("missing %q in LND torrc", req)
		}
	}
}

func TestTorConfigWithSyncthing(t *testing.T) {
	cfg := config.Default()
	cfg.SyncthingEnabled = true
	content := mustBuildTorConfig(t, cfg)

	// Web UI still accessible over Tor
	if !strings.Contains(content, "syncthing") {
		t.Error("missing syncthing hidden service")
	}
	if !strings.Contains(content, "HiddenServicePort 8384") {
		t.Error("missing Syncthing web UI port")
	}

	// Sync protocol goes over clearnet — no hidden service
	if strings.Contains(content, "syncthing-sync") {
		t.Error("should not have syncthing-sync hidden service")
	}
	if strings.Contains(content, "HiddenServicePort 22000") {
		t.Error("should not have port 22000 hidden service")
	}
}

func TestTorConfigNoSyncthingWithoutInstall(t *testing.T) {
	cfg := config.Default()
	content := mustBuildTorConfig(t, cfg)

	if strings.Contains(content, "syncthing") {
		t.Error("should not have syncthing without install")
	}
}

func TestTorConfigFullStack(t *testing.T) {
	cfg := &config.AppConfig{
		Network:          "mainnet",
		SyncthingEnabled: true,
	}
	content := mustBuildTorConfig(t, cfg)

	required := []string{
		"SOCKSPort 9050",
		"ControlPort 9051",
		"bitcoin-p2p",
		"lnd-grpc",
		"lnd-rest",
		"syncthing",
		"HiddenServicePort 8384",
	}
	for _, req := range required {
		if !strings.Contains(content, req) {
			t.Errorf("full stack torrc missing %q", req)
		}
	}

	// Sync protocol over clearnet, not Tor
	if strings.Contains(content, "syncthing-sync") {
		t.Error("full stack should not have syncthing-sync hidden service")
	}
}

func TestTorConfigMainnetPorts(t *testing.T) {
	cfg := config.Default()
	content := mustBuildTorConfig(t, cfg)

	if !strings.Contains(content, "HiddenServicePort 8333") {
		t.Error("mainnet torrc should use port 8333 for P2P")
	}
	if strings.Contains(content, "HiddenServicePort 8332") {
		t.Error("mainnet torrc should not have RPC hidden service")
	}
}

func TestTorConfigTestnet4Ports(t *testing.T) {
	cfg := &config.AppConfig{Network: "testnet4"}
	content := mustBuildTorConfig(t, cfg)

	if !strings.Contains(content, "HiddenServicePort 48333") {
		t.Error("testnet4 torrc should use port 48333 for P2P")
	}
	if strings.Contains(content, "HiddenServicePort 48332") {
		t.Error("testnet4 torrc should not have RPC hidden service")
	}
}

func TestTorConfigPublicSignetPorts(t *testing.T) {
	cfg := config.Default()
	cfg.Network = config.NetworkPublicSignet
	content := mustBuildTorConfig(t, cfg)

	if !strings.Contains(content, "HiddenServicePort 38333 127.0.0.1:38333") {
		t.Error("public-signet torrc should use port 38333 for P2P")
	}
	if strings.Contains(content, "HiddenServicePort 38332") {
		t.Error("public-signet torrc should not have an RPC hidden service")
	}
}

func TestTorConfigRejectsUnknownProfile(t *testing.T) {
	cfg := config.Default()
	cfg.Network = "signet"
	if _, err := BuildTorConfig(cfg); err == nil {
		t.Fatal("raw signet profile generated a Tor config")
	}
}

func TestTorConfigControlPortAlways(t *testing.T) {
	// The install-path routing gate (torgate.go) reads bootstrap
	// progress from the control port unconditionally, so every
	// generated torrc must include it — LND or not.
	cfg := config.Default()
	content := mustBuildTorConfig(t, cfg)

	if !strings.Contains(content, "ControlPort 9051") {
		t.Error("ControlPort must be present in every config (install gate depends on it)")
	}
	if !strings.Contains(content, "CookieAuthentication 1") {
		t.Error("control port must require cookie auth")
	}
}

func withTorAddonTestDeps(t *testing.T) {
	t.Helper()
	oldPresent := torBinaryPresentForAddon
	oldEnabled := torServiceEnabledForAddon
	oldActive := torServiceActiveForAddon
	oldReadConfig := readTorConfigForAddon
	oldReadOnion := readSyncthingOnionForAddon
	oldSleep := sleepForTorAddon
	oldRun := runTorServiceAction
	t.Cleanup(func() {
		torBinaryPresentForAddon = oldPresent
		torServiceEnabledForAddon = oldEnabled
		torServiceActiveForAddon = oldActive
		readTorConfigForAddon = oldReadConfig
		readSyncthingOnionForAddon = oldReadOnion
		sleepForTorAddon = oldSleep
		runTorServiceAction = oldRun
	})
}

func TestSyncthingTorPrerequisiteRequiresExpectedBaseState(t *testing.T) {
	withTorAddonTestDeps(t)
	cfg := config.Default()
	torBinaryPresentForAddon = func() bool { return true }
	torServiceEnabledForAddon = func() bool { return true }
	torServiceActiveForAddon = func() bool { return true }
	readTorConfigForAddon = func(string) ([]byte, error) {
		return []byte(mustBuildTorConfig(t, cfg)), nil
	}
	if err := verifySyncthingTorPrerequisite(cfg); err != nil {
		t.Fatal(err)
	}

	torServiceEnabledForAddon = func() bool { return false }
	if err := verifySyncthingTorPrerequisite(cfg); err == nil {
		t.Fatal("disabled Tor accepted as Syncthing prerequisite")
	}
	torServiceEnabledForAddon = func() bool { return true }
	torServiceActiveForAddon = func() bool { return false }
	if err := verifySyncthingTorPrerequisite(cfg); err == nil {
		t.Fatal("inactive Tor accepted as Syncthing prerequisite")
	}
	torServiceActiveForAddon = func() bool { return true }
	readTorConfigForAddon = func(string) ([]byte, error) {
		return []byte("foreign configuration\n"), nil
	}
	if err := verifySyncthingTorPrerequisite(cfg); err == nil {
		t.Fatal("unexpected base torrc accepted")
	}
}

func TestSyncthingTorRestartNeverEnablesService(t *testing.T) {
	withTorAddonTestDeps(t)
	var actions []string
	runTorServiceAction = func(action string) error {
		actions = append(actions, action)
		return nil
	}
	torServiceActiveForAddon = func() bool { return true }
	readSyncthingOnionForAddon = func(string) ([]byte, error) {
		return []byte(strings.Repeat("a", 56) + ".onion\n"), nil
	}
	sleepForTorAddon = func(time.Duration) {}
	if err := restartTorForSyncthing(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"restart"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("Tor actions=%v want=%v", actions, want)
	}
}

func TestInitialTorOperationEnablesThenRestarts(t *testing.T) {
	withTorAddonTestDeps(t)
	var actions []string
	runTorServiceAction = func(action string) error {
		actions = append(actions, action)
		return nil
	}
	if err := enableAndRestartTor(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"enable", "restart"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("Tor actions=%v want=%v", actions, want)
	}
}

func TestWaitForSyncthingOnionRejectsMalformedHostname(t *testing.T) {
	withTorAddonTestDeps(t)
	readSyncthingOnionForAddon = func(string) ([]byte, error) {
		return []byte("short.onion\n"), nil
	}
	// End the fixed retry loop without wall-clock delay.
	sleepForTorAddon = func(time.Duration) {}
	if err := waitForSyncthingOnion(); err == nil {
		t.Fatal("malformed Syncthing onion accepted")
	}
	readSyncthingOnionForAddon = func(string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	if err := waitForSyncthingOnion(); err == nil {
		t.Fatal("missing Syncthing onion accepted")
	}
}
