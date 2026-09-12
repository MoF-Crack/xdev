//go:build windows

// Guard tests for CleanupStaleGOROOT (audit-fixes ticket 01 / X-07).
//
// These tests write real HKCU\Environment values; they use the same
// save/restore pattern as the CleanupUserPath tests in win_path_test.go so
// the user's original GOROOT value is always restored, even when absent
// before the test (restore then deletes the probe value).
package env

import (
	"os"
	"path/filepath"
	"testing"
)

// saveGOROOT returns the current GOROOT registry value ("" when absent).
func saveGOROOT(t *testing.T) string {
	t.Helper()
	v, err := GetUserEnvVar("GOROOT")
	if err != nil {
		return ""
	}
	return v
}

// restoreGOROOT puts the saved value back; "" means the value must be absent
// after the test (DeleteUserEnvVar is idempotent — FILE_NOT_FOUND is OK).
func restoreGOROOT(t *testing.T, original string) {
	t.Helper()
	if original == "" {
		if err := DeleteUserEnvVar("GOROOT"); err != nil {
			t.Fatalf("failed to delete GOROOT during restore: %v", err)
		}
		return
	}
	if err := SetUserEnvVar("GOROOT", original); err != nil {
		t.Fatalf("failed to restore GOROOT=%q: %v", original, err)
	}
}

// G1: the exact historical bug layout — GOROOT points at the parent of the
// real toolchain (strip:0 keeps the archive's top-level go/ directory).
func TestCleanupStaleGOROOT_RemovesWrongLayer(t *testing.T) {
	orig := saveGOROOT(t)
	defer restoreGOROOT(t, orig)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "go", "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SetUserEnvVar("GOROOT", root); err != nil {
		t.Fatalf("SetUserEnvVar: %v", err)
	}

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	if _, err := GetUserEnvVar("GOROOT"); err == nil {
		t.Errorf("stale GOROOT=%q should have been deleted from the registry", root)
	}
}

// G2: a value that is itself a valid GOROOT (pkg/tool exists) must be kept —
// manual installs and multi-GOROOT setups must never be clobbered.
func TestCleanupStaleGOROOT_KeepsValidValue(t *testing.T) {
	orig := saveGOROOT(t)
	defer restoreGOROOT(t, orig)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SetUserEnvVar("GOROOT", root); err != nil {
		t.Fatalf("SetUserEnvVar: %v", err)
	}

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	got, err := GetUserEnvVar("GOROOT")
	if err != nil {
		t.Fatalf("valid GOROOT was deleted: %v", err)
	}
	if got != root {
		t.Errorf("GOROOT = %q, want preserved %q", got, root)
	}
}

// G3: unknown provenance (neither V nor V\go looks like a GOROOT) —
// conservatively keep the value.
func TestCleanupStaleGOROOT_KeepsUnknown(t *testing.T) {
	orig := saveGOROOT(t)
	defer restoreGOROOT(t, orig)

	root := t.TempDir() // exists but contains no pkg/tool anywhere
	if err := SetUserEnvVar("GOROOT", root); err != nil {
		t.Fatalf("SetUserEnvVar: %v", err)
	}

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	got, err := GetUserEnvVar("GOROOT")
	if err != nil {
		t.Fatalf("GOROOT of unknown origin was deleted: %v", err)
	}
	if got != root {
		t.Errorf("GOROOT = %q, want preserved %q", got, root)
	}
}

// G0: no GOROOT at all — cleanup must be a clean no-op.
func TestCleanupStaleGOROOT_NoopWhenAbsent(t *testing.T) {
	orig := saveGOROOT(t)
	defer restoreGOROOT(t, orig)

	if err := DeleteUserEnvVar("GOROOT"); err != nil {
		t.Fatalf("DeleteUserEnvVar: %v", err)
	}
	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("expected no error when GOROOT is absent, got %v", err)
	}
}

// Layout probe sanity: isGOROOTDir must mirror the go command's pkg/tool
// heuristic without touching the registry.
func TestIsGOROOTDir_Probe(t *testing.T) {
	root := t.TempDir()
	if isGOROOTDir(root) {
		t.Errorf("isGOROOTDir(empty dir) = true, want false")
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isGOROOTDir(root) {
		t.Errorf("isGOROOTDir(dir with pkg/tool) = false, want true")
	}
	if isGOROOTDir("") {
		t.Error(`isGOROOTDir("") = true, want false`)
	}
}
