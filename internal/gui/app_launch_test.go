// app_launch_test.go tests LaunchTool / ToolCommand (Spec 023 T-C1).
// 场景覆盖：启动成功、未安装、无 gui 形态、未声明/非法/缺失 gui_binary、
// nil manifest、ToolCommand 的 shims 与未安装分支。
package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// dummyGUIBinary 复制一个无害且快速退出的可执行文件到 path 作为假 GUI 二进制
// （Windows 用 where.exe，Unix 用 /bin/true——两者无参数调用立即退出）。
func dummyGUIBinary(t *testing.T, path string) {
	t.Helper()
	var src string
	if runtime.GOOS == "windows" {
		src = `C:\Windows\System32\where.exe`
	} else {
		src = "/bin/true"
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read dummy binary %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("write dummy binary %s: %v", path, err)
	}
}

// launchTestApp 构建 registry 含给定假插件、指向临时 xdev home 的 App。
func launchTestApp(t *testing.T, tools ...*guiFakePlugin) (*App, string) {
	t.Helper()
	reg := plugin.NewRegistry()
	for _, p := range tools {
		reg.Register(p)
	}
	base := t.TempDir()
	app := integrityTestApp(t, reg, base)
	return app, base
}

// guiPlugin 构造一个 gui 形态的假插件。
func guiPlugin(name string) *guiFakePlugin {
	return &guiFakePlugin{
		name: name,
		manifest: &manifest.Tool{
			Name:       name,
			FormFactor: []string{"gui", "cli"},
			GUIBinary:  "bin/app.exe",
		},
	}
}

func TestLaunchTool_Success(t *testing.T) {
	app, base := launchTestApp(t, guiPlugin("vscode"))
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "vscode", "1.0.0")
	dummyGUIBinary(t, filepath.Join(base, "install", "vscode", "1.0.0", "bin", "app.exe"))

	if err := app.LaunchTool("vscode"); err != nil {
		t.Fatalf("LaunchTool: %v", err)
	}
}

func TestLaunchTool_NotInstalled(t *testing.T) {
	// 无 current/<tool> junction。
	app, _ := launchTestApp(t, guiPlugin("vscode"))
	err := app.LaunchTool("vscode")
	if err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("want not-installed error, got %v", err)
	}
}

func TestLaunchTool_NoGUIPlugin(t *testing.T) {
	app, base := launchTestApp(t, &guiFakePlugin{name: "cli-only", manifest: &manifest.Tool{
		Name:       "cli-only",
		FormFactor: []string{"cli", "tui"},
	}})
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "cli-only", "1.0.0")

	err := app.LaunchTool("cli-only")
	if err == nil || !strings.Contains(err.Error(), "无 gui 形态") {
		t.Fatalf("want no-gui-form error, got %v", err)
	}
}

func TestLaunchTool_NoGUIBinary(t *testing.T) {
	app, base := launchTestApp(t, &guiFakePlugin{name: "gui-nobin", manifest: &manifest.Tool{
		Name:       "gui-nobin",
		FormFactor: []string{"gui"},
	}})
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "gui-nobin", "1.0.0")

	err := app.LaunchTool("gui-nobin")
	if err == nil || !strings.Contains(err.Error(), "gui_binary") {
		t.Fatalf("want no-gui_binary error, got %v", err)
	}
}

func TestLaunchTool_MissingBinary(t *testing.T) {
	app, base := launchTestApp(t, guiPlugin("vscode"))
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "vscode", "1.0.0")
	// 声明了 gui_binary 但文件不存在。

	err := app.LaunchTool("vscode")
	if err == nil || !strings.Contains(err.Error(), "GUI 二进制不存在") {
		t.Fatalf("want missing-binary error, got %v", err)
	}
}

func TestLaunchTool_TraversalGUIBinary(t *testing.T) {
	app, base := launchTestApp(t, &guiFakePlugin{name: "evil", manifest: &manifest.Tool{
		Name:       "evil",
		FormFactor: []string{"gui"},
		GUIBinary:  "bin/../../outside.exe",
	}})
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "evil", "1.0.0")
	// 外部文件存在也不得启动（越界路径被拒绝）。
	if err := os.WriteFile(filepath.Join(base, "outside.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := app.LaunchTool("evil")
	if err == nil || (!strings.Contains(err.Error(), "越界") && !strings.Contains(err.Error(), "相对路径")) {
		t.Fatalf("want traversal-rejection error, got %v", err)
	}
}

func TestLaunchTool_NilManifest(t *testing.T) {
	app, base := launchTestApp(t, &guiFakePlugin{name: "nomanifest"})
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "nomanifest", "1.0.0")

	err := app.LaunchTool("nomanifest")
	if err == nil || !strings.Contains(err.Error(), "无 manifest") {
		t.Fatalf("want nil-manifest error, got %v", err)
	}
}

func TestLaunchTool_UnknownTool(t *testing.T) {
	app, _ := launchTestApp(t)
	err := app.LaunchTool("no-such-tool")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want tool-not-found error, got %v", err)
	}
}

func TestToolCommand_Shims(t *testing.T) {
	app, base := launchTestApp(t, &guiFakePlugin{name: "vscode", shims: []string{"code", "code-insiders"}, manifest: &manifest.Tool{
		Name:  "vscode",
		Shims: []string{"code", "code-insiders"},
	}})
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "vscode", "1.0.0")

	if got := app.ToolCommand("vscode"); got != "code" {
		t.Errorf("ToolCommand = %q, want code (first shim)", got)
	}
}

func TestToolCommand_NoShims(t *testing.T) {
	// 纯 GUI 工具（无 shims）：无法猜测 CLI 命令 → 空串。
	app, base := launchTestApp(t, guiPlugin("guiapp"))
	linkCurrent(t, filepath.Join(base, "xmdev"), filepath.Join(base, "install"), "guiapp", "1.0.0")

	if got := app.ToolCommand("guiapp"); got != "" {
		t.Errorf("ToolCommand = %q, want empty (no shims)", got)
	}
}

func TestToolCommand_NotInstalled(t *testing.T) {
	app, _ := launchTestApp(t, &guiFakePlugin{name: "vscode", shims: []string{"code"}, manifest: &manifest.Tool{
		Name:  "vscode",
		Shims: []string{"code"},
	}})
	if got := app.ToolCommand("vscode"); got != "" {
		t.Errorf("ToolCommand = %q, want empty (not installed)", got)
	}
}

func TestToolCommand_UnknownTool(t *testing.T) {
	app, _ := launchTestApp(t)
	if got := app.ToolCommand("no-such-tool"); got != "" {
		t.Errorf("ToolCommand = %q, want empty (unknown tool)", got)
	}
}
