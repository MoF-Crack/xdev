package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// newSimpleInstaller creates an Installer with a custom registry for InstallMany tests.
func newSimpleInstaller(t *testing.T, reg *plugin.Registry) *Installer {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   tmp,
			InstallDir: filepath.Join(tmp, "install"),
			CacheDir:   filepath.Join(tmp, "cache"),
			ShimDir:    filepath.Join(tmp, "shims"),
		},
		Path: config.PathConfig{Mode: "shim"},
	}
	return New(cfg, reg)
}

// makeSimpleRegistry creates a registry with fake plugins that return fixed versions and paths.
func makeSimpleRegistry(t *testing.T, toolNames ...string) *plugin.Registry {
	t.Helper()
	reg := plugin.NewRegistry()
	for _, name := range toolNames {
		reg.Register(&simplePlugin{name: name})
	}
	return reg
}

// simplePlugin is a minimal plugin.Plugin for InstallMany tests.
type simplePlugin struct {
	name string
}

func (p *simplePlugin) Name() string                              { return p.name }
func (p *simplePlugin) DisplayName() string                       { return p.name }
func (p *simplePlugin) Description() string                       { return "test tool " + p.name }
func (p *simplePlugin) Homepage() string                          { return "" }
func (p *simplePlugin) Category() string                          { return "language" }
func (p *simplePlugin) Manifest() *manifest.Tool                  { return nil }
func (p *simplePlugin) Shims() []string                           { return []string{p.name} }
func (p *simplePlugin) DefaultVersion() string                    { return "1.0.0" }
func (p *simplePlugin) RequiredPlugin() []string                  { return nil }
func (p *simplePlugin) EnvVars(string) map[string]string          { return nil }

// PathAppend 实现 plugin.Plugin（P1，2026-10-25）。
func (p *simplePlugin) PathAppend(string) []string { return nil }
func (p *simplePlugin) PostInstall(context.Context, string) error { return nil }
func (p *simplePlugin) Deactivate(string) error                   { return nil }

func (p *simplePlugin) ResolveVersion(spec string) (string, error) {
	if spec == "" {
		return p.DefaultVersion(), nil
	}
	return spec, nil
}

func (p *simplePlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	return &manifest.Archive{
		OS:   "any",
		Arch: "any",
		URL:  "https://example.test/" + p.name + "/" + version + ".zip",
	}, nil
}

func (p *simplePlugin) InstallDir(root, version string) (string, error) {
	return filepath.Join(root, p.name, version), nil
}

func TestInstallMany_Empty(t *testing.T) {
	ins := newSimpleInstaller(t, makeSimpleRegistry(t))
	results := ins.InstallMany(nil, []BatchInstallItem{})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestInstallMany_Unsupported(t *testing.T) {
	ins := newSimpleInstaller(t, makeSimpleRegistry(t, "go"))
	items := []BatchInstallItem{{ToolName: "ghost-tool", Version: "1.0"}}
	results := ins.InstallMany(nil, items)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "unsupported" {
		t.Errorf("Status = %q, want unsupported", results[0].Status)
	}
	if results[0].Error == nil {
		t.Error("expected error for unsupported tool, got nil")
	}
}

// makeIntactInstall creates an install dir that passes VerifyInstallIntegrity
// (non-empty + at least one shim binary present).
func makeIntactInstall(t *testing.T, installPath, tool string) {
	t.Helper()
	bin := tool
	if runtime.GOOS == "windows" {
		bin = tool + ".exe"
	}
	if err := os.MkdirAll(filepath.Join(installPath, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installPath, "bin", bin), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write shim binary: %v", err)
	}
}

func TestInstallMany_AlreadyInstalled(t *testing.T) {
	reg := makeSimpleRegistry(t, "go")
	ins := newSimpleInstaller(t, reg)

	// Pre-create an INTACT install dir to simulate already-installed
	// (Spec 021 D8: an incomplete dir is no longer treated as installed).
	p, _ := reg.Get("go")
	installPath, dirErr := p.InstallDir(ins.cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	makeIntactInstall(t, installPath, "go")

	items := []BatchInstallItem{{ToolName: "go", Version: "1.0.0"}}
	results := ins.InstallMany(nil, items)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "already_installed" {
		t.Errorf("Status = %q, want already_installed", results[0].Status)
	}
	if results[0].InstallPath != installPath {
		t.Errorf("InstallPath = %q, want %q", results[0].InstallPath, installPath)
	}
}

// TestInstallMany_IncompleteFails verifies batch mode no longer reports an
// incomplete install as "already_installed" — it fails with
// ErrIncompleteInstall, matching InstallWithProgress (Spec 021 D8).
func TestInstallMany_IncompleteFails(t *testing.T) {
	reg := makeSimpleRegistry(t, "go")
	ins := newSimpleInstaller(t, reg)

	p, _ := reg.Get("go")
	installPath, dirErr := p.InstallDir(ins.cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}

	items := []BatchInstallItem{{ToolName: "go", Version: "1.0.0"}}
	results := ins.InstallMany(nil, items)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("Status = %q, want failed (incomplete install must not be treated as installed)", results[0].Status)
	}
	if results[0].Error == nil || !errors.Is(results[0].Error, ErrIncompleteInstall) {
		t.Errorf("Error = %v, want ErrIncompleteInstall", results[0].Error)
	}
}

func TestInstallMany_MixedResults(t *testing.T) {
	reg := makeSimpleRegistry(t, "go")
	ins := newSimpleInstaller(t, reg)

	// Pre-create an INTACT install dir for "go" (already installed)
	p, _ := reg.Get("go")
	installPath, dirErr := p.InstallDir(ins.cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	makeIntactInstall(t, installPath, "go")

	items := []BatchInstallItem{
		{ToolName: "go", Version: "1.0.0"},  // already installed
		{ToolName: "ghost", Version: "1.0"}, // unsupported
	}
	results := ins.InstallMany(nil, items)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Status != "already_installed" {
		t.Errorf("results[0].Status = %q, want already_installed", results[0].Status)
	}
	if results[1].Status != "unsupported" {
		t.Errorf("results[1].Status = %q, want unsupported", results[1].Status)
	}
}
