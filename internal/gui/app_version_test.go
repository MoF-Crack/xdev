package gui

import (
	"path/filepath"
	"testing"
)

func TestExtractVersion_Semver(t *testing.T) {
	got := extractVersion("3.12.7")
	if got != "3.12.7" {
		t.Errorf("extractVersion(%q) = %q, want %q", "3.12.7", got, "3.12.7")
	}
}

func TestExtractVersion_VPrefix(t *testing.T) {
	got := extractVersion("v18.0.0")
	if got != "18.0.0" {
		t.Errorf("extractVersion(%q) = %q, want %q", "v18.0.0", got, "18.0.0")
	}
}

func TestExtractVersion_GoPrefix(t *testing.T) {
	got := extractVersion("go1.22.0")
	if got != "1.22.0" {
		t.Errorf("extractVersion(%q) = %q, want %q", "go1.22.0", got, "1.22.0")
	}
}

func TestExtractVersion_YarnOutput(t *testing.T) {
	got := extractVersion("1.22.22")
	if got != "1.22.22" {
		t.Errorf("extractVersion(%q) = %q, want %q", "1.22.22", got, "1.22.22")
	}
}

func TestExtractVersion_PnpmOutput(t *testing.T) {
	got := extractVersion("9.1.2")
	if got != "9.1.2" {
		t.Errorf("extractVersion(%q) = %q, want %q", "9.1.2", got, "9.1.2")
	}
}

func TestExtractVersion_NpmOutput(t *testing.T) {
	got := extractVersion("npm 10.8.2")
	if got != "10.8.2" {
		t.Errorf("extractVersion(%q) = %q, want %q", "npm 10.8.2", got, "10.8.2")
	}
}

func TestExtractVersion_WindowsSuffix(t *testing.T) {
	got := extractVersion("2.47.0.windows.1")
	if got != "2.47.0" {
		t.Errorf("extractVersion(%q) = %q, want %q", "2.47.0.windows.1", got, "2.47.0")
	}
}

func TestExtractVersion_Empty(t *testing.T) {
	got := extractVersion("")
	if got != "" {
		t.Errorf("extractVersion(%q) = %q, want %q", "", got, "")
	}
}

func TestExtractVersion_NoVersion(t *testing.T) {
	got := extractVersion("no version here")
	if got != "" {
		t.Errorf("extractVersion(%q) = %q, want %q", "no version here", got, "")
	}
}

func TestExtractVersion_CjcOutput(t *testing.T) {
	got := extractVersion("Cangjie Compiler: 1.1.3 (cjnative)")
	if got != "1.1.3" {
		t.Errorf("extractVersion(%q) = %q, want %q", "Cangjie Compiler: 1.1.3 (cjnative)", got, "1.1.3")
	}
}

func TestExtractVersion_CjcOutput_Old(t *testing.T) {
	got := extractVersion("Cangjie Compiler: 0.53.4")
	if got != "0.53.4" {
		t.Errorf("extractVersion(%q) = %q, want %q", "Cangjie Compiler: 0.53.4", got, "0.53.4")
	}
}

func TestIsVersionLike_TwoDots(t *testing.T) {
	if !isVersionLike("1.22.0") {
		t.Errorf("isVersionLike(%q) = false, want true", "1.22.0")
	}
}

func TestIsVersionLike_OneDot(t *testing.T) {
	if !isVersionLike("1.22") {
		t.Errorf("isVersionLike(%q) = false, want true", "1.22")
	}
}

func TestIsVersionLike_NoDot(t *testing.T) {
	if isVersionLike("1") {
		t.Errorf("isVersionLike(%q) = true, want false", "1")
	}
}

func TestIsVersionLike_Empty(t *testing.T) {
	if isVersionLike("") {
		t.Errorf("isVersionLike(%q) = true, want false", "")
	}
}

func TestIsInsideNodeDir_NodePath(t *testing.T) {
	// 路径包含 \node\ 但磁盘上不存在 node.exe，修复后应返回 false
	// （真实场景中 xdev 管理的 node 路径会先被 isInsideDir 检测到，
	// 不会走到 isInsideNodeDir 分支）
	if isInsideNodeDir("E:\\XDev\\install\\node\\20\\yarn.cmd") {
		t.Errorf("isInsideNodeDir(%q) = true, want false (no node.exe exists on disk)", "E:\\XDev\\install\\node\\20\\yarn.cmd")
	}
}

func TestIsInsideNodeDir_NonNodePath(t *testing.T) {
	if isInsideNodeDir("C:\\Python312\\pip.exe") {
		t.Errorf("isInsideNodeDir(%q) = true, want false", "C:\\Python312\\pip.exe")
	}
}

func TestIsInsideDir_Subdirectory(t *testing.T) {
	// Use platform-native separators so filepath.Clean behaves consistently
	// on Windows and Unix CI runners.
	file := filepath.Join("XDev", "shims", "node.exe")
	dir := filepath.Join("XDev", "shims")
	if !isInsideDir(file, dir) {
		t.Errorf("isInsideDir(%q, %q) = false, want true", file, dir)
	}
}

func TestIsInsideDir_NotSubdirectory(t *testing.T) {
	file := filepath.Join("Tools", "node.exe")
	dir := filepath.Join("XDev", "shims")
	if isInsideDir(file, dir) {
		t.Errorf("isInsideDir(%q, %q) = true, want false", file, dir)
	}
}
