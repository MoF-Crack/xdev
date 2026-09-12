// register_b5_guard_test.go — 审计修复 Batch 5（票 39 / P2-31、N-09）守卫：
// 用户插件注册必须"一次目录扫描、每个文件只 DoFile 一次、vfox 加载失败不静默"。
//
// 宿主安全：所有用例都经 injectHome（t.Setenv("XMDEV_HOME", tmp) + 重置全局
// runCtx）把 home 指到 t.TempDir()，绝不写真实 xdev home。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xmdemt/xdev/internal/plugin"
)

// probeLuaPlugin 生成一个合法的 xdev 原生格式插件；顶层 print 是 DoFile
// 次数的可观测副作用（沙箱开放 base 库，gopher-lua 的 print 走 stdout）。
func probeLuaPlugin(name, marker string) string {
	return `print("` + marker + `")
PLUGIN = {
    name = "` + name + `",
    display_name = "` + name + `",
    category = "system",
    shims = {},
}

function available()
    return {}
end

function archive(version, os, arch)
    return {
        url = "https://example.com/` + name + `/x.zip",
        format = "zip",
    }
end
`
}

// minimalVfoxPlugin 生成一个合法的 vfox 兼容格式插件（PLUGIN.info +
// install()/env_keys()）——xdev 格式加载必然失败（缺 archive()），只有
// vfox 回退路径能加载它。
func minimalVfoxPlugin(name string) string {
	return `PLUGIN = {
    info = {
        name = "` + name + `",
        version = "1.0.0",
        display_name = "` + name + `",
    },
}

function available()
    return {}
end

function install(version)
    return { url = "https://example.com/` + name + `/x.zip", format = "zip" }
end

function env_keys()
    return {}
end
`
}

// captureStdout 把 os.Stdout 临时指向一个文件，返回取回内容的函数。
// gopher-lua 的 base print 用 fmt.Print（调用时解析 os.Stdout），因此
// 插件顶层 print 的内容会被这里捕获。
func captureStdout(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout-*.log")
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = f
	return func() string {
		os.Stdout = orig
		if err := f.Close(); err != nil {
			t.Fatalf("close stdout capture: %v", err)
		}
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("read stdout capture: %v", err)
		}
		return string(data)
	}
}

// captureStderr 同理，用于断言 vfox 加载失败的日志（P2-31：不再静默）。
func captureStderr(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr-*.log")
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = f
	return func() string {
		os.Stderr = orig
		if err := f.Close(); err != nil {
			t.Fatalf("close stderr capture: %v", err)
		}
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("read stderr capture: %v", err)
		}
		return string(data)
	}
}

// TestRegisterUserPlugins_SingleScanAndSingleDoFile（P2-31 主判据）：
// 目录只扫一次；xdev 原生格式加载成功的文件只被 DoFile 一次。
//
// 修复前：registerLuaPlugins + registerVfoxPlugins 各扫一次、各 DoFile 一次
// → 扫描计数 2、插件标记出现 2 次。
func TestRegisterUserPlugins_SingleScanAndSingleDoFile(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)

	pluginsDir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const marker = "B5-DOFILE-MARK"
	if err := os.WriteFile(filepath.Join(pluginsDir, "b5probe.lua"),
		[]byte(probeLuaPlugin("b5probe", marker)), 0o644); err != nil {
		t.Fatal(err)
	}

	// 目录扫描计数（插件目录的 ReadDir 调用次数）。
	var scans int32
	origReadDir := pluginsReadDir
	pluginsReadDir = func(dir string) ([]os.DirEntry, error) {
		if strings.EqualFold(filepath.Clean(dir), filepath.Clean(pluginsDir)) {
			atomic.AddInt32(&scans, 1)
		}
		return origReadDir(dir)
	}
	defer func() { pluginsReadDir = origReadDir }()

	catDir := t.TempDir()
	copyManifest(t, catDir, "go.yaml", "go.yaml")
	catalog := loadTestCatalog(t, catDir)

	readStdout := captureStdout(t)
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)
	out := readStdout()

	if got := atomic.LoadInt32(&scans); got != 1 {
		t.Errorf("plugins dir scanned %d times per RegisterAll, want 1 (P2-31/N-09)", got)
	}
	if n := strings.Count(out, marker); n != 1 {
		t.Errorf("plugin top-level code executed %d times, want 1 — the file was DoFile'd once per engine (P2-31/N-09); stdout=%q", n, out)
	}
	if _, ok := r.Get("b5probe"); !ok {
		t.Error("xdev-format plugin not registered")
	}
}

// TestRegisterUserPlugins_VfoxFallbackAndFailureLogged（P2-31 语义保留）：
//   - vfox 格式文件（xdev 加载必失败）经回退路径注册成功，仍打 info 行；
//   - 两种格式都失败时两个错误都打印（修复前 vfox 侧静默 continue）；
//   - 既有冲突警告语义不变（内置冲突 / vfox 重名冲突）。
func TestRegisterUserPlugins_VfoxFallbackAndFailureLogged(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)

	pluginsDir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pluginsDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("vfoxplug.lua", minimalVfoxPlugin("vfoxplug"))      // vfox 回退成功
	write("vfoxdup.lua", minimalVfoxPlugin("vfoxdup"))        // 先注册 vfoxdup
	write("vfoxdup2.lua", minimalVfoxPlugin("vfoxdup"))       // 重名 → vfox 冲突警告
	write("broken.lua", "PLUGIN = {\n")                       // 语法错误：两种格式都失败
	write("goconflict.lua", probeLuaPlugin("go", "CONFLICT")) // 与内置 go 冲突

	catDir := t.TempDir()
	copyManifest(t, catDir, "go.yaml", "go.yaml")
	catalog := loadTestCatalog(t, catDir)

	readStderr := captureStderr(t)
	readStdout := captureStdout(t) // goconflict.lua 的顶层 print 不进测试日志
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)
	errOut := readStderr()
	_ = readStdout()

	if _, ok := r.Get("vfoxplug"); !ok {
		t.Error("vfox-format plugin not registered through the fallback path")
	}
	if !strings.Contains(errOut, "info: loaded vfox plugin vfoxplug") {
		t.Errorf("vfox load info line missing; stderr=%q", errOut)
	}
	if !strings.Contains(errOut, "warning: vfox plugin vfoxdup conflicts with existing plugin") {
		t.Errorf("vfox conflict warning semantics changed; stderr=%q", errOut)
	}
	if !strings.Contains(errOut, "warning: Lua plugin go conflicts with built-in plugin") {
		t.Errorf("built-in conflict warning semantics changed; stderr=%q", errOut)
	}
	// P2-31 核心：两种格式都失败必须有日志（含 vfox 侧错误）。
	if !strings.Contains(errOut, "failed to load Lua plugin broken.lua") ||
		!strings.Contains(errOut, "vfox format also failed") {
		t.Errorf("failed plugin load is silent or incomplete (vfox error must be reported); stderr=%q", errOut)
	}
	// 冲突的内置插件名仍然是内置实现，未被 Lua 插件顶掉。
	if p, ok := r.Get("go"); ok {
		if _, isGeneric := p.(*plugin.Generic); !isGeneric {
			t.Errorf("built-in go plugin was replaced by %T", p)
		}
	}
}
