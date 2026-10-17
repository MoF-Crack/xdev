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
			L, err := e.newSandboxedVM()
			if err != nil {
				e.mu.Unlock()
				return nil, nil, err
			}
			if err := L.DoFile(path); err != nil {
				L.Close()
				e.mu.Unlock()
				label := "lua"
				if vfox {
					label = "vfox"
				}
				return nil, nil, fmt.Errorf("load %s plugin %s: %w", label, path, err)
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
