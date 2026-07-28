// internal/installer/certwatch_test.go

package installer

import (
	"strings"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/paths"
)

// The certificate watch is what keeps the staged TLS cert copy
// current when LND regenerates the cert on its own
// (tlsautorefresh at startup) — pin the units' load-bearing
// lines.
func TestLNDCertWatchUnits(t *testing.T) {
	pathUnit, serviceUnit := lndCertWatchUnits()

	// The path unit watches LND's OWN certificate file (the
	// source), not the staged copy, and triggers the stage
	// service.
	for _, want := range []string{
		"PathChanged=" + paths.LNDTLSCert,
		"Unit=" + paths.LNDCertStageServiceName,
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(pathUnit, want) {
			t.Errorf("path unit missing %q:\n%s", want, pathUnit)
		}
	}

	// The service is a oneshot running the installed binary's
	// stage command as root (no User= line).
	for _, want := range []string{
		"Type=oneshot",
		"ExecStart=" + paths.BinaryPath + " stage-lnd-cert",
	} {
		if !strings.Contains(serviceUnit, want) {
			t.Errorf("service unit missing %q:\n%s",
				want, serviceUnit)
		}
	}
	if strings.Contains(serviceUnit, "User=") {
		t.Error("stage service must run as root " +
			"(it writes the root-owned board)")
	}
}
