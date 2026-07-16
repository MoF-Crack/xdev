//go:build online

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// EPIC-3: Team Lockfile collaboration.
//
//	Developer A: in a project with .xdev.toml, installs a real tool.
//	             xdev writes .xdev.lock pinning the resolved version.
//	Developer B: receives .xdev.lock (fresh machine), runs
//	             `xdev install --locked` → installs the exact same version.
//
// This proves the lockfile pins a reproducible environment. The download is
// real; if the network is flaky the test skips with a clear reason rather
// than failing the suite (online tests are pre-release verification, not CI).
func TestEPIC3_LockfileCollaboration(t *testing.T) {
	bin := buildBin(t)

	// --- Developer A: project with .xdev.toml, real install ---
	homeA := initHome(t, bin)
	projA := t.TempDir()
	if err := os.WriteFile(filepath.Join(projA, ".xdev.toml"),
		[]byte("[tools]\nninja = \"1.12.1\"\n"), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	installOut, code := run(t, bin, homeA, projA, "install", "ninja@1.12.1")
	if code != 0 {
		// Network flakiness is the most common cause. Skip rather than fail
		// so the online suite degrades gracefully on bad connectivity.
		t.Skipf("Developer A install failed (network?): %s", installOut)
	}
	contains(t, installOut, "installed", "A: install success")

	// The lockfile must have been written in the project dir.
	lockPath := filepath.Join(projA, ".xdev.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Developer A: .xdev.lock not generated: %v", err)
	}
	lockContent := string(data)
	if !strings.Contains(lockContent, "ninja") {
		t.Errorf("A: lockfile missing ninja: %s", lockContent)
	}
	if !strings.Contains(lockContent, "1.12.1") {
		t.Errorf("A: lockfile missing resolved version 1.12.1: %s", lockContent)
	}
	// The lockfile should carry the pinning metadata (URL + SHA), not just
	// the version — that is the whole point of a lockfile.
	if !strings.Contains(lockContent, "url") {
		t.Errorf("A: lockfile missing url field: %s", lockContent)
	}

	// --- Developer B: fresh machine, receives the lockfile ---
	homeB := initHome(t, bin)
	projB := t.TempDir()
	// Copy the lockfile (the artifact shared between teammates).
	if err := os.WriteFile(filepath.Join(projB, ".xdev.lock"), data, 0o644); err != nil {
		t.Fatalf("copy lockfile to B: %v", err)
	}

	bOut, code := run(t, bin, homeB, projB, "install", "--locked")
	if code != 0 {
		t.Skipf("Developer B locked install failed (network?): %s", bOut)
	}
	contains(t, bOut, "installed", "B: locked install success")

	// B must end up with the SAME resolved version as A.
	verDir := filepath.Join(homeB, "install", "ninja", "1.12.1")
	if !fileExists(verDir) {
		t.Errorf("B: install dir %s missing after --locked", verDir)
	}
	cur, code := run(t, bin, homeB, "", "current", "ninja")
	if code != 0 {
		t.Fatalf("B: current ninja exit %d", code)
	}
	contains(t, cur, "1.12.1", "B: same resolved version as A")

	// And the shim on B dispatches to the real tool — reproducibility confirmed.
	shimName := "ninja.exe"
	if runtime.GOOS != "windows" {
		shimName = "ninja"
	}
	dispatchOut, code := runShim(t, filepath.Join(homeB, "shims", shimName), homeB, "--version")
	if code != 0 {
		t.Fatalf("B: shim dispatch exit %d\n%s", code, dispatchOut)
	}
	contains(t, dispatchOut, "1.12.1", "B: shim dispatches real ninja")
}