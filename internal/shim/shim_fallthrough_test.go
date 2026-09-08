package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
