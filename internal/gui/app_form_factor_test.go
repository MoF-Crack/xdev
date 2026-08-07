// app_form_factor_test.go tests the form factor fields (formFactor /
// guiBinary) surfaced in ToolInfo from the manifest (Spec 023 T-A6).
package gui

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

func TestPluginToToolInfo_FormFactorFromManifest(t *testing.T) {
	app := newTestApp()
	p := &guiFakePlugin{
		name:  "vscode",
		shims: []string{"code"},
		manifest: &manifest.Tool{
			Name:       "vscode",
			Shims:      []string{"code"},
			FormFactor: []string{"gui", "cli"},
			GUIBinary:  "bin/code.exe",
		},
	}
	info := app.pluginToToolInfo(p)
	if !reflect.DeepEqual(info.FormFactor, []string{"gui", "cli"}) {
		t.Errorf("FormFactor = %v, want [gui cli]", info.FormFactor)
	}
	if info.GUIBinary != "bin/code.exe" {
		t.Errorf("GUIBinary = %q, want bin/code.exe", info.GUIBinary)
	}
}

func TestPluginToToolInfo_FormFactorInferredCli(t *testing.T) {
	// 无 form_factor 的 manifest（现有工具）：由 shims 推断 [cli]。
	app := newTestApp()
	p := &guiFakePlugin{
		name:  "go",
		shims: []string{"go"},
		manifest: &manifest.Tool{
			Name:  "go",
			Shims: []string{"go"},
		},
	}
	info := app.pluginToToolInfo(p)
	if !reflect.DeepEqual(info.FormFactor, []string{"cli"}) {
		t.Errorf("FormFactor = %v, want [cli] (inferred)", info.FormFactor)
	}
	if info.GUIBinary != "" {
		t.Errorf("GUIBinary = %q, want empty", info.GUIBinary)
	}
}

func TestPluginToToolInfo_NilManifestSafe(t *testing.T) {
	// manifest 为 nil（Lua 插件/测试桩）必须安全，字段留空。
	app := newTestApp()
	p := &guiFakePlugin{name: "lua-tool", shims: []string{"lua-tool"}}
	info := app.pluginToToolInfo(p)
	if info.FormFactor != nil {
		t.Errorf("FormFactor = %v, want nil for nil manifest", info.FormFactor)
	}
	if info.GUIBinary != "" {
		t.Errorf("GUIBinary = %q, want empty for nil manifest", info.GUIBinary)
	}
}

func TestToolInfo_JSONFormFactorFields(t *testing.T) {
	info := ToolInfo{
		Name:        "vscode",
		DisplayName: "VSCode",
		FormFactor:  []string{"gui", "cli"},
		GUIBinary:   "bin/code.exe",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["formFactor"]; !ok {
		t.Error("expected formFactor key in JSON")
	}
	if _, ok := m["guiBinary"]; !ok {
		t.Error("expected guiBinary key in JSON")
	}
	ff, ok := m["formFactor"].([]interface{})
	if !ok || len(ff) != 2 {
		t.Errorf("formFactor = %v, want array of 2", m["formFactor"])
	}
	if m["guiBinary"] != "bin/code.exe" {
		t.Errorf("guiBinary = %v, want bin/code.exe", m["guiBinary"])
	}
}

func TestToolInfo_JSONOmitEmptyFormFactor(t *testing.T) {
	// 无形态的 ToolInfo：formFactor/guiBinary 不出现在 JSON（omitempty）。
	info := ToolInfo{Name: "bare"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["formFactor"]; ok {
		t.Error("empty FormFactor must be omitted from JSON")
	}
	if _, ok := m["guiBinary"]; ok {
		t.Error("empty GUIBinary must be omitted from JSON")
	}
}

// 确保 guiFakePlugin 仍满足 Plugin 接口（编译期检查）。
var _ plugin.Plugin = (*guiFakePlugin)(nil)
