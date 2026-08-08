// internal/paths/paths_test.go

package paths

import (
	"net"
	"strings"
	"testing"
)

// The loopback endpoints must be literal IPv4 addresses — never
// a host name. The node disables IPv6 while /etc/hosts (a file
// we do not own) still maps localhost to ::1, so an endpoint
// that regressed to a name could resolve to an address the box
// cannot use. Pinning the literal here keeps that regression
// impossible for every consumer at once.
func TestLoopbackEndpointsAreLiteralIPv4(t *testing.T) {
	endpoints := map[string]string{
		"LNDGRPCEndpoint": LNDGRPCEndpoint,
		"LNDRESTEndpoint": LNDRESTEndpoint,
		"LNDP2PBind":      LNDP2PBind,
	}
	for name, ep := range endpoints {
		host, port, err := net.SplitHostPort(ep)
		if err != nil {
			t.Errorf("%s = %q: not host:port: %v", name, ep, err)
			continue
		}
		if host != "127.0.0.1" {
			t.Errorf("%s = %q: host is %q, want the literal "+
				"IPv4 loopback address 127.0.0.1", name, ep, host)
		}
		if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
			t.Errorf("%s = %q: host %q is not a literal IPv4 "+
				"address", name, ep, host)
		}
		if port == "" {
			t.Errorf("%s = %q: missing port", name, ep)
		}
	}
}

func TestServiceLayoutMarkerHasRootOwnedParent(t *testing.T) {
	if !strings.HasPrefix(ServiceLayoutMarker, VarLibVPN+"/") {
		t.Errorf("service-layout marker %q is not below %q",
			ServiceLayoutMarker, VarLibVPN)
	}
	if strings.HasPrefix(ServiceLayoutMarker, ConfigDir+"/") {
		t.Errorf("service-layout marker %q is below admin-owned %q",
			ServiceLayoutMarker, ConfigDir)
	}
}

func TestLNDBackupUsesProjectOwnedExportBoundary(t *testing.T) {
	for name, got := range map[string]string{
		"stage":  LNDBackupStage,
		"export": LNDBackupExport,
	} {
		if !strings.HasPrefix(got, ExportDir+"/") {
			t.Errorf("%s path %q is not below export boundary %q",
				name, got, ExportDir)
		}
		if strings.HasPrefix(got, SyncthingDataDir+"/") {
			t.Errorf("%s path %q is below Syncthing private state",
				name, got)
		}
	}
	if pathDir := strings.TrimSuffix(
		LNDBackupStage, "/lnd-backup-stage"); pathDir != ExportDir {
		t.Errorf("stage and export boundary disagree: %q, %q",
			pathDir, ExportDir)
	}
	if BackupExportService !=
		"/etc/systemd/system/lnd-backup-export.service" {
		t.Errorf("backup export service path is %q",
			BackupExportService)
	}
}
