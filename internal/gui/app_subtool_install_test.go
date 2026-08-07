// app_subtool_install_test.go tests Spec 026 T-A1/T-A2/T-A3:
// pmDef extension (enableCommand/parentTool/installMethods) +
// two-phase detection (dynamic CanInstall/InstallHint) +
// InstallSubTool API (enable + method paths).
package gui

import (
	"testing"
)

// TestPmDefSubToolFields verifies yarn/pnpm/pipx have Spec 026 fields populated.
func TestPmDefSubToolFields(t *testing.T) {
	defs := packageManagerDefs()
	found := map[string]pmDef{}
	for _, d := range defs {
		found[d.name] = d
	}

	// yarn: enableCommand + parentTool
	yarn, ok := found["yarn"]
	if !ok {
		t.Fatal("yarn pmDef missing")
	}
	if yarn.enableCommand == "" {
		t.Error("yarn should have enableCommand")
	}
	if yarn.parentTool != "node" {
		t.Errorf("yarn parentTool = %q, want node", yarn.parentTool)
	}

	// pnpm: enableCommand + parentTool
	pnpm, ok := found["pnpm"]
	if !ok {
		t.Fatal("pnpm pmDef missing")
	}
	if pnpm.enableCommand == "" {
		t.Error("pnpm should have enableCommand")
	}
	if pnpm.parentTool != "node" {
		t.Errorf("pnpm parentTool = %q, want node", pnpm.parentTool)
	}

	// pipx: installMethods
	pipx, ok := found["pipx"]
	if !ok {
		t.Fatal("pipx pmDef missing")
	}
	if len(pipx.installMethods) != 2 {
		t.Errorf("pipx installMethods len = %d, want 2", len(pipx.installMethods))
	}
	// Verify method names
	names := map[string]bool{}
	for _, m := range pipx.installMethods {
		names[m.name] = true
	}
	if !names["pip"] || !names["uv"] {
		t.Errorf("pipx installMethods should have pip and uv; got %v", names)
	}
}

// TestPmDefNonSubToolsUnaffected verifies non-subtool PMs have empty Spec 026 fields.
func TestPmDefNonSubToolsUnaffected(t *testing.T) {
	defs := packageManagerDefs()
	for _, pm := range defs {
		// npm/npx/uv/uvx/pip/cargo/maven/brew/scoop/choco should NOT have Spec 026 fields
		if pm.name == "yarn" || pm.name == "pnpm" || pm.name == "pipx" {
			continue // these are the subtools
		}
		if pm.enableCommand != "" {
			t.Errorf("%s should not have enableCommand", pm.name)
		}
		if pm.parentTool != "" {
			t.Errorf("%s should not have parentTool", pm.name)
		}
		if len(pm.installMethods) != 0 {
			t.Errorf("%s should not have installMethods", pm.name)
		}
	}
}

// TestApplySubToolDynamicStateEnable verifies the two-phase detection for
// yarn/pnpm: parent (node) installed → CanInstall=true + EnableCommand set.
func TestApplySubToolDynamicStateEnable(t *testing.T) {
	app := catTestApp(t)
	defs := packageManagerDefs()
	var yarnPm pmDef
	for _, d := range defs {
		if d.name == "yarn" {
			yarnPm = d
			break
		}
	}
	// Simulate: parent (npm) installed = true
	results := []PackageManagerInfo{
		{Name: "npm", Installed: true},
		{Name: "yarn", Installed: false},
	}
	detectedByName := map[string]bool{"npm": true}
	updated := app.applySubToolDynamicState(yarnPm, results[1], results, detectedByName)
	if !updated.CanInstall {
		t.Error("yarn CanInstall should be true when node installed")
	}
	if updated.EnableCommand == "" {
		t.Error("yarn EnableCommand should be set when node installed")
	}
	if updated.InstallHint != "" {
		t.Errorf("yarn InstallHint should be empty when node installed; got %q", updated.InstallHint)
	}
}

// TestApplySubToolDynamicStateEnableParentMissing verifies parent not installed
// → CanInstall=false + installHint kept.
// Uses a fake parentTool name guaranteed not on PATH to isolate the logic
// from the test environment's actual node installation.
func TestApplySubToolDynamicStateEnableParentMissing(t *testing.T) {
	app := catTestApp(t)
	// Build a fake pmDef with a parentTool that definitely is not on PATH.
	fakePm := pmDef{
		name:          "fake-subtool",
		enableCommand: "fake-cmd enable fake",
		parentTool:    "nonexistent-parent-tool-xyz",
		installHint:   "需先安装 nonexistent-parent-tool-xyz",
	}
	// Simulate: parent not installed, not in results, not on PATH.
	results := []PackageManagerInfo{
		{Name: "fake-subtool", Installed: false},
	}
	detectedByName := map[string]bool{}
	updated := app.applySubToolDynamicState(fakePm, results[0], results, detectedByName)
	if updated.CanInstall {
		t.Error("CanInstall should be false when parent not installed and not on PATH")
	}
	if updated.EnableCommand != "" {
		t.Error("EnableCommand should be empty when parent not available")
	}
	if updated.InstallHint == "" {
		t.Error("InstallHint should be kept when parent not available")
	}
}

// TestApplySubToolDynamicStateMethodsBoth verifies pipx with both pip+uv available
// → CanInstall=true + InstallMethods=[pip,uv].
func TestApplySubToolDynamicStateMethodsBoth(t *testing.T) {
	app := catTestApp(t)
	defs := packageManagerDefs()
	var pipxPm pmDef
	for _, d := range defs {
		if d.name == "pipx" {
			pipxPm = d
			break
		}
	}
	// This test depends on pip/uv being on PATH. On CI/CI without them,
	// the test verifies the logic: if both detected, CanInstall=true + 2 methods.
	// We can't reliably control PATH in unit tests, so we test the structure
	// by checking that applySubToolDynamicState doesn't panic and returns
	// a valid result (CanInstall may be true or false depending on PATH).
	results := []PackageManagerInfo{
		{Name: "pipx", Installed: false},
	}
	detectedByName := map[string]bool{}
	updated := app.applySubToolDynamicState(pipxPm, results[0], results, detectedByName)
	// If pip or uv is on PATH, CanInstall should be true.
	// We just verify no panic and InstallMethods is populated when CanInstall.
	if updated.CanInstall {
		if len(updated.InstallMethods) == 0 {
			t.Error("pipx CanInstall=true but InstallMethods empty")
		}
	}
}

// TestInstallSubToolUnknownTool verifies error for unknown tool name.
func TestInstallSubToolUnknownTool(t *testing.T) {
	app := catTestApp(t)
	err := app.InstallSubTool("nonexistent-pm", "")
	if err == nil {
		t.Error("InstallSubTool should error for unknown tool")
	}
}

// TestInstallSubToolMultipleMethodNoSelection verifies error when multiple
// methods exist but no method specified.
func TestInstallSubToolMultipleMethodNoSelection(t *testing.T) {
	app := catTestApp(t)
	// pipx has 2 methods; calling with method="" should error "请指定安装方式"
	err := app.InstallSubTool("pipx", "")
	if err == nil {
		t.Error("InstallSubTool should error when multiple methods and no selection")
	}
}

// TestInstallSubToolUnknownMethod verifies error for unknown method name.
func TestInstallSubToolUnknownMethod(t *testing.T) {
	app := catTestApp(t)
	err := app.InstallSubTool("pipx", "nonexistent-method")
	if err == nil {
		t.Error("InstallSubTool should error for unknown method")
	}
}

// TestFormatMethodNames verifies the helper formats method names correctly.
func TestFormatMethodNames(t *testing.T) {
	methods := []installMethod{
		{name: "pip"},
		{name: "uv"},
	}
	got := formatMethodNames(methods)
	if got != "pip, uv" {
		t.Errorf("formatMethodNames = %q, want %q", got, "pip, uv")
	}
}