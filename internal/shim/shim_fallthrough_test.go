package shim

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// makeFakeBin 在 dir 下创建一个可执行的假二进制。
func makeFakeBin(t *testing.T, dir, name, marker string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	n := name
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	p := filepath.Join(dir, n)
	if err := os.WriteFile(p, []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// excludedFrom 是测试构造排除集的助手。
func excludedFrom(dirs ...string) map[string]bool {
	out := map[string]bool{}
	for _, d := range dirs {
		addExcludedDir(out, d)
	}
	return out
}

// TestLookupOnPath_FindsRealTool (Spec 038 核心行为)：shim 目录遮蔽
// PATH 时，透传解析跳过 shims 目录命中真实工具。
// 注意：必须 t.Setenv PATH——exec.LookPath 兜底用的是进程真实 PATH，
// 否则测试机的孤儿 shim（如 E:\XDev\shims\git.exe）会污染断言。
func TestLookupOnPath_FindsRealTool(t *testing.T) {
	shimDir := t.TempDir()
	realDir := t.TempDir()
	makeFakeBin(t, shimDir, "git", "SHIM")
	realPath := makeFakeBin(t, realDir, "git", "REAL")

	pathValue := shimDir + string(os.PathListSeparator) + realDir
	t.Setenv("PATH", pathValue)
	got, err := lookupOnPath("git", pathValue, excludedFrom(shimDir))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got == "" {
		t.Fatal("expected a real tool hit outside the shims dir")
	}
	if !sameFilePath(got, realPath) {
		t.Errorf("resolved %q, want the REAL git at %q (not the shim)", got, realPath)
	}
}

// TestLookupOnPath_SkipsXdevHome：current/ 与 home 根也必须排除，
// 防止透传循环回 xdev 自身。
func TestLookupOnPath_SkipsXdevHome(t *testing.T) {
	home := t.TempDir()
	makeFakeBin(t, home, "fakealias", "HOME-ROOT")
	makeFakeBin(t, filepath.Join(home, "current"), "fakealias", "CURRENT")

	pathValue := home + string(os.PathListSeparator) + filepath.Join(home, "current")
	got, err := lookupOnPath("fakealias", pathValue, excludedFrom(home, filepath.Join(home, "current")))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got != "" {
		t.Errorf("expected xdev-managed dirs to be excluded, got %q", got)
	}
}

// TestLookupOnPath_MissingEverywhere：所有位置都找不到 → 空结果 + 无错误
// （调用方保留原始 Lookup 错误）。
func TestLookupOnPath_MissingEverywhere(t *testing.T) {
	empty := t.TempDir()
	got, err := lookupOnPath("nonexistent-tool-xyz", empty, excludedFrom(empty))
	if err != nil {
		t.Fatalf("lookupOnPath should not error on miss: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

// TestLookupOnPath_RejectsPathSeparators：含路径分隔符的别名不做 PATH
// 查找（exec.LookPath 会把含分隔符的输入当相对路径，语义不同）。
func TestLookupOnPath_RejectsPathSeparators(t *testing.T) {
	dir := t.TempDir()
	makeFakeBin(t, dir, "evil", "X")
	sep := "/"
	if runtime.GOOS == "windows" {
		sep = "\\"
	}
	got, err := lookupOnPath("sub"+sep+"evil", dir, excludedFrom(dir))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got != "" {
		t.Errorf("aliases with path separators must not resolve, got %q", got)
	}
}

// TestDefaultExcludedDirs_CoversShims：生产排除集必须包含
// XMDEV_HOME/shims、current 与 home 根（Spec 038）。
func TestDefaultExcludedDirs_CoversShims(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest() // 测试隔离：shim_alias_case_test.go 等按字母序先跑、消费 sync.Once 后不重置
	excluded := defaultExcludedDirs()
	for _, d := range []string{
		home,
		filepath.Join(home, "current"),
		filepath.Join(home, "shims"),
	} {
		if !excluded[strings.ToLower(filepath.Clean(d))] {
			t.Errorf("excluded set must contain %q", d)
		}
	}
}

// TestLookupOnPath_EmptyPathIsEmpty：PATH 为空时透传返回空（不会从
// 进程真实 PATH 或当前目录意外命中）。
func TestLookupOnPath_EmptyPathIsEmpty(t *testing.T) {
	// PATH 为空 → 手动扫描必空 → LookPath 兜底用进程真实 PATH。
	// 无法保证 LookPath 找到什么，但可以保证：若它在 excluded 目录
	// 里命中，函数返回空。
	shimDir := t.TempDir()
	makeFakeBin(t, shimDir, "definitely-not-on-real-path-xyz", "SHIM")
	got, err := lookupOnPath("definitely-not-on-real-path-xyz", "", excludedFrom(shimDir))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got != "" {
		t.Errorf("LookPath fallback must respect exclusions, got %q", got)
	}
}

// linkDirAlias 创建指向 target 目录的别名 link：Windows 用目录 junction
// （mklink /J：不需要开发者模式/管理员权限），其他平台用符号链接。返回
// false 表示本机不支持该别名形式（调用方 skip）。
func linkDirAlias(t *testing.T, link, target string) bool {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Logf("mklink /J unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
			return false
		}
		return true
	}
	if err := os.Symlink(target, link); err != nil {
		t.Logf("os.Symlink unavailable: %v", err)
		return false
	}
	return true
}

// fileDirIsSameDir 判断 file 所在目录与 dir 是否同一目录：用文件标识
// （os.SameFile）而非字面路径比对，因此 junction / 符号链接 / 8.3 短名
// 形式都能识别。「分辨别目录是否等于 shim 目录」正是防自我 exec 递归的
// 判据（C-09 / 守卫 35-1）。
func fileDirIsSameDir(file, dir string) bool {
	parent, err := os.Stat(filepath.Dir(file))
	if err != nil {
		return false
	}
	want, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return os.SameFile(parent, want)
}

// TestLookupOnPath_SkipsAliasedShimDir（C-09 / P1-33，守卫 35-1）：
// PATH 条目可以是 shim 目录的 junction / 符号链接别名（用户把 shims 目录
// junction 到别处，或 PATH 里写的本来就是别名路径）。只比对字面路径时该
// 条目会绕过排除集 → 透传命中别名目录里的 shim 副本 → shim 再 exec 自己
// → 无限递归。归一化（Abs+EvalSymlinks）+ 文件标识兜底后必须跳过它，并
// 继续命中后面真正的工具目录。
//
// 实测（Go 1.26.5 / Windows）：filepath.EvalSymlinks **不解析**目录
// junction（返回 junction 路径本身），所以仅靠归一化不足；同一目录的
// 识别必须叠加 os.SameFile 兜底。
func TestLookupOnPath_SkipsAliasedShimDir(t *testing.T) {
	base := t.TempDir()
	shimDir := filepath.Join(base, "shims")
	realDir := filepath.Join(base, "realbin")
	aliasDir := filepath.Join(base, "shims-alias")
	makeFakeBin(t, shimDir, "git", "SHIM")
	realPath := makeFakeBin(t, realDir, "git", "REAL")
	if !linkDirAlias(t, aliasDir, shimDir) {
		t.Skip("directory junction/symlink unavailable on this host")
	}

	// 场景 A：PATH 里是别名，排除集里是真实 shim 目录。
	pathValue := aliasDir + string(os.PathListSeparator) + realDir
	got, err := lookupOnPath("git", pathValue, excludedFrom(shimDir))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got == "" {
		t.Fatal("expected the real tool behind the aliased shims dir, got a miss")
	}
	if fileDirIsSameDir(got, shimDir) {
		t.Errorf("resolved %q inside the shims dir (via %q) — the shim would exec itself recursively", got, aliasDir)
	}
	if !sameFilePath(got, realPath) {
		t.Errorf("resolved %q, want the REAL git at %q", got, realPath)
	}

	// 场景 B：反向——排除集由别名构建（进程自身经 junction 到达时就是
	// 这种形态），PATH 里是真实 shim 目录：同样不得命中。
	if gotB, _ := lookupOnPath("git", shimDir, excludedFrom(aliasDir)); gotB != "" {
		t.Errorf("PATH entry %q must be excluded when the excluded dir is its alias, got %q", shimDir, gotB)
	}

	// 场景 C：PATH 只有别名、后面没有真实工具 → 必须返回未命中：宁可让
	// 调用方报 "not found / run xdev install"，也绝不自我 exec。
	if gotC, _ := lookupOnPath("git", aliasDir, excludedFrom(shimDir)); gotC != "" {
		t.Errorf("aliased shims dir must never resolve, got %q", gotC)
	}
}

// TestLookupOnPath_SkipsRelativeBareEntry（C-09，守卫 35-1）：
// PATH 条目是裸名/相对路径（"shims"、"./shims"、"realbin/../shims"）时，
// 归一化（filepath.Abs）后同样落在排除目录上——修复前只比对字面值，
// 这类条目会解析到 shim 目录并自我 exec。字面形态（含尾分隔符）保留的
// 「末位兜底」比对也必须继续生效，同时真实工具目录仍要能透传命中。
func TestLookupOnPath_SkipsRelativeBareEntry(t *testing.T) {
	home := t.TempDir()
	shimDir := filepath.Join(home, "shims")
	realDir := filepath.Join(home, "realbin")
	makeFakeBin(t, shimDir, "git", "SHIM")
	realPath := makeFakeBin(t, realDir, "git", "REAL")
	t.Chdir(home)

	sep := string(os.PathSeparator)
	for _, entry := range []string{
		"shims",                                // 裸名（无分隔符）
		"." + sep + "shims",                    // 显式相对路径
		"realbin" + sep + ".." + sep + "shims", // 相对路径含 ..
		shimDir + sep,                          // 字面绝对路径 + 尾分隔符（末位兜底）
	} {
		got, err := lookupOnPath("git", entry, excludedFrom(shimDir))
		if err != nil {
			t.Fatalf("lookupOnPath(%q): %v", entry, err)
		}
		if got != "" {
			t.Errorf("PATH entry %q resolved into the excluded shims dir (%q) — self-exec recursion risk", entry, got)
		}
	}

	// 真实工具目录（相对裸名形式）仍必须透传命中——排除逻辑不能误伤。
	got, err := lookupOnPath("git", "realbin", excludedFrom(shimDir))
	if err != nil {
		t.Fatalf("lookupOnPath(realbin): %v", err)
	}
	if got == "" || !sameFilePath(got, realPath) {
		t.Errorf("relative real tool dir no longer resolves: got %q, want %q", got, realPath)
	}
}

// sameFilePath 比较两个路径是否指向同一文件（大小写不敏感）。
func sameFilePath(a, b string) bool {
	ra, err1 := filepath.Abs(a)
	if err1 != nil {
		return false
	}
	rb, err2 := filepath.Abs(b)
	if err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strEqFold(ra, rb)
	}
	return ra == rb
}

func strEqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
