// internal/installer/syncthing_test.go

package installer

import (
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
		paths.SyncthingDataDir: {
			owner: "syncthing:vpn-lnd-backup", mode: 0710,
		},
		paths.SyncthingBackupStage: {
			owner: "lnd:lnd", mode: 0700,
		},
		paths.SyncthingBackup: {
			owner: "syncthing:vpn-lnd-backup", mode: 0770,
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
	source := "/var/lib/lnd/data/chain/bitcoin/mainnet/channel.backup"
	dest := "/var/lib/syncthing/lnd-backup/channel.backup"
	stage := "/var/lib/syncthing/lnd-backup-stage/channel.backup.tmp"
	pathUnit, copyUnit := channelBackupUnits(source, stage, dest)
	if !strings.Contains(pathUnit, "PathChanged="+source) {
		t.Error("path unit watches the wrong source")
	}
	for _, want := range []string{
		"User=lnd",
		"Group=lnd",
		"SupplementaryGroups=vpn-lnd-backup",
		"UMask=0027",
		"-m 0640 -g vpn-lnd-backup",
		stage,
		"/usr/bin/mv -f " + stage + " " + dest,
	} {
		if !strings.Contains(copyUnit, want) {
			t.Errorf("backup copy unit missing %q", want)
		}
	}
	if strings.Contains(copyUnit, "debian-tor") {
		t.Error("backup copy unit has Tor control-cookie access")
	}
	if strings.Contains(copyUnit, dest+".tmp") {
		t.Error("backup temp file is staged inside the synchronized folder")
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
