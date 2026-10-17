package plugin

import (
	"testing"
)

// 第八轮审计 A-01（P0）守卫：**插件表本身**的读取也必须受保护。
//
// 缺陷现场：第七轮 F-10 只把「结果字段读取」（getFieldString/Int/StringSlice）
// 包进了 extractField→L.PCall，但插件表自身的读取仍是裸 L.GetField/L.GetGlobal。
// gopher-lua 的 L.GetField 会执行 __index 元方法，而 ls.Panic 是 Go panic
// （panicWithTraceback），直接穿透 LoadPlugin 打崩宿主进程。
//
// 影响：任意 `$XMDEV_HOME/plugins/*.lua` 都能让 CLI/TUI/GUI 每次启动都崩溃
//（registerUserPlugins 在 RegisterAll 里加载每个用户插件）—— 已用真实二进制
// 复现：`xdev available` → `panic: …p16-boom:install` → exit 2。
//
// 本用例不带 recover：若修复失效，测试进程会直接 panic（这就是红）。
func TestLoadPlugin_ThrowingIndexMetamethodOnPluginTableDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	// PLUGIN 自己带一个抛错的 __index —— 任何未受保护的字段读取都会触发它。
	src := `
PLUGIN = setmetatable({ name = "a01tool", display_name = "A01 Tool" }, {
  __index = function(_, k) error("a01-boom:" .. tostring(k)) end
})
function available() return {"1.0.0"} end
function archive(version, os_name, arch)
  return { url = "https://example.test/a01.zip" }
end
`
	path := writeTempLua(t, dir, "a01.lua", src)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin must tolerate a throwing __index on the PLUGIN table, got: %v", err)
	}
	if p == nil {
		t.Fatal("LoadPlugin returned a nil plugin with a nil error")
	}
	// name/display_name 是**真实存在**的键，必须正常读到（不受 __index 影响）。
	if p.Name() != "a01tool" {
		t.Errorf("Name() = %q, want a01tool", p.Name())
	}
	if p.DisplayName() != "A01 Tool" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "A01 Tool")
	}
	// category/shims 缺失 → __index 抛错 → 必须回落到默认值而不是崩溃。
	if p.Category() != "system" {
		t.Errorf("Category() = %q, want the default %q after a throwing __index", p.Category(), "system")
	}
}

// 对照：正常的 PLUGIN（无元方法）必须照常工作——防止保护层把合法插件读坏。
func TestLoadPlugin_NormalPluginStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "ok.lua", validPluginLua)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}
	if p.Name() != "testtool" {
		t.Errorf("Name() = %q, want testtool", p.Name())
	}
	if p.Category() != "build" {
		t.Errorf("Category() = %q, want build", p.Category())
	}
}
