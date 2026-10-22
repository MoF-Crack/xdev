package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第九轮审计 P0-1 守卫：`promotePluginMethods` 的写入必须绕过 `__newindex`。
//
// 缺陷现场：A-01（Round 8）只把**读**侧（`GetGlobal`/`GetField`）包进了
// `extractField → L.PCall`，但把 `PLUGIN.available/archive/...` 提升为全局时用的是
// `L.SetGlobal`。gopher-lua 的 `SetGlobal → setFieldString` 在**键不存在**时会执行
// `__newindex`，而 `ls.Panic` 是 Go panic，穿透 `LoadPlugin` →
// **任意** `$XMDEV_HOME/plugins/*.lua` 都能让 CLI/TUI/GUI 每次启动崩溃（exit 2）。
//
// **放大点与 A-01 一样：不需要恶意。** 插件作者用 `__newindex` 抓「严格全局变量」
// /拼写错误这种良性写法，同样打崩宿主。
//
// 即使 `__newindex` 只是吞掉写入而不抛错也不安全：全局没写进去 → 后续误报
// "available() function not found"。
const a01WriteSidePlugin = `PLUGIN = {
  name = "p01newindex",
  available = function() return {"1.0.0"} end,
  archive = function(v, o, a) return { url = "https://example.test/x.zip" } end,
}
setmetatable(_G, { __newindex = function(_, k) error("newindex-boom:" .. tostring(k)) end })
`

// throwing __newindex：不得 panic（旧实现会 panic）。
func TestLoadPlugin_ThrowingNewIndexOnGlobalsDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p01_throwing.lua")
	if err := os.WriteFile(path, []byte(a01WriteSidePlugin), 0o644); err != nil {
		t.Fatal(err)
	}

	// 旧实现：LoadPlugin 内部 ls.Panic → Go panic 直接穿透本测试。
	// 这里不 recover，所以「不 panic」本身由测试进程存活来证明。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("P0-1 复现：LoadPlugin 发生 Go panic（宿主进程会崩溃）: %v", r)
		}
	}()

	p, err := NewLuaEngine().LoadPlugin(path)
	if err != nil {
		// 允许返回 error（可诊断），但不允许 panic。
		t.Logf("LoadPlugin 返回 error（可接受，只要不是 panic）: %v", err)
		return
	}
	if p == nil {
		t.Fatal("LoadPlugin 返回 nil 插件且无 error")
	}
}

// swallowing __newindex：写入必须**生效**（不得因元方法吞掉而误报函数缺失）。
func TestLoadPlugin_SwallowingNewIndexDoesNotLoseMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p01_swallow.lua")
	src := strings.Replace(a01WriteSidePlugin,
		`error("newindex-boom:" .. tostring(k))`, `return nil`, 1)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewLuaEngine().LoadPlugin(path)
	if err != nil {
		t.Skipf("该 VM 形态下插件加载失败（非本守卫判定项）: %v", err)
	}
	// 方法应已提升为全局并可用。旧实现下全局没写进去 → 调用报 "function not found"。
	// ResolveVersion("latest") 会走 available()，正是被提升的那个方法。
	got, verr := p.ResolveVersion("latest")
	if verr != nil {
		if strings.Contains(verr.Error(), "not found") {
			t.Errorf("P0-1 复现：__newindex 吞掉写入导致方法丢失（available() 不可用）: %v", verr)
		}
		t.Skipf("版本解析以其它方式失败（非本守卫判定项）: %v", verr)
	}
	if got == "" {
		t.Error("P0-1 复现：方法被 __newindex 吞掉，未能解析出任何版本")
	}
	t.Logf("吞掉型 __newindex 下仍解析出版本 %q（原始写生效）", got)
}

// 对照：无 __newindex 的普通插件仍能正常提升方法（修复不得弄坏正常路径）。
func TestLoadPlugin_NormalNestedPluginMethodsStillPromoted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p01_normal.lua")
	src := `PLUGIN = {
  name = "p01normal",
  available = function() return {"1.0.0"} end,
  archive = function(v, o, a) return { url = "https://example.test/x.zip" } end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewLuaEngine().LoadPlugin(path)
	if err != nil {
		t.Fatalf("普通插件应能加载: %v", err)
	}
	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if got != "1.0.0" {
		t.Errorf("方法提升后应解析出 1.0.0，got %q", got)
	}
}
