package installer

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// fileSHA computes the hex SHA-256 of a file (test helper).
func fileSHA(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s for sha: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fakePlugin is a minimal plugin.Plugin implementation used to exercise
// the install workflow end-to-end without depending on real manifests,
// the network, or any concrete tool's logic.
//
// It returns a fixed resolved version and a tiny archive descriptor. The
// test pre-seeds the cache directory with a real zip so the extract step
// succeeds offline. EnvVars() returns nil so Activate() never touches the
// user's environment variables / registry.
type fakePlugin struct {
	name    string
	shims   []string
	archive *manifest.Archive
}

func (f *fakePlugin) Name() string                                   { return f.name }
func (f *fakePlugin) DisplayName() string                            { return f.name }
func (f *fakePlugin) Description() string                            { return "fake test tool" }
func (f *fakePlugin) Homepage() string                               { return "" }
func (f *fakePlugin) Category() string                               { return "language" }
func (f *fakePlugin) Manifest() *manifest.Tool                       { return nil }
func (f *fakePlugin) ResolveVersion(spec string) (string, error)     { return "1.0.0", nil }
func (f *fakePlugin) DefaultVersion() string                         { return "1.0.0" }
func (f *fakePlugin) InstallDir(root, version string) string         { return filepath.Join(root, f.name, version) }
func (f *fakePlugin) Shims() []string                                { return f.shims }
func (f *fakePlugin) EnvVars(installPath string) map[string]string   { return nil }

func (f *fakePlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	a := *f.archive // return a copy so callers can't mutate the fixture
	return &a, nil
}

// PostInstall records that it ran via a marker file so tests can assert
// the hook fires inside the install workflow.
func (f *fakePlugin) PostInstall(ctx context.Context, installPath string) error {
	return os.WriteFile(filepath.Join(installPath, ".postinstall"), []byte("ran"), 0o644)
}

func (f *fakePlugin) Deactivate(installPath string) error { return nil }

func (f *fakePlugin) RequiredPlugin() []string { return nil }

// Ensure fakePlugin satisfies the Plugin interface at compile time.
var _ plugin.Plugin = (*fakePlugin)(nil)

// makeFakeZip writes a valid zip at dest containing a single file
// "<name>/bin/<name><exeSuffix>". The binary name matches the tool/shim
// name so post-extract integrity checks (VerifyInstallIntegrity) pass.
func makeFakeZip(t *testing.T, dest, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	binName := name
	if runtime.GOOS == "windows" {
		binName = name + ".exe"
	}
	path := name + "/bin/" + binName
	fw, err := w.Create(path)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := fw.Write([]byte("#!/bin/sh\necho hello\n")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

// makeFlatFakeZip writes a zip with binaries at archive root (no top-level
// directory). Used to exercise strip-layout integrity failures.
func makeFlatFakeZip(t *testing.T, dest, binName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	name := binName
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name + ".exe"
	}
	fw, err := w.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := fw.Write([]byte("MZ-fake-binary")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

// newTestInstaller builds an Installer wired to a temp xdev home with the
// minimal directory layout Activate() expects, plus a dummy xdev binary
// so shim creation can resolve a shim source.
func newTestInstaller(t *testing.T) (*Installer, *config.Config, string) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    tmp,
			InstallDir:  filepath.Join(tmp, "install"),
			CacheDir:    filepath.Join(tmp, "cache"),
			ShimDir:     filepath.Join(tmp, "shims"),
			ManifestDir: filepath.Join(tmp, "manifests"),
		},
		Path: config.PathConfig{Mode: "shim"},
	}
	for _, d := range []string{cfg.Core.InstallDir, cfg.Core.CacheDir, cfg.Core.ShimDir, cfg.Core.ManifestDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Dummy xdev binary so ResolveShimExe() resolves current/xdev/xdev.exe.
	xdevExeName := "xdev.exe"
	if runtime.GOOS != "windows" {
		xdevExeName = "xdev"
	}
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	xdevInstallDir := filepath.Join(cfg.Core.InstallDir, "xdev", "1.0.0")
	if err := os.MkdirAll(xdevInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir xdev install: %v", err)
	}
	if err := copyFile(selfExe, filepath.Join(xdevInstallDir, xdevExeName)); err != nil {
		t.Fatalf("copy dummy xdev: %v", err)
	}
	// Junction current/xdev -> install/xdev/1.0.0 so ResolveShimExe finds it.
	currentXdev := filepath.Join(cfg.Core.XMDevDir, "current", "xdev")
	if err := makeLink(xdevInstallDir, currentXdev); err != nil {
		t.Fatalf("link current/xdev: %v", err)
	}
	ins := New(cfg, plugin.NewRegistry())
	return ins, cfg, tmp
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o755)
}

// makeLink creates a directory junction (Windows) or symlink (Unix),
// mirroring how installer.go wires up current/ links.
func makeLink(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		util.HideWindow(cmd)
		if _, err := cmd.CombinedOutput(); err != nil {
			return err
		}
		return nil
	}
	return os.Symlink(target, link)
}

// TestInstallWorkflow_HappyPath drives the full Install flow with a fake
// plugin and an offline cache hit, asserting that the returned InstallResult
// carries the spec/resolved/URL/SHA256 fields populated during resolution.
func TestInstallWorkflow_HappyPath(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const (
		toolName = "fakego"
		spec     = "^1.0"
		resolved = "1.0.0"
		url      = "https://example.test/fakego/fakego-1.0.0.zip"
	)

	// Seed the cache with a real zip so the download step is a cache hit.
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-"+resolved+".zip")
	makeFakeZip(t, cacheFile, toolName)
	// Use the archive's actual SHA-256 so the cache-hit verification path
	// matches (rather than re-downloading). This also exercises the
	// verified-cache branch end to end.
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{"fakego"},
		archive: &manifest.Archive{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Format: "zip",
			URL:    url,
			SHA256: realSHA,
			Strip:  1,
		},
	}

	res, err := ins.Install(context.Background(), p, spec)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// --- InstallResult fields (the core assertion of this test) ---
	if res.Tool != toolName {
		t.Errorf("Tool = %q, want %q", res.Tool, toolName)
	}
	if res.Spec != spec {
		t.Errorf("Spec = %q, want %q (spec must round-trip to lockfile)", res.Spec, spec)
	}
	if res.Version != resolved {
		t.Errorf("Version = %q, want %q", res.Version, resolved)
	}
	if res.URL != url {
		t.Errorf("URL = %q, want %q", res.URL, url)
	}
	if res.SHA256 != realSHA {
		t.Errorf("SHA256 = %q, want %q", res.SHA256, realSHA)
	}
	if !res.Activated {
		t.Error("Activated = false, want true")
	}
	if res.InstallPath != filepath.Join(cfg.Core.InstallDir, toolName, resolved) {
		t.Errorf("InstallPath = %q", res.InstallPath)
	}

	// --- Side effects: extracted tree, activation junction, shim ---
	if _, err := os.Stat(filepath.Join(res.InstallPath, "bin")); err != nil {
		t.Errorf("extracted bin/ missing: %v", err)
	}
	// PostInstall marker proves the hook fired inside the workflow.
	if _, err := os.Stat(filepath.Join(res.InstallPath, ".postinstall")); err != nil {
		t.Errorf("PostInstall hook did not run (.postinstall missing): %v", err)
	}
	// current/<tool> should now resolve to the install path.
	currentLink := filepath.Join(cfg.Core.XMDevDir, "current", toolName)
	if target, err := os.Readlink(currentLink); err != nil {
		t.Errorf("current/%s is not a link: %v", toolName, err)
	} else if filepath.Clean(target) != filepath.Clean(res.InstallPath) {
		t.Errorf("current/%s -> %q, want %q", toolName, target, res.InstallPath)
	}
	// shim file created.
	shimName := "fakego.exe"
	if runtime.GOOS != "windows" {
		shimName = "fakego"
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.ShimDir, shimName)); err != nil {
		t.Errorf("shim %s missing: %v", shimName, err)
	}
}

// TestInstallWorkflow_NoActivate asserts that --no-activate skips the
// Activate step (no current/ link, no shim) while still returning a fully
// populated InstallResult.
func TestInstallWorkflow_NoActivate(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	cacheFile := filepath.Join(cfg.Core.CacheDir, "fakego-1.0.0.zip")
	makeFakeZip(t, cacheFile, "fakego")
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  "fakego",
		shims: []string{"fakego"},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: realSHA, Strip: 1,
		},
	}

	res, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if res.Activated {
		t.Error("Activated = true, want false with NoActivate")
	}
	if res.URL == "" || res.SHA256 == "" {
		t.Error("URL/SHA256 should be populated even when NoActivate")
	}
	// No current/ link should exist.
	currentLink := filepath.Join(cfg.Core.XMDevDir, "current", "fakego")
	if _, err := os.Lstat(currentLink); err == nil {
		t.Error("current/fakego link should not exist with NoActivate")
	}
}

// TestInstallWorkflow_AlreadyInstalled exercises the already-installed
// fast path: when the install dir already exists, no download/extract
// happens, Activate runs, and InstallResult is still fully populated.
func TestInstallWorkflow_AlreadyInstalled(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{
		name:  "fakego",
		shims: []string{"fakego"},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: "abc",
		},
	}
	// Pre-create the install dir to trigger the already-installed branch.
	// The fixture includes the shim binary so VerifyInstallIntegrity
	// considers the install complete (a bare/empty dir would now be
	// rejected as incomplete).
	installPath := filepath.Join(cfg.Core.InstallDir, "fakego", "1.0.0")
	if err := os.MkdirAll(filepath.Join(installPath, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	binName := "fakego"
	if runtime.GOOS == "windows" {
		binName = "fakego.exe"
	}
	if err := os.WriteFile(filepath.Join(installPath, "bin", binName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake shim binary: %v", err)
	}

	res, err := ins.Install(context.Background(), p, "1.0.0")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if !res.Activated {
		t.Error("Activated = false, want true on already-installed")
	}
	// Even on the fast path, archive metadata must be populated.
	if res.URL == "" || res.SHA256 == "" {
		t.Errorf("URL=%q SHA256=%q, both should be populated", res.URL, res.SHA256)
	}
}

// TestInstallWorkflow_ResolveError ensures a resolution failure returns an
// error and never reaches download/extract.
func TestInstallWorkflow_ResolveError(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	// A plugin whose ResolveVersion always errors.
	p := &errPlugin{name: "broken"}
	_, err := ins.Install(context.Background(), p, "does-not-exist")
	if err == nil {
		t.Fatal("expected error from ResolveVersion, got nil")
	}
	if !strings.Contains(err.Error(), "no such version") {
		t.Errorf("unexpected error: %v", err)
	}
	// Nothing should have been written to the cache.
	if entries, _ := os.ReadDir(cfg.Core.CacheDir); len(entries) != 0 {
		t.Errorf("cache should be empty, got %d entries", len(entries))
	}
}

// errPlugin fails ResolveVersion to exercise the error path.
type errPlugin struct{ name string }

func (e *errPlugin) Name() string                                 { return e.name }
func (e *errPlugin) DisplayName() string                          { return e.name }
func (e *errPlugin) Description() string                          { return "" }
func (e *errPlugin) Homepage() string                             { return "" }
func (e *errPlugin) Category() string                             { return "language" }
func (e *errPlugin) Manifest() *manifest.Tool                     { return nil }
func (e *errPlugin) ResolveVersion(string) (string, error)        { return "", errNoVersion }
func (e *errPlugin) DefaultVersion() string                       { return "" }
func (e *errPlugin) ArchiveFor(string) (*manifest.Archive, error) { return nil, errNoVersion }
func (e *errPlugin) InstallDir(root, v string) string             { return filepath.Join(root, e.name, v) }
func (e *errPlugin) Shims() []string                              { return nil }
func (e *errPlugin) EnvVars(string) map[string]string             { return nil }
func (e *errPlugin) PostInstall(context.Context, string) error    { return nil }
func (e *errPlugin) Deactivate(string) error                      { return nil }
func (e *errPlugin) RequiredPlugin() []string                     { return nil }

var errNoVersion = errString("no such version")

type errString string

func (e errString) Error() string { return string(e) }

// TestInstallWorkflow_IncompleteInstall asserts that an existing install
// directory that fails integrity (missing shim binary) is NOT silently
// activated: InstallWithProgress returns ErrIncompleteInstall.
func TestInstallWorkflow_IncompleteInstall(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{
		name:  "fakego",
		shims: []string{"fakego"},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: "abc",
		},
	}
	// Pre-create an install dir that is non-empty but lacks the shim binary.
	installPath := filepath.Join(cfg.Core.InstallDir, "fakego", "1.0.0")
	if err := os.MkdirAll(filepath.Join(installPath, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}

	_, err := ins.Install(context.Background(), p, "1.0.0")
	if err == nil {
		t.Fatal("expected ErrIncompleteInstall, got nil")
	}
	if !errors.Is(err, ErrIncompleteInstall) {
		t.Fatalf("errors.Is(err, ErrIncompleteInstall) = false, got: %v", err)
	}
	ie, ok := err.(*IncompleteInstallError)
	if !ok {
		t.Fatalf("error type = %T, want *IncompleteInstallError", err)
	}
	if ie.Tool != "fakego" || ie.Version != "1.0.0" {
		t.Errorf("IncompleteInstallError fields: tool=%q version=%q", ie.Tool, ie.Version)
	}
	if len(ie.Missing) == 0 || ie.Missing[0] != "fakego" {
		t.Errorf("Missing = %v, want [fakego]", ie.Missing)
	}
}

// TestInstallWorkflow_ForceReinstall asserts that Force removes an existing
// complete install directory and reinstalls from scratch.
func TestInstallWorkflow_ForceReinstall(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "fakego"
	// Seed the cache so the reinstall path is a verified cache hit (offline).
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{"fakego"},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: realSHA, Strip: 1,
		},
	}

	// Pre-create a "complete" install dir with a stale marker file.
	installPath := filepath.Join(cfg.Core.InstallDir, toolName, "1.0.0")
	binName := "fakego"
	if runtime.GOOS == "windows" {
		binName = "fakego.exe"
	}
	if err := os.MkdirAll(filepath.Join(installPath, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installPath, "bin", binName), []byte("stale"), 0o755); err != nil {
		t.Fatalf("write stale shim: %v", err)
	}
	staleMarker := filepath.Join(installPath, "stale-marker.txt")
	if err := os.WriteFile(staleMarker, []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}

	res, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{Force: true})
	if err != nil {
		t.Fatalf("Force reinstall failed: %v", err)
	}
	if !res.Activated {
		t.Error("Activated = false, want true after force reinstall")
	}
	// The old directory was removed: stale marker must be gone, and a fresh
	// extraction (with .postinstall marker) must exist.
	if _, err := os.Stat(staleMarker); err == nil {
		t.Error("stale-marker.txt still exists — old install dir was not removed")
	}
	if _, err := os.Stat(filepath.Join(installPath, ".postinstall")); err != nil {
		t.Errorf("fresh install missing .postinstall marker: %v", err)
	}
	// The fake zip extracts <name>/bin/<name><exeSuffix> with strip=1,
	// so the freshly extracted binary must exist again.
	extractedName := "fakego"
	if runtime.GOOS == "windows" {
		extractedName = "fakego.exe"
	}
	if _, err := os.Stat(filepath.Join(installPath, "bin", extractedName)); err != nil {
		t.Errorf("reinstalled extracted binary missing: %v", err)
	}
}

// TestInstallWorkflow_PostExtractIntegrityRejectsStripMistake asserts that
// a flat-root archive installed with strip=1 fails the post-extract
// integrity hard gate (instead of silently leaving an empty/broken dir).
func TestInstallWorkflow_PostExtractIntegrityRejectsStripMistake(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "flattool"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFlatFakeZip(t, cacheFile, toolName)
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/flattool-1.0.0.zip", SHA256: realSHA,
			// Wrong strip for a flat-root archive: binaries sit at root.
			Strip: 1,
		},
	}

	_, err := ins.Install(context.Background(), p, "1.0.0")
	if err == nil {
		t.Fatal("expected post-extract integrity failure, got nil")
	}
	if !strings.Contains(err.Error(), "post-extract integrity check failed") {
		t.Fatalf("error = %q, want post-extract integrity failure", err)
	}
	if !strings.Contains(err.Error(), "strip=1") {
		t.Fatalf("error = %q, want strip layout hint", err)
	}
	// Broken install dir must not remain as "installed".
	installPath := filepath.Join(cfg.Core.InstallDir, toolName, "1.0.0")
	if _, err := os.Stat(installPath); err == nil {
		t.Error("broken install dir still exists after integrity failure")
	}
}

// TestEnsureInstallIntegrity_OK covers the success path used as a hard gate.
func TestEnsureInstallIntegrity_OK(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	bin := "tool"
	if runtime.GOOS == "windows" {
		bin = "tool.exe"
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", bin), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &fakePlugin{name: "tool", shims: []string{"tool"}}
	if err := ensureInstallIntegrity(p, dir, 1); err != nil {
		t.Fatalf("ensureInstallIntegrity OK path failed: %v", err)
	}
}
