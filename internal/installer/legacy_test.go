package installer

import (
	"strings"
	"testing"
)

func TestServiceLayoutConflict(t *testing.T) {
	if err := serviceLayoutConflict(false, "", nil); err != nil {
		t.Fatalf("fresh layout refused: %v", err)
	}
	if err := serviceLayoutConflict(
		true, serviceLayoutMarkerContent, []string{"ignored"}); err != nil {
		t.Fatalf("marked layout refused: %v", err)
	}
	if err := serviceLayoutConflict(true, "wrong\n", nil); err == nil ||
		!strings.Contains(err.Error(), "marker") {
		t.Fatalf("invalid marker error: %v", err)
	}
	err := serviceLayoutConflict(false, "", []string{
		"/var/lib/lnd", "/etc/lnd/lnd.conf",
	})
	if err == nil || !strings.Contains(err.Error(), "migration") ||
		!strings.Contains(err.Error(), "/var/lib/lnd") {
		t.Fatalf("legacy layout error: %v", err)
	}
}

func TestInterruptedFreshInstallRemainsResumable(t *testing.T) {
	// The first invocation is accepted as fresh and writes the
	// marker before any ledger-recorded step. On the next
	// invocation, an early completed ledger entry is therefore
	// authorized by the marker rather than classified as legacy.
	if err := serviceLayoutConflict(false, "", nil); err != nil {
		t.Fatalf("initial fresh-layout guard: %v", err)
	}
	if err := serviceLayoutConflict(true, serviceLayoutMarkerContent,
		[]string{"/etc/vpn/install-state.json"}); err != nil {
		t.Fatalf("interrupted marked install refused resume: %v", err)
	}

	if err := serviceLayoutConflict(false, "",
		[]string{"/etc/vpn/install-state.json"}); err == nil {
		t.Error("unmarked ledger was accepted as a resumable fresh install")
	}
}
