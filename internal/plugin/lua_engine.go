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
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
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
	// memLimitOverride, when non-nil, replaces the per-call Lua heap-growth
	// limit（Round 12 test seam）。用指针而不是 `uint64`：**0 是有意义的取值**
	//（关闭监控），必须能与「未设置」区分开。
	memLimitOverride *uint64
}

// effectiveMemLimit 返回本次调用生效的内存上限（字节；0 = 关闭监控）。
func (e *LuaEngine) effectiveMemLimit() uint64 {
	if e.memLimitOverride != nil {
		return *e.memLimitOverride
	}
	return luaMemoryLimit()
}

type cachedVM struct {
	L  *lua.LState
	mu sync.Mutex
	// A VM that failed mid-call is rebuilt on the next use. Contending
	// callers share the result of one cache initialization, including errors.
	poisoned bool
	initErr  error
}

// loadTimeout is the maximum time allowed for a plugin's top-level chunk
// (L.DoFile). 第十轮审计 P1-2：此前加载阶段**完全没有超时**。
const loadTimeout = 5 * time.Second

// loadProtected runs the chunk and its metadata/promotion callback under one
// VM context. Cancellation must reach the VM, including its coroutines, before
// this function may close a failed state or publish a successful state.
func (e *LuaEngine) loadProtected(path string, afterLoad func(*lua.LState) error) (*lua.LState, error) {
	L, err := e.newSandboxedVM()
	if err != nil {
		return nil, err
	}
	err = e.runWithContext(L, e.callTimeout(loadTimeout), func() error {
		if err := L.DoFile(path); err != nil {
			return err
		}
		if afterLoad != nil {
			return afterLoad(L)
		}
		return nil
	})
	if err != nil {
		// runWithContext has returned from the execution itself. There is no
		// detached Lua worker which could still access this state.
		L.Close()
		return nil, fmt.Errorf("load lua plugin %s: %w", path, err)
	}
	return L, nil
}

// callTimeout returns def unless a test injected a shorter override.
func (e *LuaEngine) callTimeout(def time.Duration) time.Duration {
	if e.callTimeoutOverride > 0 {
		return e.callTimeoutOverride
	}
	return def
}

// callProtected converts a panic on the VM owner goroutine to an error. Stack
// unwinding finishes before the caller can poison, release or close the VM.
func callProtected(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin call panicked: %v", r)
		}
	}()
	return fn()
}

// HeapAlloc is process-wide. Count starts as well as active calls so a short
// overlapping call entirely between samples invalidates the old baseline too.
// luaSampleMu protects accounting and sample snapshots only, never plugin code.
var luaActive atomic.Int32
var luaSampleMu sync.Mutex
var luaStarts uint64

func beginLuaExecution() {
	luaSampleMu.Lock()
	luaStarts++
	luaActive.Add(1)
	luaSampleMu.Unlock()
}

// runWithContext owns the VM until fn has actually returned. Pure Lua checks
// cancellation on every instruction; coroutine resume/wrap rebind that same
// budget before entering a child VM (see lua_execution.go). No execution worker
// is abandoned, and context restoration/Close cannot race with Lua execution.
// A blocking Go callback must return cooperatively: context cannot forcibly
// interrupt Go/native code. Hard deadlines for those require process isolation.
func (e *LuaEngine) runWithContext(L *lua.LState, timeout time.Duration, fn func() error) error {
	return e.runWithContextLimited(L, timeout, e.effectiveMemLimit(), fn)
}

// runWithContextLimited 是 runWithContext 的完整形态，可注入内存上限
// （0 表示不监控；测试用小上限来快速触发）。
func (e *LuaEngine) runWithContextLimited(L *lua.LState, timeout time.Duration, memLimit uint64, fn func() error) error {
	beginLuaExecution()
	defer luaActive.Add(-1)

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

	// 内存看门狗：超限时 cancel 这个 ctx，让 Lua 主循环在下一条指令处中断。
	// 返回的 stop 会停掉轮询 goroutine（否则每次调用泄漏一个 ticker）；
	// 返回的 exceeded 用来在下面**区分**「内存中止」与「时间超时」。
	stopWatch, memExceeded := watchMemory(ctx, cancel, memLimit)
	callErr := callProtected(fn)
	stopWatch() // join the sampler before reading its final verdict

	// PCall/pcall and coroutine.resume may catch a cancellation error. The
	// outer boundary must still report the exhausted budget, even if the
	// callback returned nil or converted it to a missing metadata field.
	if ctx.Err() != nil {
		detail := ctx.Err().Error()
		if callErr != nil {
			detail = callErr.Error()
		}
		if memExceeded() {
			return fmt.Errorf("Lua memory limit exceeded (%d MiB heap growth) — "+
				"plugin execution aborted: %s", memLimit>>20, detail)
		}
		return fmt.Errorf("timed out after %s (Lua execution aborted: %s)", timeout, detail)
	}
	return callErr
}

// luaMemoryLimitDefault 是单次 Lua 调用允许的**堆增长**上限。
//
// 第十二轮审计：实测一个普通的 200 万次表构造循环
// （`local t = {} for i=1,2000000 do t[i] = {i} end`）在 **367ms** 内分配
// **325 MiB** 且**完全没有上限** —— 而这远不是最坏情况（`while true` 里塞表
// 可以在超时窗口内吃光内存）。64 MiB 比任何正常 `available`/`archive`/`env_vars`
// 返回值高三个数量级（它们只返回几个字符串），因此不会误伤合法插件。
//
// 可用环境变量 `XDEV_LUA_MEM_LIMIT_MB` 覆盖（0 = 关闭监控）。
const luaMemoryLimitDefault = 64 << 20 // 64 MiB

// luaMemoryLimit 返回当前生效的内存上限（字节；0 = 不监控）。
func luaMemoryLimit() uint64 {
	if v := os.Getenv("XDEV_LUA_MEM_LIMIT_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return uint64(n) << 20
		}
		log.Printf("[plugin] WARNING: ignoring invalid XDEV_LUA_MEM_LIMIT_MB=%q", v)
	}
	return luaMemoryLimitDefault
}

// watchMemory 轮询堆占用，超过 limit（**相对于开始时的基线**）时调用 cancel
// 中断 Lua 执行。
//
// 返回：
//   - stop：停止轮询（必须调用，否则每次插件调用泄漏一个 ticker goroutine）
//   - exceeded：报告**是否**因内存超限而中断。调用方据此区分「内存中止」与
//     「时间超时」—— 不区分的话，41ms 就被掐掉的调用会报 `timed out after 10s`，
//     既不符合事实、又把排查方向引向网络/超时（真凶是插件分配量）。
//
// 为什么用**增量**而不是绝对 `HeapAlloc`：宿主进程本身可能已经占了几百 MiB
// （GUI/构建缓存等），用绝对阈值会让所有插件调用立刻超限。我们只约束**这次调用
// 新增**的内存。
//
// 为什么不用 gopher-lua 的 `SetMx`：它超限直接 `os.Exit(3)` —— 一个插件就能把整个
// CLI 杀掉。这里改走 ctx 取消，插件线程会真正停下并返回普通错误。
//
// limit == 0 时直接返回空操作（不启 goroutine）。
func watchMemory(ctx context.Context, cancel context.CancelFunc, limit uint64) (stop func(), exceeded func() bool) {
	var tripped atomic.Bool
	if limit == 0 {
		return func() {}, func() bool { return false }
	}
	base, epoch, valid := luaHeapSample()
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		// 20ms 的采样间隔：325 MiB 在 367ms 内产生（约 0.9 MiB/ms），
		// 20ms 的粒度足以在远超上限前就抓住；同时不会明显增加 CPU 负担。
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		// 必须**连续两次**超限才中止：HeapAlloc 是进程级指标，GC 未回收的
		// 垃圾或宿主的瞬时分配都可能让单次采样出现 >limit 的尖峰。整包测试
		// 中曾因此把一次正常的 `available()` 误判为 64 MiB 超限。两次采样
		// 只需要多 20ms，而真实失控分配（325 MiB/367ms）仍远早于超时。
		over := 0
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				heap, starts, alone := luaHeapSample()
				if !alone || !valid || starts != epoch {
					base, epoch, valid, over = heap, starts, alone, 0
					continue
				}
				var used uint64
				if heap > base {
					used = heap - base
				}
				if used <= limit {
					over = 0
					continue
				}
				over++
				if over < 2 {
					continue
				}
				// 20261021 审计 P2-8：把判定依据一起打出来，便于区分
				// “真超限”与“并发/宿主分配污染基线”。
				log.Printf("[plugin] Lua memory limit exceeded: used=%.1f MiB > limit=%.1f MiB "+
					"(base=%.1f MiB, total_heap=%.1f MiB, goroutines=%d, samples=%d) — aborting plugin execution",
					float64(used)/(1<<20), float64(limit)/(1<<20),
					float64(base)/(1<<20), float64(heap)/(1<<20),
					runtime.NumGoroutine(), over)
				tripped.Store(true)
				cancel() // 让 mainLoopWithContext 在下一条指令处中断
				return
			}
		}
	}()
	return func() { close(done); <-joined }, tripped.Load
}

// A sample is usable only during an uninterrupted single-execution interval.
// If calls overlap, resume sampling from a fresh baseline once they separate.
func luaHeapSample() (heap, starts uint64, alone bool) {
	luaSampleMu.Lock()
	defer luaSampleMu.Unlock()
	if luaActive.Load() != 1 {
		return 0, luaStarts, false
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc, luaStarts, true
}

// NewLuaEngine creates a new Lua engine.
func NewLuaEngine() *LuaEngine {
	return &LuaEngine{vms: make(map[string]*cachedVM)}
}

// vmFor returns a VM locked for exclusive use by the caller. A newly inserted
// per-path slot is locked before publication, so same-path callers wait for that
// initialization; other paths only share the short map lock. Arbitrary plugin
// code (loading, metadata or promotion) never executes under e.mu.
func (e *LuaEngine) vmFor(path string, vfox bool) (*lua.LState, *cachedVM, error) {
	for {
		e.mu.Lock()
		cv, ok := e.vms[path]
		if !ok {
			cv = &cachedVM{}
			cv.mu.Lock() // new slot: this cannot wait for another caller
			e.vms[path] = cv
			e.mu.Unlock()

			L, err := e.loadProtected(path, func(L *lua.LState) error {
				promotePluginMethods(L, getGlobalProtected(L, "PLUGIN"))
				return nil
			})
			if err != nil {
				cv.initErr = err
				e.mu.Lock()
				if e.vms[path] == cv {
					delete(e.vms, path)
				}
				e.mu.Unlock()
				cv.mu.Unlock()
				return nil, nil, err
			}
			cv.L = L
			return L, cv, nil
		}
		e.mu.Unlock()

		cv.mu.Lock()
		if cv.initErr != nil {
			err := cv.initErr
			cv.mu.Unlock()
			return nil, nil, err
		}
		if cv.poisoned {
			// Execution is synchronous and has exited before poisoning. It
			// is safe to close now, under the same per-VM ownership lock.
			if cv.L != nil && !cv.L.IsClosed() {
				cv.L.Close()
			}
			e.mu.Lock()
			if e.vms[path] == cv {
				delete(e.vms, path)
			}
			e.mu.Unlock()
			cv.mu.Unlock()
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

	if err := protectCoroutineResumes(L); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to protect coroutine execution: %w", err)
	}
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
