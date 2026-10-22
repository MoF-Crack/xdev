package gui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// guiFakePlugin is a minimal Plugin for GUI integrity tests.
// manifest is optional (nil → no FormFactor/GUIBinary in ToolInfo).
type guiFakePlugin struct {
	name     string
	shims    []string
	manifest *manifest.Tool
}

func (g *guiFakePlugin) Name() string                          { return g.name }
func (g *guiFakePlugin) DisplayName() string                   { return g.name }
func (g *guiFakePlugin) Description() string                   { return "" }
func (g *guiFakePlugin) Homepage() string                      { return "" }
func (g *guiFakePlugin) Category() string                      { return "language" }
func (g *guiFakePlugin) Manifest() *manifest.Tool              { return g.manifest }
func (g *guiFakePlugin) ResolveVersion(string) (string, error) { return "1.0.0", nil }
func (g *guiFakePlugin) DefaultVersion() string                { return "1.0.0" }
func (g *guiFakePlugin) ArchiveFor(string) (*manifest.Archive, error) {
	return nil, nil
}
func (g *guiFakePlugin) InstallDir(root, v string) (string, error) {
	return filepath.Join(root, g.name, v), nil
}
func (g *guiFakePlugin) Shims() []string                  { return g.shims }
func (g *guiFakePlugin) EnvVars(string) map[string]string { return nil }
func (g *guiFakePlugin) PostInstall(context.Context, string) error {
	return nil
}
func (g *guiFakePlugin) Deactivate(string) error  { return nil }
func (g *guiFakePlugin) RequiredPlugin() []string { return nil }

var _ plugin.Plugin = (*guiFakePlugin)(nil)

// integrityTestApp builds an App wired to a temp install dir.
func integrityTestApp(t *testing.T, reg *plugin.Registry, installDir string) *App {
	t.Helper()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    filepath.Join(installDir, "xmdev"),
			InstallDir:  filepath.Join(installDir, "install"),
			CacheDir:    filepath.Join(installDir, "cache"),
			ShimDir:     filepath.Join(installDir, "shims"),
			ManifestDir: filepath.Join(installDir, "manifests"),
		},
		Path: config.PathConfig{Mode: "shim"},
	}
	for _, d := range []string{cfg.Core.InstallDir, cfg.Core.CacheDir, cfg.Core.ShimDir, cfg.Core.ManifestDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return NewApp(cfg, &manifest.Catalog{}, reg)
}

func writeInstallBinary(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckInstallIntegrity_Complete(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "go", shims: []string{"go"}})
	app := integrityTestApp(t, reg, base)

	bin := "go"
	if runtime.GOOS == "windows" {
		bin = "go.exe"
	}
	writeInstallBinary(t, filepath.Join(base, "install", "go", "1.0.0", "bin", bin))

	info := app.CheckInstallIntegrity("go", "1.0.0")
	if !info.OK {
		t.Errorf("OK = false, want true (reason: %s, missing: %v)", info.Reason, info.Missing)
	}
	if info.Path == "" || info.Tool != "go" || info.Version != "1.0.0" {
		t.Errorf("unexpected info fields: %+v", info)
	}
}

func TestCheckInstallIntegrity_Incomplete(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "go", shims: []string{"go", "gofmt"}})
	app := integrityTestApp(t, reg, base)

	// Non-empty dir but no shim binaries.
	dir := filepath.Join(base, "install", "go", "1.0.0")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := app.CheckInstallIntegrity("go", "1.0.0")
	if info.OK {
		t.Error("OK = true, want false for incomplete install")
	}
	if len(info.Missing) != 2 {
		t.Errorf("Missing = %v, want [go gofmt]", info.Missing)
	}
}

func TestCheckInstallIntegrity_NotInstalled(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "go", shims: []string{"go"}})
	app := integrityTestApp(t, reg, base)

	info := app.CheckInstallIntegrity("go", "9.9.9")
	if info.OK {
		t.Error("OK = true, want false for not-installed version")
	}
	if info.Reason == "" {
		t.Error("Reason should be non-empty for not-installed version")
	}
}

func TestCheckInstallIntegrity_UnknownTool(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	app := integrityTestApp(t, reg, base)

	info := app.CheckInstallIntegrity("ghost", "1.0.0")
	if info.OK {
		t.Error("OK = true, want false for unknown tool")
	}
}

func TestGetInstalledVersions_IntactFlag(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "go", shims: []string{"go"}})
	app := integrityTestApp(t, reg, base)

	// v1.0.0 complete, v1.1.0 incomplete (empty dir).
	bin := "go"
	if runtime.GOOS == "windows" {
		bin = "go.exe"
	}
	writeInstallBinary(t, filepath.Join(base, "install", "go", "1.0.0", "bin", bin))
	if err := os.MkdirAll(filepath.Join(base, "install", "go", "1.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	versions := app.GetInstalledVersions("go")
	if len(versions) != 2 {
		t.Fatalf("len = %d, want 2", len(versions))
	}
	intactByVersion := map[string]bool{}
	for _, v := range versions {
		intactByVersion[v.Version] = v.Intact
	}
	if !intactByVersion["1.0.0"] {
		t.Error("1.0.0 should be intact")
	}
	if intactByVersion["1.1.0"] {
		t.Error("1.1.0 should NOT be intact (empty dir)")
	}
}

func TestVersionInfo_JSONIncludesIntact(t *testing.T) {
	data, err := json.Marshal(VersionInfo{Version: "1.0.0", Installed: true, Active: false, Intact: false, Integrity: "missing binary go"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "installed", "active", "intact", "integrity"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in %s", key, string(data))
		}
	}
	if v, ok := m["intact"].(bool); !ok || v {
		t.Errorf("intact should serialize as false bool: %s", string(data))
	}

	// omitempty: no integrity key when intact.
	data2, _ := json.Marshal(VersionInfo{Version: "1.0.0", Installed: true, Active: true, Intact: true})
	var m2 map[string]interface{}
	json.Unmarshal(data2, &m2)
	if _, exists := m2["integrity"]; exists {
		t.Errorf("integrity should be omitted when empty: %s", string(data2))
	}
}

func TestPreviewInstall_UsesInMemoryCatalog(t *testing.T) {
	app := newTestApp(t)
	tool := &manifest.Tool{
		Name: "uv",
		VersionList: []manifest.Version{
			{Version: "0.12.5", Unverified: true, Archives: []manifest.Archive{{Form: "cli"}}},
		},
	}
	app.cat = &manifest.Catalog{}
	// Catalog.Tool looks up by name via the unexported map; register through
	// a plugin so lookupToolManifest can still find it.
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "uv", manifest: tool})
	app.registry = reg

	preview := app.PreviewInstall("uv", "0.12.5")
	if !preview.Unverified {
		t.Fatal("expected sniffed version to be unverified")
	}
	if preview.Integrity.Reason == "" {
		t.Error("integrity reason should be populated for a missing install")
	}
	if preview.Tool != "uv" || preview.Version != "0.12.5" {
		t.Errorf("preview identity = %s@%s", preview.Tool, preview.Version)
	}
}

func TestReloadCatalog_UpdatesGenericPlugin(t *testing.T) {
	tmp := t.TempDir()
	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("name: uv\ndisplay_name: UV\nversions:\n  - version: \"0.12.5\"\n    unverified: true\n")
	if err := os.WriteFile(filepath.Join(manifestDir, "uv.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g := plugin.NewGeneric(&manifest.Tool{Name: "uv", VersionList: []manifest.Version{{Version: "0.7.6"}}})
	app.registry.Register(g)

	if err := app.reloadCatalog(); err != nil {
		t.Fatalf("reloadCatalog: %v", err)
	}
	if !app.IsVersionUnverified("uv", "0.12.5") {
		t.Fatal("reloaded catalog should mark 0.12.5 unverified")
	}
	if _, err := g.ResolveVersion("0.12.5"); err != nil {
		t.Fatalf("generic plugin should see sniffed version after reload: %v", err)
	}
}
