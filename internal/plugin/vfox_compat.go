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
	L, err := e.newSandboxedVM()
	if err != nil {
		return nil, err
	}
	defer L.Close()

	if err := L.DoFile(path); err != nil {
		label := "lua"
		if vfox {
			label = "vfox"
		}
		return nil, fmt.Errorf("load %s plugin %s: %w", label, path, err)
	}

	pluginTable := L.GetGlobal("PLUGIN")
	if pluginTable.Type() != lua.LTTable {
		return nil, fmt.Errorf("%s plugin %s: PLUGIN table not found", pluginLabel(vfox), path)
	}

	var name, displayName string
	if vfox {
		infoTable := L.GetField(pluginTable, "info")
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
	if fn := L.GetGlobal("available"); fn.Type() != lua.LTFunction {
		return nil, fmt.Errorf("%s plugin %s: available() function not found", pluginLabel(vfox), path)
	}
	if fn := L.GetGlobal(specs.archiveFn); fn.Type() != lua.LTFunction {
		return nil, fmt.Errorf("%s plugin %s: %s() function not found", pluginLabel(vfox), path, specs.archiveFn)
	}

	hasEnv := L.GetGlobal(specs.envVarsFn).Type() == lua.LTFunction

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
func promotePluginMethods(L *lua.LState, pluginTable lua.LValue) {
	if pluginTable == nil || pluginTable.Type() != lua.LTTable {
		return
	}
	for _, name := range []string{"available", "archive", "install", "env_vars", "env_keys"} {
		if L.GetGlobal(name).Type() == lua.LTFunction {
			continue
		}
		if field := L.GetField(pluginTable, name); field.Type() == lua.LTFunction {
			L.SetGlobal(name, field)
		}
	}
}

// --- LuaEngine: shared call helpers ---

// callAvailable runs the Lua available() function. For vfox plugins,
// each entry may be a string or a table with a "version" field.
func (e *LuaEngine) callAvailable(p *luaBasePlugin) ([]string, error) {
	L, done, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer done()

	// Security (M-12): Add execution timeout
	doneCh := make(chan error, 1)
	go func() {
		if err := L.CallByParam(lua.P{
			Fn:      L.GetGlobal("available"),
			NRet:    1,
			Protect: true,
		}); err != nil {
			doneCh <- fmt.Errorf("available(): %w", err)
			return
		}
		doneCh <- nil
	}()
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("available() timed out after 30s for plugin %s", p.name)
	}

	result := L.Get(-1)
	L.Pop(1)

	if result.Type() != lua.LTTable {
		return nil, fmt.Errorf("available() must return a table, got %s", result.Type())
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
	return versions, nil
}

// callArchive runs the Lua archive() or install() function (based on specs).
func (e *LuaEngine) callArchive(p *luaBasePlugin, version, goos, arch string) (*LuaArchive, error) {
	L, done, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer done()

	fnName := p.specs.archiveFn
	// Security (M-12): Add execution timeout
	doneCh := make(chan error, 1)
	go func() {
		if err := L.CallByParam(lua.P{
			Fn:      L.GetGlobal(fnName),
			NRet:    1,
			Protect: true,
		}, lua.LString(version), lua.LString(goos), lua.LString(arch)); err != nil {
			doneCh <- fmt.Errorf("%s(%s,%s,%s): %w", fnName, version, goos, arch, err)
			return
		}
		doneCh <- nil
	}()
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(60 * time.Second):
		return nil, fmt.Errorf("%s() timed out after 60s for plugin %s", fnName, p.name)
	}

	result := L.Get(-1)
	L.Pop(1)

	if result.Type() != lua.LTTable {
		return nil, fmt.Errorf("%s() must return a table, got %s", fnName, result.Type())
	}

	a := &LuaArchive{
		URL:    getFieldString(L, result, "url"),
		Format: getFieldString(L, result, "format"),
		Strip:  getFieldInt(L, result, "strip"),
	}
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
	L, done, err := e.vmFor(p.path, p.specs.vfoxFormat)
	if err != nil {
		return nil, err
	}
	defer done()

	fnName := p.specs.envVarsFn
	// Security (M-12): Add execution timeout
	doneCh := make(chan error, 1)
	go func() {
		if err := L.CallByParam(lua.P{
			Fn:      L.GetGlobal(fnName),
			NRet:    1,
			Protect: true,
		}, lua.LString(installPath)); err != nil {
			doneCh <- fmt.Errorf("%s(%s): %w", fnName, installPath, err)
			return
		}
		doneCh <- nil
	}()
	select {
	case err := <-doneCh:
		if err != nil {
			return nil, err
		}
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("%s() timed out after 10s for plugin %s", fnName, p.name)
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
