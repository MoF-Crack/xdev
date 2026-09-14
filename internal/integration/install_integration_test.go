//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageToolDir pre-creates install/<tool>/<ver> with a bin/<tool> file,
// simulating an already-extracted (already-installed) tool. This lets us
// exercise use/current/which/list/uninstall without any network or
// extraction. Returns the version dir path.
func stageToolDir(t *testing.T, home, tool, ver string) string {
	t.Helper()
	verDir := filepath.Join(home, "install", tool, ver)
	binDir := filepath.Join(verDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("stage %s/%s: %v", tool, ver, err)
	}
	binName := tool
	if isWindows() && !strings.HasSuffix(binName, ".exe") {
		binName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	return verDir
}

func isWindows() bool {
	return filepath.Separator == '\\'
}

// INST-A1: use on a pre-staged install dir switches the active version and
// updates current/<tool>. The --no-path-fix flag (injected by run()) now
// also prevents registry env-var writes, so the host environment is left
// untouched.
func TestUse_SwitchesActiveVersion(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")

	out, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatalf("use failed (code=%d): %s", code, out)
	}
	contains(t, out, "active", "use success")

	link := filepath.Join(home, "current", "node")
	if !fileExists(link) {
		t.Fatalf("current/node not created after use")
	}
	// current/<tool> should resolve to the staged install dir.
	if target, err := os.Readlink(link); err != nil {
		t.Errorf("current/node is not a link: %v", err)
	} else if filepath.Clean(target) != filepath.Clean(filepath.Join(home, "install", "node", "20.18.0")) {
		t.Errorf("current/node -> %q, want install/node/20.18.0", target)
	}
}

// INST-A2: current reports the activated version after use.
func TestCurrent_ReportsActiveVersion(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "python", "3.12.0")
	_, code := run(t, bin, home, "", "use", "python@3.12.0")
	if code != 0 {
		t.Fatal("use python precondition failed")
	}

	out, code := run(t, bin, home, "", "current", "python")
	if code != 0 {
		t.Fatalf("current python: exit %d, out=%s", code, out)
	}
	contains(t, out, "3.12.0", "current python version")
}

// INST-A3: which <tool> prints the install path of the active version.
func TestWhich_ResolvesInstallPath(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	verDir := stageToolDir(t, home, "go", "1.22.0")
	_, code := run(t, bin, home, "", "use", "go@1.22.0")
	if code != 0 {
		t.Fatal("use go precondition failed")
	}

	out, code := run(t, bin, home, "", "which", "go")
	if code != 0 {
		t.Fatalf("which go: exit %d, out=%s", code, out)
	}
	contains(t, out, verDir, "which go path")
}

// INST-A4: list shows the installed tool with its activated version.
func TestList_ShowsInstalledTool(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")
	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use node precondition failed")
	}

	out, code := run(t, bin, home, "", "list")
	if code != 0 {
		t.Fatalf("list: exit %d, out=%s", code, out)
	}
	contains(t, out, "node", "list node")
	contains(t, out, "20.18.0", "list node version")
}

// INST-A5: uninstall removes the install dir AND the current link.
func TestUninstall_RemovesInstallAndCurrent(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	verDir := stageToolDir(t, home, "node", "20.18.0")
	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use node precondition failed")
	}

	out, code := run(t, bin, home, "", "uninstall", "node@20.18.0")
	if code != 0 {
		t.Fatalf("uninstall: exit %d, out=%s", code, out)
	}
	if fileExists(verDir) {
		t.Errorf("install dir %s still exists after uninstall", verDir)
	}
	if fileExists(filepath.Join(home, "current", "node")) {
		t.Error("current/link still exists after uninstall")
	}
}

// INST-A6: switching between two staged versions re-points current/.
func TestUse_SwitchesBetweenVersions(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")
	stageToolDir(t, home, "node", "22.4.0")

	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use 20.18.0 precondition failed")
	}
	if tgt, err := os.Readlink(filepath.Join(home, "current", "node")); err == nil && !strings.HasSuffix(tgt, "20.18.0") {
		t.Errorf("expected current/node -> 20.18.0, got %s", tgt)
	}

	_, code = run(t, bin, home, "", "use", "node@22.4.0")
	if code != 0 {
		t.Fatal("use 22.4.0 failed")
	}
	if tgt, err := os.Readlink(filepath.Join(home, "current", "node")); err == nil && !strings.HasSuffix(tgt, "22.4.0") {
		t.Errorf("expected current/node -> 22.4.0, got %s", tgt)
	}
	// The previously active dir must still exist (switching is non-destructive).
	if !fileExists(filepath.Join(home, "install", "node", "20.18.0")) {
		t.Error("20.18.0 install dir was removed by switch")
	}
}

// INST-L1: chmodded: a project dir with .xdev.toml; `xdev install` (or any
// install) on a known-absent tool is not required here — instead verify the
// lockfile round-trip shell: write .xdev.lock by hand, then `xdev install
// --locked` rejects a known-missing tool gracefully rather than crashing.
//
// This isolates the --locked code path without network. We seed a lockfile
// with a real tool name but a version that is definitely not installed and
// cannot be downloaded offline; the locked installer reports "unsupported
// tool" or a download error but must not panic.
func TestInstall_Locked_MissingToolHandlesGracefully(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	// nosh is not a registered tool; lockfile references it.
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ".xdev.lock"), []byte(
		`version = 1
[tools.nosh]
version = "1.0.0"
resolved = "1.0.0"
sha256 = "0"
url = "https://invalid.example/nosh-1.0.0.zip"
install_path = ""
`), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	out, code := run(t, bin, home, proj, "install", "--locked")
	// The locked installer skips tools that aren't registered (e.g. a typo
	// in the lockfile) with a warning rather than failing the whole run.
	// The contract we enforce: NO panic, and the unsupported tool is named.
	notContains(t, out, "panic:", "install --locked graceful")
	contains(t, out, "unsupported tool", "install --locked names the bad tool")
	_ = code // exit 0 (skip-and-continue) is acceptable here
}

// INST-L2: --from-local with a .xdev.toml listing an unsupported tool
// reports a clear error rather than crashing. Exercises installFromLocal's
// project-config-driven path without needing real archives.
func TestInstall_FromLocal_UnsupportedToolClearError(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ".xdev.toml"),
		[]byte("[tools]\nnosh = '1.0.0'\n"), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	out, code := run(t, bin, home, proj, "install", "--from-local")
	// The from-local installer skips unregistered tools with a warning
	// rather than aborting. Contract: no panic, and the bad tool is named.
	notContains(t, out, "panic:", "install --from-local graceful")
	contains(t, out, "unsupported tool", "install --from-local names the bad tool")
	_ = code
}
