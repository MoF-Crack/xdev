//go:build windows

package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- isXDevRoot tests ----

func TestIsXDevRoot_ValidRoot(t *testing.T) {
	if !isXDevRoot("e:\\xdev") {
		t.Errorf("isXDevRoot(%q) = false, want true", "e:\\xdev")
	}
}

func TestIsXDevRoot_Subdir(t *testing.T) {
	if isXDevRoot("e:\\xdev\\shims") {
		t.Errorf("isXDevRoot(%q) = true, want false", "e:\\xdev\\shims")
	}
}

func TestIsXDevRoot_OtherDrive(t *testing.T) {
	if !isXDevRoot("d:\\xdev") {
		t.Errorf("isXDevRoot(%q) = false, want true", "d:\\xdev")
	}
}

func TestIsXDevRoot_NotXDev(t *testing.T) {
	if isXDevRoot("e:\\tools") {
		t.Errorf("isXDevRoot(%q) = true, want false", "e:\\tools")
	}
}

// ---- ToolBinPaths tests ----

func TestToolBinPaths_Java(t *testing.T) {
	installPath := filepath.Join(os.TempDir(), "java-17")
	got := ToolBinPaths("java", installPath)
	want := []string{filepath.Join(installPath, "bin")}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(java) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(java)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Node(t *testing.T) {
	installPath := filepath.Join(os.TempDir(), "node-20")
	got := ToolBinPaths("node", installPath)
	want := []string{installPath}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(node) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(node)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Python(t *testing.T) {
	installPath := filepath.Join(os.TempDir(), "python-3.12")
	got := ToolBinPaths("python", installPath)
	want := []string{installPath, filepath.Join(installPath, "Scripts")}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(python) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(python)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Go(t *testing.T) {
	installPath := t.TempDir()
	// Create standard bin/ directory (strip:1 layout)
	binDir := filepath.Join(installPath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := ToolBinPaths("go", installPath)
	want := []string{binDir}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(go) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(go)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Unknown(t *testing.T) {
	installPath := filepath.Join(os.TempDir(), "unknown-tool")
	// For unknown tools, ToolBinPaths checks if installPath/bin exists.
	// Since we don't create the directory, it falls back to installPath.
	got := ToolBinPaths("unknown", installPath)
	if len(got) == 0 {
		t.Fatalf("ToolBinPaths(unknown) returned empty, expected at least one path")
	}
	// Without a real bin directory, it should return installPath
	if got[0] != installPath {
		t.Errorf("ToolBinPaths(unknown)[0] = %q, want %q", got[0], installPath)
	}
}

// ---- CleanupUserPath tests ----
// These tests save and restore the original user PATH to avoid side effects.

func saveUserPath(t *testing.T) string {
	t.Helper()
	path, err := ReadUserPath()
	if err != nil {
		t.Fatalf("failed to read user PATH: %v", err)
	}
	return path
}

func restoreUserPath(t *testing.T, original string) {
	t.Helper()
	if err := WriteUserPath(original); err != nil {
		t.Fatalf("failed to restore user PATH: %v", err)
	}
	BroadcastEnvironmentChange()
}

func TestCleanupUserPath_ShimMode(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	xdevDir := "E:\\XDev"
	shimDir := xdevDir + "\\shims"
	// Set up a PATH with install and current entries that should be cleaned
	testPath := strings.Join([]string{
		"C:\\Windows\\System32",
		xdevDir + "\\install\\java\\25\\bin",
		xdevDir + "\\current\\java\\bin",
		shimDir,
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath(xdevDir, shimDir, "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}
	if !changed {
		t.Error("expected changed=true, got false")
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath after cleanup: %v", err)
	}

	// install entries should be removed
	if strings.Contains(strings.ToLower(result), "\\xdev\\install\\") {
		t.Errorf("install entry should be removed, got PATH=%q", result)
	}
	// current entries should be removed in shim mode (except current\xdev)
	if strings.Contains(strings.ToLower(result), "\\xdev\\current\\java") {
		t.Errorf("current/java entry should be removed in shim mode, got PATH=%q", result)
	}
	// shim dir should be first
	parts := strings.Split(result, ";")
	if len(parts) > 0 && strings.ToLower(filepath.Clean(parts[0])) != strings.ToLower(filepath.Clean(shimDir)) {
		t.Errorf("shim dir should be first entry, got PATH=%q", result)
	}
}

func TestCleanupUserPath_DirectMode(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	xdevDir := "E:\\XDev"
	shimDir := xdevDir + "\\shims"
	// In direct mode, current entries should be preserved
	testPath := strings.Join([]string{
		"C:\\Windows\\System32",
		xdevDir + "\\current\\java\\bin",
		shimDir,
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath(xdevDir, shimDir, "direct")
	if err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}
	if !changed {
		t.Error("expected changed=true, got false")
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath after cleanup: %v", err)
	}

	// current entries should be preserved in direct mode
	normalized := strings.ToLower(result)
	if !strings.Contains(normalized, "\\xdev\\current\\java") {
		t.Errorf("current entry should be preserved in direct mode, got PATH=%q", result)
	}
	// shim dir should be removed in direct mode
	if strings.Contains(normalized, strings.ToLower(filepath.Clean(shimDir))) {
		t.Errorf("shim dir should be removed in direct mode, got PATH=%q", result)
	}
}

func TestCleanupUserPath_RemoveXDevRoot(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	xdevDir := "E:\\XDev"
	shimDir := xdevDir + "\\shims"
	// Bare XDev root directory should be removed
	testPath := strings.Join([]string{
		xdevDir,
		"C:\\Windows\\System32",
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath(xdevDir, shimDir, "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}
	if !changed {
		t.Error("expected changed=true, got false")
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath after cleanup: %v", err)
	}

	// Bare XDev root should be removed
	normalized := strings.ToLower(result)
	for _, p := range strings.Split(normalized, ";") {
		cleaned := strings.ToLower(filepath.Clean(p))
		if isXDevRoot(cleaned) {
			t.Errorf("bare XDev root should be removed, but found %q in PATH=%q", p, result)
		}
	}
}

func TestCleanupUserPath_KeepCurrentXdev(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	xdevDir := "E:\\XDev"
	shimDir := xdevDir + "\\shims"
	// current\xdev entry should be preserved even in shim mode
	testPath := strings.Join([]string{
		"C:\\Windows\\System32",
		xdevDir + "\\current\\xdev",
		shimDir,
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	_, err := CleanupUserPath(xdevDir, shimDir, "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath after cleanup: %v", err)
	}

	normalized := strings.ToLower(result)
	if !strings.Contains(normalized, "\\xdev\\current\\xdev") {
		t.Errorf("current\\xdev entry should be preserved in shim mode, got PATH=%q", result)
	}
}

// ---- Ticket 11 (audit-fixes / P0-13) home anchoring guards ----

// H1: an unrelated user directory that merely LOOKS like an xdev root
// (D:\xdev — the user's own project dir) must never be deleted. The old
// drive-agnostic isXDevRoot rule removed it on every install/doctor run.
func TestCleanupUserPath_KeepsUnrelatedXdevDir(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	home := "E:\\XDev"
	shimDir := home + "\\shims"
	testPath := strings.Join([]string{
		"D:\\xdev", // unrelated user directory on another drive
		home + "\\install\\go\\1.22",
		"C:\\Windows\\System32",
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath(home, shimDir, "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}
	if !changed {
		t.Error("expected changed=true (home install entry removed)")
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "d:\\xdev") {
		t.Errorf("unrelated D:\\xdev directory was deleted — home anchoring regression: PATH=%q", result)
	}
	if strings.Contains(strings.ToLower(result), "\\xdev\\install\\") {
		t.Errorf("home install entry should be removed, PATH=%q", result)
	}
}

// H2: xdev-owned entries under the actual home are still removed.
func TestCleanupUserPath_RemovesUnderHome(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	home := "E:\\XDev"
	shimDir := home + "\\shims"
	testPath := strings.Join([]string{
		home + "\\install\\go\\1.22",
		home + "\\current\\node",
		"C:\\Windows\\System32",
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	if _, err := CleanupUserPath(home, shimDir, "shim"); err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	normalized := strings.ToLower(result)
	if strings.Contains(normalized, "install\\go") {
		t.Errorf("home install entry not removed: PATH=%q", result)
	}
	if strings.Contains(normalized, "current\\node") {
		t.Errorf("home current entry not removed (shim mode): PATH=%q", result)
	}
}

// H3: the historical legacy substrings keep their cleanup semantics.
func TestCleanupUserPath_KeepsHistoricalSubstrings(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	home := "E:\\XDev"
	shimDir := home + "\\shims"
	testPath := strings.Join([]string{
		"C:\\work\\ReverseEng\\xmdevenvmt", // historical legacy entry
		"D:\\tools",                        // unrelated dir, no substring
		"C:\\Windows\\System32",
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	if _, err := CleanupUserPath(home, shimDir, "shim"); err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	normalized := strings.ToLower(result)
	if strings.Contains(normalized, "\\reverseeng\\xmdevenvmt") {
		t.Errorf("historical legacy entry should still be removed: PATH=%q", result)
	}
	if !strings.Contains(normalized, "d:\\tools") {
		t.Errorf("unrelated D:\\tools must be kept: PATH=%q", result)
	}
}

// H4: home prefix matching is case-insensitive (Windows semantics).
func TestCleanupUserPath_CaseInsensitiveHomePrefix(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	home := "E:\\XDev"
	shimDir := home + "\\shims"
	testPath := strings.Join([]string{
		"E:\\XDEV\\INSTALL\\JAVA\\25", // uppercase variant of home install
		"C:\\Windows\\System32",
	}, ";")
	if err := WriteUserPath(testPath); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	if _, err := CleanupUserPath(home, shimDir, "shim"); err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	if strings.Contains(strings.ToLower(result), "install\\java") {
		t.Errorf("case-variant home install entry not removed: PATH=%q", result)
	}
}
