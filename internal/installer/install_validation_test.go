package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/plugin"
)

// 20261021 审计 P1-5（阶段 1）：安装/激活/卸载在写任何文件之前必须
// fail-closed。此前 plugin.InstallDir 会把非法版本替换成
// "unsafe-version-blocked" 后继续安装（宿主上实测留下过该目录）。

func newValidationInstaller(t *testing.T, plugins ...plugin.Plugin) *Installer {
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

// unsafeVersionPlugin resolves to a traversal version regardless of spec.
type unsafeVersionPlugin struct{ *fakePlugin }

func (u *unsafeVersionPlugin) ResolveVersion(string) (string, error) { return "../evil", nil }

func TestValidateInstallComponent(t *testing.T) {
	valid := []string{"go", "go1.22.10", "1.2.2-alpha", "2026.2.0.1"}
	for _, v := range valid {
		if err := validateInstallComponent("version", v); err != nil {
			t.Errorf("validateInstallComponent(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"", ".", "..", "../evil", `..\evil`, `a/b`, `a\b`}
	for _, v := range invalid {
		if err := validateInstallComponent("version", v); err == nil {
			t.Errorf("validateInstallComponent(%q) = nil, want rejection", v)
		}
	}
}

func TestInstallWithProgress_RejectsUnsafeVersionBeforeAnyWrite(t *testing.T) {
	p := &unsafeVersionPlugin{fakePlugin: &fakePlugin{name: "safe-tool"}}
	ins := newValidationInstaller(t, p)

	_, err := ins.InstallWithProgress(context.Background(), p, "x", nil, InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe version") {
		t.Fatalf("InstallWithProgress = %v, want unsafe-version error", err)
	}
	if _, statErr := os.Stat(filepath.Join(ins.cfg.Core.InstallDir, "safe-tool")); statErr == nil {
		t.Error("unsafe version must not create any install directory")
	}
}

func TestInstallWithProgress_RejectsUnsafeToolNameBeforeAnyWrite(t *testing.T) {
	p := &fakePlugin{name: "../evil"}
	ins := newValidationInstaller(t, p)

	_, err := ins.InstallWithProgress(context.Background(), p, "x", nil, InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe tool name") {
		t.Fatalf("InstallWithProgress = %v, want unsafe-tool-name error", err)
	}
}

func TestActivate_RejectsUnsafeVersion(t *testing.T) {
	p := &fakePlugin{name: "safe-tool"}
	ins := newValidationInstaller(t, p)

	err := ins.Activate(p, filepath.Join(ins.cfg.Core.InstallDir, "safe-tool", "1.0.0"), "../evil")
	if err == nil || !strings.Contains(err.Error(), "unsafe version") {
		t.Fatalf("Activate = %v, want unsafe-version error", err)
	}
}

func TestUninstall_RejectsUnsafeVersion(t *testing.T) {
	p := &fakePlugin{name: "safe-tool"}
	ins := newValidationInstaller(t, p)

	err := ins.Uninstall(p, "../evil")
	if err == nil || !strings.Contains(err.Error(), "unsafe version") {
		t.Fatalf("Uninstall = %v, want unsafe-version error", err)
	}
}

func TestInstallNPMPackage_RejectsUnsafeVersion(t *testing.T) {
	p := &fakePlugin{name: "safe-tool"}
	ins := newValidationInstaller(t, p)

	_, err := ins.InstallNPMPackage(context.Background(), p, nil, "../evil", "ignored")
	if err == nil || !strings.Contains(err.Error(), "unsafe version") {
		t.Fatalf("InstallNPMPackage = %v, want unsafe-version error", err)
	}
}
