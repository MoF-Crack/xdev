// app_dependencies_test.go tests CheckToolDependencies (Spec 023 T-B1).
// 场景覆盖：无 depends、未知工具、平台不匹配、xdev 已装（满足/版本过低）、
// 未找到、仅存在性、版本未知宽松判定。
package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// depsTestApp 构建一个 registry 含给定假插件（带 manifest）的 App。
func depsTestApp(t *testing.T, tools ...*guiFakePlugin) *App {
	t.Helper()
	reg := plugin.NewRegistry()
	for _, p := range tools {
		reg.Register(p)
	}
	return integrityTestApp(t, reg, t.TempDir())
}

// linkCurrent 创建 current/<tool> 链接指向 <installDir>/<tool>/<ver>
// （Windows 用 junction 免特权，Unix 用符号链接）。
func linkCurrent(t *testing.T, xmdeDir, installDir, tool, ver string) {
	t.Helper()
	target := filepath.Join(installDir, tool, ver)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(xmdeDir, "current", tool)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(link, target); err != nil {
			t.Fatalf("create junction %s -> %s: %v", link, target, err)
		}
	} else {
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("create symlink %s -> %s: %v", link, target, err)
		}
	}
}

func TestCheckToolDependencies_NoDepends(t *testing.T) {
	app := depsTestApp(t, &guiFakePlugin{name: "demo", manifest: &manifest.Tool{Name: "demo"}})
	got := app.CheckToolDependencies("demo")
	if got != nil {
		t.Errorf("want nil for no depends, got %v", got)
	}
}

func TestCheckToolDependencies_UnknownTool(t *testing.T) {
	app := depsTestApp(t)
	got := app.CheckToolDependencies("no-such-tool")
	if got != nil {
		t.Errorf("want nil for unknown tool, got %v", got)
	}
}

func TestCheckToolDependencies_PlatformMismatch(t *testing.T) {
	// 选择与当前平台不同的平台（跨平台 CI 均可运行）。
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "darwin"
	}
	app := depsTestApp(t, &guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0", Platforms: []string{other}},
		},
	}})
	got := app.CheckToolDependencies("demo")
	if len(got) != 1 {
		t.Fatalf("got %d statuses, want 1", len(got))
	}
	st := got[0]
	if st.Satisfied {
		t.Error("Satisfied = true, want false on platform mismatch")
	}
	if st.ReasonCode != "platform" {
		t.Errorf("ReasonCode = %q, want platform", st.ReasonCode)
	}
	if st.CurrentVersion != "" {
		t.Errorf("CurrentVersion = %q, want empty (not checked on platform mismatch)", st.CurrentVersion)
	}
}

func TestCheckToolDependencies_XdevManagedSatisfied(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0", Platforms: []string{"any"}},
		},
	}})
	app := integrityTestApp(t, reg, base)
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "node", "22.11.1")

	got := app.CheckToolDependencies("demo")
	if len(got) != 1 {
		t.Fatalf("got %d statuses, want 1", len(got))
	}
	st := got[0]
	if !st.Satisfied {
		t.Errorf("Satisfied = false, want true: %s", st.Reason)
	}
	if st.CurrentVersion != "22.11.1" {
		t.Errorf("CurrentVersion = %q, want 22.11.1 (xdev active version)", st.CurrentVersion)
	}
	if st.Reason != "" {
		t.Errorf("Reason = %q, want empty on success", st.Reason)
	}
}

func TestCheckToolDependencies_VersionTooLow(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0"},
		},
	}})
	app := integrityTestApp(t, reg, base)
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "node", "1.0.0")

	got := app.CheckToolDependencies("demo")
	st := got[0]
	if st.Satisfied {
		t.Error("Satisfied = true, want false for version below minimum")
	}
	if st.ReasonCode != "version_low" {
		t.Errorf("ReasonCode = %q, want version_low", st.ReasonCode)
	}
	if st.ReasonParams["installed"] != "1.0.0" || st.ReasonParams["required"] != "22.0.0" {
		t.Errorf("ReasonParams = %v, want installed=1.0.0 required=22.0.0", st.ReasonParams)
	}
}

func TestCheckToolDependencies_MissingTool(t *testing.T) {
	app := depsTestApp(t, &guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "definitely-not-installed-tool-xyz-12345"},
		},
	}})
	got := app.CheckToolDependencies("demo")
	st := got[0]
	if st.Satisfied {
		t.Error("Satisfied = true, want false for missing tool")
	}
	if st.ReasonCode != "not_found" {
		t.Errorf("ReasonCode = %q, want not_found", st.ReasonCode)
	}
}

func TestCheckToolDependencies_ExistenceOnly(t *testing.T) {
	// 无 MinVersion：仅存在性检查。
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node"},
		},
	}})
	app := integrityTestApp(t, reg, base)
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "node", "20.0.0")

	got := app.CheckToolDependencies("demo")
	st := got[0]
	if !st.Satisfied {
		t.Errorf("Satisfied = false, want true (existence-only): %s", st.Reason)
	}
	if st.CurrentVersion != "20.0.0" {
		t.Errorf("CurrentVersion = %q, want 20.0.0", st.CurrentVersion)
	}
}

func TestCheckToolDependencies_VersionUnknownLenient(t *testing.T) {
	// 激活版本目录名为不可解析串 → 存在即满足（宽松判定，记录原因）。
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0"},
		},
	}})
	app := integrityTestApp(t, reg, base)
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "node", "unknown-ver")

	got := app.CheckToolDependencies("demo")
	st := got[0]
	if !st.Satisfied {
		t.Errorf("Satisfied = false, want true (lenient when version unknown): %s", st.Reason)
	}
	if st.ReasonCode != "version_unknown" {
		t.Errorf("ReasonCode = %q, want version_unknown", st.ReasonCode)
	}
	if st.CurrentVersion != "unknown-ver" {
		t.Errorf("CurrentVersion = %q, want unknown-ver (raw active version)", st.CurrentVersion)
	}
}

func TestCheckToolDependencies_MultipleDeps(t *testing.T) {
	// 混合：node 满足、git 缺失 → 两条状态独立。
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", manifest: &manifest.Tool{
		Name: "demo",
		Depends: []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0", InstallPath: "npm"},
			{Tool: "definitely-not-installed-tool-xyz-12345", Platforms: []string{"any"}},
		},
	}})
	app := integrityTestApp(t, reg, base)
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "node", "22.5.0")

	got := app.CheckToolDependencies("demo")
	if len(got) != 2 {
		t.Fatalf("got %d statuses, want 2", len(got))
	}
	if !got[0].Satisfied {
		t.Errorf("dep[0] (node) not satisfied: %s", got[0].Reason)
	}
	if got[0].InstallPath != "npm" {
		t.Errorf("dep[0].InstallPath = %q, want npm (pass-through)", got[0].InstallPath)
	}
	if got[1].Satisfied {
		t.Error("dep[1] (missing) satisfied, want false")
	}
}
