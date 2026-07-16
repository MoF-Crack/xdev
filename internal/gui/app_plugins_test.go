package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/plugin"
)

func TestListPluginsReportsRegistryPluginTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lua")
	script := `PLUGIN = { info = { name = "demo", version = "1.0.0" } }
function available() return {{ version = "1.0.0" }} end
function install(version, ctx) return { url = "https://example.com/demo.zip" } end
function env_keys() return {} end`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil { t.Fatal(err) }
	vfox, err := plugin.NewLuaEngine().LoadVfoxPlugin(path)
	if err != nil { t.Fatal(err) }
	reg := plugin.NewRegistry()
	reg.Register(vfox)
	app := newTestApp()
	app.cfg.Core.XMDevDir = dir
	app.registry = reg

	got := app.ListPlugins()
	if len(got) != 1 { t.Fatalf("ListPlugins() length = %d, want 1: %#v", len(got), got) }
	if got[0].Type != "vfox" { t.Fatalf("Type = %q, want vfox", got[0].Type) }
	data, err := json.Marshal(got[0])
	if err != nil { t.Fatal(err) }
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil { t.Fatal(err) }
	if encoded["type"] != "vfox" { t.Fatalf("JSON type = %#v", encoded["type"]) }
}
