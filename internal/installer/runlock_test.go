package installer

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

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
	for i := 0; i < 10; i++ {
		tmp := filepath.Join(root, "replacement")
		if err := os.WriteFile(tmp, []byte{byte(i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, ledger); err != nil {
			t.Fatal(err)
		}
		if second, err := acquireRunLock(dir, lock); err == nil {
			second.Close()
			t.Fatal("second lock acquired while first remained held")
		} else if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("unexpected contention error: %v", err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := acquireRunLock(dir, lock)
	if err != nil {
		t.Fatalf("lock did not release on close: %v", err)
	}
	third.Close()
	if _, err := os.Lstat(lock); err != nil {
		t.Fatalf("stable lock path was removed: %v", err)
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
