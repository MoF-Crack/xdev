// activate_shim_validation_test.go — F-26 guards.
//
// Pre-fix Activate validated nothing about the shim names it iterated: the
// FIRST invalid name aborted the whole loop with a hard error, so the
// remaining valid shims were never installed and activation was rolled back
// entirely. removeShims already had the right behaviour for the same data
// (skip the invalid name with a WARNING and keep going) — Activate did not.
package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestActivate_SkipsInvalidShimAndInstallsRest asserts an invalid shim name is
// skipped (with a warning) while the remaining valid shims are still created.
func TestActivate_SkipsInvalidShimAndInstallsRest(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	// The invalid name must not abort the loop for the valid ones that follow
	// it — put the bad name FIRST to prove the loop continues past it.
	p := &fakePlugin{
		name:  "mixedshim",
		shims: []string{"../escape", "goodshim", "also.good"},
	}
	installPath := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "mixedshim", "1.0.0")

	if err := ins.Activate(p, installPath, "1.0.0"); err != nil {
		t.Fatalf("Activate returned an error for a plugin with one invalid shim: %v", err)
	}

	// The valid shims must exist (Windows also writes <name>.exe).
	for _, name := range []string{"goodshim", "also.good"} {
		candidates := []string{filepath.Join(cfg.Core.ShimDir, name)}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(cfg.Core.ShimDir, name+".exe"))
		}
		found := false
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("valid shim %q was not installed (an invalid sibling aborted the loop)", name)
		}
	}

	// The invalid name must not have created anything (inside or outside).
	if _, err := os.Stat(filepath.Join(cfg.Core.ShimDir, "..", "escape")); err == nil {
		t.Error("invalid shim name escaped the shim directory")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Core.ShimDir), "escape")); err == nil {
		t.Error("invalid shim name created an entry outside the shim directory")
	}

	// The link must still be in place: skipping an invalid shim is not a
	// reason to roll activation back.
	link := filepath.Join(cfg.Core.XMDevDir, "current", "mixedshim")
	if _, err := os.Readlink(link); err != nil {
		t.Errorf("current/mixedshim link missing after activation with a skipped shim: %v", err)
	}
}

// TestActivate_AllShimsInvalidStillActivates asserts a plugin whose shim list
// is entirely invalid does not fail activation (the link is valid; only the
// unusable shim entries are skipped).
func TestActivate_AllShimsInvalidStillActivates(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "badshims", shims: []string{"../a", "b/c", ".."}}
	installPath := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "badshims", "1.0.0")

	if err := ins.Activate(p, installPath, "1.0.0"); err != nil {
		t.Fatalf("Activate failed although every shim was merely invalid (skippable): %v", err)
	}
	link := filepath.Join(cfg.Core.XMDevDir, "current", "badshims")
	if _, err := os.Readlink(link); err != nil {
		t.Errorf("current/badshims link missing: %v", err)
	}
}

// TestActivate_ValidShimsUnaffected guards the normal path: with only valid
// names nothing changes.
func TestActivate_ValidShimsUnaffected(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "okshims", shims: []string{"ok1", "ok2"}}
	installPath := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "okshims", "1.0.0")

	if err := ins.Activate(p, installPath, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	for _, name := range []string{"ok1", "ok2"} {
		candidates := []string{filepath.Join(cfg.Core.ShimDir, name)}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(cfg.Core.ShimDir, name+".exe"))
		}
		found := false
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("valid shim %q missing", name)
		}
	}
}

// TestActivate_ShimValidationMatchesRemoveShims pins the alignment: the same
// names removeShims skips must be the ones Activate skips.
func TestActivate_ShimValidationMatchesRemoveShims(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "parity", shims: []string{"../x", "valid"}}
	installPath := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "parity", "1.0.0")

	if err := ins.Activate(p, installPath, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	// removeShims must not choke on the same list (it skips + continues).
	ins.removeShims(p)

	// After removal only the invalid entry's (nonexistent) path remains
	// untouched — the valid shim is gone.
	candidates := []string{filepath.Join(cfg.Core.ShimDir, "valid")}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(cfg.Core.ShimDir, "valid.exe"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			t.Errorf("valid shim %q still present after removeShims", c)
		}
	}
	// Sanity: no stray entry outside the shim dir.
	if entries, err := os.ReadDir(cfg.Core.XMDevDir); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "x") && e.Name() == "x" {
				t.Errorf("invalid shim name produced a stray entry: %s", e.Name())
			}
		}
	}
}
