package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/paths"
)

func TestWalletExistsAtUsesRegularFileOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallet.db")
	if exists, err := walletExistsAt(path); err != nil || exists {
		t.Fatalf("missing wallet: exists=%v err=%v", exists, err)
	}
	if err := os.WriteFile(path, []byte("wallet"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, err := walletExistsAt(path); err != nil || !exists {
		t.Fatalf("regular wallet: exists=%v err=%v", exists, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatal(err)
	}
	if _, err := walletExistsAt(path); err == nil {
		t.Fatal("symlink accepted as wallet state")
	}
}

func TestSyncthingResidueIncludesStagedCredentials(t *testing.T) {
	seen := make(map[string]bool)
	for _, path := range syncthingResiduePaths {
		seen[path] = true
	}
	for _, path := range []string{
		paths.StateSyncthingAPIKey,
		paths.StateSyncthingWebPassword,
	} {
		if !seen[path] {
			t.Errorf("staged credential %s is not install residue", path)
		}
	}
}

func TestParseSyncthingDevicesUsesCurrentDaemonFacts(t *testing.T) {
	raw := []byte(`[
  {"deviceID":"LOCAL","name":"this node"},
  {"deviceID":"B","name":" Laptop "},
  {"deviceID":"A","name":""},
  {"deviceID":"B","name":"stale duplicate"},
  {"deviceID":" ","name":"invalid"}
]`)
	devices, err := parseSyncthingDevices(raw, "LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %+v", devices)
	}
	if devices[0].DeviceID != "B" || devices[0].Name != "Laptop" ||
		devices[1].DeviceID != "A" || devices[1].Name != "Syncthing device" {
		t.Fatalf("unexpected current device view: %+v", devices)
	}
	if _, err := parseSyncthingDevices([]byte(`{}`), "LOCAL"); err == nil {
		t.Fatal("malformed device list accepted")
	}
}
