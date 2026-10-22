package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// Ticket 03 (audit-fixes / P0-05) guard tests: Uninstall must not
// self-deadlock when it re-points the active version. Historically Uninstall
// held the tool lock for its whole span and called Activate (which acquires
// the same tool lock) inside it — the nested acquisition blocked for the
// full 30s lock timeout on both Unix (flock is per open-file-description,
// so a second acquire from the same process hangs) and Windows (the named
// mutex is reentrant only per-thread; goroutine migration hangs the same
// way). The fix releases the uninstall lock before the re-Activate, mirroring
// the InstallWithProgress critical section (Spec 037).
//
// The wall-clock guard turns a regression back into a fast, visible failure
// (< 2s vs the ~30s deadlock stall).

// makeFakeInstalledVersion creates install/<tool>/<ver> with a stub binary
// so the dir looks like a real install tree.
func makeFakeInstalledVersion(t *testing.T, cfgInstallDir, tool, ver string) string {
	t.Helper()
	dir := filepath.Join(cfgInstallDir, tool, ver)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	binName := tool
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte("stub"), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	return dir
}

// U1: uninstalling the ACTIVE version with another version installed must
// re-point quickly (< 2s, not the ~30s deadlock stall) and leave the
// current link on the remaining version with working shims.
func TestUninstall_ActivatedVersion_RepointsFast(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	vB := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "2.0.0")

	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, vA, "1.0.0"); err != nil {
		t.Fatalf("Activate A: %v", err)
	}

	start := time.Now()
	if err := ins.Uninstall(p, "1.0.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("Uninstall of active version took %v — nested tool-lock deadlock regression (expected < 2s)", elapsed)
	}

	// current link re-pointed to B
	got, ok := util.ActiveVersion(cfg.Core.XMDevDir, "fakego")
	if !ok || got != "2.0.0" {
		t.Errorf("active version after re-point = %q (ok=%v), want 2.0.0", got, ok)
	}
	// the old install tree is gone, the new one remains
	if _, err := os.Stat(vA); !os.IsNotExist(err) {
		t.Errorf("uninstalled version dir still exists: %v", err)
	}
	if _, err := os.Stat(vB); err != nil {
		t.Errorf("remaining version dir missing: %v", err)
	}
	// shims still installed (Activate re-created them)
	shimName := "fakego"
	if runtime.GOOS == "windows" {
		shimName += ".exe"
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.ShimDir, shimName)); err != nil {
		t.Errorf("shim missing after re-point: %v", err)
	}
}

// U2: uninstalling the LAST (active) version must remove the current link
// and the shims without any deadlock.
func TestUninstall_LastVersion_RemovesLink(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, vA, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	start := time.Now()
	if err := ins.Uninstall(p, "1.0.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("Uninstall of last version took %v — lock regression (expected < 2s)", elapsed)
	}

	// current link removed
	link := filepath.Join(cfg.Core.XMDevDir, "current", "fakego")
	if _, err := os.Lstat(link); err == nil {
		t.Error("current/fakego link should be removed after uninstalling the last version")
	}
	// shims removed (no other version keeps them)
	shimName := "fakego"
	if runtime.GOOS == "windows" {
		shimName += ".exe"
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.ShimDir, shimName)); err == nil {
		t.Error("shim should be removed after uninstalling the last version")
	}
	if _, err := os.Stat(vA); !os.IsNotExist(err) {
		t.Errorf("uninstalled version dir still exists: %v", err)
	}
}
