package installer

import (
	"errors"
	"reflect"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/config"
)

func withFirewallTestDeps(t *testing.T) {
	t.Helper()
	oldObserve := observeSSHForFirewall
	oldInstall := installUFWForFirewall
	oldRead := readUFWDefaultForFirewall
	oldWrite := writeUFWDefaultForFirewall
	oldStatus := readUFWStatusForFeature
	oldRun := runFirewallCommand
	t.Cleanup(func() {
		observeSSHForFirewall = oldObserve
		installUFWForFirewall = oldInstall
		readUFWDefaultForFirewall = oldRead
		writeUFWDefaultForFirewall = oldWrite
		readUFWStatusForFeature = oldStatus
		runFirewallCommand = oldRun
	})
}

func TestConfigureFirewallRefusesBeforeMutationWhenSSHObservationFails(t *testing.T) {
	withFirewallTestDeps(t)
	observeSSHForFirewall = func() (SSHObservation, error) {
		return SSHObservation{}, errors.New("sshd unavailable")
	}
	mutated := false
	installUFWForFirewall = func() error { mutated = true; return nil }
	readUFWDefaultForFirewall = func() (string, error) { mutated = true; return "", nil }
	writeUFWDefaultForFirewall = func([]byte) error { mutated = true; return nil }
	runFirewallCommand = func([]string) error { mutated = true; return nil }
	if err := configureInitialFirewall(config.Default()); err == nil {
		t.Fatal("firewall rewrite accepted unavailable SSH observation")
	}
	if mutated {
		t.Fatal("firewall path mutated before SSH observation succeeded")
	}
}

func TestConfigureFirewallUsesFreshObservedPorts(t *testing.T) {
	withFirewallTestDeps(t)
	observeSSHForFirewall = func() (SSHObservation, error) {
		return SSHObservation{Ports: []int{2222, 22022}, PasswordAuth: true}, nil
	}
	installUFWForFirewall = func() error { return nil }
	readUFWDefaultForFirewall = func() (string, error) { return "IPV6=yes\n", nil }
	writeUFWDefaultForFirewall = func([]byte) error { return nil }
	var got [][]string
	runFirewallCommand = func(args []string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}
	cfg := config.Default()
	cfg.P2PMode = "hybrid"
	cfg.SyncthingEnabled = true
	if err := configureInitialFirewall(cfg); err != nil {
		t.Fatal(err)
	}
	want := buildInitialFirewallCommands(cfg, []int{2222, 22022})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n got: %#v\nwant: %#v", got, want)
	}
	for _, command := range got {
		if reflect.DeepEqual(command, []string{"ufw", "allow", "22/tcp"}) {
			t.Fatal("fell back to port 22 instead of observed ports")
		}
	}
}

func TestConfigureFirewallReobservesAfterPackagePreparation(t *testing.T) {
	withFirewallTestDeps(t)
	calls := 0
	observeSSHForFirewall = func() (SSHObservation, error) {
		calls++
		if calls == 1 {
			return SSHObservation{Ports: []int{22}}, nil
		}
		return SSHObservation{}, errors.New("sshd changed during preparation")
	}
	installUFWForFirewall = func() error { return nil }
	rewritten := false
	readUFWDefaultForFirewall = func() (string, error) { rewritten = true; return "", nil }
	writeUFWDefaultForFirewall = func([]byte) error { rewritten = true; return nil }
	runFirewallCommand = func([]string) error { rewritten = true; return nil }
	if err := configureInitialFirewall(config.Default()); err == nil {
		t.Fatal("rewrite accepted failed final SSH observation")
	}
	if calls != 2 || rewritten {
		t.Fatalf("calls=%d rewritten=%v", calls, rewritten)
	}
}

func TestFeatureFirewallRefusesInactiveBeforeMutation(t *testing.T) {
	withFirewallTestDeps(t)
	readUFWStatusForFeature = func() (string, error) {
		return "Status: inactive\n", nil
	}
	mutated := false
	runFirewallCommand = func([]string) error {
		mutated = true
		return nil
	}
	if err := allowHybridP2PFirewallRules(); err == nil {
		t.Fatal("P2P rules accepted inactive UFW")
	}
	if mutated {
		t.Fatal("P2P rule path mutated inactive UFW")
	}
}

func TestHybridP2PFirewallAddsAndVerifiesOnlyOwnedRules(t *testing.T) {
	withFirewallTestDeps(t)
	statusCalls := 0
	readUFWStatusForFeature = func() (string, error) {
		statusCalls++
		if statusCalls == 1 {
			return "Status: active\n", nil
		}
		return "Status: active\n\n" +
			"9735/tcp                  ALLOW       Anywhere\n" +
			"8080/tcp                  ALLOW       Anywhere\n" +
			"22000/tcp                 ALLOW       Anywhere\n", nil
	}
	var got [][]string
	runFirewallCommand = func(args []string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}
	if err := allowHybridP2PFirewallRules(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ufw", "allow", "9735/tcp"},
		{"ufw", "allow", "8080/tcp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSyncthingFirewallAddsAndVerifiesOnlyOwnedRule(t *testing.T) {
	withFirewallTestDeps(t)
	statusCalls := 0
	readUFWStatusForFeature = func() (string, error) {
		statusCalls++
		if statusCalls == 1 {
			return "Status: active\n", nil
		}
		return "Status: active\n\n" +
			"22000/tcp                 ALLOW       Anywhere\n", nil
	}
	var got [][]string
	runFirewallCommand = func(args []string) error {
		got = append(got, append([]string(nil), args...))
		return nil
	}
	if err := allowSyncthingFirewallRule(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ufw", "allow", "22000/tcp"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFeatureFirewallFailsWhenLiveVerificationMissesRule(t *testing.T) {
	withFirewallTestDeps(t)
	readUFWStatusForFeature = func() (string, error) {
		return "Status: active\n", nil
	}
	runFirewallCommand = func([]string) error { return nil }
	if err := allowSyncthingFirewallRule(); err == nil {
		t.Fatal("Syncthing rule reported success without live verification")
	}
}
