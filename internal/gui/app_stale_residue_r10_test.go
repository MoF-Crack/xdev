// app_stale_residue_r10_test.go — 第十轮审计 P3-8（P3）守卫（GUI 侧）。
//
// SetPathMode 迁移 PATH 时会 `ReadDir(current)` 枚举"已激活工具"。
// `current/` 下可能残留 xdev 自己的旁路条目：
//   - `.stale-*`（removeLinkEntry 对遗留真实目录的 rename 产物）；
//   - `.tmp-*`（Activate 用 os.CreateTemp 预留的临时链接名）。
//
// 它们不是工具，却会被同形扫描当成一个活跃工具（审计实测
// `[tool tool.stale-demo]`），于是 direct 模式会把**残留目录的 bin 路径**
// 写进用户 PATH（并可能为它建 shim）—— 用户 PATH 里出现指向
// `current/tool.stale-xxxx/bin` 的死条目。
package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// TestSetPathMode_IgnoresStaleAndTmpResidue 是缺陷的端到端复现：
// 残留目录的 bin 路径绝不能进 PATH。
//
// PATH 走 env 的内存后端，宿主零接触（与 TestInitConfig_NoDeadlock 同法）。
func TestSetPathMode_IgnoresStaleAndTmpResidue(t *testing.T) {
	app := newTestApp(t)
	cfg := app.getCfg()

	currentDir := filepath.Join(cfg.Core.XMDevDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 真工具 demo：current/demo 是指向 install/demo/1.0.0 的链接，
	// 且该目录里确实有 bin/ 子目录（direct 模式的 ToolBinPaths 需要它存在）。
	realTarget := filepath.Join(cfg.Core.InstallDir, "demo", "1.0.0")
	if err := os.MkdirAll(filepath.Join(realTarget, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkCurrent(t, cfg.Core.XMDevDir, cfg.Core.InstallDir, "demo", "1.0.0")

	// 残留：.stale- 与 .tmp- 两个旁路目录（各自带 bin/ 子目录，使缺陷
	// 一旦触发就真的会往 PATH 里写路径）。
	staleBin := filepath.Join(currentDir, "demo.stale-abc", "bin")
	tmpBin := filepath.Join(currentDir, "demo.tmp-123", "bin")
	for _, d := range []string{staleBin, tmpBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "demo", shims: []string{"demo"}, manifest: &manifest.Tool{Name: "demo"}})
	app.registry = reg

	// PATH 内存后端：初始值模拟一个普通用户 PATH。
	getPath, restorePath := env.UseInMemoryUserPathForTesting(`C:\Windows\System32`)
	t.Cleanup(restorePath)

	// 先切到 direct（会为每个"活跃工具"追加 current/<tool>/bin）。
	if err := app.SetPathMode("direct"); err != nil {
		t.Fatalf("SetPathMode(direct): %v", err)
	}
	afterDirect := getPath()

	// 真工具的 bin 路径必须在（证明用例真的走到了追加逻辑）。
	realBin := filepath.Join(currentDir, "demo", "bin")
	if !strings.Contains(strings.ToLower(afterDirect), strings.ToLower(realBin)) {
		t.Fatalf("真工具路径 %s 未进 PATH，用例前提不成立：%q", realBin, afterDirect)
	}
	// P3-8 核心断言：残留目录的 bin 路径绝不能进 PATH。
	for _, bad := range []string{
		filepath.Join(currentDir, "demo.stale-abc", "bin"),
		filepath.Join(currentDir, "demo.tmp-123", "bin"),
	} {
		if strings.Contains(strings.ToLower(afterDirect), strings.ToLower(bad)) {
			t.Errorf("P3-8 复现：残留目录的 bin 路径被写进用户 PATH：%s\n完整 PATH: %q", bad, afterDirect)
		}
	}
}
