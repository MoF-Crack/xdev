package installer

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// depPlugin is a plugin.Plugin with configurable RequiredPlugin and archive.
type depPlugin struct {
	name     string
	requires []string
	archive  *manifest.Archive
}

func (p *depPlugin) Name() string                              { return p.name }
func (p *depPlugin) DisplayName() string                       { return p.name }
func (p *depPlugin) Description() string                       { return "test tool " + p.name }
func (p *depPlugin) Homepage() string                          { return "" }
func (p *depPlugin) Category() string                          { return "language" }
func (p *depPlugin) Manifest() *manifest.Tool                  { return nil }
func (p *depPlugin) Shims() []string                           { return []string{p.name} }
func (p *depPlugin) DefaultVersion() string                    { return "1.0.0" }
func (p *depPlugin) RequiredPlugin() []string                  { return p.requires }
func (p *depPlugin) EnvVars(string) map[string]string          { return nil }
func (p *depPlugin) PostInstall(context.Context, string) error { return nil }
func (p *depPlugin) Deactivate(string) error                   { return nil }

func (p *depPlugin) ResolveVersion(spec string) (string, error) {
	if spec == "" {
		return p.DefaultVersion(), nil
	}
	return spec, nil
}

func (p *depPlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	if p.archive != nil {
		return p.archive, nil
	}
	return &manifest.Archive{
		OS:   "any",
		Arch: "any",
		URL:  "https://example.test/" + p.name + "/" + version + ".zip",
	}, nil
}

func (p *depPlugin) InstallDir(root, version string) string {
	return filepath.Join(root, p.name, version)
}

// newDepTestInstaller creates an Installer with the given plugins registered.
func newDepTestInstaller(t *testing.T, plugins ...*depPlugin) *Installer {
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
	reg := plugin.NewRegistry()
	for _, p := range plugins {
		reg.Register(p)
	}
	return New(cfg, reg)
}

// makeInstalledDir creates a fake installation directory so dependency checks pass.
func makeInstalledDir(t *testing.T, ins *Installer, toolName, version string) {
	t.Helper()
	p, ok := ins.reg.Get(toolName)
	if !ok {
		t.Fatalf("tool %q not in registry", toolName)
	}
	path := p.InstallDir(ins.cfg.Core.InstallDir, version)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// TestInstall_DepSatisfied verifies that when a required dependency is already
// installed, installation proceeds past the dependency check. Uses a pre-seeded
// cache (cache hit) with matching SHA-256 to avoid network access, matching the
// pattern in TestInstallWorkflow_HappyPath.
func TestInstall_DepSatisfied(t *testing.T) {
	ins := newDepTestInstaller(t,
		&depPlugin{name: "java"},
	)

	// Mark java as installed (creates the install directory that the dependency check verifies)
	makeInstalledDir(t, ins, "java", "1.0.0")

	// Configure flutter's archive for cache-hit: pre-seed cache file with matching SHA-256
	toolName := "flutter"
	ver := "1.0.0"
	format := "zip"
	cacheFile := filepath.Join(ins.cfg.Core.CacheDir, toolName+"-"+ver+"."+format)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("create cache file: %v", err)
	}
	zipW := zip.NewWriter(f)
	// Binary name must match the tool shim so post-extract integrity passes.
	binName := toolName
	if runtime.GOOS == "windows" {
		binName = toolName + ".exe"
	}
	fw, _ := zipW.Create(toolName + "/bin/" + binName)
	fw.Write([]byte("#!/bin/sh\necho hello\n"))
	zipW.Close()
	f.Close()
	realSHA := fileSHA(t, cacheFile)

	flutter := &depPlugin{
		name:     toolName,
		requires: []string{"java"},
		archive: &manifest.Archive{
			OS:     "any",
			Arch:   "any",
			Format: format,
			URL:    "https://example.test/" + toolName + "/" + ver + "." + format,
			SHA256: realSHA,
			Strip:  1,
		},
	}

	res, err := ins.InstallWithProgress(context.Background(), flutter, ver, nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("InstallWithProgress returned error when dependency satisfied: %v", err)
	}
	if res.Tool != toolName {
		t.Errorf("Tool = %q, want %q", res.Tool, toolName)
	}
	if res.Version != ver {
		t.Errorf("Version = %q, want %q", res.Version, ver)
	}
}

// TestInstall_DepMissing verifies that when a required dependency is NOT installed,
// installation fails with a descriptive error.
func TestInstall_DepMissing(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	// java is in registry but NOT installed (no install directory)
	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
	want := "required plugin \"java\" is not installed"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

// TestInstall_DepUnsupported verifies that when a required dependency is NOT in the registry,
// installation fails with a "not supported" error.
func TestInstall_DepUnsupported(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"ghost-tool"}}
	// ghost-tool is NOT registered
	ins := newDepTestInstaller(t, flutter)

	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected error for unsupported dependency, got nil")
	}
	want := "required plugin \"ghost-tool\" is not supported"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

// TestUninstall_IsRequiredBy verifies that uninstalling a tool that is required by
// another installed tool fails with a descriptive error.
func TestUninstall_IsRequiredBy(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	// Both flutter and java are "installed" (directories exist)
	makeInstalledDir(t, ins, "java", "1.0.0")
	makeInstalledDir(t, ins, "flutter", "1.0.0")

	err := ins.Uninstall(java, "1.0.0")
	if err == nil {
		t.Fatal("expected error when uninstalling a required tool, got nil")
	}
	want := "cannot uninstall \"java\": required by \"flutter\""
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

// TestUninstall_NotRequired verifies that uninstalling a tool that is NOT required
// by any other tool succeeds.
func TestUninstall_NotRequired(t *testing.T) {
	go_ := &depPlugin{name: "go"}
	ins := newDepTestInstaller(t, go_)

	makeInstalledDir(t, ins, "go", "1.0.0")

	err := ins.Uninstall(go_, "1.0.0")
	if err != nil {
		t.Errorf("unexpected error for non-required tool: %v", err)
	}
}
