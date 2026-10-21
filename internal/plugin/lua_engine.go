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
// 因为一个插件的内存用量而退出，比超时更糟（一个失控插件能杀掉整个 CLI）。
//
// 第十二轮审计改用**自建内存看门狗**（`watchMemory`）：同样轮询
// `runtime.ReadMemStats`，但超限时**取消这个 ctx** —— 于是 Lua 主循环在下一条指令
// 处抛错、插件线程**真的停下**，调用方拿到一个普通错误。既不退出进程，也不需要
// gopher-lua 层的字节配额（它没有分配钩子：`newLTable` 直接 `make`）。
func (e *LuaEngine) runWithContext(L *lua.LState, timeout time.Duration, fn func() error) error {
	return e.runWithContextLimited(L, timeout, e.effectiveMemLimit(), fn)
}

// runWithContextLimited 是 runWithContext 的完整形态，可注入内存上限
// （0 表示不监控；测试用小上限来快速触发）。
func (e *LuaEngine) runWithContextLimited(L *lua.LState, timeout time.Duration, memLimit uint64, fn func() error) error {
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
	defer stopWatch()

	doneCh := make(chan error, 1)
	callProtected(doneCh, fn)

	select {
	case err := <-doneCh:
		return err
	case <-ctx.Done():
		// ctx 已取消，主循环会在下一条指令处抛错并让 doneCh 收到结果。
		// 给它一个很短的窗口把错误写出来（通常立刻就到）。
		detail := "context deadline exceeded"
		select {
		case err := <-doneCh:
			if err != nil {
				detail = err.Error()
			}
		case <-time.After(50 * time.Millisecond):
		}

		// 第十二轮审计：**必须区分内存中止与时间超时**。
		//
		// 否则一次 41ms 就被内存看门狗掐掉的调用会报
		// `timed out after 10s` —— 用户看到「等了 10 秒」，实际只过了 41 毫秒，
		// 而且完全看不出真正原因是内存。这种**错误归因**比没有错误更糟：
		// 它会把排查方向引向网络/超时，而真凶是插件的分配量。
		if memExceeded() {
			return fmt.Errorf("Lua memory limit exceeded (%d MiB heap growth) — "+
				"plugin execution aborted: %s", memLimit>>20, detail)
		}

		// 时间超时：**错误文案必须保留 "timed out"** —— 这是仓库既有契约
		//（`lua_hardening_test.go` 断言 `strings.Contains(err, "timed out")`，
		// `vfox_compat.go` 的调用方也按该子串区分「超时」与「插件自身报错」）。
		// ctx 的原始文案是 `context deadline exceeded`，直接透出会破坏该契约，
		// 因此统一包装成 `timed out after <d> (Lua execution aborted: <detail>)`。
		return fmt.Errorf("timed out after %s (Lua execution aborted: %s)", timeout, detail)
	}
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
	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	done := make(chan struct{})
	go func() {
		// 20ms 的采样间隔：325 MiB 在 367ms 内产生（约 0.9 MiB/ms），
		// 20ms 的粒度足以在远超上限前就抓住；同时不会明显增加 CPU 负担。
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				var used uint64
				if m.HeapAlloc > base.HeapAlloc {
					used = m.HeapAlloc - base.HeapAlloc
				}
				if used > limit {
					log.Printf("[plugin] Lua memory limit exceeded: %.1f MiB > %.1f MiB — aborting plugin execution",
						float64(used)/(1<<20), float64(limit)/(1<<20))
					tripped.Store(true)
					cancel() // 让 mainLoopWithContext 在下一条指令处中断
					return
				}
			}
		}
	}()
	return func() { close(done) }, tripped.Load
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
