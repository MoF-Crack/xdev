package plugin

// lua_hardening_test.go holds the guard tests for 票 20 (Batch 4) —
// the Lua engine fixes F-04 (poisoned VM after a call timeout),
// F-05 ("latest" semantics for Lua plugins) and F-06 (loadstring
// sandbox escape). They live in their own file so the hardening
// behaviour is auditable in one place.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// slowAvailableLua burns a bounded amount of CPU inside available()
// before returning a version. It is deliberately *bounded*: the orphan
// goroutine left behind by a timed-out call (F-04) finishes by itself
// instead of spinning until the test binary exits.
const slowAvailableLua = `
PLUGIN = { name = %q, display_name = "Slow Probe", category = "build" }
function available()
    local n = 0
    while n < %d do n = n + 1 end
    return {"9.9.9"}
end
function archive(v, o, a) return { url = "https://example.com/slow.zip" } end
`

// writeSlowPlugin writes a plugin whose available() spins for roughly
// spinIterations VM opcodes — long enough to overshoot the injected
// test timeout, short enough to finish well within the production one.
func writeSlowPlugin(t *testing.T, dir, name string, spinIterations int) string {
	t.Helper()
	return writeTempLua(t, dir, name+".lua", fmt.Sprintf(slowAvailableLua, name, spinIterations))
}

// writeVersionPlugin writes a Lua plugin whose available() returns the
// given Lua table literal, so a test can control the version order.
func writeVersionPlugin(t *testing.T, dir, name, luaList string) string {
	t.Helper()
	src := fmt.Sprintf(`
PLUGIN = { name = %q, display_name = "Version Probe", category = "build" }
function available()
    return %s
end
function archive(v, o, a) return { url = "https://example.com/" .. v .. ".zip" } end
`, name, luaList)
	return writeTempLua(t, dir, name+".lua", src)
}

// writeSandboxProbePlugin writes a plugin whose available() executes the
// given statement, so a test can assert the sandbox rejects it.
func writeSandboxProbePlugin(t *testing.T, dir, file, name, statement string) string {
	t.Helper()
	src := fmt.Sprintf(`
PLUGIN = { name = %q, display_name = "Sandbox Probe", category = "build" }
function available()
    %s
    return {"1.0.0"}
end
function archive(v, o, a) return { url = "https://example.com/" .. v .. ".zip" } end
`, name, statement)
	return writeTempLua(t, dir, file, src)
}

// LU1 (票 20 / F-06): Lua 5.1 ships loadstring as an alias of load. The
// sandbox overrides dofile/loadfile/load but not loadstring, so a plugin
// could compile arbitrary chunks through loadstring and escape the
// sandbox restrictions. All four must be rejected.
func TestDoString_LoadstringBlocked(t *testing.T) {
	cases := []struct {
		name      string
		statement string
	}{
		{"loadstring", `local f = loadstring("return 1")`},
		{"load", `local f = load("return 1")`},
		{"loadfile", `local f = loadfile("other.lua")`},
		{"dofile", `dofile("other.lua")`},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeSandboxProbePlugin(t, dir, fmt.Sprintf("probe%d.lua", i), fmt.Sprintf("probe%d", i), tc.statement)

			engine := NewLuaEngine()
			p, err := engine.LoadPlugin(path)
			if err != nil {
				t.Fatalf("LoadPlugin() error: %v", err)
			}

			_, err = engine.callAvailable(p.luaBasePlugin)
			if err == nil {
				t.Fatalf("available() must fail when %s() is invoked — sandbox escape", tc.name)
			}
			if !strings.Contains(err.Error(), "not allowed") {
				t.Errorf("error = %q, want it to contain %q", err.Error(), "not allowed")
			}
		})
	}
}

// LU2 (票 20 / F-05, 红线): a plugin that lists versions newest-first
// (the common case, e.g. vfox plugins) used to resolve "latest" to the
// *oldest* version because ResolveVersion passed the table tail as the
// latest hint. Regression: this test failed with "1.0.0" before the fix.
func TestLatest_NewestFirstPlugin_InstallsHighest(t *testing.T) {
	dir := t.TempDir()
	path := writeVersionPlugin(t, dir, "newestfirst", `{"2.0.0", "1.0.0"}`)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("ResolveVersion(latest) error: %v", err)
	}
	if got != "2.0.0" {
		t.Fatalf("ResolveVersion(latest) = %q, want %q (table order must not define latest)", got, "2.0.0")
	}
}

// LU3 (票 20 / F-05): the "latest" result must be the highest version for
// every table order, while explicit specs (exact / range) keep resolving
// against the plugin's own list.
func TestLatest_DescendingOrderUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		list  string
		spec  string
		want  string
	}{
		{"ascending latest", `{"1.0.0", "1.1.0", "2.0.0"}`, "latest", "2.0.0"},
		{"descending latest", `{"2.0.0", "1.1.0", "1.0.0"}`, "latest", "2.0.0"},
		{"unsorted latest", `{"1.1.0", "2.0.0", "1.0.0"}`, "latest", "2.0.0"},
		{"duplicates latest", `{"2.0.0", "1.0.0", "2.0.0"}`, "latest", "2.0.0"},
		{"exact spec", `{"2.0.0", "1.1.0", "1.0.0"}`, "1.1.0", "1.1.0"},
		{"range spec", `{"2.0.0", "1.1.0", "1.0.0"}`, "^1.0", "1.1.0"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeVersionPlugin(t, dir, fmt.Sprintf("order%d", i), tc.list)

			engine := NewLuaEngine()
			p, err := engine.LoadPlugin(path)
			if err != nil {
				t.Fatalf("LoadPlugin() error: %v", err)
			}

			got, err := p.ResolveVersion(tc.spec)
			if err != nil {
				t.Fatalf("ResolveVersion(%s) error: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Errorf("ResolveVersion(%s) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

// LU4 (票 20 / F-05): when nothing in the list parses as semver the
// resolver cannot order it, so the old deterministic table-tail
// behaviour is kept instead of relying on an unspecified sort. This must
// not panic and must stay stable.
func TestLatest_AllNonSemver_FallsBackToTableTail(t *testing.T) {
	dir := t.TempDir()
	path := writeVersionPlugin(t, dir, "revisionseries", `{"r29", "r28"}`)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("ResolveVersion(latest) error: %v", err)
	}
	if got != "r28" {
		t.Errorf("ResolveVersion(latest) = %q, want %q (deterministic table-tail fallback)", got, "r28")
	}
}

// spinIterations is the loop count used by LU5. It is calibrated so the
// bounded spin inside available() overshoots the injected 250ms timeout
// (measured ≈1.3s here, i.e. ≈4e7 loop iterations/s in gopher-lua) but
// still finishes well inside the production timeout, letting the orphan
// goroutine from the first call die on its own.
const spinIterations = 50000000

// LU5 (票 20 / F-04, 红线): a timed-out call leaves an orphan goroutine
// running inside the VM, so that LState is poisoned and must be evicted;
// the second call has to build a brand new VM and return normally.
// Before the fix the second call reused the corrupted LState and failed
// with "available(): runtime error: index out of range [-1]" (see the
// red/green record in the Batch 4 report).
func TestVMTimeout_SecondCallFreshVMNoPanic(t *testing.T) {
	dir := t.TempDir()
	path := writeSlowPlugin(t, dir, "slowvm", spinIterations)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	// Inject a short timeout so the test does not wait the production 30s.
	engine.callTimeoutOverride = 250 * time.Millisecond
	if _, err := p.ResolveVersion("latest"); err == nil {
		t.Fatalf("first ResolveVersion() should time out for a plugin that spins for > %v", engine.callTimeoutOverride)
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("first ResolveVersion() error = %v, want a timeout", err)
	}

	// The timed-out VM must have been flagged while cv.mu was still held.
	// Eviction is lazy (the next vmFor drops the entry), so it is still in
	// the map here — what matters is that it is marked poisoned and is
	// never handed out again.
	engine.mu.Lock()
	poisonedVM := engine.vms[path]
	engine.mu.Unlock()
	if poisonedVM == nil {
		t.Fatal("expected the timed-out VM to still be cached (eviction happens on the next vmFor)")
	}
	if !poisonedVM.poisoned {
		t.Fatal("timed-out VM was not marked poisoned — the next caller would reuse its corrupted LState")
	}

	// Restore the production timeout and call again: this must drop the
	// poisoned VM, build a fresh one and return the real version list.
	engine.callTimeoutOverride = 0
	got, err := p.ResolveVersion("latest")
	if err != nil {
		t.Fatalf("second ResolveVersion() after a timeout: %v (want a fresh VM and a normal result)", err)
	}
	if got != "9.9.9" {
		t.Errorf("second ResolveVersion() = %q, want %q", got, "9.9.9")
	}

	engine.mu.Lock()
	rebuiltVM := engine.vms[path]
	engine.mu.Unlock()
	if rebuiltVM == nil || rebuiltVM == poisonedVM {
		t.Error("the second call did not replace the poisoned VM in the cache")
	} else if rebuiltVM.poisoned {
		t.Error("the rebuilt VM must not be poisoned")
	}
}

// LU5b (票 20 / F-04): direct unit test of the poisoned-VM eviction in
// vmFor — a poisoned entry is dropped and the next caller gets a new
// LState; the poisoned LState itself is never handed out again.
func TestVMPoisoned_EvictedAndRebuilt(t *testing.T) {
	dir := t.TempDir()
	path := writeVersionPlugin(t, dir, "poisonme", `{"1.0.0"}`)

	engine := NewLuaEngine()
	L1, cv1, err := engine.vmFor(path, false)
	if err != nil {
		t.Fatalf("first vmFor() error: %v", err)
	}
	if cv1.poisoned {
		t.Fatal("fresh VM must not be poisoned")
	}
	// Simulate the timeout branch of callAvailable/callArchive/callEnvVars:
	// flag the VM under cv.mu, then release it.
	cv1.poisoned = true
	cv1.mu.Unlock()

	L2, cv2, err := engine.vmFor(path, false)
	if err != nil {
		t.Fatalf("second vmFor() error: %v", err)
	}
	defer cv2.mu.Unlock()
	if cv2 == cv1 || L2 == L1 {
		t.Fatal("vmFor() handed back the poisoned VM instead of building a fresh one")
	}
	if cv2.poisoned {
		t.Error("the rebuilt VM must not be poisoned")
	}
	engine.mu.Lock()
	cur := engine.vms[path]
	engine.mu.Unlock()
	if cur != cv2 {
		t.Error("the cache does not point at the rebuilt VM")
	}
}

// LU6 (票 20 / F-04): the call goroutine is detached from the caller so a
// timeout can abandon it. A panic inside such a detached plugin call must
// be converted into a doneCh error — otherwise it would take down the
// process (and with it the GUI) long after the caller has moved on.
func TestOrphanCall_PanicRecoveredToDoneCh(t *testing.T) {
	doneCh := make(chan error, 1)
	callProtected(doneCh, func() error { panic("plugin boom") })

	select {
	case err := <-doneCh:
		if err == nil {
			t.Fatal("panic must surface as a non-nil error on doneCh")
		}
		if !strings.Contains(err.Error(), "panicked") || !strings.Contains(err.Error(), "plugin boom") {
			t.Errorf("error = %q, want it to mention the panic and its value", err.Error())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result on doneCh after a panicking call")
	}

	// A normal call still reports its result through the same channel.
	okCh := make(chan error, 1)
	callProtected(okCh, func() error { return nil })
	select {
	case err := <-okCh:
		if err != nil {
			t.Errorf("normal call reported %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result on doneCh after a normal call")
	}
}
