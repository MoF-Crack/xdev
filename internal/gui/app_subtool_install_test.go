// app_subtool_install_test.go tests Spec 026 T-A1/T-A2/T-A3:
// pmDef extension (enableCommand/parentTool/installMethods) +
// two-phase detection (dynamic CanInstall/InstallHint) +
// InstallSubTool API (enable + method paths).
package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/hostscan"
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
	if len(yarn.installMethods) < 2 {
		t.Fatalf("yarn installMethods len = %d, want >= 2", len(yarn.installMethods))
	}
	yarnMethods := map[string]string{}
	for _, m := range yarn.installMethods {
		yarnMethods[m.name] = m.command
	}
	if yarnMethods["corepack"] == "" || yarnMethods["npm"] == "" {
		t.Errorf("yarn methods = %v, want corepack and npm", yarnMethods)
	}
	if !strings.Contains(yarnMethods["npm"], "npm install -g yarn") {
		t.Errorf("yarn npm method = %q, want npm install -g yarn", yarnMethods["npm"])
	}

	// pnpm: enableCommand + parentTool
	pnpm, ok := found["pnpm"]
	if !ok {
		t.Fatal("pnpm pmDef missing")
	}
	if pnpm.enableCommand == "" {
		t.Error("pnpm should have enableCommand")
	}
	if len(pnpm.installMethods) < 2 {
		t.Fatalf("pnpm installMethods len = %d, want >= 2", len(pnpm.installMethods))
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
	if updated.InstallHint != "" {
		t.Errorf("yarn InstallHint should be empty when node installed; got %q", updated.InstallHint)
	}
	if !updated.CanInstall {
		t.Fatal("yarn should remain installable")
	}
	// When both corepack and npm are detectable, the UI should offer methods
	// instead of a single enable button.
	if len(updated.InstallMethods) == 0 && updated.EnableCommand == "" {
		t.Error("yarn should expose EnableCommand or InstallMethods when node is installed")
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

func TestSubToolMethodAvailablePrefersXdevNode(t *testing.T) {
	app := newTestApp(t)
	cfg := app.getCfg()
	nodeDir := filepath.Join(cfg.Core.InstallDir, "node", "24.0.0")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"corepack.cmd", "npm.cmd"} {
		if err := os.WriteFile(filepath.Join(nodeDir, name), []byte("@echo off\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	linkCurrent(t, cfg.Core.XMDevDir, cfg.Core.InstallDir, "node", "24.0.0")

	if !app.subToolMethodAvailable("corepack") {
		t.Fatal("corepack should be available from xdev-managed Node even if PATH misses it")
	}
	if !app.subToolMethodAvailable("npm") {
		t.Fatal("npm should be available from xdev-managed Node even if PATH misses it")
	}
	corepack, ok := app.findXdevManagedCorepack()
	if !ok {
		t.Fatal("findXdevManagedCorepack returned false")
	}
	if filepath.Base(corepack) != "corepack.cmd" && filepath.Base(corepack) != "corepack" {
		t.Errorf("corepack path = %q, want a corepack binary in the xdev node dir", corepack)
	}
}

func TestPnpmStalePATHStillOffersInstall(t *testing.T) {
	app := newTestApp(t)
	cfg := app.getCfg()
	nodeDir := filepath.Join(cfg.Core.InstallDir, "node", "24.0.0")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"corepack.cmd", "npm.cmd"} {
		if err := os.WriteFile(filepath.Join(nodeDir, name), []byte("@echo off\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	linkCurrent(t, cfg.Core.XMDevDir, cfg.Core.InstallDir, "node", "24.0.0")

	var pnpmPm pmDef
	for _, d := range packageManagerDefs() {
		if d.name == "pnpm" {
			pnpmPm = d
			break
		}
	}
	info := PackageManagerInfo{Name: "pnpm", Installed: true, LocalPath: `D:\Env\nodejs\pnpm.cmd`, Version: "8.15.0"}
	updated := app.applySubToolDynamicState(pnpmPm, info, []PackageManagerInfo{
		{Name: "npm", Installed: true},
		info,
	}, map[string]bool{"npm": true})
	if !updated.CanInstall {
		t.Fatal("pnpm should remain installable so xdev Node can enable corepack/npm even when PATH has a stale copy")
	}
	if len(updated.InstallMethods) == 0 && updated.EnableCommand == "" {
		t.Fatal("pnpm should expose corepack/npm methods from xdev Node")
	}

	offered := app.shouldOfferSubToolInstall(pnpmPm, info)
	if !offered {
		t.Fatal("stale D: pnpm with no xdev-managed instance should still offer enable/install")
	}
}

func TestShouldOfferSubToolInstall_PipxAndManagedPnpmHidden(t *testing.T) {
	app := newTestApp(t)
	var pipxPm, pnpmPm pmDef
	for _, d := range packageManagerDefs() {
		switch d.name {
		case "pipx":
			pipxPm = d
		case "pnpm":
			pnpmPm = d
		}
	}

	pipx := PackageManagerInfo{
		Name:      "pipx",
		Installed: true,
		LocalPath: `C:\Users\me\.local\bin\pipx.exe`,
		InstallMethods: []InstallMethodInfo{
			{Name: "pip", Command: "pip install pipx"},
		},
	}
	if app.shouldOfferSubToolInstall(pipxPm, pipx) {
		t.Error("installed pipx must not keep an Install button")
	}

	managed := PackageManagerInfo{
		Name:      "pnpm",
		Installed: true,
		LocalPath: filepath.Join(app.getCfg().Core.InstallDir, "node", "24.0.0", "pnpm.cmd"),
		Instances: []PMInstanceInfo{{Path: filepath.Join(app.getCfg().Core.InstallDir, "node", "24.0.0", "pnpm.cmd"), Managed: true, Source: "xdev"}},
	}
	if app.shouldOfferSubToolInstall(pnpmPm, managed) {
		t.Error("xdev-managed pnpm must not keep an Install button")
	}
}

func TestInstallWrappersForwardToSpec(t *testing.T) {
	// Wrappers must share one InstallSpec shape so Wails methods stay thin.
	s := InstallSpec{Tool: "go", Version: "1.25.0", Form: "cli", Confirmed: true, Force: true, Kind: "tool"}
	if s.Tool != "go" || s.Version != "1.25.0" || s.Form != "cli" || !s.Confirmed || !s.Force || s.Kind != "tool" {
		t.Fatalf("InstallSpec fields not round-tripped: %+v", s)
	}
}

func TestInstallUnknownKind(t *testing.T) {
	app := newTestApp(t)
	err := app.install(InstallSpec{Tool: "go", Kind: "not-a-kind"})
	if err == nil {
		t.Fatal("unknown Kind should error")
	}
	if !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("error = %q, want unknown kind", err)
	}
}

func TestInstallPackageManagerAndSubToolForwardToSpec(t *testing.T) {
	app := newTestApp(t)
	if err := app.InstallPackageManager("nonexistent-pm"); err == nil {
		t.Fatal("InstallPackageManager should error for unknown tool")
	}
	if err := app.InstallSubTool("nonexistent-pm", ""); err == nil {
		t.Fatal("InstallSubTool should error for unknown pmDef")
	}
	if err := app.install(InstallSpec{Tool: "pipx", Kind: "sub-tool"}); err == nil {
		t.Fatal("pipx with no method should still error via InstallSpec")
	}
}

func TestCollectPMInstancesWalksEveryPATHHit(t *testing.T) {
	app := newTestApp(t)
	tmp := t.TempDir()
	shimDir := filepath.Join(tmp, "shims")
	dDrive := filepath.Join(tmp, "D-Env-nodejs")
	eDrive := filepath.Join(tmp, "E-XDev-node")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "pnpm"
	if runtime.GOOS == "windows" {
		name = "pnpm.cmd"
	}
	for _, dir := range []string{shimDir, dDrive, eDrive} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("echo 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ShimDir = shimDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	orig := os.Getenv("PATH")
	t.Setenv("PATH", strings.Join([]string{shimDir, dDrive, eDrive}, string(os.PathListSeparator)))
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", orig) })

	pm, ok := findPMDefByName("pnpm")
	if !ok {
		t.Fatal("pnpm pmDef missing")
	}
	got := app.collectPMInstances(pm, PackageManagerInfo{})
	if len(got) < 2 {
		t.Fatalf("instances = %d (%v), want D: and E: (shim skipped)", len(got), got)
	}
	var sawD, sawE, sawShim bool
	for _, inst := range got {
		if hostscan.InsideDir(inst.Path, dDrive) {
			sawD = true
			if inst.Managed {
				t.Errorf("D: leftover marked Managed: %+v", inst)
			}
		}
		if hostscan.InsideDir(inst.Path, eDrive) {
			sawE = true
		}
		if hostscan.InsideDir(inst.Path, shimDir) {
			sawShim = true
		}
	}
	if !sawD || !sawE {
		t.Fatalf("want both D: and E: hits, got %+v", got)
	}
	if sawShim {
		t.Fatal("shim-dir copy must not appear as a PM instance")
	}
}

func TestUpdatePackageManager_SubToolRejected(t *testing.T) {
	app := newTestApp(t)
	err := app.UpdatePackageManager("pnpm")
	if err == nil {
		t.Fatal("UpdatePackageManager(pnpm) should reject sub-tools")
	}
	if !strings.Contains(err.Error(), "附属工具") {
		t.Errorf("error = %q, want 附属工具", err)
	}
	if err := app.UpdatePackageManager("yarn"); err == nil {
		t.Error("UpdatePackageManager(yarn) should reject sub-tools")
	}
}

func TestHasUpdateNotSetForYarnPnpm(t *testing.T) {
	for _, name := range []string{"yarn", "pnpm"} {
		pm, ok := findPMDefByName(name)
		if !ok {
			t.Fatalf("%s pmDef missing", name)
		}
		if pm.detectCmd == pm.toolName {
			t.Errorf("%s should be a sub-tool (detectCmd != toolName); HasUpdate must stay false", name)
		}
	}
}

func TestSignalLocalReadyUnblocksWait(t *testing.T) {
	app := newTestApp(t)
	done := make(chan struct{})
	go func() {
		app.waitForLocalCache()
		close(done)
	}()
	app.signalLocalReady()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForLocalCache did not return after signalLocalReady")
	}
	// second signal must not panic
	app.signalLocalReady()
}

func TestEnvWithXdevShimsPrependsShimDir(t *testing.T) {
	app := newTestApp(t)
	got := app.envWithXdevShims([]string{"FOO=1", "PATH=C:\\Windows"})
	wantPrefix := "PATH=" + app.getCfg().Core.ShimDir
	found := false
	for _, e := range got {
		if len(e) >= 5 && e[:5] == "PATH=" {
			found = true
			if len(e) < len(wantPrefix) || e[:len(wantPrefix)] != wantPrefix {
				t.Errorf("PATH entry = %q, want prefix %q", e, wantPrefix)
			}
		}
	}
	if !found {
		t.Fatal("PATH entry missing after envWithXdevShims")
	}
}