package hostsnapshot

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// RunWithPinnedHome pins XMDEV_HOME to a throwaway directory for the whole
// test binary and then runs the suite under the default host-pollution
// snapshot. It is the recommended TestMain body for any package whose tests
// can reach the real home-resolution / shim paths:
//
//	func TestMain(m *testing.M) { os.Exit(hostsnapshot.RunWithPinnedHome(m)) }
//
// Why the pin is required, not merely nice: config.ResolveHomeDir memoizes its
// answer in a package-level sync.Once. A single call made while XMDEV_HOME is
// unset resolves (and WRITES a cache into) the developer's REAL xdev home, and
// from then on the cached value wins even in tests that do set XMDEV_HOME, so
// every later shim/cache write lands in the real home. That is exactly how a
// real E:\XDev\config.yaml was overwritten during the audit-fixes batch.
//
// The snapshot options are computed BEFORE pinning so that the audit keeps
// watching the real home (see DefaultOptions/realHomePaths); child processes
// inherit os.Environ(), so the pin covers them too.
func RunWithPinnedHome(m *testing.M) int {
	opts := DefaultOptions()

	home, err := os.MkdirTemp("", "xdev-test-home-")
	if err == nil {
		if serr := os.Setenv("XMDEV_HOME", home); serr != nil {
			_ = os.RemoveAll(home)
			home = ""
		}
	}
	if home != "" {
		// Drop any home latched before the pin (package-level init, TestMain
		// ordering) so the throwaway home really takes effect.
		config.ResetHomeDirForTest()
	}

	code := RunWithHostSnapshot(m, opts)

	if home != "" {
		_ = os.RemoveAll(home)
	}
	return code
}
