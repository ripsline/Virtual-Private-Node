package installer

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/helper"
	"github.com/virtualprivatenode/vpn/internal/logger"
	"github.com/virtualprivatenode/vpn/internal/paths"
	"github.com/virtualprivatenode/vpn/internal/system"
)

const (
	autoUnlockVerificationTimeout = 120 * time.Second
	autoUnlockPollInterval        = time.Second
	autoUnlockRPCTimeout          = 5 * time.Second
)

const lndVerificationDropIn = `[Service]
Restart=no
`

type autoUnlockUnit int

const (
	autoUnlockUnitUnknown autoUnlockUnit = iota
	autoUnlockUnitPlain
	autoUnlockUnitEnabled
)

type autoUnlockArtifacts struct {
	unit          autoUnlockUnit
	password      bool
	passwordStage bool
	verifyDrop    bool
}

type lndUnitStatus struct {
	invocationID     string
	mainPID          int
	activeState      string
	subState         string
	result           string
	execMainStatus   string
	restart          string
	fragmentPath     string
	dropInPaths      []string
	needDaemonReload string
	execStart        string
}

type invocationResult int

const (
	invocationReady invocationResult = iota
	invocationExited
	invocationTimedOut
)

type autoUnlockOps struct {
	loadConfig       func() (*config.AppConfig, error)
	saveConfig       func(*config.AppConfig) error
	artifacts        func() (autoUnlockArtifacts, error)
	writeUnit        func(autoUnlockUnit) error
	writePassword    func(string) error
	removePassword   func() error
	writeVerifyDrop  func() error
	removeVerifyDrop func() error
	validateUnit     func() error
	daemonReload     func() error
	restartLND       func() error
	unitStatus       func() (lndUnitStatus, error)
	processArgs      func(int) ([]string, error)
	walletState      func() (lnrpc.WalletState, error)
	getInfo          func(string) error
	now              func() time.Time
	sleep            func(time.Duration)
}

// AutoUnlockResult is the structured, credential-free result returned through
// the privileged helper. Expected recovery outcomes are data, not error text.
type AutoUnlockResult = helper.AutoUnlockResult
type AutoUnlockOutcome = helper.AutoUnlockOutcome

const (
	AutoUnlockEnabled              = helper.AutoUnlockEnabled
	AutoUnlockDisabled             = helper.AutoUnlockDisabled
	AutoUnlockVerificationFailed   = helper.AutoUnlockVerificationFailed
	AutoUnlockVerificationTimedOut = helper.AutoUnlockVerificationTimedOut
	AutoUnlockStillEnabled         = helper.AutoUnlockStillEnabled
	AutoUnlockRepairRequired       = helper.AutoUnlockRepairRequired
)

func repairRequired(stage string, cause ...error) AutoUnlockResult {
	if len(cause) > 0 && cause[0] != nil {
		logger.System("auto-unlock: %s: %v", stage, cause[0])
	} else {
		logger.System("auto-unlock: %s", stage)
	}
	return AutoUnlockResult{
		Outcome:    AutoUnlockRepairRequired,
		FailedStep: stage,
	}
}

func enableAutoUnlock(password string, ops autoUnlockOps) AutoUnlockResult {
	cfg, err := ops.loadConfig()
	if err != nil {
		return repairRequired("read node configuration", err)
	}
	if cfg.AutoUnlock {
		return repairRequired("require disabled starting state")
	}

	artifacts, err := ops.artifacts()
	if err != nil || artifacts.unit == autoUnlockUnitUnknown {
		return repairRequired("inspect disabled starting state", err)
	}

	// Restart=no is installed before touching either the candidate credential
	// or ExecStart. If this process or the host stops, the persistent drop-in
	// prevents an unverified password from becoming a restart loop.
	if err := ops.writeVerifyDrop(); err != nil {
		return repairRequired("install one-attempt restart policy", err)
	}
	if err := normalizeDisabledDisk(ops); err != nil {
		return repairRequired("restore disabled starting state", err)
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "no", true); err != nil {
		return repairRequired("load disabled starting state", err)
	}

	before, err := ops.unitStatus()
	if err != nil {
		return repairRequired("observe current LND invocation", err)
	}
	if before.mainPID == 0 || verifyProcessArgs(ops, before.mainPID, false) != nil {
		// An interrupted older enable can leave an auto-unlock process alive
		// while the still-false desired state has just been normalized on disk.
		// Converge that recognizable shape to a plain locked invocation before
		// testing the newly supplied password.
		if err := ops.restartLND(); err != nil {
			return repairRequired("restore disabled LND invocation", err)
		}
		outcome, err := waitForInvocation(
			ops, before.invocationID, false, lnrpc.WalletState_LOCKED,
			cfg.Network, autoUnlockVerificationTimeout,
		)
		if err != nil || outcome != invocationReady {
			return repairRequired("prove disabled LND invocation", err)
		}
		before, err = ops.unitStatus()
		if err != nil {
			return repairRequired("observe restored LND invocation", err)
		}
	}

	restarted := false
	fail := func(
		outcome AutoUnlockOutcome, stage, detail string, cause error,
	) AutoUnlockResult {
		if cause != nil {
			logger.System("auto-unlock: %s: %v", stage, cause)
		} else {
			logger.System("auto-unlock: %s", stage)
		}
		if err := restoreDisabled(ops, cfg, before, restarted); err != nil {
			return repairRequired(stage,
				fmt.Errorf("recovery failed: %w", err))
		}
		return AutoUnlockResult{Outcome: outcome, Detail: detail}
	}

	if err := ops.writePassword(password); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"write candidate wallet password",
			"VPN could not store the password safely. The previous disabled setting was restored.", err)
	}
	if err := ops.writeUnit(autoUnlockUnitEnabled); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"install auto-unlock unit",
			"VPN could not install auto-unlock safely. The previous disabled setting was restored.", err)
	}
	if err := ops.validateUnit(); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"validate auto-unlock unit",
			"VPN could not validate the auto-unlock service. The previous disabled setting was restored.", err)
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitEnabled, "no", true); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"load one-attempt auto-unlock unit",
			"VPN could not load auto-unlock safely. The previous disabled setting was restored.", err)
	}

	restarted = true
	restartErr := ops.restartLND()
	invocation, verifyErr := waitForInvocation(
		ops, before.invocationID, true, lnrpc.WalletState_SERVER_ACTIVE,
		cfg.Network, autoUnlockVerificationTimeout,
	)
	if invocation == invocationExited {
		return fail(AutoUnlockVerificationFailed,
			"verify candidate wallet password", "", verifyErr)
	}
	if invocation == invocationTimedOut {
		return fail(AutoUnlockVerificationTimedOut,
			"wait for LND to become ready", "", verifyErr)
	}
	if restartErr != nil || verifyErr != nil {
		cause := verifyErr
		if restartErr != nil {
			cause = restartErr
		}
		return fail(AutoUnlockVerificationFailed,
			"verify restarted LND",
			"VPN could not verify auto-unlock because a system operation failed. LND has been returned to the locked state.", cause)
	}

	// The password has now been proved. Removing the override and reloading
	// changes only the policy PID 1 applies to a future failure; the verified
	// LND invocation stays online and is checked again below.
	if err := ops.removeVerifyDrop(); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"remove one-attempt restart policy",
			"VPN verified the password but could not finish auto-unlock safely. LND has been returned to the locked state.", err)
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitEnabled, "on-failure", false); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"restore normal restart policy",
			"VPN verified the password but could not finish auto-unlock safely. LND has been returned to the locked state.", err)
	}
	if err := verifySameReadyInvocation(ops, before.invocationID, cfg.Network); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"recheck verified LND invocation",
			"VPN could not prove that LND stayed ready. LND has been returned to the locked state.", err)
	}

	cfg.AutoUnlock = true
	if err := ops.saveConfig(cfg); err != nil {
		return fail(AutoUnlockVerificationFailed,
			"publish enabled setting",
			"VPN verified the password but could not publish auto-unlock safely. LND has been returned to the locked state.", err)
	}
	published, err := ops.loadConfig()
	if err != nil || !published.AutoUnlock {
		return fail(AutoUnlockVerificationFailed,
			"confirm enabled setting",
			"VPN could not confirm the auto-unlock setting. LND has been returned to the locked state.", err)
	}
	return AutoUnlockResult{Outcome: AutoUnlockEnabled}
}

func disableAutoUnlockTransition(ops autoUnlockOps) AutoUnlockResult {
	cfg, err := ops.loadConfig()
	if err != nil {
		return repairRequired("read node configuration", err)
	}
	if !cfg.AutoUnlock {
		return repairRequired("require enabled starting state")
	}
	artifacts, err := ops.artifacts()
	if err != nil || artifacts.unit == autoUnlockUnitUnknown {
		return repairRequired("inspect enabled starting state", err)
	}
	if !artifacts.password {
		// Password deletion is the irreversible commit point. This exact shape
		// is an interrupted disable after that point: finish publication rather
		// than attempting to recreate a credential VPN no longer has.
		if artifacts.unit != autoUnlockUnitPlain {
			return repairRequired("classify interrupted disable")
		}
		if err := finishDisabledAfterPasswordRemoval(ops, cfg); err != nil {
			return repairRequired("finish interrupted disable", err)
		}
		return AutoUnlockResult{Outcome: AutoUnlockDisabled}
	}

	if err := ops.writeVerifyDrop(); err != nil {
		return repairRequired("install one-attempt restart policy", err)
	}
	if err := ops.writeUnit(autoUnlockUnitPlain); err != nil {
		return restoreEnabledResult(ops, cfg, "install plain LND unit", err)
	}
	if err := ops.validateUnit(); err != nil {
		return restoreEnabledResult(ops, cfg, "validate plain LND unit", err)
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "no", true); err != nil {
		return restoreEnabledResult(ops, cfg, "load plain LND unit", err)
	}

	before, err := ops.unitStatus()
	if err != nil {
		return restoreEnabledResult(ops, cfg, "observe current LND invocation", err)
	}
	restartErr := ops.restartLND()
	invocation, verifyErr := waitForInvocation(
		ops, before.invocationID, false, lnrpc.WalletState_LOCKED,
		cfg.Network, autoUnlockVerificationTimeout,
	)
	if restartErr != nil || invocation != invocationReady || verifyErr != nil {
		cause := verifyErr
		if restartErr != nil {
			cause = restartErr
		}
		return restoreEnabledResult(ops, cfg, "prove locked LND invocation", cause)
	}

	if err := ops.removeVerifyDrop(); err != nil {
		return restoreEnabledResult(ops, cfg, "remove one-attempt restart policy", err)
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "on-failure", false); err != nil {
		return restoreEnabledResult(ops, cfg, "restore normal restart policy", err)
	}
	if err := verifySameLockedInvocation(ops, before.invocationID); err != nil {
		return restoreEnabledResult(ops, cfg, "recheck locked LND invocation", err)
	}

	removeErr := ops.removePassword()
	afterRemoval, inspectErr := ops.artifacts()
	if inspectErr != nil {
		return repairRequired("confirm wallet password removal", inspectErr)
	}
	if afterRemoval.password {
		return restoreEnabledResult(ops, cfg, "remove wallet password", removeErr)
	}
	// An observed absence is past the irreversible commit point, but success
	// still requires durability. Calling remove again when the name is absent
	// retries the parent-directory sync without recreating credential data.
	durabilityErr := removeErr
	if removeErr != nil {
		durabilityErr = ops.removePassword()
	}
	if err := finishDisabledAfterPasswordRemoval(ops, cfg); err != nil {
		return repairRequired("publish disabled setting", err)
	}
	if durabilityErr != nil {
		return repairRequired("confirm durable wallet password removal", durabilityErr)
	}
	return AutoUnlockResult{Outcome: AutoUnlockDisabled}
}

func normalizeDisabledDisk(ops autoUnlockOps) error {
	if err := ops.writeUnit(autoUnlockUnitPlain); err != nil {
		return err
	}
	if err := ops.validateUnit(); err != nil {
		return err
	}
	if err := ops.removePassword(); err != nil {
		return err
	}
	artifacts, err := ops.artifacts()
	if err != nil {
		return err
	}
	if artifacts.unit != autoUnlockUnitPlain || artifacts.password ||
		artifacts.passwordStage || !artifacts.verifyDrop {
		return errors.New("disabled disk state did not converge")
	}
	return nil
}

func restoreDisabled(
	ops autoUnlockOps, cfg *config.AppConfig, before lndUnitStatus,
	restarted bool,
) error {
	var lockedPreviousID string
	if err := ops.writeVerifyDrop(); err != nil {
		return err
	}
	if err := normalizeDisabledDisk(ops); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "no", true); err != nil {
		return err
	}
	if restarted {
		current, err := ops.unitStatus()
		if err != nil {
			return err
		}
		lockedPreviousID = current.invocationID
		if err := ops.restartLND(); err != nil {
			return err
		}
		outcome, err := waitForInvocation(
			ops, current.invocationID, false, lnrpc.WalletState_LOCKED,
			cfg.Network, autoUnlockVerificationTimeout,
		)
		if err != nil || outcome != invocationReady {
			return errors.New("could not restore locked LND invocation")
		}
	} else if before.mainPID > 0 {
		current, err := ops.unitStatus()
		if err != nil || current.invocationID != before.invocationID {
			return errors.New("original LND invocation changed")
		}
		if err := verifyProcessArgs(ops, current.mainPID, false); err != nil {
			return err
		}
	}
	if err := ops.removeVerifyDrop(); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "on-failure", false); err != nil {
		return err
	}
	if restarted {
		if err := verifySameLockedInvocation(ops, lockedPreviousID); err != nil {
			return err
		}
	}
	cfg.AutoUnlock = false
	if err := ops.saveConfig(cfg); err != nil {
		observed, loadErr := ops.loadConfig()
		if loadErr != nil || observed.AutoUnlock {
			return err
		}
	}
	final, err := ops.artifacts()
	if err != nil || final.unit != autoUnlockUnitPlain || final.password ||
		final.passwordStage || final.verifyDrop {
		return errors.New("disabled state did not pass final inspection")
	}
	return nil
}

func restoreEnabledResult(
	ops autoUnlockOps, cfg *config.AppConfig, failedStage string, cause error,
) AutoUnlockResult {
	if cause != nil {
		logger.System("auto-unlock: %s: %v", failedStage, cause)
	} else {
		logger.System("auto-unlock: %s", failedStage)
	}
	if err := restoreEnabled(ops, cfg); err != nil {
		return repairRequired(failedStage,
			fmt.Errorf("recovery failed: %w", err))
	}
	return AutoUnlockResult{Outcome: AutoUnlockStillEnabled}
}

func restoreEnabled(ops autoUnlockOps, cfg *config.AppConfig) error {
	artifacts, err := ops.artifacts()
	if err != nil || !artifacts.password {
		return errors.New("stored wallet password is unavailable")
	}
	if err := ops.writeVerifyDrop(); err != nil {
		return err
	}
	if err := ops.writeUnit(autoUnlockUnitEnabled); err != nil {
		return err
	}
	if err := ops.validateUnit(); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitEnabled, "no", true); err != nil {
		return err
	}
	before, err := ops.unitStatus()
	if err != nil {
		return err
	}
	if err := ops.restartLND(); err != nil {
		return err
	}
	outcome, err := waitForInvocation(
		ops, before.invocationID, true, lnrpc.WalletState_SERVER_ACTIVE,
		cfg.Network, autoUnlockVerificationTimeout,
	)
	if err != nil || outcome != invocationReady {
		return errors.New("previous enabled state did not restart")
	}
	if err := ops.removeVerifyDrop(); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitEnabled, "on-failure", false); err != nil {
		return err
	}
	if err := verifySameReadyInvocation(ops, before.invocationID, cfg.Network); err != nil {
		return err
	}
	cfg.AutoUnlock = true
	if err := ops.saveConfig(cfg); err != nil {
		observed, loadErr := ops.loadConfig()
		if loadErr != nil || !observed.AutoUnlock {
			return err
		}
	}
	return nil
}

func finishDisabledAfterPasswordRemoval(
	ops autoUnlockOps, cfg *config.AppConfig,
) error {
	if err := ops.writeVerifyDrop(); err != nil {
		return err
	}
	if err := ops.writeUnit(autoUnlockUnitPlain); err != nil {
		return err
	}
	if err := ops.validateUnit(); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "no", true); err != nil {
		return err
	}
	if err := ensureLockedRuntime(ops, cfg.Network); err != nil {
		return err
	}
	if err := ops.removeVerifyDrop(); err != nil {
		return err
	}
	if err := reloadAndVerifyUnit(ops, autoUnlockUnitPlain, "on-failure", false); err != nil {
		return err
	}
	if err := verifyCurrentLockedInvocation(ops); err != nil {
		return err
	}
	cfg.AutoUnlock = false
	if err := ops.saveConfig(cfg); err != nil {
		observed, loadErr := ops.loadConfig()
		if loadErr != nil || observed.AutoUnlock {
			return err
		}
	}
	final, err := ops.artifacts()
	if err != nil {
		return err
	}
	if final.unit != autoUnlockUnitPlain || final.password ||
		final.passwordStage || final.verifyDrop {
		return errors.New("disabled state did not pass final inspection")
	}
	return nil
}

func ensureLockedRuntime(ops autoUnlockOps, network string) error {
	status, err := ops.unitStatus()
	if err != nil {
		return err
	}
	if status.mainPID > 0 {
		if err := verifyProcessArgs(ops, status.mainPID, false); err == nil {
			state, stateErr := ops.walletState()
			if stateErr == nil && state == lnrpc.WalletState_LOCKED {
				return nil
			}
		}
	}
	if err := ops.restartLND(); err != nil {
		return err
	}
	outcome, err := waitForInvocation(
		ops, status.invocationID, false, lnrpc.WalletState_LOCKED,
		network, autoUnlockVerificationTimeout,
	)
	if err != nil || outcome != invocationReady {
		return errors.New("could not prove a locked LND invocation")
	}
	return nil
}

func reloadAndVerifyUnit(
	ops autoUnlockOps, unit autoUnlockUnit, restart string, drop bool,
) error {
	if err := ops.daemonReload(); err != nil {
		return err
	}
	status, err := ops.unitStatus()
	if err != nil {
		return err
	}
	return verifyLoadedUnit(status, unit, restart, drop)
}

func verifyLoadedUnit(
	status lndUnitStatus, unit autoUnlockUnit, restart string, drop bool,
) error {
	if status.fragmentPath != paths.LNDService {
		return fmt.Errorf("loaded fragment is %q", status.fragmentPath)
	}
	if status.needDaemonReload != "no" {
		return fmt.Errorf("systemd still requires daemon-reload")
	}
	if status.restart != restart {
		return fmt.Errorf("loaded Restart=%s, want %s", status.restart, restart)
	}
	wantDrops := []string(nil)
	if drop {
		wantDrops = []string{paths.LNDVerificationDropIn}
	}
	if !equalStrings(status.dropInPaths, wantDrops) {
		return fmt.Errorf("loaded drop-ins are %q", status.dropInPaths)
	}
	if !loadedExecMatches(status.execStart, unit == autoUnlockUnitEnabled) {
		return errors.New("loaded ExecStart is not the exact VPN LND command")
	}
	return nil
}

func waitForInvocation(
	ops autoUnlockOps, previousID string, withUnlock bool,
	wantState lnrpc.WalletState, network string, timeout time.Duration,
) (invocationResult, error) {
	deadline := ops.now().Add(timeout)
	var lastErr error
	for {
		status, statusErr := ops.unitStatus()
		if statusErr != nil {
			lastErr = statusErr
		}
		if statusErr == nil && status.invocationID != "" &&
			status.invocationID != previousID {
			if status.mainPID == 0 {
				if status.activeState == "failed" ||
					status.activeState == "inactive" {
					return invocationExited, fmt.Errorf(
						"new LND invocation exited: active=%s sub=%s result=%s status=%s",
						status.activeState, status.subState,
						status.result, status.execMainStatus)
				}
			} else {
				if err := verifyProcessArgs(ops, status.mainPID, withUnlock); err != nil {
					return invocationReady, err
				}
				state, err := ops.walletState()
				if err != nil {
					lastErr = err
				}
				if err == nil && state == wantState {
					if wantState == lnrpc.WalletState_SERVER_ACTIVE {
						if err := ops.getInfo(network); err != nil {
							lastErr = err
							// SERVER_ACTIVE and authenticated GetInfo are one
							// postcondition. A transient RPC failure keeps polling.
						} else {
							return invocationReady, nil
						}
					} else {
						return invocationReady, nil
					}
				}
			}
		}
		if !ops.now().Before(deadline) {
			return invocationTimedOut, lastErr
		}
		ops.sleep(autoUnlockPollInterval)
	}
}

func verifySameReadyInvocation(
	ops autoUnlockOps, previousID, network string,
) error {
	status, err := ops.unitStatus()
	if err != nil {
		return err
	}
	if status.invocationID == "" || status.invocationID == previousID || status.mainPID == 0 {
		return errors.New("verified LND invocation is not running")
	}
	if err := verifyProcessArgs(ops, status.mainPID, true); err != nil {
		return err
	}
	state, err := ops.walletState()
	if err != nil || state != lnrpc.WalletState_SERVER_ACTIVE {
		return errors.New("verified LND invocation is not SERVER_ACTIVE")
	}
	return ops.getInfo(network)
}

func verifySameLockedInvocation(ops autoUnlockOps, previousID string) error {
	status, err := ops.unitStatus()
	if err != nil {
		return err
	}
	if status.invocationID == "" || status.invocationID == previousID || status.mainPID == 0 {
		return errors.New("locked LND invocation is not running")
	}
	return verifyLockedStatus(ops, status)
}

func verifyCurrentLockedInvocation(ops autoUnlockOps) error {
	status, err := ops.unitStatus()
	if err != nil {
		return err
	}
	if status.invocationID == "" || status.mainPID == 0 {
		return errors.New("locked LND invocation is not running")
	}
	return verifyLockedStatus(ops, status)
}

func verifyLockedStatus(ops autoUnlockOps, status lndUnitStatus) error {
	if err := verifyProcessArgs(ops, status.mainPID, false); err != nil {
		return err
	}
	state, err := ops.walletState()
	if err != nil || state != lnrpc.WalletState_LOCKED {
		return errors.New("LND did not remain LOCKED")
	}
	return nil
}

func verifyProcessArgs(ops autoUnlockOps, pid int, withUnlock bool) error {
	args, err := ops.processArgs(pid)
	if err != nil {
		return err
	}
	want := expectedLNDArgs(withUnlock)
	if !equalStrings(args, want) {
		return fmt.Errorf("LND process arguments do not match the loaded unit")
	}
	return nil
}

func expectedLNDArgs(withUnlock bool) []string {
	args := []string{"/usr/local/bin/lnd", "--configfile=/etc/lnd/lnd.conf"}
	if withUnlock {
		args = append(args, "--wallet-unlock-password-file="+paths.LNDWalletPassword)
	}
	return args
}

func loadedExecMatches(raw string, withUnlock bool) bool {
	const prefix = "argv[]="
	start := strings.Index(raw, prefix)
	if start < 0 {
		return false
	}
	argv := raw[start+len(prefix):]
	if end := strings.Index(argv, " ;"); end >= 0 {
		argv = argv[:end]
	} else if end := strings.IndexByte(argv, '}'); end >= 0 {
		argv = strings.TrimSpace(argv[:end])
	}
	return argv == strings.Join(expectedLNDArgs(withUnlock), " ")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── Production operations ────────────────────────────────────────────────

type autoUnlockFS struct {
	lndUID int
	lndGID int
}

func productionAutoUnlockOps() (autoUnlockOps, error) {
	identity, err := user.Lookup(lndUser)
	if err != nil {
		return autoUnlockOps{}, fmt.Errorf("resolve lnd user: %w", err)
	}
	uid, err := strconv.Atoi(identity.Uid)
	if err != nil {
		return autoUnlockOps{}, fmt.Errorf("parse lnd uid: %w", err)
	}
	gid, err := strconv.Atoi(identity.Gid)
	if err != nil {
		return autoUnlockOps{}, fmt.Errorf("parse lnd gid: %w", err)
	}
	fs := &autoUnlockFS{lndUID: uid, lndGID: gid}
	return autoUnlockOps{
		loadConfig:       config.Load,
		saveConfig:       config.Save,
		artifacts:        fs.inspectArtifacts,
		writeUnit:        fs.writeUnit,
		writePassword:    fs.writePassword,
		removePassword:   fs.removePassword,
		writeVerifyDrop:  fs.writeVerifyDrop,
		removeVerifyDrop: fs.removeVerifyDrop,
		validateUnit:     validateInstalledLNDUnit,
		daemonReload: func() error {
			return system.SudoRun("systemctl", "daemon-reload")
		},
		restartLND: func() error {
			return system.SudoRun("systemctl", "restart", "lnd.service")
		},
		unitStatus:  readLNDUnitStatus,
		processArgs: readProcessArgs,
		walletState: readLNDWalletState,
		getInfo:     authenticatedLNDGetInfo,
		now:         time.Now,
		sleep:       time.Sleep,
	}, nil
}

func (fs *autoUnlockFS) inspectArtifacts() (autoUnlockArtifacts, error) {
	var out autoUnlockArtifacts
	unit, exists, err := readExactFile(paths.LNDService, 0o644, 0, 0)
	if err != nil {
		return out, fmt.Errorf("inspect LND unit: %w", err)
	}
	if !exists {
		return out, errors.New("LND unit is missing")
	}
	switch string(unit) {
	case lndServiceUnit(lndUser, false):
		out.unit = autoUnlockUnitPlain
	case lndServiceUnit(lndUser, true):
		out.unit = autoUnlockUnitEnabled
	default:
		out.unit = autoUnlockUnitUnknown
	}
	out.password, err = exactFileExists(
		paths.LNDWalletPassword, 0o400, fs.lndUID, fs.lndGID)
	if err != nil {
		return out, fmt.Errorf("inspect wallet password: %w", err)
	}
	out.passwordStage, err = passwordStageExists(fs.lndUID, fs.lndGID)
	if err != nil {
		return out, fmt.Errorf("inspect staged wallet password: %w", err)
	}

	if err := validateOptionalDropInDir(); err != nil {
		return out, err
	}
	drop, exists, err := readExactFile(
		paths.LNDVerificationDropIn, 0o644, 0, 0)
	if err != nil {
		return out, fmt.Errorf("inspect verification drop-in: %w", err)
	}
	if exists {
		if string(drop) != lndVerificationDropIn {
			return out, errors.New("verification drop-in has unexpected content")
		}
		out.verifyDrop = true
	}
	return out, nil
}

func (fs *autoUnlockFS) writeUnit(unit autoUnlockUnit) error {
	var content string
	switch unit {
	case autoUnlockUnitPlain:
		content = lndServiceUnit(lndUser, false)
	case autoUnlockUnitEnabled:
		content = lndServiceUnit(lndUser, true)
	default:
		return errors.New("refusing to write unknown LND unit variant")
	}
	if err := validateExactDir(filepath.Dir(paths.LNDService), 0o755, 0, 0); err != nil {
		return err
	}
	return replaceExactFile(paths.LNDService, []byte(content), 0o644, 0, 0)
}

func (fs *autoUnlockFS) writePassword(password string) error {
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return errors.New("wallet password has an invalid shape")
	}
	if err := validateExactDir(
		filepath.Dir(paths.LNDWalletPassword), 0o750,
		fs.lndUID, fs.lndGID,
	); err != nil {
		return err
	}
	return publishWalletPassword([]byte(password), fs.lndUID, fs.lndGID)
}

func (fs *autoUnlockFS) removePassword() error {
	canonicalErr := removeExactFile(
		paths.LNDWalletPassword, 0o400, fs.lndUID, fs.lndGID)
	stageErr := removeWalletPasswordStage(fs.lndUID, fs.lndGID)
	if canonicalErr != nil {
		return canonicalErr
	}
	return stageErr
}

func publishWalletPassword(data []byte, uid, gid int) error {
	return publishWalletPasswordAt(
		paths.LNDWalletPassword, paths.LNDWalletPasswordStage,
		data, uid, gid,
	)
}

func publishWalletPasswordAt(
	canonical, stage string, data []byte, uid, gid int,
) error {
	if exists, err := passwordStageExistsAt(stage, uid, gid); err != nil {
		return err
	} else if exists {
		return errors.New("staged wallet password already exists")
	}
	if info, err := os.Lstat(canonical); err == nil {
		if err := validateFileInfo(
			canonical, info, false, 0o400, uid, gid,
		); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	f, err := os.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged wallet password: %w", err)
	}
	cleanup := func() {
		f.Close()
		if os.Remove(stage) == nil {
			_ = syncDir(filepath.Dir(stage))
		}
	}
	failed := true
	defer func() {
		if failed {
			cleanup()
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write staged wallet password: %w", err)
	}
	if err := f.Chmod(0o400); err != nil {
		return fmt.Errorf("protect staged wallet password: %w", err)
	}
	if err := f.Chown(uid, gid); err != nil {
		return fmt.Errorf("own staged wallet password: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync staged wallet password: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close staged wallet password: %w", err)
	}
	if err := os.Rename(stage, canonical); err != nil {
		return fmt.Errorf("publish wallet password: %w", err)
	}
	if err := syncDir(filepath.Dir(canonical)); err != nil {
		return err
	}
	failed = false
	return nil
}

func passwordStageExists(uid, gid int) (bool, error) {
	return passwordStageExistsAt(paths.LNDWalletPasswordStage, uid, gid)
}

func passwordStageExistsAt(path string, uid, gid int) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("wallet-password stage is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("wallet-password stage ownership is unavailable")
	}
	mode := info.Mode().Perm()
	rootPhase := int(stat.Uid) == 0 && int(stat.Gid) == 0 &&
		(mode == 0o600 || mode == 0o400)
	lndPhase := int(stat.Uid) == uid && int(stat.Gid) == gid && mode == 0o400
	if !rootPhase && !lndPhase {
		return false, fmt.Errorf(
			"wallet-password stage metadata is %d:%d %04o",
			stat.Uid, stat.Gid, mode)
	}
	return true, nil
}

func removeWalletPasswordStage(uid, gid int) error {
	return removeWalletPasswordStageAt(paths.LNDWalletPasswordStage, uid, gid)
}

func removeWalletPasswordStageAt(path string, uid, gid int) error {
	exists, err := passwordStageExistsAt(path, uid, gid)
	if err != nil {
		return err
	}
	if !exists {
		return syncDir(filepath.Dir(path))
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func (fs *autoUnlockFS) writeVerifyDrop() error {
	parent := filepath.Dir(paths.LNDServiceDropInDir)
	if err := validateExactDir(parent, 0o755, 0, 0); err != nil {
		return err
	}
	if _, err := os.Lstat(paths.LNDServiceDropInDir); os.IsNotExist(err) {
		if err := os.Mkdir(paths.LNDServiceDropInDir, 0o755); err != nil {
			return fmt.Errorf("create LND drop-in directory: %w", err)
		}
		if err := os.Chmod(paths.LNDServiceDropInDir, 0o755); err != nil {
			return fmt.Errorf("set LND drop-in directory mode: %w", err)
		}
		if err := os.Chown(paths.LNDServiceDropInDir, 0, 0); err != nil {
			return fmt.Errorf("own LND drop-in directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect LND drop-in directory: %w", err)
	}
	if err := validateExactDir(paths.LNDServiceDropInDir, 0o755, 0, 0); err != nil {
		return err
	}
	if err := syncDir(parent); err != nil {
		return err
	}
	return replaceExactFile(
		paths.LNDVerificationDropIn, []byte(lndVerificationDropIn),
		0o644, 0, 0,
	)
}

func (fs *autoUnlockFS) removeVerifyDrop() error {
	if err := validateOptionalDropInDir(); err != nil {
		return err
	}
	if err := removeExactFile(paths.LNDVerificationDropIn, 0o644, 0, 0); err != nil {
		return err
	}
	if _, err := os.Lstat(paths.LNDServiceDropInDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(paths.LNDServiceDropInDir); err != nil {
		return fmt.Errorf("remove LND drop-in directory: %w", err)
	}
	return syncDir(filepath.Dir(paths.LNDServiceDropInDir))
}

func validateOptionalDropInDir() error {
	info, err := os.Lstat(paths.LNDServiceDropInDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect LND drop-in directory: %w", err)
	}
	if err := validateFileInfo(paths.LNDServiceDropInDir, info, true, 0o755, 0, 0); err != nil {
		return err
	}
	entries, err := os.ReadDir(paths.LNDServiceDropInDir)
	if err != nil {
		return fmt.Errorf("read LND drop-in directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(paths.LNDVerificationDropIn) {
			return fmt.Errorf("unexpected LND service drop-in %q", entry.Name())
		}
	}
	return nil
}

func validateInstalledLNDUnit() error {
	out, err := system.SudoRunCombinedOutput(
		"systemd-analyze", "verify", paths.LNDService)
	if err != nil {
		return fmt.Errorf("systemd rejected LND unit: %s: %w",
			strings.TrimSpace(out), err)
	}
	return nil
}

func readLNDUnitStatus() (lndUnitStatus, error) {
	properties := []string{
		"InvocationID", "MainPID", "ActiveState", "SubState", "Result",
		"ExecMainStatus", "Restart", "FragmentPath", "DropInPaths",
		"NeedDaemonReload", "ExecStart",
	}
	args := []string{"show", "lnd.service", "--no-pager"}
	for _, property := range properties {
		args = append(args, "--property="+property)
	}
	out, err := system.SudoRunOutput("systemctl", args...)
	if err != nil {
		return lndUnitStatus{}, err
	}
	values := make(map[string]string, len(properties))
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = value
		}
	}
	pid, err := strconv.Atoi(values["MainPID"])
	if err != nil {
		return lndUnitStatus{}, fmt.Errorf("parse LND MainPID %q: %w", values["MainPID"], err)
	}
	drops := []string(nil)
	if values["DropInPaths"] != "" {
		drops = strings.Fields(values["DropInPaths"])
	}
	return lndUnitStatus{
		invocationID: values["InvocationID"], mainPID: pid,
		activeState: values["ActiveState"], subState: values["SubState"],
		result: values["Result"], execMainStatus: values["ExecMainStatus"],
		restart: values["Restart"], fragmentPath: values["FragmentPath"],
		dropInPaths: drops, needDaemonReload: values["NeedDaemonReload"],
		execStart: values["ExecStart"],
	}, nil
}

func readProcessArgs(pid int) ([]string, error) {
	if pid <= 0 {
		return nil, errors.New("LND has no main process")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, fmt.Errorf("read LND process arguments: %w", err)
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts, nil
}

func readLNDWalletState() (lnrpc.WalletState, error) {
	conn, err := directLNDConn()
	if err != nil {
		return lnrpc.WalletState_WAITING_TO_START, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), autoUnlockRPCTimeout)
	defer cancel()
	resp, err := lnrpc.NewStateClient(conn).GetState(ctx, &lnrpc.GetStateRequest{})
	if err != nil {
		return lnrpc.WalletState_WAITING_TO_START, err
	}
	return resp.GetState(), nil
}

func authenticatedLNDGetInfo(network string) error {
	profile, err := config.NetworkConfigFromName(network)
	if err != nil {
		return err
	}
	conn, err := directLNDConn()
	if err != nil {
		return err
	}
	defer conn.Close()
	macaroon, err := os.ReadFile(paths.LNDMacaroon(profile.LNDNetwork))
	if err != nil {
		return fmt.Errorf("read LND admin macaroon: %w", err)
	}
	md := metadata.New(map[string]string{"macaroon": hex.EncodeToString(macaroon)})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	ctx, cancel := context.WithTimeout(ctx, autoUnlockRPCTimeout)
	defer cancel()
	_, err = lnrpc.NewLightningClient(conn).GetInfo(ctx, &lnrpc.GetInfoRequest{})
	return err
}

func directLNDConn() (*grpc.ClientConn, error) {
	certData, err := os.ReadFile(paths.LNDTLSCert)
	if err != nil {
		return nil, fmt.Errorf("read LND TLS certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certData) {
		return nil, errors.New("parse LND TLS certificate")
	}
	return grpc.NewClient(paths.LNDGRPCEndpoint,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, "")))
}

func readExactFile(
	path string, mode os.FileMode, uid, gid int,
) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateFileInfo(path, info, false, mode, uid, gid); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func exactFileExists(path string, mode os.FileMode, uid, gid int) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := validateFileInfo(path, info, false, mode, uid, gid); err != nil {
		return false, err
	}
	return true, nil
}

func validateExactDir(path string, mode os.FileMode, uid, gid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect directory %s: %w", path, err)
	}
	return validateFileInfo(path, info, true, mode, uid, gid)
}

func validateFileInfo(
	path string, info os.FileInfo, wantDir bool,
	mode os.FileMode, uid, gid int,
) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	if wantDir && !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if !wantDir && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm() != mode {
		return fmt.Errorf("%s mode is %04o, want %04o", path, info.Mode().Perm(), mode)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s ownership is unavailable", path)
	}
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		return fmt.Errorf("%s owner is %d:%d, want %d:%d",
			path, stat.Uid, stat.Gid, uid, gid)
	}
	return nil
}

func replaceExactFile(
	path string, data []byte, mode os.FileMode, uid, gid int,
) error {
	if info, err := os.Lstat(path); err == nil {
		if err := validateFileInfo(path, info, false, mode, uid, gid); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	fail := func(stage string, stageErr error) error {
		tmp.Close()
		return fmt.Errorf("%s %s: %w", stage, path, stageErr)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail("chmod", err)
	}
	if err := tmp.Chown(uid, gid); err != nil {
		return fail("chown", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("sync", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish %s: %w", path, err)
	}
	return syncDir(dir)
}

func removeExactFile(path string, mode os.FileMode, uid, gid int) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		// Absence is only a durable fact after the containing directory has
		// been synchronized. This also lets a caller retry a sync that failed
		// after an earlier successful unlink without recreating the file.
		return syncDir(filepath.Dir(path))
	}
	if err != nil {
		return err
	}
	if err := validateFileInfo(path, info, false, mode, uid, gid); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return err
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("%s still exists after removal", path)
		}
		return err
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", path, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}
