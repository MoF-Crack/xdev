package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Every nonterminating fixture runs in its own test process. Both the Go test
// timeout and CommandContext bound that process, including a regression which
// stops checking context entirely. Fixtures never start descendant processes.
func TestLuaExecutionBoundaries(t *testing.T) {
	if name := os.Getenv("XDEV_LUA_BOUNDARY_CASE"); name != "" {
		runLuaBoundaryCase(t, name)
		return
	}
	for _, name := range []string{
		"load-spin", "load-create", "load-wrap", "load-tail",
		"top-create", "top-wrap", "call-create", "nested-wrap",
		"metadata", "vfox-metadata", "global-metadata", "promotion",
		"yield-resume", "call-yield-resume", "dynamic-fields", "coroutine-errors",
		"parallel-plugins", "parallel-default", "same-path", "blocked-host", "required-metadata-error",
	} {
		t.Run(name, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLuaExecutionBoundaries$", "-test.v", "-test.timeout=5s")
			cmd.Env = append(os.Environ(), "XDEV_LUA_BOUNDARY_CASE="+name, "XDEV_LUA_MEM_LIMIT_MB=0")
			out, err := cmd.CombinedOutput()
			t.Logf("child %s: %s", name, out)
			if err != nil {
				t.Fatalf("child failed (outer context: %v): %v", ctx.Err(), err)
			}
		})
	}
}

func boundaryEngine() *LuaEngine {
	e := NewLuaEngine()
	e.callTimeoutOverride = 80 * time.Millisecond
	e.memLimitOverride = u64ptr(0)
	return e
}

func assertLuaExecutionIdle(t *testing.T) {
	t.Helper()
	if n := luaActive.Load(); n != 0 {
		t.Fatalf("returned with %d Lua executions still active", n)
	}
	stack := make([]byte, 128<<10)
	stack = stack[:runtime.Stack(stack, true)]
	if strings.Contains(string(stack), "gopher-lua.mainLoop") {
		t.Fatalf("returned while a Lua VM loop is still running:\n%s", stack)
	}
}

func requireLuaTimeout(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want explicit timeout error, got %v", err)
	}
	assertLuaExecutionIdle(t)
}

func runLuaBoundaryCase(t *testing.T, name string) {
	e := boundaryEngine()
	dir := t.TempDir()
	base := `PLUGIN = {name="boundary"}
function available() return {"1.0.0"} end
function archive() return {url="https://example.test/a.zip"} end
`
	switch name {
	case "load-spin", "load-create", "load-wrap", "load-tail":
		code := map[string]string{
			"load-spin":   `while true do end`,
			"load-create": `local c=coroutine.create(function() while true do end end); coroutine.resume(c)`,
			"load-wrap":   `local f=coroutine.wrap(function() while true do end end); f()`,
			"load-tail":   `local function f() return f() end; f()`,
		}[name]
		_, err := e.LoadPlugin(writeTempLua(t, dir, "spin.lua", code))
		requireLuaTimeout(t, err)
	case "top-create", "top-wrap", "call-create", "nested-wrap":
		code := map[string]string{
			"top-create": `local c=coroutine.create(function() while true do end end)
function archive() coroutine.resume(c); return {url="https://example.test/a.zip"} end`,
			"top-wrap": `local f=coroutine.wrap(function() while true do end end)
function archive() f(); return {url="https://example.test/a.zip"} end`,
			"call-create": `function archive()
local c=coroutine.create(function() while true do end end); coroutine.resume(c)
return {url="https://example.test/a.zip"} end`,
			"nested-wrap": `local f=coroutine.wrap(function()
local g=coroutine.wrap(function() local c=coroutine.create(function() while true do end end); coroutine.resume(c) end); g() end)
function archive() f(); return {url="https://example.test/a.zip"} end`,
		}[name]
		p, err := e.LoadPlugin(writeTempLua(t, dir, "co.lua", base+code))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			_, err = p.ArchiveFor("1.0.0")
			requireLuaTimeout(t, err)
		}
	case "metadata", "vfox-metadata", "global-metadata":
		code := map[string]string{
			"metadata": base + `setmetatable(PLUGIN, {__index=function() while true do end end})`,
			"vfox-metadata": base + `PLUGIN.info=setmetatable({name="boundary"}, {__index=function() while true do end end})
function install() return {url="https://example.test/a.zip"} end`,
			"global-metadata": `setmetatable(_G, {__index=function() while true do end end})`,
		}[name]
		path := writeTempLua(t, dir, "meta.lua", code)
		var err error
		if name == "vfox-metadata" {
			_, err = e.LoadVfoxPlugin(path)
		} else {
			_, err = e.LoadPlugin(path)
		}
		requireLuaTimeout(t, err)
	case "promotion":
		path := writeTempLua(t, dir, "promotion.lua", base)
		p, err := e.LoadPlugin(path)
		if err != nil {
			t.Fatal(err)
		}
		// The public loader succeeded. First-use cache creation now encounters
		// a method lookup which does not return.
		writeTempLua(t, dir, "promotion.lua", base+`setmetatable(PLUGIN, {__index=function(_, k)
if k=="install" then while true do end end end})`)
		_, err = p.ArchiveFor("1.0.0")
		requireLuaTimeout(t, err)
		good, err := e.LoadPlugin(writeTempLua(t, dir, "good.lua", base))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := good.ArchiveFor("1.0.0"); err != nil {
			t.Fatal(err)
		}
	case "yield-resume", "call-yield-resume":
		code := `local c=coroutine.create(function()
for i=1,3 do coroutine.yield(i) end end)
local w=coroutine.wrap(function()
for i=1,3 do coroutine.yield(i) end end)
function archive()
local ok,n=coroutine.resume(c); assert(ok, n)
local m=w(); assert(n==m)
return {url="https://example.test/"..n..".zip"} end`
		if name == "call-yield-resume" {
			code = `local c,w
function archive()
if not c then
c=coroutine.create(function() for i=1,3 do coroutine.yield(i) end end)
w=coroutine.wrap(function() for i=1,3 do coroutine.yield(i) end end)
end
local ok,n=coroutine.resume(c); assert(ok,n)
local m=w(); assert(n==m)
return {url="https://example.test/"..n..".zip"} end`
		}
		p, err := e.LoadPlugin(writeTempLua(t, dir, "yield.lua", base+code))
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 3; i++ {
			// Explicitly let the preceding load/call deadline expire. A thread
			// must use this call's budget, including when hidden in wrap().
			time.Sleep(100 * time.Millisecond)
			a, err := p.ArchiveFor("1.0.0")
			if err != nil {
				t.Fatalf("resume %d: %v", i, err)
			}
			if want := fmt.Sprintf("https://example.test/%d.zip", i); a.URL != want {
				t.Fatalf("resume %d: URL=%q, want %q", i, a.URL, want)
			}
		}
		assertLuaExecutionIdle(t)
	case "dynamic-fields":
		for _, vfox := range []bool{false, true} {
			code := `local data={name="dynamic", display_name="Dynamic Name", category="build", shims={"dy"}}
PLUGIN=setmetatable({}, {__index=function(_,k) return data[k] end})
data.info=setmetatable({}, {__index=function(_,k) return data[k] end})
data.available=function() return {{version="2.0.0"}, "1.0.0"} end
data.archive=function() return setmetatable({}, {__index=function(_,k)
if k=="url" then return "https://example.test/dynamic.zip" end
if k=="strip" then return 2 end end}) end
data.install=data.archive
`
			path := writeTempLua(t, dir, fmt.Sprintf("dynamic-%t.lua", vfox), code)
			var p *luaBasePlugin
			if vfox {
				got, err := e.LoadVfoxPlugin(path)
				if err != nil {
					t.Fatal(err)
				}
				p = got.luaBasePlugin
			} else {
				got, err := e.LoadPlugin(path)
				if err != nil {
					t.Fatal(err)
				}
				p = got.luaBasePlugin
			}
			if p.name != "dynamic" || p.displayName != "Dynamic Name" || p.category != "build" || len(p.shims) != 1 || p.shims[0] != "dy" {
				t.Fatalf("dynamic metadata lost: %+v", p)
			}
			a, err := e.callArchive(p, "1.0.0", "linux", "amd64")
			if err != nil || a.Strip != 2 {
				t.Fatalf("dynamic archive: %+v %v", a, err)
			}
			if _, err := e.callAvailable(p); err != nil {
				t.Fatal(err)
			}
		}
	case "coroutine-errors":
		code := `function archive()
local c=coroutine.create(function(x) coroutine.yield(x,nil,"tail"); error("co-boom") end)
local ok,x,z,tail=coroutine.resume(c,7); assert(ok and x==7 and z==nil and tail=="tail")
assert(coroutine.status(c)=="suspended")
local ok,msg=coroutine.resume(c); assert(not ok and string.find(msg,"co%-boom"))
assert(coroutine.status(c)=="dead")
local f=coroutine.wrap(function(x) coroutine.yield(x,nil,"tail"); error("wrap-boom") end)
local x,z,tail=f(9); assert(x==9 and z==nil and tail=="tail")
local ok,msg=pcall(f); assert(not ok and string.find(msg,"wrap%-boom"))
assert(not pcall(coroutine.create,42)); assert(not pcall(coroutine.wrap,42)); assert(not pcall(coroutine.resume,42))
return {url="https://example.test/ok.zip"} end`
		p, err := e.LoadPlugin(writeTempLua(t, dir, "errors.lua", base+code))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.ArchiveFor("1.0.0"); err != nil {
			t.Fatal(err)
		}
	case "required-metadata-error":
		for _, vfox := range []bool{false, true} {
			code := `PLUGIN=setmetatable({}, {__index=function(_,k) if k=="name" then error("required-field-error") end end})`
			if vfox {
				code = `PLUGIN={info=setmetatable({}, {__index=function(_,k) if k=="name" then error("required-field-error") end end})}`
			}
			path := writeTempLua(t, dir, "throwing.lua", code)
			var err error
			if vfox {
				_, err = e.LoadVfoxPlugin(path)
			} else {
				_, err = e.LoadPlugin(path)
			}
			if err == nil || !strings.Contains(err.Error(), "required-field-error") {
				t.Errorf("vfox=%t: lost required metadata error: %v", vfox, err)
			}
		}
	case "parallel-plugins", "parallel-default":
		if name == "parallel-default" {
			t.Setenv("XDEV_LUA_MEM_LIMIT_MB", "")
			e.memLimitOverride = nil
		}
		runLuaParallelCase(t, e, dir, base)
	case "same-path":
		path := writeTempLua(t, dir, "counter.lua", base+`local n=0
function archive() n=n+1; return {url="https://example.test/"..n..".zip"} end`)
		p, err := e.LoadPlugin(path)
		if err != nil {
			t.Fatal(err)
		}
		const count = 12
		out := make(chan string, count)
		var wg sync.WaitGroup
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a, err := p.ArchiveFor("1.0.0")
				if err != nil {
					out <- err.Error()
					return
				}
				out <- a.URL
			}()
		}
		wg.Wait()
		close(out)
		seen := map[string]bool{}
		for url := range out {
			seen[url] = true
		}
		for i := 1; i <= count; i++ {
			if !seen[fmt.Sprintf("https://example.test/%d.zip", i)] {
				t.Fatalf("missing result %d: %v", i, seen)
			}
		}
		assertLuaExecutionIdle(t)
	case "blocked-host":
		// A context cannot forcibly stop a blocking Go callback. Keep VM
		// ownership until it returns, and report the elapsed deadline then.
		L, err := e.newSandboxedVM()
		if err != nil {
			t.Fatal(err)
		}
		defer L.Close()
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		L.SetGlobal("block", L.NewFunction(func(L *lua.LState) int { close(started); <-release; return 0 }))
		go func() {
			done <- e.runWithContextLimited(L, 40*time.Millisecond, 0, func() error { return L.DoString("block()") })
		}()
		<-started
		select {
		case err := <-done:
			close(release)
			t.Fatalf("returned before host callback exited: %v", err)
		case <-time.After(160 * time.Millisecond):
		}
		close(release)
		requireLuaTimeout(t, <-done)
	default:
		t.Fatalf("unknown fixture %q", name)
	}
}

func runLuaParallelCase(t *testing.T, e *LuaEngine, dir, base string) {
	t.Helper()
	good, err := e.LoadPlugin(writeTempLua(t, dir, "parallel-good.lua", base))
	if err != nil {
		t.Fatal(err)
	}
	// This deterministic barrier runs from a metadata metamethod during
	// cache promotion. The second plugin must work before it is released.
	// newSandboxedVM has no arbitrary host hooks, so use the supported print
	// output as the barrier emitted by the Lua fixture.
	badPath := writeTempLua(t, dir, "parallel-bad.lua", base+`setmetatable(PLUGIN,{__index=function(_,k)
if k=="install" then print("LUA_PROMOTION_ENTERED"); while true do end end end})`)
	e.callTimeoutOverride = 600 * time.Millisecond
	badDone := make(chan error, 1)
	// Signal entry by observing that the engine cache initialization is
	// active. The fixture's stack confirms it reached the Lua promotion.
	go func() {
		_, cv, err := e.vmFor(badPath, false)
		if cv != nil {
			cv.mu.Unlock()
		}
		badDone <- err
	}()
	deadline := time.After(time.Second)
	for {
		stack := make([]byte, 128<<10)
		stack = stack[:runtime.Stack(stack, true)]
		if strings.Contains(string(stack), "promotePluginMethods") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("promotion did not start")
		default:
			runtime.Gosched()
		}
	}
	start := time.Now()
	_, err = good.ArchiveFor("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("unrelated plugin waited for promotion: %v", elapsed)
	}
	requireLuaTimeout(t, <-badDone)
}
