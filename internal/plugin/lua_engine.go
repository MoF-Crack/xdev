// lua_engine.go provides the Lua VM sandbox and function calling
// infrastructure used by both LuaPlugin and VfoxPlugin.
//
// # Sandbox Security
//
// The Lua VM is sandboxed by:
//  1. Skipping the io and os libraries (no file I/O or process execution)
//  2. Restricting package.loaders to only the preload loader (index 1),
//     which prevents loading arbitrary Lua files or C shared libraries
//  3. Only opening safe standard libraries: package, base, table, string,
//     math, coroutine
//
// gopher-lua uses "package.loaders" (not "package.searchers" like PUC-Rio
// Lua 5.3+). The preload loader only finds modules that were explicitly
// preloaded via package.preload, which none are by default.
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
			promotePluginMethods(L, L.GetGlobal("PLUGIN"))
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
	`); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to override dofile/loadfile/load/loadstring: %w", err)
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

	return L, nil
}

// LuaArchive holds the result of the Lua archive()/install() function.
type LuaArchive struct {
	URL    string
	Format string
	Strip  int
}

// getFieldString extracts a string field from a Lua table.
func getFieldString(L *lua.LState, table lua.LValue, key string) string {
	v := L.GetField(table, key)
	if v.Type() == lua.LTString {
		return string(v.(lua.LString))
	}
	return ""
}

// getFieldInt extracts an integer field from a Lua table.
func getFieldInt(L *lua.LState, table lua.LValue, key string) int {
	v := L.GetField(table, key)
	if v.Type() == lua.LTNumber {
		return int(v.(lua.LNumber))
	}
	return 0
}

// getFieldStringSlice extracts a string slice field from a Lua table.
func getFieldStringSlice(L *lua.LState, table lua.LValue, key string) []string {
	v := L.GetField(table, key)
	if v.Type() != lua.LTTable {
		return nil
	}
	var out []string
	L.ForEach(v.(*lua.LTable), func(_ lua.LValue, val lua.LValue) {
		if s, ok := val.(lua.LString); ok {
			out = append(out, string(s))
		}
	})
	return out
}
