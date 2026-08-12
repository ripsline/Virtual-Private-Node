//go:build linux

package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	configBoundaryChild = "VPN_CONFIG_BOUNDARY_CHILD"
	configBoundaryUID   = 65534
	configBoundaryGID   = 65534
)

func TestSystemConfigReadableButNotWritableByVPNPrincipal(t *testing.T) {
	if os.Getenv(configBoundaryChild) == "1" {
		testConfigBoundaryChild(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root to create the root:vpn ownership boundary")
	}
	// T.TempDir has a root-only parent that the credential-dropped child
	// cannot traverse. Create the test directory directly under the system
	// temporary directory so every ancestor required by the child is reachable.
	base, err := os.MkdirTemp("", "vpn-config-boundary-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove boundary test directory: %v", err)
		}
	})
	if err := os.Chown(base, 0, configBoundaryGID); err != nil {
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
		GroupGID: configBoundaryGID,
	}
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(base, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(replacement, configBoundaryUID, configBoundaryGID); err != nil {
		t.Fatal(err)
	}
	testBinary := filepath.Join(base, "config-boundary-test")
	copyFile(t, os.Args[0], testBinary)
	if err := os.Chown(testBinary, 0, configBoundaryGID); err != nil {
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
		"VPN_CONFIG_BOUNDARY_REPLACEMENT="+replacement,
		"VPN_CONFIG_BOUNDARY_CONTROL="+filepath.Join(base, "child-writable"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid:    configBoundaryUID,
		Gid:    configBoundaryGID,
		Groups: []uint32{configBoundaryGID},
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
	if os.Getuid() != configBoundaryUID || os.Geteuid() != configBoundaryUID ||
		os.Getgid() != configBoundaryGID || os.Getegid() != configBoundaryGID {
		t.Fatalf("child identity is uid=%d euid=%d gid=%d egid=%d, want %d:%d",
			os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid(),
			configBoundaryUID, configBoundaryGID)
	}
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatalf("read child supplementary groups: %v", err)
	}
	if len(groups) != 1 || groups[0] != configBoundaryGID {
		t.Fatalf("child supplementary groups are %v, want [%d]", groups, configBoundaryGID)
	}
	if err := os.WriteFile(
		os.Getenv("VPN_CONFIG_BOUNDARY_CONTROL"), []byte("writable"), 0o600); err != nil {
		t.Fatalf("boundary harness is not writable by child: %v", err)
	}

	path := os.Getenv("VPN_CONFIG_BOUNDARY_PATH")
	store := &Store{
		Dir:              filepath.Dir(path),
		Path:             path,
		OwnerUID:         0,
		GroupGID:         configBoundaryGID,
		RequireRootWrite: true,
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("vpn principal cannot read config: %v", err)
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("vpn principal passed the root-only writer gate")
	}
	requirePermissionDenied(t, "overwrite config",
		os.WriteFile(path, []byte("changed"), 0o640))
	if f, err := os.CreateTemp(filepath.Dir(path), ".replace-*"); err == nil {
		f.Close()
		t.Fatal("vpn principal created a replacement in config directory")
	} else {
		requirePermissionDenied(t, "create replacement in config directory", err)
	}
	requirePermissionDenied(t, "replace config by rename", os.Rename(
		os.Getenv("VPN_CONFIG_BOUNDARY_REPLACEMENT"), path))
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after rejected mutations: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("config changed after rejected mutations")
	}
}

func requirePermissionDenied(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("vpn principal could %s", operation)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("%s failed for the wrong reason: %v", operation, err)
	}
}
