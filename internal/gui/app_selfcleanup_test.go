package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 冒烟反馈 #5：EnsureSelfInstalled 应清理双二进制时代残留的 xdev-gui 副本。
func TestRemoveObsoleteGUICopy(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()

	guiName := "xdev-gui"
	if runtime.GOOS == "windows" {
		guiName = "xdev-gui.exe"
	}
	guiPath := filepath.Join(home, guiName)
	if err := os.WriteFile(guiPath, []byte("legacy gui binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	a.removeObsoleteGUICopy(home)

	if _, err := os.Stat(guiPath); !os.IsNotExist(err) {
		t.Errorf("obsolete xdev-gui copy should be removed, stat err=%v", err)
	}
}

// 无残留时为 no-op（常见路径，不报错）。
func TestRemoveObsoleteGUICopy_NoLeftover(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()

	a.removeObsoleteGUICopy(home) // must not panic or error

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("home should stay untouched, got %d entries", len(entries))
	}
}

// 只清理精确文件名——home 根其他文件（如 config.yaml）不受影响。
// 注意：精确名是平台产物名（Windows 为 xdev-gui.exe，Unix 为 xdev-gui）——
// 生产从未在 Unix 发售过 .exe 命名副本，断言必须按平台取期望名。
func TestRemoveObsoleteGUICopy_OnlyExactName(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()
	guiName := "xdev-gui.exe"
	if runtime.GOOS != "windows" {
		guiName = "xdev-gui"
	}
	guiPath := filepath.Join(home, guiName)
	keepPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(guiPath, []byte("legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.removeObsoleteGUICopy(home)

	if _, err := os.Stat(guiPath); !os.IsNotExist(err) {
		t.Errorf("%s should be removed, err=%v", guiName, err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("config.yaml must survive: %v", err)
	}
}
