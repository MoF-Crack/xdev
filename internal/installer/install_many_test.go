package installer

import (
	"context"
	"os"
	"path/filepath"
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

func (p *simplePlugin) Name() string                         { return p.name }
func (p *simplePlugin) DisplayName() string                  { return p.name }
func (p *simplePlugin) Description() string                  { return "test tool " + p.name }
func (p *simplePlugin) Homepage() string                     { return "" }
func (p *simplePlugin) Category() string                     { return "language" }
func (p *simplePlugin) Manifest() *manifest.Tool             { return nil }
func (p *simplePlugin) Shims() []string                      { return []string{p.name} }
func (p *simplePlugin) DefaultVersion() string               { return "1.0.0" }
func (p *simplePlugin) RequiredPlugin() []string             { return nil }
func (p *simplePlugin) EnvVars(string) map[string]string     { return nil }
func (p *simplePlugin) PostInstall(context.Context, string) error { return nil }
func (p *simplePlugin) Deactivate(string) error              { return nil }

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

func (p *simplePlugin) InstallDir(root, version string) string {
	return filepath.Join(root, p.name, version)
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

func TestInstallMany_AlreadyInstalled(t *testing.T) {
	reg := makeSimpleRegistry(t, "go")
	ins := newSimpleInstaller(t, reg)

	// Pre-create the install directory to simulate already-installed
	p, _ := reg.Get("go")
	installPath := p.InstallDir(ins.cfg.Core.InstallDir, "1.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}

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

func TestInstallMany_MixedResults(t *testing.T) {
	reg := makeSimpleRegistry(t, "go")
	ins := newSimpleInstaller(t, reg)

	// Pre-create install dir for "go" (already installed)
	p, _ := reg.Get("go")
	installPath := p.InstallDir(ins.cfg.Core.InstallDir, "1.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

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
