package plugin

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
	lua "github.com/yuin/gopher-lua"
)

// VfoxPlugin is a vfox-format Lua plugin. It embeds luaBasePlugin
// and only overrides the constructor.
type VfoxPlugin struct {
	*luaBasePlugin
}

// Ensure *VfoxPlugin satisfies the Plugin interface at compile time.
var _ Plugin = (*VfoxPlugin)(nil)

// --- LuaEngine: plugin loading ---

// LoadPlugin loads an xdev-format Lua plugin and returns a *LuaPlugin.
func (e *LuaEngine) LoadPlugin(path string) (*LuaPlugin, error) {
	base, err := e.loadLuaBase(path, luaCallSpecs{
		archiveFn:  "archive",
		envVarsFn:  "env_vars",
		descPrefix: "Lua plugin: ",
		vfoxFormat: false,
	}, false)
	if err != nil {
		return nil, err
	}
	return &LuaPlugin{base}, nil
}

// LoadVfoxPlugin loads a vfox-format Lua plugin and returns a *VfoxPlugin.
func (e *LuaEngine) LoadVfoxPlugin(path string) (*VfoxPlugin, error) {
	base, err := e.loadLuaBase(path, luaCallSpecs{
		archiveFn:  "install",
		envVarsFn:  "env_keys",
		descPrefix: "vfox plugin: ",
		vfoxFormat: true,
	}, true)
	if err != nil {
		return nil, err
	}
	return &VfoxPlugin{base}, nil
}

// loadLuaBase is the shared loader for both xdev and vfox Lua plugins.
// When vfox=true, it reads PLUGIN.info for metadata; otherwise it reads
// PLUGIN fields directly. It validates required functions based on the
// specs and returns a populated luaBasePlugin.
func (e *LuaEngine) loadLuaBase(path string, specs luaCallSpecs, vfox bool) (*luaBasePlugin, error) {
	// 第十轮审计 P1-2（P1）：加载必须走**带超时**的 loadProtected。
	//
	// 此前这里是裸 `L.DoFile(path)`（且 `defer L.Close()`）—— 插件顶层
	// `while true do end` 就能让 CLI/TUI/GUI **每次启动永久挂起**（无日志、无错误），
	// 因为 `registerUserPlugins` 在 `RegisterAll` 里对每个 `.lua` 执行它。
	// 注意这条路径**不经过 `vmFor`**（它自建 VM 并在返回前 Close），所以只在
	// `vmFor` 里加超时是不够的 —— 必须同时修这里。
	//
	// 超时后**不能** `L.Close()`：孤儿 goroutine 仍在 L 上执行（见 loadProtected 的
	// 注释），Close 会与它竞争。loadProtected 在超时分支直接丢弃 VM，因此这里改为
	// 只在成功路径 Close。
	L, err := e.loadProtected(path)
	if err != nil {
		label := "lua"
		if vfox {
			label = "vfox"
		}
		if strings.Contains(err.Error(), "timed out") {
			return nil, err
		}
		return nil, fmt.Errorf("load %s plugin %s: %w", label, path, err)
	}
	defer L.Close()

	// 第八轮审计 A-01（P0）：PLUGIN 表本身的读取必须走受保护调用 —— 插件可以给
	// PLUGIN 挂抛错的 __index 元方法，裸 L.GetGlobal/L.GetField 会以 Go panic
	// 打崩宿主（`xdev available` 即 exit 2）。
	pluginTable := getGlobalProtected(L, "PLUGIN")
	if pluginTable.Type() != lua.LTTable {
		return nil, fmt.Errorf("%s plugin %s: PLUGIN table not found", pluginLabel(vfox), path)
	}

	var name, displayName string
	if vfox {
		infoTable := getFieldProtected(L, pluginTable, "info")
		if infoTable.Type() == lua.LTTable {
			name = getFieldString(L, infoTable, "name")
			displayName = getFieldString(L, infoTable, "display_name")
		}
		if name == "" {
			name = getFieldString(L, pluginTable, "name")
		}
		if name == "" {
			return nil, fmt.Errorf("vfox plugin %s: PLUGIN.info.name is required", path)
		}
	} else {
		name = getFieldString(L, pluginTable, "name")
		if name == "" {
			return nil, fmt.Errorf("lua plugin %s: PLUGIN.name is required", path)
		}
		displayName = getFieldString(L, pluginTable, "display_name")
	}
	if displayName == "" {
		displayName = name
	}

	// Security (C-2): Validate plugin name is a safe path component.
	// This prevents path traversal when the name is used in InstallDir.
	if _, err := util.SafeName(name); err != nil {
		return nil, fmt.Errorf("%s plugin %s: PLUGIN.name %q is not a safe path component: %w", pluginLabel(vfox), path, name, err)
	}

	category := getFieldString(L, pluginTable, "category")
	if category == "" {
		category = "system"
	}
	shims := getFieldStringSlice(L, pluginTable, "shims")
	if len(shims) == 0 && vfox {
		shims = []string{name}
	}

	promotePluginMethods(L, pluginTable)
	if fn := getGlobalProtected(L, "available"); fn.Type() != lua.LTFunction {
		return nil, fmt.Errorf("%s plugin %s: available() function not found", pluginLabel(vfox), path)
	}
	if fn := getGlobalProtected(L, specs.archiveFn); fn.Type() != lua.LTFunction {
		return nil, fmt.Errorf("%s plugin %s: %s() function not found", pluginLabel(vfox), path, specs.archiveFn)
	}

	hasEnv := getGlobalProtected(L, specs.envVarsFn).Type() == lua.LTFunction

	return &luaBasePlugin{
		path:        path,
		name:        name,
		displayName: displayName,
		category:    category,
		shims:       shims,
		hasEnv:      hasEnv,
		engine:      e,
		specs:       specs,
	}, nil
}

func pluginLabel(vfox bool) string {
	if vfox {
		return "vfox"
	}
	return "lua"
}

// promotePluginMethods copies PLUGIN.available / PLUGIN.archive /
// PLUGIN.env_vars (and vfox aliases) to globals when the global is
// missing. Some templates nest the required functions inside PLUGIN.
//
// 第九轮审计 P0-1（P0）：写入必须用**原始写**（RawSetString），不能用 `L.SetGlobal`。
//
// A-01（Round 8）只把**读**侧（`GetGlobal`/`GetField`）包进了 `extractField →
// L.PCall`，但这里把字段提升为全局用的是 `L.SetGlobal` —— 而 gopher-lua 的
// `SetGlobal → setFieldString` 在**键不存在**时会走 `__newindex` 元方法。插件只要
//
//	setmetatable(_G, { __newindex = function(_, k) error("boom:"..k) end })
//
// 就能让 `ls.Panic`（Go panic，穿透一切 defer 之外的普通调用）打崩宿主：实测
// `LoadPlugin` 直接 panic、进程 exit 2。**放大点与 A-01 完全一样，且不需要恶意**
// —— 「严格全局变量」这种良性写法（插件作者自己用 `__newindex` 抓拼写错误）同样
// 打崩 CLI/TUI/GUI 的**每一次启动**（`registerUserPlugins` 在 `RegisterAll` 里
// 加载每个用户插件）。
//
// 即使 `__newindex` 只是吞掉写入而不抛错，用 `SetGlobal` 也不安全：实测全局没写
// 进去 → 后续报 "available() function not found"（误报函数缺失）。
//
// `L.RawSetString` 绕过元方法直接写 globals 表，既避免 panic 又保证写入生效。
func promotePluginMethods(L *lua.LState, pluginTable lua.LValue) {
	if pluginTable == nil || pluginTable.Type() != lua.LTTable {
		return
	}
	for _, name := range []string{"available", "archive", "install", "env_vars", "env_keys"} {
		if getGlobalProtected(L, name).Type() == lua.LTFunction {
			continue
		}
		if field := getFieldProtected(L, pluginTable, name); field.Type() == lua.LTFunction {
			// 原始写：不触发 _G 的 __newindex（见上面的 P0-1 说明）。
			globals, ok := L.Get(lua.GlobalsIndex).(*lua.LTable)
			if !ok {
				return // 理论上不可能；防御性返回而不是 panic
			}
			globals.RawSetString(name, field)
		}
	}
}

// --- LuaEngine: shared call helpers ---

// Default Lua execution timeouts (Security M-12). These are the production
// values; LuaEngine.callTimeout applies a test override when one is set
// (票 20 / F-04).
const (
	availableCallTimeout = 30 * time.Second
	archiveCallTimeout   = 60 * time.Second
	envVarsCallTimeout   = 10 * time.Second
)

// callAvailable runs the Lua available() function. For vfox plugins,
// each entry may be a string or a table with a "version" field.
func (e *LuaEngine) callAvailable(p *luaBasePlugin) ([]string, error) {
	L, cv, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer cv.mu.Unlock()

	// Security (M-12): Add execution timeout.
	//
	// 第十轮审计 P1-1（P1）：**结果提取必须在同一个超时窗口内**（同 callArchive）。
	// `available()` 的提取有两处会执行插件代码：
	//   - vfox 格式：`getFieldString(L, entry, "version")` 走 `L.GetField` → 执行
	//     条目表的 `__index`；
	//   - `L.ForEach` 本身是原始遍历（不走 `__pairs`），但回调里的上述读取会。
	// 审计实测（注入调用超时 400ms，`__index` 内跑 1e8 次有界自增 ≈2.3s/次）：
	// vfox 的 `available()` 条目 `__index` → `err=<nil> elapsed=2.90s`。
	// 因此把「调用 + 遍历 + 提取」整体放进受保护的闭包。
	resCh := make(chan []string, 1)
	extractErrCh := make(chan error, 1)
	doneCh := make(chan error, 1)
	callProtected(doneCh, func() error {
		if err := L.CallByParam(lua.P{
			Fn:      getGlobalProtected(L, "available"),
			NRet:    1,
			Protect: true,
		}); err != nil {
			return fmt.Errorf("available(): %w", err)
		}
		result := L.Get(-1)
		L.Pop(1)
		if result.Type() != lua.LTTable {
			extractErrCh <- fmt.Errorf("available() must return a table, got %s", result.Type())
			return nil
		}
		var versions []string
		L.ForEach(result.(*lua.LTable), func(_ lua.LValue, v lua.LValue) {
			if p.specs.vfoxFormat {
				switch entry := v.(type) {
				case lua.LString:
					versions = append(versions, string(entry))
				case *lua.LTable:
					ver := getFieldString(L, entry, "version")
					if ver != "" {
						versions = append(versions, ver)
					}
				}
			} else {
				if s, ok := v.(lua.LString); ok {
					versions = append(versions, string(s))
				}
			}
		})
		resCh <- versions
		return nil
	})
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(e.callTimeout(availableCallTimeout)):
		// 票 20 / F-04: the abandoned goroutine still runs on this LState,
		// so flag the VM before cv.mu is released — the next caller must
		// rebuild it instead of sharing the corrupted stack.
		cv.poisoned = true
		return nil, fmt.Errorf("available() timed out after %s for plugin %s", e.callTimeout(availableCallTimeout), p.name)
	}

	select {
	case e := <-extractErrCh:
		return nil, e
	default:
	}
	select {
	case versions := <-resCh:
		return versions, nil
	default:
		return nil, fmt.Errorf("available() returned without a result table")
	}
}

// callArchive runs the Lua archive() or install() function (based on specs).
func (e *LuaEngine) callArchive(p *luaBasePlugin, version, goos, arch string) (*LuaArchive, error) {
	L, cv, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer cv.mu.Unlock()

	fnName := p.specs.archiveFn
	// Security (M-12): Add execution timeout.
	//
	// 第十轮审计 P1-1（P1）：**结果提取必须在同一个超时窗口内**。
	// 此前 `time.After` 只守 `L.CallByParam`，而提取（`L.Get`/`getFieldString`/
	// `getFieldInt`）在 select **之后**执行 —— 那些 helper 的 `L.GetField` 会执行
	// 插件返回的表上的 `__index`，而 `extractField` 的 `L.PCall` 会**一直等它返回**。
	// 于是 `__index` 里写 `while true do end` 就能让宿主永久挂起，尽管调用本身
	// 早已"超时失败"。审计实测（注入调用超时 400ms，`__index` 内跑 1e8 次有界自增
	// ≈2.3s/次）：`err=<nil> elapsed=7.43s`（3 次字段读取）。
	// 把提取搬进受超时保护的闭包后，整个「调用 + 提取」是一个原子单元。
	type archiveResult struct {
		url    string
		format string
		strip  int
		err    error
	}
	resCh := make(chan archiveResult, 1)
	doneCh := make(chan error, 1)
	callProtected(doneCh, func() error {
		if err := L.CallByParam(lua.P{
			Fn:      getGlobalProtected(L, fnName),
			NRet:    1,
			Protect: true,
		}, lua.LString(version), lua.LString(goos), lua.LString(arch)); err != nil {
			return fmt.Errorf("%s(%s,%s,%s): %w", fnName, version, goos, arch, err)
		}
		// 提取在**同一 goroutine 内**完成（P1-1）。
		result := L.Get(-1)
		L.Pop(1)
		if result.Type() != lua.LTTable {
			resCh <- archiveResult{err: fmt.Errorf("%s() must return a table, got %s", fnName, result.Type())}
			return nil
		}
		resCh <- archiveResult{
			url:    getFieldString(L, result, "url"),
			format: getFieldString(L, result, "format"),
			strip:  getFieldInt(L, result, "strip"),
		}
		return nil
	})
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(e.callTimeout(archiveCallTimeout)):
		// 票 20 / F-04: poison before releasing cv.mu (see callAvailable).
		cv.poisoned = true
		return nil, fmt.Errorf("%s() timed out after %s for plugin %s", fnName, e.callTimeout(archiveCallTimeout), p.name)
	}

	var got archiveResult
	select {
	case got = <-resCh:
	default:
		return nil, fmt.Errorf("%s() returned without a result table", fnName)
	}
	if got.err != nil {
		return nil, got.err
	}

	a := &LuaArchive{URL: got.url, Format: got.format, Strip: got.strip}
	if a.URL == "" {
		return nil, fmt.Errorf("%s() result missing 'url' field", fnName)
	}
	// Security (H-4): Validate that the plugin-returned URL uses HTTPS.
	// A malicious plugin could return an HTTP URL, making the download
	// vulnerable to MITM attacks, or a file:// URL for SSRF.
	if !strings.HasPrefix(a.URL, "https://") {
		return nil, fmt.Errorf("%s() returned non-HTTPS URL %q — only HTTPS URLs are allowed for security", fnName, a.URL)
	}
	if a.Format == "" {
		a.Format = "zip"
	}
	return a, nil
}

// callEnvVars runs the optional Lua env_vars() or env_keys() function.
func (e *LuaEngine) callEnvVars(p *luaBasePlugin, installPath string) (map[string]string, error) {
	L, cv, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer cv.mu.Unlock()

	fnName := p.specs.envVarsFn
	// Security (M-12): Add execution timeout
	doneCh := make(chan error, 1)
	callProtected(doneCh, func() error {
		if err := L.CallByParam(lua.P{
			Fn:      getGlobalProtected(L, fnName),
			NRet:    1,
			Protect: true,
		}, lua.LString(installPath)); err != nil {
			return fmt.Errorf("%s(%s): %w", fnName, installPath, err)
		}
		return nil
	})
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(e.callTimeout(envVarsCallTimeout)):
		// 票 20 / F-04: poison before releasing cv.mu (see callAvailable).
		cv.poisoned = true
		return nil, fmt.Errorf("%s() timed out after %s for plugin %s", fnName, e.callTimeout(envVarsCallTimeout), p.name)
	}

	result := L.Get(-1)
	L.Pop(1)

	if result.Type() != lua.LTTable {
		return nil, fmt.Errorf("%s() must return a table, got %s", fnName, result.Type())
	}

	out := map[string]string{}
	L.ForEach(result.(*lua.LTable), func(k lua.LValue, v lua.LValue) {
		ks, kok := k.(lua.LString)
		vs, vok := v.(lua.LString)
		if kok && vok {
			key := string(ks)
			// Security (H-4): Only allow env var names in the whitelist.
			// A malicious plugin could return arbitrary keys like "Path"
			// or "SystemRoot" that would corrupt the user's environment.
			if !util.IsAllowedEnvVarName(key) {
				log.Printf("[plugin] WARNING: Lua plugin %q returned env var %q not in whitelist — skipping", p.name, key)
				return
			}
			out[key] = string(vs)
		}
	})
	return out, nil
}
