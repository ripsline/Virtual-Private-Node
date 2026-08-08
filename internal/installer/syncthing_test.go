// internal/installer/syncthing_test.go

package installer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/paths"
)

// The Syncthing REST transport must fail on HTTP errors: the
// old curl transport exited 0 on a 403, which once reported a
// pairing as done when the daemon had rejected the API key.
// These tests run the real transport against a local test
// server and pin both directions: success passes the body
// through, and every error status is a named error.
func TestSyncthingAPI(t *testing.T) {
	var gotMethod, gotKey, gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotKey = r.Header.Get("X-API-Key")
			gotType = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			switch r.URL.Path {
			case "/ok":
				w.Write([]byte(`{"ok":true}`))
			case "/empty":
				w.WriteHeader(http.StatusNoContent)
			case "/forbidden":
				w.WriteHeader(http.StatusForbidden)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	defer srv.Close()
	old := syncthingGUIBase
	syncthingGUIBase = srv.URL
	defer func() { syncthingGUIBase = old }()

	// Success: body through, headers set, request body sent.
	body, err := syncthingAPI("POST", "key123", "/ok", `{"a":1}`)
	if err != nil {
		t.Fatalf("POST /ok: %v", err)
	}
	if body != `{"ok":true}` {
		t.Errorf("POST /ok body: got %q", body)
	}
	if gotMethod != "POST" || gotKey != "key123" ||
		gotType != "application/json" || gotBody != `{"a":1}` {
		t.Errorf("request not as sent: method=%q key=%q type=%q body=%q",
			gotMethod, gotKey, gotType, gotBody)
	}

	// A bodyless 2xx (some DELETE/POST answers) is success.
	if _, err := syncthingAPI(
		"DELETE", "key123", "/empty", ""); err != nil {
		t.Errorf("DELETE /empty: %v", err)
	}
	if gotBody != "" || gotType != "" {
		t.Errorf("empty-body request carried body=%q type=%q",
			gotBody, gotType)
	}

	// The load-bearing case: an HTTP error IS an error, and a
	// 403 names the API-key remedy.
	_, err = syncthingAPI("GET", "stale-key", "/forbidden", "")
	if err == nil {
		t.Fatal("403 answer reported as success")
	}
	if !strings.Contains(err.Error(), "HTTP 403") ||
		!strings.Contains(err.Error(), "API key") {
		t.Errorf("403 error lacks status or hint: %v", err)
	}

	// Any other error status also fails, naming the call.
	_, err = syncthingAPI("GET", "key123", "/missing", "")
	if err == nil {
		t.Fatal("404 answer reported as success")
	}
	if !strings.Contains(err.Error(), "HTTP 404") ||
		!strings.Contains(err.Error(), "GET /missing") {
		t.Errorf("404 error lacks status or call name: %v", err)
	}
}

// A daemon that is not there is a transport error, not a
// silent success.
func TestSyncthingAPIDaemonDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // deliberately: the address now refuses
	old := syncthingGUIBase
	syncthingGUIBase = srv.URL
	defer func() { syncthingGUIBase = old }()

	if _, err := syncthingAPI("GET", "k", "/ok", ""); err == nil {
		t.Error("unreachable daemon reported as success")
	}
}

func TestSyncthingServiceIdentityBoundary(t *testing.T) {
	unit := syncthingServiceUnit()
	for _, want := range []string{
		"User=syncthing",
		"Group=syncthing",
		"SupplementaryGroups=vpn-lnd-backup",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("Syncthing unit missing %q", want)
		}
	}
	if strings.Contains(unit, "debian-tor") {
		t.Error("Syncthing unit has Tor control-cookie access")
	}
}

func TestSyncthingDirectoryIdentityBoundary(t *testing.T) {
	specs := make(map[string]syncthingDirSpec)
	for _, spec := range syncthingDirSpecs() {
		specs[spec.path] = spec
	}
	for path, want := range map[string]syncthingDirSpec{
		paths.SyncthingDir: {
			owner: "syncthing:syncthing", mode: 0700,
		},
		paths.SyncthingDataDir: {
			owner: "syncthing:syncthing", mode: 0700,
		},
		paths.ExportDir: {
			owner: "root:vpn-lnd-backup", mode: 0750,
		},
		paths.LNDBackupStage: {
			owner: "lnd:lnd", mode: 0700,
		},
		paths.LNDBackupExport: {
			owner: "lnd:vpn-lnd-backup", mode: 0750,
		},
	} {
		got, ok := specs[path]
		if !ok {
			t.Errorf("missing directory spec for %s", path)
			continue
		}
		if got.owner != want.owner || got.mode != want.mode {
			t.Errorf("%s: got %s %04o, want %s %04o",
				path, got.owner, got.mode, want.owner, want.mode)
		}
	}
}

func TestChannelBackupUnitIdentityBoundary(t *testing.T) {
	source := paths.ChannelBackup("mainnet")
	pathUnit, exportUnit, err := channelBackupUnits("mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pathUnit, "PathChanged="+source) {
		t.Error("path unit watches the wrong source")
	}
	if !strings.Contains(pathUnit,
		"Unit=lnd-backup-export.service") {
		t.Error("path unit does not target the backup export service")
	}
	for _, want := range []string{
		"User=lnd",
		"Group=lnd",
		"SupplementaryGroups=vpn-lnd-backup",
		"UMask=0027",
		"ExecStart=" + paths.BinaryPath +
			" publish-lnd-backup mainnet",
	} {
		if !strings.Contains(exportUnit, want) {
			t.Errorf("backup export unit missing %q", want)
		}
	}
	if strings.Contains(exportUnit, "debian-tor") {
		t.Error("backup export unit has Tor control-cookie access")
	}
	for _, forbidden := range []string{
		"/usr/bin/install", "/usr/bin/mv", source,
		paths.LNDBackupStage, paths.LNDBackupExport,
	} {
		if strings.Contains(exportUnit, forbidden) {
			t.Errorf("backup unit exposes implementation path/command %q",
				forbidden)
		}
	}
	if got := strings.Count(exportUnit, "UMask=0027"); got != 1 {
		t.Errorf("backup unit has %d publisher umasks, want 1", got)
	}
	if strings.Contains(exportUnit, "UMask=0077") {
		t.Error("publisher inherited the private-daemon umask")
	}
	if _, _, err := channelBackupUnits("mainnet /tmp/source"); err == nil {
		t.Error("backup unit accepted a caller-supplied path as a network")
	}
}

func TestBackupFolderRegisteredRequiresExactID(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"exact":      {`[{"id":"lnd-backup","label":"Backup"}]`, true},
		"label only": {`[{"id":"documents","label":"old lnd-backup files"}]`, false},
		"longer ID":  {`[{"id":"lnd-backup-old"}]`, false},
		"absent":     {`[{"id":"documents"}]`, false},
	} {
		got, err := backupFolderRegistered(tc.body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
	if _, err := backupFolderRegistered(`not json`); err == nil {
		t.Error("malformed folder response accepted")
	}
}

func TestBackupFolderConfigUsesReadOnlyProjectExport(t *testing.T) {
	var folder struct {
		ID      string `json:"id"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		Devices []struct {
			DeviceID string `json:"deviceID"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(
		[]byte(renderBackupFolderConfig("LOCAL-ID")), &folder); err != nil {
		t.Fatal(err)
	}
	if folder.ID != "lnd-backup" ||
		folder.Path != paths.LNDBackupExport ||
		folder.Type != "sendonly" || len(folder.Devices) != 1 ||
		folder.Devices[0].DeviceID != "LOCAL-ID" {
		t.Errorf("unexpected folder config: %+v", folder)
	}
	if strings.HasPrefix(folder.Path, paths.SyncthingDataDir+"/") {
		t.Errorf("folder path %q is below Syncthing private state",
			folder.Path)
	}
}
