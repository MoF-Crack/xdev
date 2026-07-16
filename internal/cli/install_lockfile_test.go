package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/lockfile"
)

// TestUpdateLockfile_PopulatesAllFields verifies that after an install,
// updateLockfile writes a .xdev.lock entry carrying the full pinning
// data: original spec (Version), resolved version, URL, SHA-256 and
// install path. This guards the InstallResult.Spec/URL/SHA256 plumbing
// added to close the lockfile loop.
func TestUpdateLockfile_PopulatesAllFields(t *testing.T) {
	tmp := t.TempDir()
	// updateLockfile resolves the project dir via FindConfigDir("."), which
	// walks up from CWD. A bare .xdev.toml marks tmp as a project root.
	if err := os.WriteFile(filepath.Join(tmp, ".xdev.toml"), []byte("[tools]\n"), 0o644); err != nil {
		t.Fatalf("write .xdev.toml: %v", err)
	}

	// Run from the temp project dir so FindConfigDir(".") resolves to it.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd) // restore even on failure

	res := &installer.InstallResult{
		Tool:        "node",
		Version:     "20.18.0", // resolved
		Spec:        "^20",      // original intent
		URL:         "https://nodejs.org/dist/v20.18.0/node-v20.18.0-win-x64.zip",
		SHA256:      "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		InstallPath: filepath.Join(tmp, "install", "node", "20.18.0"),
		Activated:   true,
	}

	updateLockfile("node", res)

	lf, err := lockfile.Load(tmp)
	if err != nil {
		t.Fatalf("lockfile.Load: %v", err)
	}
	if lf == nil {
		t.Fatal(".xdev.lock was not created")
	}
	locked, ok := lf.Tools["node"]
	if !ok {
		t.Fatal("node missing from .xdev.lock")
	}

	// Spec is stored in Version; resolved concrete version in Resolved.
	if locked.Version != "^20" {
		t.Errorf("Version = %q, want %q (spec should round-trip)", locked.Version, "^20")
	}
	if locked.Resolved != "20.18.0" {
		t.Errorf("Resolved = %q, want %q", locked.Resolved, "20.18.0")
	}
	if locked.URL != res.URL {
		t.Errorf("URL = %q, want %q", locked.URL, res.URL)
	}
	if locked.SHA256 != res.SHA256 {
		t.Errorf("SHA256 = %q, want %q", locked.SHA256, res.SHA256)
	}
	if locked.InstallPath != res.InstallPath {
		t.Errorf("InstallPath = %q, want %q", locked.InstallPath, res.InstallPath)
	}
}

// TestUpdateLockfile_NoProjectSkips confirms that outside a project
// (no .xdev.toml ancestor) updateLockfile is a silent no-op and writes
// nothing.
func TestUpdateLockfile_NoProjectSkips(t *testing.T) {
	tmp := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	res := &installer.InstallResult{Tool: "node", Version: "20.18.0", Spec: "20"}
	updateLockfile("node", res) // should be a no-op

	if _, err := os.Stat(filepath.Join(tmp, lockfile.FileName)); err == nil {
		t.Error(".xdev.lock should not be created outside a project")
	}
}

// TestUpdateLockfile_AppendsAcrossInstalls verifies that successive
// installs of different tools accumulate into the same .xdev.lock
// rather than overwriting it.
func TestUpdateLockfile_AppendsAcrossInstalls(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".xdev.toml"), []byte("[tools]\n"), 0o644); err != nil {
		t.Fatalf("write .xdev.toml: %v", err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	updateLockfile("node", &installer.InstallResult{
		Tool: "node", Version: "20.18.0", Spec: "^20", URL: "u1", SHA256: "h1",
		InstallPath: filepath.Join(tmp, "install", "node", "20.18.0"),
	})
	updateLockfile("python", &installer.InstallResult{
		Tool: "python", Version: "3.12.0", Spec: "^3.12", URL: "u2", SHA256: "h2",
		InstallPath: filepath.Join(tmp, "install", "python", "3.12.0"),
	})

	lf, err := lockfile.Load(tmp)
	if err != nil || lf == nil {
		t.Fatalf("lockfile.Load: %v (%v)", lf, err)
	}
	if len(lf.Tools) != 2 {
		t.Fatalf("expected 2 locked tools, got %d", len(lf.Tools))
	}
	if _, ok := lf.Tools["node"]; !ok {
		t.Error("node overwritten by second install")
	}
	if _, ok := lf.Tools["python"]; !ok {
		t.Error("python missing after second install")
	}
}
