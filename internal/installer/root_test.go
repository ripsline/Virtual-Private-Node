package installer

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

const requireRootTestsEnv = "VPN_REQUIRE_ROOT_TESTS"

// requireRootTestEnvironment keeps target-specific ownership tests useful in
// ordinary developer runs while making the declared privileged gate fail
// closed. It never attempts to elevate the test process.
func requireRootTestEnvironment(t *testing.T) {
	t.Helper()

	required := rootTestsRequired(t)

	var reason string
	switch {
	case runtime.GOOS != "linux":
		reason = fmt.Sprintf("requires Linux; running on %s", runtime.GOOS)
	case os.Geteuid() != 0:
		reason = fmt.Sprintf(
			"requires effective UID 0; running with EUID %d", os.Geteuid())
	default:
		return
	}

	if required {
		t.Fatalf("privileged tests were required but cannot run: %s", reason)
	}
	t.Skip(reason)
}

// requireRootTestCapability handles narrower host restrictions discovered
// after the Linux/root prerequisite passes, such as a filesystem that rejects
// ownership changes. Required mode turns those unavailable assertions into
// failures instead of allowing an incomplete privileged gate to pass.
func requireRootTestCapability(t *testing.T, capability string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if rootTestsRequired(t) {
		t.Fatalf("privileged tests require %s: %v", capability, err)
	}
	t.Skipf("requires %s: %v", capability, err)
}

func rootTestsRequired(t *testing.T) bool {
	t.Helper()
	required := os.Getenv(requireRootTestsEnv)
	if required != "" && required != "1" {
		t.Fatalf("%s must be unset or 1, got %q",
			requireRootTestsEnv, required)
	}
	return required == "1"
}
