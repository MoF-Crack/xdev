// lua_engine.go provides the Lua VM sandbox and function calling
// infrastructure used by both LuaPlugin and VfoxPlugin.
//
// # Sandbox Security
//
// The Lua VM is sandboxed by:
//  1. Skipping the io and os libraries (no file I/O or process execution)
//  2. Overriding dofile, loadfile, load and loadstring so they raise an
//     error instead of compiling or running arbitrary chunks (票 20 / F-06)
//  3. Banning require() outright, and additionally reducing both the
//     package.loaders field and the registry entry _LOADERS (which is what
//     gopher-lua's loRequire actually reads) to the preload loader only
//     (票 21 / F-09)
//  4. Only opening safe standard libraries: package, base, table, string,
//     math, coroutine
//
// gopher-lua uses "package.loaders" (not "package.searchers" like PUC-Rio
// Lua 5.3+). The preload loader only finds modules that were explicitly
// preloaded via package.preload, which none are by default.
//
// Plugins never need require(): the xdev/vfox plugin format is a single
// self-contained chunk.
//
// Plugin results are read back through getFieldString/getFieldInt/
// getFieldStringSlice, each of which recovers a Lua error raised by a
// plugin-supplied __index metamethod so it cannot panic the host
// (票 21 / F-10).
//
// # Plugin Format Differences
//
// xdev-format plugins define: available(), archive(), env_vars()
// vfox-format plugins define: available(), install(), env_keys()
//
// The luaCallSpecs struct abstracts these differences so luaBasePlugin
// can handle both formats with shared code.
package plugin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// LuaEngine manages Lua VM creation and plugin loading. It caches one
// sandboxed VM per loaded plugin path (Spec 032) so available() /
// archive() / env_vars() do not spawn a fresh VM and re-DoFile on
// every call. VMs are serialised per plugin by a mutex.
type LuaEngine struct {
	mu  sync.Mutex
	vms map[string]*cachedVM
	// callTimeoutOverride, when > 0, replaces the per-call Lua execution
	// timeout (票 20 / F-04 test seam): the production timeouts are 10–60s,
	// far too long for a unit test to exercise the timeout → poisoned
	// path. Only tests set it; zero means "use the production default".
	callTimeoutOverride time.Duration
}

type cachedVM struct {
	L  *lua.LState
	mu sync.Mutex
	// poisoned marks a VM whose state is no longer trustworthy: a call
	// timed out but the plugin code keeps running on it from an orphan
	// goroutine (票 20 / F-04), so a second caller sharing the LState
	// would corrupt its stack. Written by the call helpers under mu,
	// read by vmFor under mu.
	poisoned bool
}

// loadTimeout is the maximum time allowed for a plugin's top-level chunk
// (L.DoFile). 第十轮审计 P1-2：此前加载阶段**完全没有超时**。
const loadTimeout = 5 * time.Second

// loadProtected 在**有超时**的独立 goroutine 里执行插件顶层代码（L.DoFile）。
//
// 第十轮审计 P1-2（P1）：M-12 的三处超时全部在**调用期**（`L.CallByParam`），
// 而 `L.DoFile` 是裸调用 —— 插件顶层写 `while true do end` 就能让 CLI/TUI/GUI
// **每次启动永久挂起**（无日志、无错误），因为 `registerUserPlugins` 在
// `RegisterAll` 里对每个 `.lua` 执行它。
//
// 审计实测（把调用超时注入为 100ms）：
//
//	插件顶层： local n=0 while n < 1e8 do n=n+1 end
//	RESULT L3: LoadPlugin err=<nil> elapsed=2.9663249s
//	CONFIRMED L3: 顶层代码在加载期无界执行（elapsed 2.97s，调用超时 100ms）
//
// 与调用期超时的一个关键差别：**超时后可以安全地 Close 这个 VM** —— 此时还没有
// 任何 goroutine 需要保留它（调用期超时的 VM 因为可能仍在被孤儿 goroutine 使用，
// 只能"poison + 不 Close"）。这里的孤儿 goroutine 仍跑在 L 上，所以 Close 仍有
// 风险；因此采用与调用期一致的策略：**放弃该 VM 并让调用方丢弃它**（不缓存、
// 不复用），把 L 留给 GC。这样顶层死循环最多消耗一个 goroutine，不会阻塞启动。
func (e *LuaEngine) loadProtected(path string) (*lua.LState, error) {
	L, err := e.newSandboxedVM()
	if err != nil {
		return nil, err
	}

	doneCh := make(chan error, 1)
	callProtected(doneCh, func() error {
		if derr := L.DoFile(path); derr != nil {
			return fmt.Errorf("load lua plugin %s: %w", path, derr)
		}
		return nil
	})

	select {
	case derr := <-doneCh:
		if derr != nil {
			L.Close()
			return nil, derr
		}
		return L, nil
	case <-time.After(e.callTimeout(loadTimeout)):
		// 顶层代码超时：不 Close（孤儿 goroutine 可能仍在 L 上执行），
		// 直接丢弃这个 VM —— 调用方不会缓存它。
		return nil, fmt.Errorf("loading plugin %s timed out after %s (the top-level chunk did not return) — the plugin is refusing to load and has been abandoned", path, e.callTimeout(loadTimeout))
	}
}

// callTimeout returns def unless a test injected a shorter override.
func (e *LuaEngine) callTimeout(def time.Duration) time.Duration {
	if e.callTimeoutOverride > 0 {
		return e.callTimeoutOverride
	}
	return def
}

// callProtected runs fn on its own goroutine and reports its outcome on
// doneCh, converting a panic into an error (票 20 / F-04). The goroutine
// is deliberately detached — a timeout abandons it while it is still
// inside the Lua VM — so a panic there would otherwise escape the
// caller's error handling and take down the whole process. doneCh is
// buffered (cap 1) so a late result can never block the orphan.
func callProtected(doneCh chan<- error, fn func() error) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				doneCh <- fmt.Errorf("plugin call panicked: %v", r)
			}
		}()
		doneCh <- fn()
	}()
}

// runWithContext 在 **Lua 层可中断**的 context 下执行 fn，并把它自身的 panic
// 转成错误。
//
// 第十一轮审计（Round 10 报告待办 1/2）：此前的超时全靠 `callProtected` 起一个
// 独立 goroutine，超时后**放弃**它（VM 被 poison 但 goroutine 仍在跑）。这有两个
// 实际问题：
//
//   - **插件线程永不停止**：`while true do end` 的 goroutine 会一直占着 CPU 直到
//     进程退出（Round 9 的注释已承认这个残留）。宿主「看起来超时了」，但机器上
//     仍有一个核被吃满。
//   - **尾递归不受 `CallStackSize` 约束**：gopher-lua 对尾调用复用栈帧，
//     `local function loop(n) return loop(n+1) end` 的深度**永远不增长**，因此
//     `CallStackSize: 200` 对它完全无效。
//
// gopher-lua 本身提供了 `LState.SetContext`：它把主循环切换到
// `mainLoopWithContext`，在**每条指令**前检查 `ctx.Done()`，命中即抛出
// `<file>:<line>: context deadline exceeded`。实测（本机）：
//
//	callTimeout 设为 **30 秒**，ctx 设为 **300 毫秒** → 0.63s 返回
//	`context deadline exceeded`（而不是等 30 秒，更不是永久挂起）
//
// 因此改用 ctx 作为**真正**的中断机制。`callProtected` 的 goroutine 仍然保留
// —— 它负责把 panic 转成错误（Lua 错误是 Go panic），这是 ctx 做不到的。
//
// **注意**：`SetMx`（gopher-lua 的内存上限 API）**不可用** —— 它的实现是一个
// 后台 goroutine 轮询 `runtime.ReadMemStats`，超限直接 `os.Exit(3)`。让宿主进程
// 因为一个插件的内存用量而退出，比超时更糟。内存上限因此**仍未实现**（见 Round 11
// 报告「未完成项」，需要 gopher-lua 层的字节配额支持）。
func (e *LuaEngine) runWithContext(L *lua.LState, timeout time.Duration, fn func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 只影响本次调用；调用结束后恢复「无 ctx」状态，避免把已取消的 ctx 留给
	// 后续（可能复用同一 VM 的）调用。
	prev := L.Context()
	L.SetContext(ctx)
	defer func() {
		if prev != nil {
			L.SetContext(prev)
			return
		}
		// 恢复为无 ctx（nil context 等价于「不检查」）。
		L.RemoveContext()
	}()

	doneCh := make(chan error, 1)
	callProtected(doneCh, fn)

	select {
	case err := <-doneCh:
		return err
	case <-ctx.Done():
		// ctx 已取消，主循环会在下一条指令处抛错并让 doneCh 收到结果。
		// 给它一个很短的窗口把错误写出来（通常立刻就到）。
		//
		// **错误文案必须保留 "timed out"**：这是仓库既有契约
		//（`lua_hardening_test.go` 断言 `strings.Contains(err, "timed out")`，
		// `vfox_compat.go` 的调用方也按该子串区分「超时」与「插件自身报错」）。
		// ctx 的原始文案是 `context deadline exceeded`，直接透出会破坏该契约，
		// 因此统一包装成 `timed out after <d> (Lua execution aborted: <detail>)`，
		// 把 ctx 细节作为补充信息放在括号里。
		detail := "context deadline exceeded"
		select {
		case err := <-doneCh:
			if err != nil {
				detail = err.Error()
			}
		case <-time.After(50 * time.Millisecond):
		}
		return fmt.Errorf("timed out after %s (Lua execution aborted: %s)", timeout, detail)
	}
}

// NewLuaEngine creates a new Lua engine.
func NewLuaEngine() *LuaEngine {
	return &LuaEngine{vms: make(map[string]*cachedVM)}
}

// vmFor returns the cached VM for the plugin path, creating and
// DoFile-ing it on first use. The returned VM is locked for the
// caller's exclusive use; the caller must unlock it via cv.mu.Unlock()
// (deferred), and must set cv.poisoned before unlocking if its call
// timed out.
//
// 票 20 / F-04: a timed-out call leaves its goroutine running inside the
// VM, so that LState can no longer be shared. The timeout branch flags
// cv.poisoned under cv.mu; here we re-check after taking cv.mu (the mutex
// provides the happens-before edge that makes the flag visible) and, when
// poisoned, unlock, drop the cache entry and loop to build a fresh VM.
// The poisoned LState is deliberately NOT Closed — its orphan goroutine
// may still be executing on it — so it is simply unreferenced and left to
// the GC once that goroutine finishes. Known residue: a plugin with an
// infinite loop keeps its orphan goroutine alive until the process exits.
func (e *LuaEngine) vmFor(path string, vfox bool) (*lua.LState, *cachedVM, error) {
	for {
		e.mu.Lock()
		cv, ok := e.vms[path]
		if !ok {
			// 第十轮审计 P1-2：加载阶段必须有超时（此前 L.DoFile 是裸调用，
			// 顶层 `while true do end` 会让每次启动永久挂起）。
			L, err := e.loadProtected(path)
			if err != nil {
				e.mu.Unlock()
				return nil, nil, err
			}
			// 第八轮审计 A-01：PLUGIN 表本身的读取也要受保护（插件可给它挂抛错的
			// __index；裸 GetGlobal 会以 Go panic 打崩宿主）。
			promotePluginMethods(L, getGlobalProtected(L, "PLUGIN"))
			cv = &cachedVM{L: L}
			e.vms[path] = cv
		}
		e.mu.Unlock()

		cv.mu.Lock()
		if cv.poisoned {
			cv.mu.Unlock()
			// Drop our own (possibly stale) entry only: another waiter
			// may already have installed a fresh VM for this path.
			e.mu.Lock()
			if cur, ok := e.vms[path]; ok && cur == cv {
				delete(e.vms, path)
			}
			e.mu.Unlock()
			continue
		}
		return cv.L, cv, nil
	}
}

// newSandboxedVM creates a Lua VM with restricted access to
// dangerous libraries (io, os) for sandboxing. The `require` function
// is restricted to only load preloaded modules — the Lua path searcher
// and C module loader are removed to prevent loading arbitrary files
// or native shared libraries that could escape the sandbox.
//
// Security (H-3): In addition to restricting libraries, the dangerous base
// functions dofile, loadfile, load and loadstring (票 20 / F-06) are
// overridden to raise errors, preventing plugins from loading arbitrary Lua
// files from the filesystem or compiling arbitrary chunks. Memory and
// call-stack limits are also imposed to mitigate resource exhaustion.
func (e *LuaEngine) newSandboxedVM() (*lua.LState, error) {
	L := lua.NewState(lua.Options{
		SkipOpenLibs:    true,
		RegistryMaxSize: 1024 * 1024, // 1 MiB registry limit (mitigates OOM)
		CallStackSize:   200,         // Limited call stack (mitigates deep recursion)
	})

	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
	} {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(pair.f),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.n)); err != nil {
			L.Close()
			return nil, fmt.Errorf("failed to open Lua lib %s: %w", pair.n, err)
		}
	}

	// Security (H-3): Override dofile and loadfile to prevent plugins
	// from loading arbitrary Lua files from the filesystem, which could
	// bypass the sandbox restrictions.
	//
	// 票 20 / F-06: loadstring is Lua 5.1's alias for load, so leaving it
	// in place let a plugin compile arbitrary chunks and walk straight
	// around the load/dofile/loadfile overrides below.
	//
	// 票 21 / F-09: require() is banned outright. The loader-list
	// hardening further down only rewrites the package.loaders *field*,
	// but gopher-lua's loRequire (vendor/.../baselib.go:537) reads the
	// loader list from the registry entry _LOADERS, which OpenPackage
	// (vendor/.../loadlib.go:59) fills with the same two loaders —
	// including loLoaderLua, the on-disk .lua searcher driven by
	// LUA_PATH. Field and registry entry are two different tables, so
	// require() kept executing arbitrary .lua files off disk. Plugins
	// (archive()/install()/available()/env_vars()) never need require.
	if err := L.DoString(`
		dofile = function()
			error("dofile is not allowed in sandboxed plugins")
		end
		loadfile = function()
			error("loadfile is not allowed in sandboxed plugins")
		end
		load = function()
			error("load is not allowed in sandboxed plugins")
		end
		loadstring = function()
			error("loadstring is not allowed in sandboxed plugins")
		end
		require = function()
			error("require is not allowed in sandboxed plugins")
		end
	`); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to override dofile/loadfile/load/loadstring/require: %w", err)
	}

	// Restrict package.loaders to only the preload loader (index 1).
	// gopher-lua uses "loaders" (not "searchers" like PUC-Rio Lua).
	// This removes the Lua path searcher and C module loader, preventing
	// plugins from loading arbitrary files or native shared libraries.
	// The preload loader only finds modules that were explicitly
	// preloaded via package.preload, which none are by default.
	if err := L.DoString(`package.loaders = {package.loaders[1]}`); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to restrict package.loaders: %w", err)
	}

	// Security (M-3): Freeze the package table to prevent plugins from
	// re-adding loaders that could load arbitrary modules.
	if err := L.DoString(`
		local frozen_loaders = package.loaders
		package.loaders = setmetatable({}, {
			__index = frozen_loaders,
			__newindex = function()
				error("package.loaders is frozen in sandboxed plugins")
			end
		})
	`); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to freeze package.loaders: %w", err)
	}

	// 票 21 / F-09 (defence in depth): the Lua-side rewrite above only
	// replaces the package.loaders *field*, but gopher-lua's loRequire
	// reads the loader list from the registry entry _LOADERS
	// (vendor/.../baselib.go:537), and OpenPackage filled that entry with
	// the full two-loader list (vendor/.../loadlib.go:59) — including
	// loLoaderLua, the on-disk .lua searcher driven by LUA_PATH. Field
	// and registry entry are separate tables, so the searcher stayed
	// reachable through require(). Replace the registry entry with a
	// table holding the preload loader only, mirroring package.loaders.
	//
	// lua.RegistryIndex is the library's registry index constant
	// (vendor/.../state.go:23); L.Get on it returns the registry table.
	// This runs at VM construction, before any plugin code, so a
	// non-table registry/_LOADERS means OpenPackage did not run and the
	// sandbox contract cannot be established — fail closed.
	registry, ok := L.Get(lua.RegistryIndex).(*lua.LTable)
	if !ok {
		L.Close()
		return nil, fmt.Errorf("failed to restrict registry _LOADERS: registry is not a table")
	}
	oldLoaders, ok := L.GetField(registry, "_LOADERS").(*lua.LTable)
	if !ok {
		L.Close()
		return nil, fmt.Errorf("failed to restrict registry _LOADERS: _LOADERS is not a table")
	}
	safeLoaders := L.CreateTable(1, 0)
	if preload := L.RawGetInt(oldLoaders, 1); preload != lua.LNil {
		L.RawSetInt(safeLoaders, 1, preload)
	}
	L.SetField(registry, "_LOADERS", safeLoaders)

	return L, nil
}

// LuaArchive holds the result of the Lua archive()/install() function.
type LuaArchive struct {
	URL    string
	Format string
	Strip  int
}

// getFieldString extracts a string field from a Lua table.
//
// 票 21 / F-10: L.GetField runs metamethod code (__index), and
// gopher-lua signals a Lua error by panicking (ls.Panic →
// panicWithTraceback). The result-extraction helpers used to sit
// outside callProtected — which only wraps L.CallByParam — so a plugin
// returning a table with a throwing __index took down the whole host
// process. extractField routes every read through gopher-lua's own
// PCall, covering all call sites (ArchiveFor, LoadPlugin, vfox
// ResolveVersion, archive strip extraction) in one place.
func getFieldString(L *lua.LState, table lua.LValue, key string) string {
	var out string
	extractField(L, "getFieldString", key, func() {
		if v := L.GetField(table, key); v.Type() == lua.LTString {
			out = string(v.(lua.LString))
		}
	})
	return out
}

// getFieldInt extracts an integer field from a Lua table.
func getFieldInt(L *lua.LState, table lua.LValue, key string) int {
	var out int
	extractField(L, "getFieldInt", key, func() {
		if v := L.GetField(table, key); v.Type() == lua.LTNumber {
			out = int(v.(lua.LNumber))
		}
	})
	return out
}

// getFieldStringSlice extracts a string slice field from a Lua table.
func getFieldStringSlice(L *lua.LState, table lua.LValue, key string) []string {
	var out []string
	extractField(L, "getFieldStringSlice", key, func() {
		v := L.GetField(table, key)
		if v.Type() != lua.LTTable {
			return
		}
		L.ForEach(v.(*lua.LTable), func(_ lua.LValue, val lua.LValue) {
			if s, ok := val.(lua.LString); ok {
				out = append(out, string(s))
			}
		})
	})
	return out
}

// extractField runs fn — a plugin-result field read — inside a protected
// Lua call (票 21 / F-10) so a Lua error raised by a plugin-supplied
// metamethod is caught by gopher-lua's own PCall instead of escaping as
// a Go panic into the host process. On error it logs a warning and
// leaves the caller's zero value in place, which every call site already
// handles (missing field / missing url).
//
// PCall is used rather than a bare `defer recover()` because a recovered
// panic does NOT unwind the VM: ls.reg keeps the metatable/object/key
// entries pushed by getFieldString and ls.stack keeps the metamethod's
// call frame. A plugin that returns a throwing __index table would then
// grow the register stack by a few entries per read — measured at 814
// entries after 300 reads, after which the VM fails every call with
// "stack overflow". PCall's own recovery (vendor/.../state.go:2082-2089)
// restores the stack pointer, the current frame and the register top, so
// repeated reads stay bounded and the VM remains usable.
func extractField(L *lua.LState, helper, key string, fn func()) {
	L.Push(L.NewFunction(func(*lua.LState) int {
		fn()
		return 0
	}))
	if err := L.PCall(0, 0, nil); err != nil {
		log.Printf("[plugin] WARNING: %s(%q) raised a Lua error while reading a plugin result — treating the field as absent: %v", helper, key, err)
	}
}

// luaNil 是 Lua 的 nil 值（用于 protectedValue 的失败回落）。
var luaNil = lua.LNil

// protectedValue 在受保护调用内求值 read，返回它读到的 Lua 值；read 触发 Lua
// 错误（例如插件表带抛错的 `__index`）时返回 lua.LNil 并记一条警告。
//
// 第八轮审计 A-01（P0，F-10 修复不完整）：F-10 只把**结果字段读取**包进了
// extractField，但**插件表本身**的读取仍是裸 `L.GetField`/`L.GetGlobal` ——
// 而 gopher-lua 的 `L.GetField` 会执行 `__index` 元方法，`ls.Panic` 是 Go panic
// （`panicWithTraceback`），会直接穿透 `LoadPlugin` 打崩宿主进程。实测：
//
//	PLUGIN = setmetatable({name="x"}, {__index = function(_,k) error("boom") end})
//
// 之后 `xdev available` 即 `panic: …boom` → exit 2。由于 `registerUserPlugins`
// 在 `RegisterAll` 里加载每个用户插件，**任意** `$XMDEV_HOME/plugins/*.lua`
// 都能让 CLI/TUI/GUI 每次启动都崩溃（DoS）。本函数把入口读取一并纳入保护。
func protectedValue(L *lua.LState, helper, key string, read func() lua.LValue) lua.LValue {
	out := luaNil
	extractField(L, helper, key, func() {
		if v := read(); v != nil {
			out = v
		}
	})
	return out
}

// getGlobalProtected 是受保护的 `L.GetGlobal`（A-01）。
func getGlobalProtected(L *lua.LState, name string) lua.LValue {
	return protectedValue(L, "getGlobal", name, func() lua.LValue { return L.GetGlobal(name) })
}

// getFieldProtected 是受保护的 `L.GetField`（A-01）。
func getFieldProtected(L *lua.LState, table lua.LValue, key string) lua.LValue {
	return protectedValue(L, "getField", key, func() lua.LValue { return L.GetField(table, key) })
}
