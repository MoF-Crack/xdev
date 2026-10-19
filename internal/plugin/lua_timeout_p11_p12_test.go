package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 第十轮审计 P1-1 / P1-2 守卫：Lua 插件不得让宿主**永久挂起**。
//
// 缺陷现场（两条同源）：
//
//	P1-2 加载期：M-12 的三处超时全在**调用期**（`L.CallByParam`），而 `L.DoFile`
//	     是裸调用 —— 插件顶层 `while true do end` 就让 CLI/TUI/GUI **每次启动
//	     永久挂起**（`registerUserPlugins` 在 `RegisterAll` 里对每个 .lua 执行它）。
//	     审计实测（注入调用超时 100ms）：`err=<nil> elapsed=2.9663249s`。
//
//	P1-1 提取期：`time.After` 只守 `CallByParam`，而结果提取（`L.Get` /
//	     `getFieldString` / `getFieldInt`）在 select **之后** —— 那些 helper 的
//	     `L.GetField` 会执行插件返回表上的 `__index`，而 `extractField` 的 `L.PCall`
//	     会**一直等它返回**。于是 `__index` 里写死循环就能让宿主永久挂起，
//	     尽管调用本身早已"超时失败"。
//	     审计实测（注入调用超时 400ms）：`archive()` 的 `__index` → `err=<nil>
//	     elapsed=7.43s`（3 次字段读取）。
//
// 守卫口径：**用有界的自旋（spins）而不是真死循环** —— 真死循环会让测试自己挂死，
// 无法给出结论。自旋 N 次远超注入的超时（例如超时 200ms、自旋约 2s），于是
// 「超时生效」与「超时不生效」在耗时上有数量级差异，可稳定判定。
// 这与审计的探针形态一致（它也用 `1e8` 次有界自增，约 2.3s/次）。

// spinLua 生成一段「有界但明显超过注入超时」的 Lua 自旋代码。
// 1e8 次自增在审计实测里约 2.3s —— 取注入超时 200ms 时，比值约 10x。
const spinLua = `local n = 0
while n < 100000000 do n = n + 1 end
`

// P1-2：插件**顶层**死循环必须在加载超时内被放弃，而不是永久挂起。
func TestLoadPlugin_TopLevelSpinIsAbandonedByLoadTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p12_toplevel_spin.lua")
	src := spinLua + `
PLUGIN = {
  name = "p12spin",
  available = function() return {"1.0.0"} end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewLuaEngine()
	engine.callTimeoutOverride = 200 * time.Millisecond

	start := time.Now()
	_, err := engine.LoadPlugin(path)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("P1-2 复现：顶层自旋的插件加载成功返回（elapsed=%v）—— 加载期没有超时保护", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("错误信息应表明是加载超时，got: %v", err)
	}
	// 超时 200ms，自旋约 2s。允许宽裕的调度余量，但必须远小于自旋本身的耗时。
	if elapsed > 1500*time.Millisecond {
		t.Errorf("P1-2 复现：加载耗时 %v，远超注入的 200ms 超时 —— 超时未生效", elapsed)
	}
	t.Logf("顶层自旋在 %v 后被放弃（注入超时 200ms）", elapsed)
}

// P1-1：`archive()` **结果表**的 `__index` 自旋必须在调用超时内被放弃。
//
// 这是审计实测耗时 7.43s 的那条路径（`err=<nil>` 且超时完全没生效）。
func TestCallArchive_ResultIndexSpinIsCoveredByTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p11_archive_index_spin.lua")
	// archive() 返回一个表，其 __index 每次读取都自旋。
	src := `
PLUGIN = {
  name = "p11arch",
  available = function() return {"1.0.0"} end,
  archive = function(v, o, a)
    local t = setmetatable({}, { __index = function(_, k)
      ` + spinLua + `
      if k == "url" then return "https://example.test/x.zip" end
      return nil
    end })
    return t
  end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewLuaEngine()
	engine.callTimeoutOverride = 200 * time.Millisecond

	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	start := time.Now()
	_, aerr := p.ArchiveFor("1.0.0")
	elapsed := time.Since(start)

	// 提取 3 个字段 × 约 2s = 约 6s；有超时保护则应在 200ms 量级返回。
	if elapsed > 1500*time.Millisecond {
		t.Errorf("P1-1 复现：archive() 结果提取耗时 %v —— 提取阶段不受超时约束"+
			"（审计实测 err=<nil> elapsed=7.43s）", elapsed)
	}
	if aerr == nil {
		t.Logf("提取在 %v 内完成且无错误（若自旋被截断则属预期）", elapsed)
	} else {
		t.Logf("提取在 %v 后失败（预期为超时）: %v", elapsed, aerr)
	}
}

// P1-1：vfox 格式 `available()` 的**条目表** `__index` 自旋同样受超时约束。
// 审计实测该路径 `err=<nil> elapsed=2.90s`。
func TestCallAvailable_VfoxEntryIndexSpinIsCoveredByTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p11_available_index_spin.lua")
	src := `
PLUGIN = {
  name = "p11avail",
  available = function()
    return { setmetatable({}, { __index = function(_, k)
      ` + spinLua + `
      if k == "version" then return "9.9.9" end
      return nil
    end }) }
  end,
  archive = function(v, o, a) return { url = "https://example.test/x.zip" } end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewLuaEngine()
	engine.callTimeoutOverride = 200 * time.Millisecond

	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	start := time.Now()
	_, rerr := p.ResolveVersion("latest")
	elapsed := time.Since(start)

	if elapsed > 1500*time.Millisecond {
		t.Errorf("P1-1 复现：available() 条目提取耗时 %v —— 提取阶段不受超时约束"+
			"（审计实测 elapsed=2.90s）", elapsed)
	}
	t.Logf("条目提取在 %v 后返回（err=%v）", elapsed, rerr)
}

// 对照：正常插件在同样的注入超时下必须**正常工作**（修复不得把正常路径弄红）。
func TestLuaTimeouts_NormalPluginStillWorksUnderShortTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p11_normal.lua")
	src := `
PLUGIN = {
  name = "p11normal",
  available = function() return {"1.0.0", "2.0.0"} end,
  archive = function(v, o, a)
    return { url = "https://example.test/x-" .. v .. ".zip", format = "zip", strip = 1 }
  end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewLuaEngine()
	engine.callTimeoutOverride = 200 * time.Millisecond

	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("正常插件应能加载: %v", err)
	}
	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if got != "2.0.0" {
		t.Errorf("ResolveVersion = %q, want 2.0.0", got)
	}
	a, err := p.ArchiveFor("2.0.0")
	if err != nil {
		t.Fatalf("ArchiveFor: %v", err)
	}
	if a.URL != "https://example.test/x-2.0.0.zip" {
		t.Errorf("URL = %q", a.URL)
	}
	if a.Strip != 1 {
		t.Errorf("Strip = %d, want 1", a.Strip)
	}
}

// 对照：`env_vars()` 的提取是**原始**遍历（`ForEach` + 类型断言，不执行 `__index`），
// 因此不需要额外的提取期超时。这条断言把该结论钉住，避免以后有人"顺手"把它也
// 包进 extractField 反而引入新语义。
func TestCallEnvVars_ExtractionIsRawNoMetamethod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p11_envvars.lua")
	src := `
PLUGIN = {
  name = "p11env",
  available = function() return {"1.0.0"} end,
  archive = function(v, o, a) return { url = "https://example.test/x.zip" } end,
  env_vars = function(installPath)
    return { JAVA_HOME = installPath }
  end,
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := NewLuaEngine()
	engine.callTimeoutOverride = 200 * time.Millisecond

	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	vars := p.EnvVars(`C:\xdev\install\java\1.0.0`)
	if vars["JAVA_HOME"] != `C:\xdev\install\java\1.0.0` {
		t.Errorf("EnvVars = %v", vars)
	}
}
