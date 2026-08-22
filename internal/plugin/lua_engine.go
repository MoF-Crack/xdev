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

	lua "github.com/yuin/gopher-lua"
)

// LuaEngine manages Lua VM creation and plugin loading. It caches one
// sandboxed VM per loaded plugin path (Spec 032) so available() /
// archive() / env_vars() do not spawn a fresh VM and re-DoFile on
// every call. VMs are serialised per plugin by a mutex.
type LuaEngine struct {
	mu  sync.Mutex
	vms map[string]*cachedVM
}

type cachedVM struct {
	L  *lua.LState
	mu sync.Mutex
}

// NewLuaEngine creates a new Lua engine.
func NewLuaEngine() *LuaEngine {
	return &LuaEngine{vms: make(map[string]*cachedVM)}
}

// vmFor returns the cached VM for the plugin path, creating and
// DoFile-ing it on first use. The returned VM is locked for the
// caller's exclusive use until done() is called.
func (e *LuaEngine) vmFor(path string, vfox bool) (*lua.LState, func(), error) {
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
	return cv.L, cv.mu.Unlock, nil
}

// newSandboxedVM creates a Lua VM with restricted access to
// dangerous libraries (io, os) for sandboxing. The `require` function
// is restricted to only load preloaded modules — the Lua path searcher
// and C module loader are removed to prevent loading arbitrary files
// or native shared libraries that could escape the sandbox.
//
// Security (H-3): In addition to restricting libraries, dangerous base
// functions dofile and loadfile are overridden to raise errors, preventing
// plugins from loading arbitrary Lua files from the filesystem. Memory
// and call-stack limits are also imposed to mitigate resource exhaustion.
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
	`); err != nil {
		L.Close()
		return nil, fmt.Errorf("failed to override dofile/loadfile/load: %w", err)
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
