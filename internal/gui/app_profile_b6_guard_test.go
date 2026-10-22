// app_profile_b6_guard_test.go 守卫票 29 的 profile 导入 / 版本切换一侧：
//
//   - 29-2（B-06 / P1-19）UseVersion 存在性门：切换到未安装的版本必须
//     明确报「未安装」，而不是走到 Activate 才失败（旧行为会先建/删
//     current/<tool> 链接，留下悬挂链接或链接层错误信息）。
//   - 29-2（B-06）ImportProfile 对 !alreadyInstalled 的 active 项跳过
//     UseVersion：该项由导入自己的安装路径处理，旧代码对「尚未落地的版本
//     目录」调用 UseVersion → 假错误进 result.Errors。
package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/plugin"
)

// TestUseVersion_NotInstalled_Rejected：未安装版本 → 明确报错且不动
// current/<tool> 链接；已安装版本仍可正常切换（防过度收紧）。
func TestUseVersion_NotInstalled_Rejected(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	// 不声明 shims：Activate 只建 current 链接，不在测试里装 shim。
	// （也不能叫 "go"——Activate 对 go 会做真实 GOROOT 注册表自愈。）
	reg.Register(&guiFakePlugin{name: "node"})
	app := integrityTestApp(t, reg, base)

	cfg := app.getCfg()
	currentLink := filepath.Join(cfg.Core.XMDevDir, "current", "node")

	err := app.UseVersion("node", "9.9.9")
	if err == nil {
		t.Fatal("切换到未安装版本必须报错（B-06 存在性门）")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("错误信息应明确「未安装」，实际: %v", err)
	}
	if _, statErr := os.Lstat(currentLink); statErr == nil {
		t.Error("存在性门必须在创建 current/<tool> 链接之前失败（旧行为会留下悬挂链接）")
	}

	// 反向守卫：已安装的版本仍必须能切换成功。
	installed := filepath.Join(cfg.Core.InstallDir, "node", "1.0.0")
	writeInstallBinary(t, filepath.Join(installed, "node"))
	if err := app.UseVersion("node", "1.0.0"); err != nil {
		t.Fatalf("已安装版本切换失败（存在性门过度收紧）: %v", err)
	}
	target, linkErr := os.Readlink(currentLink)
	if linkErr != nil {
		t.Fatalf("切换成功后 current/node 链接缺失: %v", linkErr)
	}
	if target != installed && !strings.EqualFold(target, installed) {
		t.Errorf("current/node 指向 %q, want %q", target, installed)
	}
}

// b6InstallRecorder 替换 ImportProfile 的安装入口：记录调用并模拟「安装
// 意图已提交、版本目录尚未落地」的窗口（正是旧代码产生假错误的时刻）。
type b6InstallRecorder struct {
	calls [][2]string
}

// TestImportProfile_ActiveNotInstalled_NoFakeError（修复前红）：
// active="true" 且尚未安装的工具项，导入后 result.Errors 必须为空。
//
// 复现路径：profile 声明 demo@1.0.0（磁盘上没有 install/demo/1.0.0）→
// installedVersions 判定 !alreadyInstalled → 安装入口被调用 → 旧代码随后
// 对**尚未落地的版本目录**调用 UseVersion → B-06 存在性门（或旧的 Activate
// 链接层失败）报错 → result.Errors 多出一条假错误：导入报告「失败」，但
// 安装其实已按预期发起。B-06 后该项跳过 UseVersion。
func TestImportProfile_ActiveNotInstalled_NoFakeError(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo"})
	app := integrityTestApp(t, reg, base)

	rec := &b6InstallRecorder{}
	oldInstall := installToolForProfile
	installToolForProfile = func(_ *App, tool, version string) error {
		rec.calls = append(rec.calls, [2]string{tool, version})
		return nil // 模拟安装成功发起；版本目录此刻还没落地
	}
	t.Cleanup(func() { installToolForProfile = oldInstall })

	profilePath := filepath.Join(base, "profile.json")
	payload := map[string]any{
		"version": "test",
		"proxy":   map[string]any{"http": "", "https": "", "noProxy": "", "enabled": false},
		"mirrors": map[string]string{},
		"tools": []map[string]any{
			{"tool": "demo", "version": "1.0.0", "active": "true"},
		},
		"envVars": []any{},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	result := app.ImportProfile(profilePath)
	if len(rec.calls) != 1 || rec.calls[0] != [2]string{"demo", "1.0.0"} {
		t.Fatalf("安装入口调用记录 = %v, want [[demo 1.0.0]]", rec.calls)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("未安装的 active 项产生了假错误（B-06）: %v（result=%+v）", result.Errors, result)
	}
	if result.Installed != 1 {
		t.Errorf("result.Installed = %d, want 1", result.Installed)
	}
	if result.Failed != 0 {
		t.Errorf("result.Failed = %d, want 0", result.Failed)
	}
	// UseVersion 必须被完全跳过：它会在 current/ 下留下指向不存在版本的链接。
	currentLink := filepath.Join(app.getCfg().Core.XMDevDir, "current", "demo")
	if _, statErr := os.Lstat(currentLink); statErr == nil {
		t.Error("未安装的版本不得被切换（current/demo 链接出现 = 旧行为）")
	}
}
