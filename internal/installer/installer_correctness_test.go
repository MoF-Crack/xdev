package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// Ticket 26 (audit-fixes / A-07/A-09/A-11/A-15) guards — installer
// correctness group.

// I1: a Force reinstall whose download fails must KEEP the previous
// installation (pre-fix the tree was removed before the download, so an
// offline/404 run destroyed a working install and left `current` dangling).
func TestForceInstall_DownloadFailKeepsOld(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	// Pre-existing (intact) installation of demo@1.0.0 with an active link.
	p := &fakePlugin{name: "demo", shims: []string{"demo"}}
	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	binDir := filepath.Join(installPath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binName := "demo"
	if runtime.GOOS == "windows" {
		binName = "demo.exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(installPath, "ORIGINAL-INSTALL.txt")
	if err := os.WriteFile(marker, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ins.Activate(p, installPath, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// The archive points at an unreachable endpoint → the download fails.
	p.archive = &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://127.0.0.1:1/demo-1.0.0.zip", Strip: 1,
	}

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{Force: true})
	if err == nil {
		t.Fatal("expected the download to fail")
	}

	// The previous installation must be untouched …
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("force reinstall deleted the previous install before the download succeeded: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, binName)); statErr != nil {
		t.Errorf("previous binary was removed by a failed force reinstall: %v", statErr)
	}
	// … and the active link still resolves.
	if v, ok := util.ActiveVersion(cfg.Core.XMDevDir, "demo"); !ok || v != "1.0.0" {
		t.Errorf("current/demo link broken after a failed force reinstall: version=%q ok=%v", v, ok)
	}
}

// I3: a cache miss is a hard error — the removed glob fallback used to
// extract ANOTHER tool's cache file into this tool's tree.
func TestCacheMiss_NoGlobFallback(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	// A foreign cache entry whose name matches the old glob `*-1.0.0.zip`.
	foreign := filepath.Join(cfg.Core.CacheDir, "lld_is_a_generic_driver._-1.0.0.zip")
	if err := os.WriteFile(foreign, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	arch := &manifest.Archive{OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip", URL: "https://example.test/demo-1.0.0.zip", Strip: 1}
	dest := filepath.Join(cfg.Core.InstallDir, "demo", "1.0.0")

	err := ins.extract("demo", arch, "1.0.0", dest)
	if err == nil {
		t.Fatal("expected a cache miss error instead of a glob match on another tool's file")
	}
	if !strings.Contains(err.Error(), "cache miss") {
		t.Errorf("error = %v, want a cache-miss error", err)
	}
	if !strings.Contains(err.Error(), "clear the cache") && !strings.Contains(err.Error(), "reinstall") {
		t.Errorf("cache-miss error should carry actionable guidance, got: %v", err)
	}
	// The foreign file must not have been extracted anywhere.
	if _, statErr := os.Stat(filepath.Join(dest, "not a zip")); statErr == nil {
		t.Error("foreign cache file content leaked into the install tree")
	}
}

// I4: a TOFU write-back for an archive declared as os/arch "any" must pin
// the hash (pre-fix the ",any" substitution targeted a non-existent entry,
// so the pin silently never landed and the next install re-ran TOFU).
func TestTOFUAnyEntry_PinWrittenBack(t *testing.T) {
	ins, cfg := tofuApp(t, "demo")

	yaml := `name: demo
display_name: Demo
category: system
versions:
  - version: 1.0.0
    stable: true
    archives:
      - os: any
        arch: any
        format: zip
        url: https://example.test/demo-1.0.0.zip
        sha256: ""
`
	if err := os.WriteFile(filepath.Join(cfg.Core.ManifestDir, "demo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(cfg.Core.CacheDir, "demo-1.0.0.zip")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("any-arch archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantSHA, err := util.FileSHA256(dest)
	if err != nil {
		t.Fatal(err)
	}

	arch := &manifest.Archive{OS: "any", Arch: "any", Format: "zip", URL: "https://example.test/demo-1.0.0.zip"}
	if err := ins.TOFUArchiveIfNeeded("demo", "1.0.0", arch, dest); err != nil {
		t.Fatalf("TOFUArchiveIfNeeded for an any/any entry: %v", err)
	}

	updated, err := os.ReadFile(filepath.Join(cfg.Core.ManifestDir, "demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), wantSHA) {
		t.Errorf("TOFU hash %s not written back for an any/any entry:\n%s", wantSHA[:16]+"...", updated)
	}
}

// I5: prependEnvPath must match the PATH key case-insensitively (Windows
// commonly carries "Path=") and must not append a SECOND PATH entry.
func TestPrependEnvPath_CaseInsensitive(t *testing.T) {
	for _, key := range []string{"PATH=", "Path=", "path="} {
		env := []string{"FOO=1", key + `C:\Windows\System32`, "BAR=2"}
		out := prependEnvPath(env, `C:\node`)
		if len(out) != 3 {
			t.Fatalf("key %q: env grew to %d entries (a duplicate PATH entry was appended): %v", key, len(out), out)
		}
		found := false
		for _, e := range out {
			if len(e) >= len(key) && strings.EqualFold(e[:len(key)], key) {
				found = true
				if !strings.HasPrefix(e[len(key):], `C:\node`) {
					t.Errorf("key %q: dir not prepended: %q", key, e)
				}
			}
		}
		if !found {
			t.Errorf("key %q: PATH entry lost", key)
		}
	}

	// No PATH at all → one entry is appended.
	out := prependEnvPath([]string{"FOO=1"}, `C:\node`)
	if len(out) != 2 || out[1] != `PATH=C:\node` {
		t.Errorf("missing PATH should be appended once, got %v", out)
	}
}

// I5b: the npm install env prepends the directory that CONTAINS the node
// binary (on Unix that is <dir>/bin, not <dir>), so the managed toolchain is
// actually found first.
func TestNPMPath_UsesNodeBinDirectory(t *testing.T) {
	binDir := filepath.Dir(filepath.Join("opt", "node", "bin", "node"))
	if binDir != filepath.Join("opt", "node", "bin") {
		t.Fatalf("precondition: filepath.Dir gave %q", binDir)
	}
	env := prependEnvPath([]string{"Path=/usr/bin"}, binDir)
	if len(env) != 1 {
		t.Fatalf("expected a single PATH entry, got %v", env)
	}
	if !strings.HasPrefix(env[0], "Path="+binDir) {
		t.Errorf("node bin dir not prepended: %q", env[0])
	}
}

// I6 (ticket 38 / P2-03): an executable/script installer fetched from a
// mirror is accepted when its hash matches the manifest's MIRROR sha256,
// even though it differs from the official one.
func TestRunInstaller_AcceptsMirrorSHA(t *testing.T) {
	ins, _ := extractTestInstaller(t)
	p := &fakePlugin{name: "mirrortool", shims: []string{"mirrortool"}}

	// The probe installer is a real, executable file with a known hash.
	officialSHA := strings.Repeat("a", 64) // deliberately NOT the real hash
	arch := writeProbeInstaller(t, ins.cfg.Core.CacheDir, "mirrortool", "1.0.0")
	realSHA := arch.SHA256
	arch.SHA256 = officialSHA
	arch.MirrorSHA256 = realSHA

	installPath := filepath.Join(ins.cfg.Core.InstallDir, "mirrortool", "1.0.0")
	t.Setenv("XDEV_LOCK_PROBE_BIN", os.Args[0])
	t.Setenv("XDEV_LOCK_PROBE_OUT", filepath.Join(t.TempDir(), "probe.txt"))
	t.Setenv("XDEV_LOCK_PROBE_DIR", ins.cfg.Core.XMDevDir)
	t.Setenv("XDEV_LOCK_PROBE_TOOL", "mirrortool")

	if err := ins.runInstaller(context.Background(), p, arch, "1.0.0", installPath); err != nil {
		t.Fatalf("installer fetched from a mirror must be accepted when its hash matches mirror_sha256: %v", err)
	}

	// Control: a hash matching NEITHER value is still rejected.
	arch.SHA256 = officialSHA
	arch.MirrorSHA256 = strings.Repeat("b", 64)
	if err := ins.runInstaller(context.Background(), p, arch, "1.0.0", installPath); err == nil {
		t.Fatal("a hash matching neither sha256 nor mirror_sha256 must be rejected")
	}
}
