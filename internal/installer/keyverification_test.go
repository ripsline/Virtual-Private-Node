package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyVerificationMarkerLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending")
	uid := os.Geteuid()
	if err := ensureKeyVerificationPendingAt(path, uid); err != nil {
		t.Fatal(err)
	}
	if err := ensureKeyVerificationPendingAt(path, uid); err != nil {
		t.Fatalf("idempotent ensure: %v", err)
	}
	pending, err := keyVerificationPendingAt(path, uid)
	if err != nil || !pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %04o, want 0600", info.Mode().Perm())
	}
	if err := clearKeyVerificationPendingAt(path, uid); err != nil {
		t.Fatal(err)
	}
	pending, err = keyVerificationPendingAt(path, uid)
	if err != nil || pending {
		t.Fatalf("after clear pending=%v err=%v", pending, err)
	}
}

func TestKeyVerificationMarkerRefusesUnsafeObjects(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pending")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureKeyVerificationPendingAt(link, os.Geteuid()); err == nil {
		t.Fatal("accepted symlink marker")
	}
	if _, err := keyVerificationPendingAt(link, os.Geteuid()); err == nil {
		t.Fatal("read symlink marker as pending")
	}
	if err := clearKeyVerificationPendingAt(link, os.Geteuid()); err == nil {
		t.Fatal("removed symlink marker")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "keep" {
		t.Fatal("symlink target changed")
	}
}
