package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 第九轮审计 P3-10 守卫：空的 format 必须被拒绝，不得产出**尾随点**文件名。
//
// 缺陷现场：旧实现容忍 `format == ""`，产出 `"go-1.0."`。Windows 会静默剥离
// 尾随点/空格，于是
//
//	cacheArchiveName("go", "1.0",     "zip") -> "go-1.0.zip"
//	cacheArchiveName("go", "1.0.zip", "")    -> "go-1.0.zip."  → 同一个文件
//
// 两个语义完全不同的条目**折叠成同一个缓存文件**：后写者覆盖先写者，其 sidecar
// pin 在下次安装时与内容不匹配（缓存串味）。这不是遍历问题（H11 已确认 `..`、
// `NUL`、`a/b`、`zip.`、`CON` 等都被正确拒绝），而是命名碰撞。
func TestCacheArchiveName_EmptyFormatRejected(t *testing.T) {
	for _, tc := range []struct {
		name          string
		tool, ver, fm string
	}{
		{"plain empty", "go", "1.0", ""},
		{"version already carries extension", "go", "1.0.zip", ""},
		{"whitespace only", "go", "1.0", "   "},
		{"tab only", "go", "1.0", "\t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cacheArchiveName(tc.tool, tc.ver, tc.fm)
			if err == nil {
				t.Errorf("P3-10 复现：cacheArchiveName(%q,%q,%q) = %q, err=nil —— "+
					"尾随点名在 Windows 上与其它条目碰撞", tc.tool, tc.ver, tc.fm, got)
			}
			if got != "" {
				t.Errorf("被拒绝时必须返回空名字，got %q", got)
			}
		})
	}
}

// 守卫：正常 format 仍照常工作（修复不得把正常路径弄红）。
func TestCacheArchiveName_NonEmptyFormatStillWorks(t *testing.T) {
	got, err := cacheArchiveName("go", "1.0", "zip")
	if err != nil {
		t.Fatalf("cacheArchiveName(go,1.0,zip) 意外失败: %v", err)
	}
	if got != "go-1.0.zip" {
		t.Errorf("got %q, want go-1.0.zip", got)
	}
}

// 守卫：**碰撞本身**——修复后两个语义不同的输入不再指向同一路径。
//
// 这是本缺陷的核心断言：即便只比较「返回的名字」，也必须能区分
// (ver="1.0.zip", format="zip") 与 (ver="1.0", format="zip")；
// 而旧实现下 (ver="1.0.zip", format="") 会与 (ver="1.0", format="zip") 同名。
func TestCacheArchiveName_NoCollisionBetweenSemanticVariants(t *testing.T) {
	a, errA := cacheArchiveName("go", "1.0", "zip")
	b, errB := cacheArchiveName("go", "1.0.zip", "zip")
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v / %v", errA, errB)
	}
	if a == b {
		t.Errorf("不同语义的条目产生同一缓存名 %q —— 缓存串味", a)
	}

	// 旧实现下会碰撞的那一对：现在空 format 直接报错，结构上不可能碰撞。
	if got, err := cacheArchiveName("go", "1.0.zip", ""); err == nil {
		t.Errorf("空 format 未被拒绝，得到 %q（旧实现下它与 %q 同名）", got, a)
	}
}

// 守卫（Windows 实机）：写两个「旧实现下会折叠」的名字，确认修复后不再折叠。
//
// 由于空 format 现在被拒绝，这里直接构造旧实现会产出的那个名字来验证
// **Windows 的归一化行为确实存在**（证明缺陷前提成立，而不是我在推测）。
func TestWindowsTrailingDotCollisionPremise(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("trailing-dot normalisation is Windows-specific")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "go-1.0.zip")
	trailing := filepath.Join(dir, "go-1.0.zip.")

	if err := os.WriteFile(plain, []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trailing, []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Skipf("this filesystem does not normalise trailing dots (%d entries) — premise not applicable here", len(entries))
	}
	// 前提成立：两个名字确实指向同一个文件 —— 这正是 P3-10 的危害来源。
	got, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "B" {
		t.Errorf("premise check: expected the trailing-dot write to have overwritten the plain one, got %q", string(got))
	}
	t.Logf("已确认前提：%q 与 %q 在 Windows 上折叠为同一文件（目录内 %d 个条目）",
		filepath.Base(plain), filepath.Base(trailing), len(entries))
}

// 守卫：拒绝错误信息必须可操作（含 P3-10 标记，便于排查）。
func TestCacheArchiveName_EmptyFormatErrorIsActionable(t *testing.T) {
	_, err := cacheArchiveName("go", "1.0", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "P3-10") {
		t.Errorf("错误信息应含 P3-10 标记以便排查，got %q", msg)
	}
	if !strings.Contains(msg, "format") {
		t.Errorf("错误信息应说明是 format 问题，got %q", msg)
	}
}
