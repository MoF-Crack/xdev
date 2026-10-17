package plugin

// lua_sandbox_f09_f10_test.go holds the guard tests for the two Lua
// sandbox defects fixed in this batch:
//
//	F-09 (P0) require() could load and execute an arbitrary .lua file
//	          from disk. The hardening rewrote the package.loaders
//	          *field*, but gopher-lua's loRequire reads the loader list
//	          from the registry's _LOADERS entry, which OpenPackage fills
//	          with the very same two loaders — including the Lua path
//	          searcher (loLoaderLua).
//	F-10 (P1) the result-extraction helpers called L.GetField outside
//	          callProtected, so a plugin table carrying a throwing
//	          __index metamethod panicked the host process.

import (
	"path/filepath"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// f09EvilModule is the payload the sandbox must never load: a module
// that would hand the host a bogus version.
const f09EvilModule = `return { version = "9.9.9" }`

// writeF09Module drops the evil module into dir and returns its path.
func writeF09Module(t *testing.T, dir string) string {
	t.Helper()
	return writeTempLua(t, dir, "f09evil.lua", f09EvilModule)
}

// F09-1 (P0): with LUA_PATH pointing at a directory that really does
// contain the module, require() inside a sandboxed VM must fail. Before
// the fix require() walked straight through the restricted
// package.loaders field (via the registry _LOADERS) and executed the
// on-disk file.
func TestRequire_DiskModuleBlocked(t *testing.T) {
	dir := t.TempDir()
	writeF09Module(t, dir)
	t.Setenv("LUA_PATH", filepath.Join(dir, "?.lua"))

	// Sanity guard: the fixture and LUA_PATH wiring must be good enough
	// that an *unsandboxed* VM loads the module. Without this the test
	// could pass for the wrong reason (a typo in the path pattern).
	plain := lua.NewState()
	func() {
		defer plain.Close()
		if err := plain.DoString(`f09_sanity = require("f09evil")`); err != nil {
			t.Fatalf("fixture check: an unsandboxed VM could not require the module: %v", err)
		}
		if v := plain.GetField(plain.GetGlobal("f09_sanity"), "version"); v.String() != "9.9.9" {
			t.Fatalf("fixture check: unsandboxed require returned %v, want version 9.9.9", v)
		}
	}()

	engine := NewLuaEngine()
	L, err := engine.newSandboxedVM()
	if err != nil {
		t.Fatalf("newSandboxedVM() error: %v", err)
	}
	defer L.Close()

	// pcall keeps the sandbox error from aborting the chunk so the test
	// can inspect the outcome itself.
	if err := L.DoString(`
		local ok, res = pcall(require, "f09evil")
		F09_OK = ok
		F09_RESULT = tostring(res)
	`); err != nil {
		t.Fatalf("DoString(pcall(require, ...)) error: %v", err)
	}

	if ok := L.GetGlobal("F09_OK"); ok == lua.LTrue {
		t.Fatalf("require() succeeded inside the sandbox — sandbox escape (F-09); it loaded %q from disk",
			L.GetGlobal("F09_RESULT").String())
	}
	if msg := L.GetGlobal("F09_RESULT").String(); !strings.Contains(msg, "require is not allowed") {
		t.Errorf("require() error = %q, want it to contain %q", msg, "require is not allowed")
	}
}

// F09-2 (P0, defence in depth): the loader list loRequire actually
// consults lives in the registry under _LOADERS. It must contain the
// preload loader only — a second entry is the Lua path searcher that
// reads .lua files off disk.
func TestRegistryLoaders_OnlyPreload(t *testing.T) {
	engine := NewLuaEngine()
	L, err := engine.newSandboxedVM()
	if err != nil {
		t.Fatalf("newSandboxedVM() error: %v", err)
	}
	defer L.Close()

	loaders, ok := L.GetField(L.Get(lua.RegistryIndex), "_LOADERS").(*lua.LTable)
	if !ok {
		t.Fatal("registry _LOADERS is not a table — require() would raise instead of loading, but the sandbox contract is unclear")
	}
	if lv := L.RawGetInt(loaders, 1); lv == lua.LNil {
		t.Error("registry _LOADERS[1] is missing — the preload loader should be the one loader kept")
	}
	if lv := L.RawGetInt(loaders, 2); lv != lua.LNil {
		t.Errorf("registry _LOADERS[2] is %v — the Lua path searcher is still reachable through require() (F-09)", lv)
	}
}

// F10-1 (P1): each extraction helper must swallow a metamethod error and
// return its zero value instead of letting gopher-lua's ls.Panic escape
// into the host process.
func TestFieldExtraction_ThrowingIndexMetamethod(t *testing.T) {
	engine := NewLuaEngine()
	L, err := engine.newSandboxedVM()
	if err != nil {
		t.Fatalf("newSandboxedVM() error: %v", err)
	}
	defer L.Close()

	if err := L.DoString(`evil = setmetatable({}, {__index = function() error("boom") end})`); err != nil {
		t.Fatalf("DoString() error: %v", err)
	}
	evil := L.GetGlobal("evil")
	if evil.Type() != lua.LTTable {
		t.Fatalf("evil is %s, want a table", evil.Type())
	}

	if got := getFieldString(L, evil, "url"); got != "" {
		t.Errorf("getFieldString() = %q, want the empty zero value", got)
	}
	if got := getFieldInt(L, evil, "strip"); got != 0 {
		t.Errorf("getFieldInt() = %d, want 0", got)
	}
	if got := getFieldStringSlice(L, evil, "shims"); got != nil {
		t.Errorf("getFieldStringSlice() = %v, want nil", got)
	}
}

// F10-3 (P1): recovering the panic is not enough — a recovered panic does
// not unwind the VM, so a bare `defer recover()` in the helpers left the
// register/frame entries pushed by L.GetField behind. A plugin returning a
// throwing __index table could therefore grow the VM stack a few entries
// per read until every later call failed with "stack overflow". The
// extraction must go through gopher-lua's PCall, which restores the stack.
func TestFieldExtraction_ThrowingIndex_NoStackGrowth(t *testing.T) {
	engine := NewLuaEngine()
	L, err := engine.newSandboxedVM()
	if err != nil {
		t.Fatalf("newSandboxedVM() error: %v", err)
	}
	defer L.Close()

	if err := L.DoString(`
		evil = setmetatable({}, {__index = function() error("boom") end})
		function probe() return {"1.0.0"} end
	`); err != nil {
		t.Fatalf("DoString() error: %v", err)
	}
	evil := L.GetGlobal("evil")

	// One warm-up read, then the register top must not move at all no
	// matter how many throwing reads follow.
	_ = getFieldString(L, evil, "url")
	top := L.GetTop()
	for i := 0; i < 300; i++ {
		_ = getFieldString(L, evil, "url")
		_ = getFieldInt(L, evil, "strip")
		_ = getFieldStringSlice(L, evil, "shims")
	}
	if got := L.GetTop(); got != top {
		t.Errorf("register top grew from %d to %d after 300 throwing reads — the recovered panics leak VM stack", top, got)
	}

	// The VM must still be usable, not stuck at "stack overflow".
	if err := L.CallByParam(lua.P{Fn: L.GetGlobal("probe"), NRet: 1, Protect: true}); err != nil {
		t.Fatalf("probe() after repeated throwing reads: %v", err)
	}
	if got := L.Get(-1); got.Type() != lua.LTTable {
		t.Errorf("probe() returned %v (%s), want a table", got, got.Type())
	}
}

// F10-2 (P1, end to end): a plugin whose archive() returns a table with a
// throwing __index metamethod used to panic the host while ArchiveFor
// extracted the "url"/"format"/"strip" fields. It must instead surface a
// controlled error, and the cached VM must stay usable afterwards.
func TestArchiveFor_ThrowingIndexMetamethod_NoPanic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "f10probe.lua", `
PLUGIN = { name = "f10probe", display_name = "F10 Probe", category = "build" }
function available() return {"1.0.0"} end
function archive(v, o, a)
    return setmetatable({}, { __index = function() error("boom") end })
end
`)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	_, err = p.ArchiveFor("1.0.0")
	if err == nil {
		t.Fatal("ArchiveFor() = nil error, want a controlled error for a table whose fields cannot be read")
	}
	if !strings.Contains(err.Error(), "url") {
		t.Errorf("ArchiveFor() error = %q, want it to report the missing 'url' field", err.Error())
	}

	// The VM must not be left unusable by the recovered metamethod error.
	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("ResolveVersion() after a recovered metamethod error: %v", err)
	}
	if got != "1.0.0" {
		t.Errorf("ResolveVersion(latest) = %q, want %q", got, "1.0.0")
	}
}
