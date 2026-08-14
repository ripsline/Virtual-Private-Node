package installer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/paths"
)

type autoUnlockFixture struct {
	cfg        config.AppConfig
	art        autoUnlockArtifacts
	loaded     autoUnlockUnit
	restart    string
	loadedDrop bool

	invocation string
	pid        int
	args       []string
	wallet     lnrpc.WalletState
	password   string

	now          time.Time
	restartCount int
	getInfoCount int
	holdEnabled  bool

	calls  map[string]int
	failOn map[string]int
	after  map[string]bool
	always map[string]bool
}

func newAutoUnlockFixture(enabled bool) *autoUnlockFixture {
	f := &autoUnlockFixture{
		now:        time.Unix(1_700_000_000, 0),
		pid:        100,
		invocation: "invocation-1",
		calls:      map[string]int{},
		failOn:     map[string]int{},
		after:      map[string]bool{},
		always:     map[string]bool{},
	}
	f.cfg = *config.Default()
	if enabled {
		f.cfg.AutoUnlock = true
		f.art = autoUnlockArtifacts{unit: autoUnlockUnitEnabled, password: true}
		f.loaded = autoUnlockUnitEnabled
		f.args = expectedLNDArgs(true)
		f.wallet = lnrpc.WalletState_SERVER_ACTIVE
		f.password = "correct"
	} else {
		f.art = autoUnlockArtifacts{unit: autoUnlockUnitPlain}
		f.loaded = autoUnlockUnitPlain
		f.args = expectedLNDArgs(false)
		f.wallet = lnrpc.WalletState_SERVER_ACTIVE
	}
	f.restart = "on-failure"
	return f
}

func (f *autoUnlockFixture) fail(name string) error {
	f.calls[name]++
	if f.always[name] {
		return fmt.Errorf("injected persistent %s failure", name)
	}
	if f.failOn[name] == f.calls[name] {
		return fmt.Errorf("injected %s failure", name)
	}
	return nil
}

func (f *autoUnlockFixture) ops() autoUnlockOps {
	return autoUnlockOps{
		loadConfig: func() (*config.AppConfig, error) {
			if err := f.fail("load-config"); err != nil {
				return nil, err
			}
			cfg := f.cfg
			return &cfg, nil
		},
		saveConfig: func(cfg *config.AppConfig) error {
			name := "save-disabled"
			if cfg.AutoUnlock {
				name = "save-enabled"
			}
			if f.after[name] {
				f.cfg = *cfg
				return fmt.Errorf("injected %s post-publication failure", name)
			}
			if err := f.fail(name); err != nil {
				return err
			}
			f.cfg = *cfg
			return nil
		},
		artifacts: func() (autoUnlockArtifacts, error) {
			if err := f.fail("inspect"); err != nil {
				return autoUnlockArtifacts{}, err
			}
			return f.art, nil
		},
		writeUnit: func(unit autoUnlockUnit) error {
			name := "write-unit-plain"
			if unit == autoUnlockUnitEnabled {
				name = "write-unit-enabled"
			}
			if err := f.fail(name); err != nil {
				return err
			}
			f.art.unit = unit
			return nil
		},
		writePassword: func(password string) error {
			if err := f.fail("write-password"); err != nil {
				return err
			}
			f.password = password
			f.art.password = true
			f.art.passwordStage = false
			return nil
		},
		removePassword: func() error {
			if f.after["remove-password"] {
				f.after["remove-password"] = false
				f.art.password = false
				f.art.passwordStage = false
				f.password = ""
				return errors.New("injected remove-password post-unlink failure")
			}
			if err := f.fail("remove-password"); err != nil {
				return err
			}
			f.art.password = false
			f.art.passwordStage = false
			f.password = ""
			return nil
		},
		writeVerifyDrop: func() error {
			if err := f.fail("write-drop"); err != nil {
				return err
			}
			f.art.verifyDrop = true
			return nil
		},
		removeVerifyDrop: func() error {
			if err := f.fail("remove-drop"); err != nil {
				return err
			}
			f.art.verifyDrop = false
			return nil
		},
		validateUnit: func() error { return f.fail("validate") },
		daemonReload: func() error {
			if err := f.fail("daemon-reload"); err != nil {
				return err
			}
			f.loaded = f.art.unit
			f.loadedDrop = f.art.verifyDrop
			if f.loadedDrop {
				f.restart = "no"
			} else {
				f.restart = "on-failure"
			}
			return nil
		},
		restartLND: func() error {
			f.restartCount++
			f.invocation = fmt.Sprintf("invocation-%d", f.restartCount+1)
			if err := f.fail("restart-lnd"); err != nil {
				return err
			}
			if f.loaded == autoUnlockUnitEnabled && f.password != "correct" {
				f.pid = 0
				f.args = nil
				f.wallet = lnrpc.WalletState_WAITING_TO_START
				return nil
			}
			f.pid = 100 + f.restartCount
			f.args = expectedLNDArgs(f.loaded == autoUnlockUnitEnabled)
			if f.loaded == autoUnlockUnitEnabled {
				if f.holdEnabled {
					f.wallet = lnrpc.WalletState_UNLOCKED
				} else {
					f.wallet = lnrpc.WalletState_SERVER_ACTIVE
				}
			} else {
				f.wallet = lnrpc.WalletState_LOCKED
			}
			return nil
		},
		unitStatus: func() (lndUnitStatus, error) {
			if err := f.fail("unit-status"); err != nil {
				return lndUnitStatus{}, err
			}
			active := "active"
			sub := "running"
			if f.pid == 0 {
				active = "failed"
				sub = "failed"
			}
			drops := []string(nil)
			if f.loadedDrop {
				drops = []string{paths.LNDVerificationDropIn}
			}
			return lndUnitStatus{
				invocationID:     f.invocation,
				mainPID:          f.pid,
				activeState:      active,
				subState:         sub,
				restart:          f.restart,
				fragmentPath:     paths.LNDService,
				dropInPaths:      drops,
				needDaemonReload: "no",
				execStart: "{ path=/usr/local/bin/lnd ; argv[]=" +
					strings.Join(expectedLNDArgs(f.loaded == autoUnlockUnitEnabled), " ") +
					" ; ignore_errors=no ; }",
			}, nil
		},
		processArgs: func(pid int) ([]string, error) {
			if err := f.fail("process-args"); err != nil {
				return nil, err
			}
			if pid != f.pid || pid == 0 {
				return nil, errors.New("process does not exist")
			}
			return append([]string(nil), f.args...), nil
		},
		walletState: func() (lnrpc.WalletState, error) {
			if err := f.fail("wallet-state"); err != nil {
				return lnrpc.WalletState_WAITING_TO_START, err
			}
			return f.wallet, nil
		},
		getInfo: func(string) error {
			f.getInfoCount++
			if err := f.fail("get-info"); err != nil {
				return err
			}
			if f.wallet != lnrpc.WalletState_SERVER_ACTIVE {
				return errors.New("LND is not ready")
			}
			return nil
		},
		now: func() time.Time { return f.now },
		sleep: func(d time.Duration) {
			f.now = f.now.Add(d)
		},
	}
}

func assertDisabled(t *testing.T, f *autoUnlockFixture) {
	t.Helper()
	if f.cfg.AutoUnlock || f.art.unit != autoUnlockUnitPlain ||
		f.art.password || f.art.passwordStage || f.art.verifyDrop ||
		f.restart != "on-failure" ||
		f.wallet != lnrpc.WalletState_LOCKED {
		t.Fatalf("not safely disabled: cfg=%+v art=%+v restart=%s wallet=%s",
			f.cfg, f.art, f.restart, f.wallet)
	}
}

func assertDisabledDisk(t *testing.T, f *autoUnlockFixture) {
	t.Helper()
	if f.cfg.AutoUnlock || f.art.unit != autoUnlockUnitPlain ||
		f.art.password || f.art.passwordStage || f.art.verifyDrop ||
		f.restart != "on-failure" {
		t.Fatalf("disabled disk state did not converge: cfg=%+v art=%+v restart=%s",
			f.cfg, f.art, f.restart)
	}
}

func assertEnabled(t *testing.T, f *autoUnlockFixture) {
	t.Helper()
	if !f.cfg.AutoUnlock || f.art.unit != autoUnlockUnitEnabled ||
		!f.art.password || f.art.verifyDrop || f.restart != "on-failure" ||
		f.wallet != lnrpc.WalletState_SERVER_ACTIVE {
		t.Fatalf("not safely enabled: cfg=%+v art=%+v restart=%s wallet=%s",
			f.cfg, f.art, f.restart, f.wallet)
	}
}

func TestEnableAutoUnlockProvesAndPublishes(t *testing.T) {
	f := newAutoUnlockFixture(false)
	result := enableAutoUnlock("correct", f.ops())
	if result.Outcome != AutoUnlockEnabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertEnabled(t, f)
	if f.restartCount != 1 || f.getInfoCount < 2 {
		t.Fatalf("restart=%d GetInfo=%d", f.restartCount, f.getInfoCount)
	}
}

func TestEnableWrongPasswordReturnsToLockedRetryState(t *testing.T) {
	f := newAutoUnlockFixture(false)
	result := enableAutoUnlock("wrong", f.ops())
	if result.Outcome != AutoUnlockVerificationFailed || result.Detail != "" {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
	if f.restartCount != 2 {
		t.Fatalf("restarts = %d, want candidate plus locked rollback", f.restartCount)
	}
}

func TestEnableTimeoutIsInconclusiveAndLocked(t *testing.T) {
	f := newAutoUnlockFixture(false)
	f.holdEnabled = true
	result := enableAutoUnlock("correct", f.ops())
	if result.Outcome != AutoUnlockVerificationTimedOut {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
	if elapsed := f.now.Sub(time.Unix(1_700_000_000, 0)); elapsed < 120*time.Second {
		t.Fatalf("timeout elapsed only %s", elapsed)
	}
}

func TestEnablePublicationFailureRollsBackDisabled(t *testing.T) {
	f := newAutoUnlockFixture(false)
	f.failOn["save-enabled"] = 1
	result := enableAutoUnlock("correct", f.ops())
	if result.Outcome != AutoUnlockVerificationFailed || result.Detail == "" {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
}

func TestEnableRecoversRecognizableInterruptedAttemptWithoutMarker(t *testing.T) {
	f := newAutoUnlockFixture(false)
	f.art = autoUnlockArtifacts{
		unit:          autoUnlockUnitEnabled,
		password:      true,
		passwordStage: true,
		verifyDrop:    true,
	}
	f.loaded = autoUnlockUnitEnabled
	f.loadedDrop = true
	f.restart = "no"
	f.args = expectedLNDArgs(true)
	f.password = "correct"
	result := enableAutoUnlock("correct", f.ops())
	if result.Outcome != AutoUnlockEnabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertEnabled(t, f)
	if f.restartCount != 2 {
		t.Fatalf("restarts = %d, want recovery plus candidate", f.restartCount)
	}
}

func TestEnableRollbackFailureRequiresRepair(t *testing.T) {
	f := newAutoUnlockFixture(false)
	f.failOn["write-unit-plain"] = 2
	result := enableAutoUnlock("wrong", f.ops())
	if result.Outcome != AutoUnlockRepairRequired || result.FailedStep == "" {
		t.Fatalf("outcome = %+v", result)
	}
}

func TestEnableFailureInjectionConvergesToDisabledState(t *testing.T) {
	tests := []struct {
		name        string
		inject      func(*autoUnlockFixture)
		wantOutcome AutoUnlockOutcome
	}{
		{"write password", func(f *autoUnlockFixture) { f.failOn["write-password"] = 1 }, AutoUnlockVerificationFailed},
		{"write unit", func(f *autoUnlockFixture) { f.failOn["write-unit-enabled"] = 1 }, AutoUnlockVerificationFailed},
		{"validate candidate", func(f *autoUnlockFixture) { f.failOn["validate"] = 2 }, AutoUnlockVerificationFailed},
		{"reload candidate", func(f *autoUnlockFixture) { f.failOn["daemon-reload"] = 2 }, AutoUnlockVerificationFailed},
		{"restart candidate", func(f *autoUnlockFixture) { f.failOn["restart-lnd"] = 1 }, AutoUnlockVerificationFailed},
		{"read candidate process", func(f *autoUnlockFixture) { f.failOn["process-args"] = 2 }, AutoUnlockVerificationFailed},
		{"authenticated GetInfo unavailable", func(f *autoUnlockFixture) { f.always["get-info"] = true }, AutoUnlockVerificationTimedOut},
		{"remove verification drop-in", func(f *autoUnlockFixture) { f.failOn["remove-drop"] = 1 }, AutoUnlockVerificationFailed},
		{"reload final policy", func(f *autoUnlockFixture) { f.failOn["daemon-reload"] = 3 }, AutoUnlockVerificationFailed},
		{"recheck process", func(f *autoUnlockFixture) { f.failOn["process-args"] = 3 }, AutoUnlockVerificationFailed},
		{"recheck state", func(f *autoUnlockFixture) { f.failOn["wallet-state"] = 2 }, AutoUnlockVerificationFailed},
		{"recheck GetInfo", func(f *autoUnlockFixture) { f.failOn["get-info"] = 2 }, AutoUnlockVerificationFailed},
		{"publish enabled config", func(f *autoUnlockFixture) { f.failOn["save-enabled"] = 1 }, AutoUnlockVerificationFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newAutoUnlockFixture(false)
			test.inject(f)
			result := enableAutoUnlock("correct", f.ops())
			if result.Outcome != test.wantOutcome {
				t.Fatalf("outcome = %+v, want %s", result, test.wantOutcome)
			}
			assertDisabledDisk(t, f)
		})
	}
}

func TestDisableAutoUnlockDeletesPasswordAndPublishes(t *testing.T) {
	f := newAutoUnlockFixture(true)
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockDisabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
	if f.restartCount != 1 {
		t.Fatalf("restarts = %d, want one", f.restartCount)
	}
}

func TestDisableFailureBeforeDeletionRestoresEnabledState(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.failOn["validate"] = 1
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockStillEnabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertEnabled(t, f)
}

func TestDisableRemovalFailureWithPasswordPresentRestoresEnabled(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.failOn["remove-password"] = 1
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockStillEnabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertEnabled(t, f)
}

func TestDisableRemovalErrorAfterUnlinkFinishesDisabled(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.after["remove-password"] = true
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockDisabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
}

func TestDisableRollbackFailureRequiresRepair(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.password = "wrong"
	f.failOn["validate"] = 1
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockRepairRequired {
		t.Fatalf("outcome = %+v", result)
	}
}

func TestDisableResumesAfterPasswordCommitPoint(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.art = autoUnlockArtifacts{unit: autoUnlockUnitPlain}
	f.loaded = autoUnlockUnitPlain
	f.args = expectedLNDArgs(false)
	f.wallet = lnrpc.WalletState_LOCKED
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockDisabled {
		t.Fatalf("outcome = %+v", result)
	}
	assertDisabled(t, f)
}

func TestDisablePublicationFailureAfterDeletionRequiresRepair(t *testing.T) {
	f := newAutoUnlockFixture(true)
	f.failOn["save-disabled"] = 1
	result := disableAutoUnlockTransition(f.ops())
	if result.Outcome != AutoUnlockRepairRequired {
		t.Fatalf("outcome = %+v", result)
	}
	if f.art.password {
		t.Fatal("password was recreated after the irreversible deletion point")
	}
}

func TestDisableFailureInjectionBeforeDeletionRestoresEnabled(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*autoUnlockFixture)
	}{
		{"write plain unit", func(f *autoUnlockFixture) { f.failOn["write-unit-plain"] = 1 }},
		{"validate plain unit", func(f *autoUnlockFixture) { f.failOn["validate"] = 1 }},
		{"reload plain unit", func(f *autoUnlockFixture) { f.failOn["daemon-reload"] = 1 }},
		{"inspect loaded plain unit", func(f *autoUnlockFixture) { f.failOn["unit-status"] = 1 }},
		{"restart plain LND", func(f *autoUnlockFixture) { f.failOn["restart-lnd"] = 1 }},
		{"inspect plain process", func(f *autoUnlockFixture) { f.failOn["process-args"] = 1 }},
		{"remove verification drop-in", func(f *autoUnlockFixture) { f.failOn["remove-drop"] = 1 }},
		{"reload final policy", func(f *autoUnlockFixture) { f.failOn["daemon-reload"] = 2 }},
		{"recheck locked process", func(f *autoUnlockFixture) { f.failOn["process-args"] = 2 }},
		{"recheck locked state", func(f *autoUnlockFixture) { f.failOn["wallet-state"] = 2 }},
		{"remove password before unlink", func(f *autoUnlockFixture) { f.failOn["remove-password"] = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newAutoUnlockFixture(true)
			test.inject(f)
			result := disableAutoUnlockTransition(f.ops())
			if result.Outcome != AutoUnlockStillEnabled {
				t.Fatalf("outcome = %+v", result)
			}
			assertEnabled(t, f)
		})
	}
}

func TestDisableFailureInjectionAfterDeletionNeverRecreatesPassword(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*autoUnlockFixture)
	}{
		{"synchronize committed deletion", func(f *autoUnlockFixture) {
			f.after["remove-password"] = true
			f.failOn["remove-password"] = 1
		}},
		{"inspect committed deletion", func(f *autoUnlockFixture) { f.failOn["inspect"] = 2 }},
		{"validate final plain unit", func(f *autoUnlockFixture) { f.failOn["validate"] = 2 }},
		{"reload final plain unit", func(f *autoUnlockFixture) { f.failOn["daemon-reload"] = 3 }},
		{"publish disabled config", func(f *autoUnlockFixture) { f.failOn["save-disabled"] = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newAutoUnlockFixture(true)
			test.inject(f)
			result := disableAutoUnlockTransition(f.ops())
			if result.Outcome != AutoUnlockRepairRequired {
				t.Fatalf("outcome = %+v", result)
			}
			if f.art.password {
				t.Fatal("password was recreated after the deletion commit point")
			}
		})
	}
}

func TestLoadedExecMatchesOnlyExactCommand(t *testing.T) {
	good := "{ path=/usr/local/bin/lnd ; argv[]=" +
		strings.Join(expectedLNDArgs(true), " ") + " ; ignore_errors=no ; }"
	if !loadedExecMatches(good, true) {
		t.Fatal("exact auto-unlock command was rejected")
	}
	for _, bad := range []string{
		strings.Replace(good, paths.LNDWalletPassword, "/tmp/password", 1),
		strings.Replace(good, " ; ignore_errors", " --extra ; ignore_errors", 1),
		strings.Replace(good, "argv[]=", "command[]=", 1),
	} {
		if loadedExecMatches(bad, true) {
			t.Fatalf("non-exact command accepted: %s", bad)
		}
	}
}
