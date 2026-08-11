package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	runLockHelperEnv  = "VPN_TEST_RUN_LOCK_HELPER"
	runLockHelperDir  = "VPN_TEST_RUN_LOCK_DIR"
	runLockHelperPath = "VPN_TEST_RUN_LOCK_PATH"
	runLockHelperWant = "VPN_TEST_RUN_LOCK_WANT"
)

func TestRunLockSubprocessHelper(t *testing.T) {
	if os.Getenv(runLockHelperEnv) != "1" {
		return
	}
	dir := os.Getenv(runLockHelperDir)
	lock := os.Getenv(runLockHelperPath)
	want := os.Getenv(runLockHelperWant)
	got, err := acquireRunLock(dir, lock)
	switch want {
	case "blocked":
		if err == nil {
			got.Close()
			t.Fatal("subprocess acquired lock held by parent")
		}
		if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("unexpected contention error: %v", err)
		}
	case "acquired":
		if err != nil {
			t.Fatalf("subprocess did not acquire released lock: %v", err)
		}
		if err := got.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper expectation %q", want)
	}
}

func runLockSubprocess(t *testing.T, dir, lock, want string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunLockSubprocessHelper$")
	cmd.Env = append(os.Environ(),
		runLockHelperEnv+"=1",
		runLockHelperDir+"="+dir,
		runLockHelperPath+"="+lock,
		runLockHelperWant+"="+want,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock subprocess (%s): %v\n%s", want, err, output)
	}
}

func TestRootRunLockSurvivesLedgerReplacement(t *testing.T) {
	requireRootTestEnvironment(t)
	root := t.TempDir()
	dir := filepath.Join(root, "runtime")
	lock := filepath.Join(dir, "install.lock")
	ledger := filepath.Join(root, "install-state.json")

	first, err := acquireRunLock(dir, lock)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		tmp := filepath.Join(root, "replacement")
		if err := os.WriteFile(tmp, []byte{byte(i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, ledger); err != nil {
			t.Fatal(err)
		}
		runLockSubprocess(t, dir, lock, "blocked")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	runLockSubprocess(t, dir, lock, "acquired")
	afterLock, err := os.Stat(lock)
	if err != nil {
		t.Fatalf("stable lock path was removed: %v", err)
	}
	if !os.SameFile(beforeLock, afterLock) {
		t.Fatal("stable lock object was replaced")
	}
}

func TestRootRunLockRefusesUnsafeObjects(t *testing.T) {
	requireRootTestEnvironment(t)
	cases := []struct {
		name string
		seed func(*testing.T, string, string)
	}{
		{"runtime symlink", func(t *testing.T, dir, _ string) {
			target := filepath.Join(filepath.Dir(dir), "target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, dir); err != nil {
				t.Fatal(err)
			}
		}},
		{"runtime mode", func(t *testing.T, dir, _ string) {
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock symlink", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("target", lock); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock directory", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(lock, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock fifo", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(lock, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock mode", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock owner", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			requireRootTestCapability(t, "changing file ownership",
				os.Chown(lock, 1, 1))
		}},
		{"lock hardlink", func(t *testing.T, dir, lock string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(lock, lock+".second"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "runtime")
			lock := filepath.Join(dir, "install.lock")
			tt.seed(t, dir, lock)
			if f, err := acquireRunLock(dir, lock); err == nil {
				f.Close()
				t.Fatal("unsafe lock object accepted")
			}
		})
	}
}
