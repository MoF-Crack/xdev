package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPluginLua = `
PLUGIN = {
    name = "testtool",
    display_name = "Test Tool",
    category = "build",
    shims = {"testtool", "tt"},
}

function available()
    return {"1.0.0", "1.1.0", "2.0.0"}
end

function archive(version, os, arch)
    return {
        url = "https://example.com/testtool/" .. version .. "/tool-" .. os .. "-" .. arch .. ".tar.gz",
        format = "tar.gz",
        strip = 1
    }
end

function env_vars(install_path)
    return {
        LUA_HOME = install_path,
        ZIG_HOME = install_path .. "/bin"
    }
end
`

func writeTempLua(t *testing.T, dir, filename, content string) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write temp lua file: %v", err)
	}
	return p
}

func TestLuaEngineLoadPlugin(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "plugin.lua", validPluginLua)

	engine := NewLuaEngine()
	plugin, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}
	if plugin.Name() != "testtool" {
		t.Errorf("Name() = %q, want %q", plugin.Name(), "testtool")
	}
	if plugin.DisplayName() != "Test Tool" {
		t.Errorf("DisplayName() = %q, want %q", plugin.DisplayName(), "Test Tool")
	}
	if plugin.Category() != "build" {
		t.Errorf("Category() = %q, want %q", plugin.Category(), "build")
	}
	shims := plugin.Shims()
	if len(shims) != 2 || shims[0] != "testtool" || shims[1] != "tt" {
		t.Errorf("Shims() = %v, want [testtool tt]", shims)
	}
}

func TestLuaEngineLoadPluginNoPluginTable(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "noplugintable.lua", `
function available() return {} end
function archive(v, o, a) return { url = "x" } end
`)

	engine := NewLuaEngine()
	_, err := engine.LoadPlugin(path)
	if err == nil {
		t.Fatal("LoadPlugin() expected error, got nil")
	}
	want := "PLUGIN table not found"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestLuaEngineLoadPluginNoName(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "noname.lua", `
PLUGIN = {
    display_name = "No Name",
    category = "system",
}
function available() return {} end
function archive(v, o, a) return { url = "x" } end
`)

	engine := NewLuaEngine()
	_, err := engine.LoadPlugin(path)
	if err == nil {
		t.Fatal("LoadPlugin() expected error, got nil")
	}
	want := "name is required"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestLuaEngineLoadPluginNoAvailable(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "noavailable.lua", `
PLUGIN = {
    name = "testtool",
    display_name = "Test Tool",
    category = "build",
}
function archive(v, o, a) return { url = "x" } end
`)

	engine := NewLuaEngine()
	_, err := engine.LoadPlugin(path)
	if err == nil {
		t.Fatal("LoadPlugin() expected error, got nil")
	}
	want := "available() function not found"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestLuaEngineLoadPluginNoArchive(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "noarchive.lua", `
PLUGIN = {
    name = "testtool",
    display_name = "Test Tool",
    category = "build",
}
function available() return {"1.0.0"} end
`)

	engine := NewLuaEngine()
	_, err := engine.LoadPlugin(path)
	if err == nil {
		t.Fatal("LoadPlugin() expected error, got nil")
	}
	want := "archive() function not found"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want containing %q", err.Error(), want)
	}
}

func TestLuaPluginProperties(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "props.lua", validPluginLua)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	if p.Name() != "testtool" {
		t.Errorf("Name() = %q, want %q", p.Name(), "testtool")
	}
	if p.DisplayName() != "Test Tool" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Test Tool")
	}
	if p.Category() != "build" {
		t.Errorf("Category() = %q, want %q", p.Category(), "build")
	}
	shims := p.Shims()
	if len(shims) != 2 || shims[0] != "testtool" || shims[1] != "tt" {
		t.Errorf("Shims() = %v, want [testtool tt]", shims)
	}
	if p.DefaultVersion() != "latest" {
		t.Errorf("DefaultVersion() = %q, want %q", p.DefaultVersion(), "latest")
	}
	if p.Description() != "Lua plugin: testtool" {
		t.Errorf("Description() = %q, want %q", p.Description(), "Lua plugin: testtool")
	}
}

func TestLuaPluginManifestReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLua(t, dir, "manifest.lua", validPluginLua)

	engine := NewLuaEngine()
	p, err := engine.LoadPlugin(path)
	if err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}
	if p.Manifest() != nil {
		t.Errorf("Manifest() = %v, want nil", p.Manifest())
	}
}

func TestLuaPluginEnvVars(t *testing.T) {
	t.Run("with env_vars function", func(t *testing.T) {
		dir := t.TempDir()
		path := writeTempLua(t, dir, "envvars.lua", validPluginLua)

		engine := NewLuaEngine()
		p, err := engine.LoadPlugin(path)
		if err != nil {
			t.Fatalf("LoadPlugin() error: %v", err)
		}

		env := p.EnvVars("/opt/testtool/1.0.0")
		if len(env) != 2 {
			t.Fatalf("EnvVars() returned %d entries, want 2", len(env))
		}
		// H-4: Only whitelisted env var names are accepted (LUA_HOME, ZIG_HOME)
		if env["LUA_HOME"] != "/opt/testtool/1.0.0" {
			t.Errorf("LUA_HOME = %q, want %q", env["LUA_HOME"], "/opt/testtool/1.0.0")
		}
		if env["ZIG_HOME"] != "/opt/testtool/1.0.0/bin" {
			t.Errorf("ZIG_HOME = %q, want %q", env["ZIG_HOME"], "/opt/testtool/1.0.0/bin")
		}
	})

	t.Run("without env_vars function", func(t *testing.T) {
		dir := t.TempDir()
		path := writeTempLua(t, dir, "noenvvars.lua", `
PLUGIN = {
    name = "noenv",
    display_name = "No Env",
    category = "system",
}
function available() return {"1.0.0"} end
function archive(v, o, a) return { url = "https://example.com/" .. v .. ".zip" } end
`)

		engine := NewLuaEngine()
		p, err := engine.LoadPlugin(path)
		if err != nil {
			t.Fatalf("LoadPlugin() error: %v", err)
		}

		env := p.EnvVars("/opt/noenv/1.0.0")
		if len(env) != 0 {
			t.Errorf("EnvVars() = %v, want empty map", env)
		}
	})
}
