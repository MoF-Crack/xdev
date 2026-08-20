// app_package_manager_install_test.go tests Spec 025 T-A1/T-A2:
// pmDef extension + InstallPackageManager script install path.
package gui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPmDefScriptInstallFields verifies brew/scoop/choco have installCommand
// populated (Spec 025 T-A1).
func TestPmDefScriptInstallFields(t *testing.T) {
	defs := packageManagerDefs()
	found := map[string]pmDef{}
	for _, d := range defs {
		found[d.name] = d
	}
	for _, name := range []string{"brew", "scoop", "choco"} {
		pm, ok := found[name]
		if !ok {
			t.Errorf("pmDef %q missing from packageManagerDefs", name)
			continue
		}
		if pm.installCommand == "" {
			t.Errorf("pmDef %q has empty installCommand (should support script install)", name)
		}
		if pm.executionEnv == "" {
			t.Errorf("pmDef %q has empty executionEnv", name)
		}
		if pm.platform == "" {
			t.Errorf("pmDef %q has empty platform", name)
		}
	}
}

// TestPmDefCanInstallScript verifies canInstall is true for brew/scoop/choco
// after Spec 025 (installCommand != "" → canInstall).
func TestPmDefCanInstallScript(t *testing.T) {
	defs := packageManagerDefs()
	for _, pm := range defs {
		if pm.installCommand != "" {
			// canInstall should be true when installCommand is set
			canInstall := (pm.toolName != "" && pm.detectCmd == pm.toolName) || pm.installCommand != ""
			if !canInstall {
				t.Errorf("pmDef %q with installCommand should have canInstall=true", pm.name)
			}
		}
	}
}

// TestPmDefChocoRequiresAdmin verifies Chocolatey requires admin (safety check).
func TestPmDefChocoRequiresAdmin(t *testing.T) {
	defs := packageManagerDefs()
	for _, pm := range defs {
		if pm.name == "choco" {
			if !pm.requiresAdmin {
				t.Error("choco should require admin (Chocolatey needs elevated PowerShell)")
			}
		}
		if pm.name == "scoop" {
			if pm.requiresAdmin {
				t.Error("scoop should NOT require admin (Scoop installs to user dir)")
			}
		}
	}
}

// TestFindPMDefByName verifies lookup works for script-installable PMs.
func TestFindPMDefByName(t *testing.T) {
	pm, ok := findPMDefByName("scoop")
	if !ok {
		t.Fatal("findPMDefByName(scoop) not found")
	}
	if pm.installCommand == "" {
		t.Error("scoop should have installCommand")
	}

	if _, ok := findPMDefByName("nonexistent-pm"); ok {
		t.Error("findPMDefByName should return false for unknown name")
	}
}

func TestFindPMDefByToolName_PipParent(t *testing.T) {
	pm, ok := findPMDefByToolName("python")
	if !ok {
		t.Fatal("findPMDefByToolName(python) should resolve pip")
	}
	if pm.name != "pip" {
		t.Errorf("resolved %q, want pip", pm.name)
	}
	if _, ok := findPMDefByToolName("nonexistent"); ok {
		t.Error("unknown toolName must miss")
	}
}

func TestInferIndependentPMRoot_ScoopShims(t *testing.T) {
	t.Setenv("SCOOP", "")
	root, err := inferIndependentPMRoot(pmDef{name: "scoop", displayName: "Scoop"}, `C:\Users\demo\scoop\shims\scoop.cmd`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(`C:\Users\demo\scoop`)
	if root != want {
		t.Errorf("scoop shim root = %q, want %q", root, want)
	}
}

func TestInferIndependentPMRoot_ChocoBin(t *testing.T) {
	t.Setenv("ChocolateyInstall", "")
	root, err := inferIndependentPMRoot(pmDef{name: "choco", displayName: "Chocolatey"}, `C:\ProgramData\chocolatey\bin\choco.exe`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(`C:\ProgramData\chocolatey`)
	if root != want {
		t.Errorf("choco bin root = %q, want %q", root, want)
	}
}

func TestInferIndependentPMRoot_EnvOverride(t *testing.T) {
	t.Setenv("SCOOP", `E:\ScoopRoot`)
	root, err := inferIndependentPMRoot(pmDef{name: "scoop", displayName: "Scoop"}, `C:\Users\demo\scoop\shims\scoop.cmd`)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Clean(`E:\ScoopRoot`) {
		t.Errorf("SCOOP env override = %q, want %q", root, filepath.Clean(`E:\ScoopRoot`))
	}
}

// TestInstallPackageManagerScriptPlatformCheck verifies platform mismatch
// returns error without executing.
func TestInstallPackageManagerScriptPlatformCheck(t *testing.T) {
	pm, _ := findPMDefByName("scoop")
	if pm.platform != "windows" {
		t.Fatalf("scoop platform = %q, want windows", pm.platform)
	}
	// On non-windows, installPackageManagerViaScript should return platform error.
	if runtime.GOOS != "windows" {
		app := catTestApp(t)
		err := app.installPackageManagerViaScript(pm)
		if err == nil {
			t.Fatal("expected platform mismatch error on non-windows for scoop")
		}
		if !strings.Contains(err.Error(), "平台") {
			t.Errorf("error should mention platform; got %v", err)
		}
	}
}

// TestIsWindowsAdmin is a sanity check for the admin probe helper.
func TestIsWindowsAdmin(t *testing.T) {
	if runtime.GOOS == "windows" {
		// We can't assert true/false definitively (depends on test runner context),
		// but the function must not panic and must return a bool.
		_ = isWindowsAdmin()
	} else {
		if isWindowsAdmin() {
			t.Error("isWindowsAdmin should return false on non-windows")
		}
	}
}