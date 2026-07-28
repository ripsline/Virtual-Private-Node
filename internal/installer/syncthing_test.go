// internal/installer/syncthing_test.go

package installer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
