package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 第十一轮审计（Round 10 报告待办 1/2）：Lua 超时必须**真的让插件线程停下来**，
// 而不是「放弃一个仍在吃 CPU 的 goroutine」。
//
// 缺陷现场（Round 9/10 的状态）：
//   - 超时靠 `callProtected` 起一个独立 goroutine，超时后**放弃**它；
//     VM 被 poison，但那个 goroutine 仍在 Lua 主循环里跑 —— **直到进程退出**。
//     宿主「看起来超时了」，机器上却有一个核被一直吃满。
//   - 尾递归不受 `CallStackSize` 约束：gopher-lua 对尾调用复用栈帧，
//     `local function loop(n) return loop(n+1) end` 的深度**永不增长**，
//     所以 `CallStackSize: 200` 对它完全无效（Round 10 实测 5 分钟未返回）。
//
// 修复：改用 gopher-lua 的 `LState.SetContext` —— 它把主循环切到
// `mainLoopWithContext`，在**每条指令**前检查 `ctx.Done()`，命中即抛
// `context deadline exceeded`。于是插件线程**真的被中断**。
//
// 本守卫钉住「真的停了」，而不只是「调用方拿到错误」—— 后者在修复前后都成立
//（旧实现超时后同样返回错误），**这正是它能长期潜伏的原因**：断言「返回了错误」
// 完全测不出「线程还在跑」。

// spinPlugin 写一个 archive() 内跑真死循环（非尾递归的 while true）的插件。
const spinPlugin = `PLUGIN = {
  name = "r11spin",
  available = function() return {"1.0.0"} end,
  archive = function()
    local n = 0
    while true do n = n + 1 end
    return { url = "https://e.test/x.zip", format = "zip" }
  end,
}
`

// tailRecursionPlugin 写一个**尾递归**死循环插件（CallStackSize 对它无效）。
const tailRecursionPlugin = `PLUGIN = {
  name = "r11tail",
  available = function() return {"1.0.0"} end,
  archive = function()
    local function loop(n) return loop(n + 1) end
    loop(1)
    return { url = "https://e.test/x.zip", format = "zip" }
  end,
}
`

func writeSpinPlugin(t *testing.T, body string) (*LuaEngine, *LuaPlugin) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "spin.lua")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewLuaEngine()
	pl, err := e.LoadPlugin(p)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	return e, pl
}

// busyGoroutines 返回当前 goroutine 数（粗粒度探针）。
func busyGoroutines() int { return runtime.NumGoroutine() }

// TestLuaTimeout_SpinLoopActuallyStops 是核心守卫：`while true` 超时后，
// **那个跑插件代码的 goroutine 必须真的退出**。
//
// **这条守卫为什么必须看 goroutine 而不是看返回值**（本用例初版就踩了）：
// 旧实现超时后**同样返回** `timed out` 错误 —— 断言「返回了错误」在修复前后
// **都成立**，这正是该缺陷能长期潜伏的原因。唯一的差别是**线程还活着**。
// 实测（本机，300ms 超时）：
//
//	旧实现（放弃 goroutine）：等待 2s 后 goroutine 数仍是 3（before=2）
//	新实现（ctx 中断）：      立刻回落到 2（before=2）
//
// 因此判据是：超时返回后、在**有界时间**内 goroutine 数回落到调用前水平。
func TestLuaTimeout_SpinLoopActuallyStops(t *testing.T) {
	engine, p := writeSpinPlugin(t, spinPlugin)
	engine.callTimeoutOverride = 300 * time.Millisecond

	before := busyGoroutines()

	start := time.Now()
	_, err := p.ArchiveFor("1.0.0")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("死循环插件应超时失败")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("错误应表明超时，got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("超时耗时应接近注入的 300ms，got %v", elapsed)
	}

	// **核心断言**：被中断的插件 goroutine 必须退出（goroutine 数回落）。
	// 旧实现下它永远不会回落 —— 那个 goroutine 会一直跑到进程退出。
	deadline := time.Now().Add(2 * time.Second)
	var last int
	for time.Now().Before(deadline) {
		last = busyGoroutines()
		if last <= before {
			t.Logf("spin: err=%v elapsed=%v goroutines before=%d after=%d（已回落）",
				err, elapsed, before, last)
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Errorf("待办 1/2 复现：超时后那个跑插件代码的 goroutine **仍在运行**"+
		"（goroutine before=%d, 2 秒后仍为 %d）—— 宿主只是「放弃了」它，"+
		"插件线程会一直吃 CPU 到进程退出", before, last)
}

// TestLuaTimeout_TailRecursionIsBounded 是**尾递归**守卫：
// `CallStackSize` 对它无效，但 ctx 每指令检查能中断它。
//
// 这是 Round 10 实测「5 分钟未返回、只能 kill」的那条路径。
//
// 判据同样是 **goroutine 必须退出**（不是「返回了错误」—— 旧实现也返回错误，
// 只是把 goroutine 丢在那里继续跑尾递归）。
func TestLuaTimeout_TailRecursionIsBounded(t *testing.T) {
	engine, p := writeSpinPlugin(t, tailRecursionPlugin)
	engine.callTimeoutOverride = 300 * time.Millisecond

	before := busyGoroutines()

	start := time.Now()
	_, err := p.ArchiveFor("1.0.0")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("尾递归死循环插件应超时失败（CallStackSize 对它无效，必须靠 ctx 中断）")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("错误应表明超时，got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("尾递归超时耗时应接近注入的 300ms，got %v —— ctx 中断未生效", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	var last int
	for time.Now().Before(deadline) {
		last = busyGoroutines()
		if last <= before {
			t.Logf("tail recursion aborted: err=%v elapsed=%v goroutines before=%d after=%d",
				err, elapsed, before, last)
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Errorf("待办 1/2 复现：尾递归插件超时后 goroutine **仍在运行**"+
		"（before=%d, 2 秒后仍为 %d）—— CallStackSize 对它无效，且旧实现不会中断它",
		before, last)
}

// 对照：正常插件在同样的超时下必须照常工作（修复不得把正常路径弄慢或弄坏）。
func TestLuaTimeout_NormalPluginUnaffected(t *testing.T) {
	engine, p := writeSpinPlugin(t, `PLUGIN = {
  name = "r11ok",
  available = function() return {"1.0.0", "2.0.0"} end,
  archive = function(v)
    return { url = "https://e.test/x-" .. v .. ".zip", format = "zip", strip = 1 }
  end,
}
`)
	engine.callTimeoutOverride = 2 * time.Second

	start := time.Now()
	a, err := p.ArchiveFor("2.0.0")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("正常插件不应失败: %v", err)
	}
	if a.URL != "https://e.test/x-2.0.0.zip" || a.Strip != 1 {
		t.Errorf("结果错误: %+v", a)
	}
	if elapsed > time.Second {
		t.Errorf("正常调用不应接近超时窗口，got %v", elapsed)
	}
}

// ctx 必须在调用结束后**被清理**：否则同一 VM 的下一次调用会带着已过期的 ctx，
// 立即以 context deadline exceeded 失败 —— 一个非常隐蔽的「第二次调用就坏」缺陷。
func TestRunWithContext_RestoresLStateContext(t *testing.T) {
	engine, p := writeSpinPlugin(t, `PLUGIN = {
  name = "r11twice",
  available = function() return {"1.0.0"} end,
  archive = function(v)
    return { url = "https://e.test/x.zip", format = "zip" }
  end,
}
`)
	engine.callTimeoutOverride = 2 * time.Second

	// 连续两次调用都必须成功：第二次若带着第一次的过期 ctx 就会立刻失败。
	for i := 0; i < 3; i++ {
		if _, err := p.ArchiveFor("1.0.0"); err != nil {
			t.Fatalf("第 %d 次调用失败（ctx 未被清理？）: %v", i+1, err)
		}
	}
}
