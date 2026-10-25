package localmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/hostscan"
	"github.com/xmdemt/xdev/internal/toolmeta"
)

// --- extractVersionFromOutput tests（Spec 033 R3：收口到 toolmeta） ---

func TestExtractVersionFromOutput_Node(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("v20.18.0")
	if got != "20.18.0" {
		t.Errorf("got %q, want %q", got, "20.18.0")
	}
}

func TestExtractVersionFromOutput_Python(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("Python 3.12.7")
	if got != "3.12.7" {
		t.Errorf("got %q, want %q", got, "3.12.7")
	}
}

func TestExtractVersionFromOutput_Yarn(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("1.22.22")
	if got != "1.22.22" {
		t.Errorf("got %q, want %q", got, "1.22.22")
	}
}

func TestExtractVersionFromOutput_Pnpm(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("9.1.2")
	if got != "9.1.2" {
		t.Errorf("got %q, want %q", got, "9.1.2")
	}
}

func TestExtractVersionFromOutput_Gradle(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("Gradle 9.4.1")
	if got != "9.4.1" {
		t.Errorf("got %q, want %q", got, "9.4.1")
	}
}

func TestExtractVersionFromOutput_Java(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("java 25.0.3")
	if got != "25.0.3" {
		t.Errorf("got %q, want %q", got, "25.0.3")
	}
}

func TestExtractVersionFromOutput_Empty(t *testing.T) {
	got := toolmeta.ExtractVersionFromOutput("")
	if got != "" {
		t.Errorf("got %q, want %q", got, "")
	}
}

// --- isInsideXdevDir tests（Spec 033 R5：委托 hostscan.InsideDir） ---

func TestIsInsideXdevDir_True(t *testing.T) {
	xdevDir := filepath.Join("C:", "Users", "dev", ".xmdev")
	path := filepath.Join("C:", "Users", "dev", ".xmdev", "install", "node", "v20")
	if !hostscan.InsideDir(path, xdevDir) {
		t.Errorf("hostscan.InsideDir(%q, %q) = false, want true", path, xdevDir)
	}
}

func TestIsInsideXdevDir_False(t *testing.T) {
	xdevDir := filepath.Join("C:", "Users", "dev", ".xmdev")
	path := filepath.Join("C:", "Program Files", "nodejs")
	if hostscan.InsideDir(path, xdevDir) {
		t.Errorf("hostscan.InsideDir(%q, %q) = true, want false", path, xdevDir)
	}
}

// --- isVersionLike tests（Spec 033 R3：收口到 toolmeta） ---

func TestIsVersionLike_Valid(t *testing.T) {
	cases := []struct {
		input string
	}{
		{"1.22.0"},
		{"20.18.0"},
		{"3.12.7"},
		{"9.4.1"},
	}
	for _, tc := range cases {
		if !toolmeta.IsVersionLike(tc.input) {
			t.Errorf("toolmeta.IsVersionLike(%q) = false, want true", tc.input)
		}
	}
}

func TestIsVersionLike_Invalid(t *testing.T) {
	cases := []struct {
		input string
	}{
		{"abc"},
		{"1"},
		{""},
	}
	for _, tc := range cases {
		if toolmeta.IsVersionLike(tc.input) {
			t.Errorf("toolmeta.IsVersionLike(%q) = true, want false", tc.input)
		}
	}
}

// --- liftCmdToInstallRoot tests ---

// makeFakeGitRoot 在临时目录中构造 Git for Windows / PortableGit 布局：
// root/cmd/git.exe（launcher）+ root/mingw64 + root/usr + root/bin。
func makeFakeGitRoot(t *testing.T, withRuntime bool) string {
	t.Helper()
	root := t.TempDir()
	cmdDir := filepath.Join(root, "cmd")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Create(filepath.Join(cmdDir, "git.exe")); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	if withRuntime {
		for _, d := range []string{"mingw64", "usr", "bin"} {
			if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestLiftCmdToInstallRoot_LiftsGitCmdDir(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	got := liftCmdToInstallRoot(filepath.Join(root, "cmd"))
	if got != root {
		t.Errorf("liftCmdToInstallRoot(%q) = %q, want %q", filepath.Join(root, "cmd"), got, root)
	}
}

func TestLiftCmdToInstallRoot_NoRuntime_KeepsDir(t *testing.T) {
	root := makeFakeGitRoot(t, false)
	cmdDir := filepath.Join(root, "cmd")
	if got := liftCmdToInstallRoot(cmdDir); got != cmdDir {
		t.Errorf("liftCmdToInstallRoot(%q) = %q, want %q (no mingw64 runtime)", cmdDir, got, cmdDir)
	}
}

func TestLiftCmdToInstallRoot_NonCmdDir_Unchanged(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	binDir := filepath.Join(root, "bin")
	if got := liftCmdToInstallRoot(binDir); got != binDir {
		t.Errorf("liftCmdToInstallRoot(%q) = %q, want %q", binDir, got, binDir)
	}
}

func TestLiftCmdToInstallRoot_CaseInsensitive(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	got := liftCmdToInstallRoot(filepath.Join(root, "CMD"))
	if got != root {
		t.Errorf("liftCmdToInstallRoot(CMD) = %q, want %q", got, root)
	}
}

// --- resolveInstallPathFromBin tests ---

func TestResolveInstallPathFromBin_LiftsGitCmdDir(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	rule := toolScanRule{name: "git", binNames: []string{"git"}}
	got := resolveInstallPathFromBin(filepath.Join(root, "cmd", "git.exe"), rule)
	if got != root {
		t.Errorf("resolveInstallPathFromBin(%q) = %q, want %q (git install root)", filepath.Join(root, "cmd", "git.exe"), got, root)
	}
}

func TestResolveInstallPathFromBin_CmdWithoutRuntime_KeepsDir(t *testing.T) {
	root := makeFakeGitRoot(t, false)
	rule := toolScanRule{name: "git", binNames: []string{"git"}}
	binPath := filepath.Join(root, "cmd", "git.exe")
	if got := resolveInstallPathFromBin(binPath, rule); got != filepath.Dir(binPath) {
		t.Errorf("resolveInstallPathFromBin(%q) = %q, want %q", binPath, got, filepath.Dir(binPath))
	}
}

func TestResolveInstallPathFromBin_NodeRoot_Unchanged(t *testing.T) {
	root := t.TempDir()
	rule := toolScanRule{name: "node", binNames: []string{"node"}}
	binPath := filepath.Join(root, "node.exe")
	if got := resolveInstallPathFromBin(binPath, rule); got != root {
		t.Errorf("resolveInstallPathFromBin(%q) = %q, want %q", binPath, got, root)
	}
}

func TestResolveInstallPathFromBin_JavaBinDir(t *testing.T) {
	root := t.TempDir()
	rule := toolScanRule{name: "java", binNames: []string{"java", "javac"}}
	binPath := filepath.Join(root, "bin", "java.exe")
	if got := resolveInstallPathFromBin(binPath, rule); got != root {
		t.Errorf("resolveInstallPathFromBin(%q) = %q, want %q", binPath, got, root)
	}
}

// --- resolveInstallPath tests ---

func TestResolveInstallPath_EnvVarPointsAtCmdLauncher(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	rule := toolScanRule{name: "git", binNames: []string{"git"}}
	got := resolveInstallPath(filepath.Join(root, "cmd", "git.exe"), rule)
	if got != root {
		t.Errorf("resolveInstallPath(%q) = %q, want %q", filepath.Join(root, "cmd", "git.exe"), got, root)
	}
}

// --- hasGitRuntime tests ---

func TestHasGitRuntime_True(t *testing.T) {
	root := makeFakeGitRoot(t, true)
	if !hasGitRuntime(root) {
		t.Errorf("hasGitRuntime(%q) = false, want true", root)
	}
}

func TestHasGitRuntime_False_LauncherOnly(t *testing.T) {
	root := makeFakeGitRoot(t, false)
	if hasGitRuntime(root) {
		t.Errorf("hasGitRuntime(%q) = true, want false (launcher-only dir)", root)
	}
}

func TestHasGitRuntime_False_EmptyDir(t *testing.T) {
	root := t.TempDir()
	if hasGitRuntime(root) {
		t.Errorf("hasGitRuntime(%q) = true, want false", root)
	}
}

// --- MigrateLocalTool guard tests ---

func TestMigrateLocalTool_RefusesGitLauncherOnlyDir(t *testing.T) {
	// cmd/ 中只有 launcher（git.exe 等），没有 mingw64/ 运行时。
	root := makeFakeGitRoot(t, false)
	xdevDir := t.TempDir()
	tool := LocalTool{Name: "git", Path: filepath.Join(root, "cmd"), Version: "2.55.0"}
	err := MigrateLocalTool(tool, xdevDir)
	if err == nil {
		t.Fatal("MigrateLocalTool accepted a git launcher-only dir, want error")
	}
	if !strings.Contains(err.Error(), "launcher-only") {
		t.Errorf("unexpected error: %v", err)
	}
	// 不应产生任何目录
	if fi, err := os.Stat(filepath.Join(xdevDir, "install", "git")); err == nil || fi != nil {
		t.Errorf("MigrateLocalTool created target dir for rejected tool")
	}
}

func TestMigrateLocalTool_RefusesNonGitToolNormalDir(t *testing.T) {
	// 非 git 工具不受 guard 影响（正常走到路径校验）。
	root := t.TempDir()
	xdevDir := t.TempDir()
	tool := LocalTool{Name: "node", Path: filepath.Join(root, "node.exe"), Version: "20.0.0"}
	err := MigrateLocalTool(tool, xdevDir)
	if err == nil {
		t.Fatal("expected error (source is a file, not a directory)")
	}
	if strings.Contains(err.Error(), "launcher-only") {
		t.Errorf("git guard wrongly applied to node: %v", err)
	}
}
