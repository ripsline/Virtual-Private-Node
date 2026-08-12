//go:build linux

package config

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const configBoundaryChild = "VPN_CONFIG_BOUNDARY_CHILD"

func TestSystemConfigReadableButNotWritableByVPNPrincipal(t *testing.T) {
	if os.Getenv(configBoundaryChild) == "1" {
		testConfigBoundaryChild(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to create the root:vpn ownership boundary")
	}
	const unprivilegedID = 65534
	base := t.TempDir()
	if err := os.Chown(base, 0, unprivilegedID); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EPERM) {
			t.Skipf("filesystem cannot represent test ownership: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o770); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		Dir:      filepath.Join(base, "vpn"),
		Path:     filepath.Join(base, "vpn", "config.json"),
		OwnerUID: 0,
		GroupGID: unprivilegedID,
	}
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(base, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(replacement, unprivilegedID, unprivilegedID); err != nil {
		t.Fatal(err)
	}
	testBinary := filepath.Join(base, "config-boundary-test")
	copyFile(t, os.Args[0], testBinary)
	if err := os.Chown(testBinary, 0, unprivilegedID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(testBinary, 0o750); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testBinary,
		"-test.run=^TestSystemConfigReadableButNotWritableByVPNPrincipal$")
	cmd.Env = append(os.Environ(),
		configBoundaryChild+"=1",
		"VPN_CONFIG_BOUNDARY_PATH="+store.Path,
		"VPN_CONFIG_BOUNDARY_REPLACEMENT="+replacement)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: unprivilegedID, Gid: unprivilegedID,
	}}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged boundary check: %v\n%s", err, output)
	}
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func testConfigBoundaryChild(t *testing.T) {
	path := os.Getenv("VPN_CONFIG_BOUNDARY_PATH")
	store := &Store{
		Dir:              filepath.Dir(path),
		Path:             path,
		OwnerUID:         0,
		GroupGID:         65534,
		RequireRootWrite: true,
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("vpn principal passed the root-only writer gate")
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("vpn principal cannot read config: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o640); err == nil {
		t.Fatal("vpn principal overwrote config")
	}
	if f, err := os.CreateTemp(filepath.Dir(path), ".replace-*"); err == nil {
		f.Close()
		t.Fatal("vpn principal created a replacement in config directory")
	}
	if err := os.Rename(
		os.Getenv("VPN_CONFIG_BOUNDARY_REPLACEMENT"), path); err == nil {
		t.Fatal("vpn principal replaced config by rename")
	}
}
