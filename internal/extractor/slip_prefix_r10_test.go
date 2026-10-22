package extractor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第十轮审计 P3-5（P3，本簇最严重项）守卫。
//
// 缺陷现场：isSafePath 用 `strings.HasPrefix(rel, "..")` 判定 zip-slip。
// 该判据把**合法**的、以两个点开头的名字（`..foo.txt`、`..cache/x.txt`）
// 也当成逃逸，于是 extractZip/extractTarFromReader 走 `_ = os.RemoveAll(dst)`
// 分支 —— 一个合法条目就会让**整个安装目标目录被删除**（数据破坏）。
//
// 审计实测：条目 `..foo.txt`、`..cache/x.txt` →
//   err=zip slip ... dst_entries=[] dst_exists=false
//
// 正确判据是「rel 恰好是 .. 或以 `..<分隔符>` 开头」。
//
// 本文件同时钉住**双向**语义（修正前缀判断时绝不能把安全门也放宽）：
//   - 合法的 `..` 前缀名必须被正常解压；
//   - 真实的 `../` 逃逸必须仍被拒绝，且绝不落到 dst 之外。

// TestIsSafePath_DotDotPrefixedNamesAreLegal 是缺陷的最小复现（纯谓词层）：
// dst 内的 `..foo.txt` / `..cache/x.txt` / `...x` 都必须在目标目录之内。
func TestIsSafePath_DotDotPrefixedNamesAreLegal(t *testing.T) {
	dst := t.TempDir()
	for _, name := range []string{"..foo.txt", "..cache/x.txt", "..foo", "...x", "..a/..b"} {
		out := filepath.Join(dst, name)
		if !isSafePath(dst, out) {
			t.Errorf("isSafePath(%q) = false：合法条目名被误判为 zip-slip —— "+
				"extractZip 会据此 RemoveAll 掉整个目标目录（P3-5）", name)
		}
	}
}

// TestIsSafePath_RealEscapesStillRejected 是反向断言：真实的逃逸（含 rel
// 恰好为 `..` 的边界）必须继续被拒绝。修正前缀判断时若写成
// `strings.HasPrefix(rel, "..") == false` 之类的反向错误，本用例即失败。
func TestIsSafePath_RealEscapesStillRejected(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"父目录本身（rel == \"..\"）": filepath.Join(dst, ".."),
		"父目录下的兄弟":              filepath.Join(dst, "..", "sibling"),
		"上两级":                  filepath.Join(dst, "..", "..", "elsewhere"),
	}
	for label, out := range cases {
		if isSafePath(dst, out) {
			t.Errorf("isSafePath(%q) = true（%s）：真实逃逸被放行 —— 安全门被放宽了", out, label)
		}
	}
}

// TestExtractZip_LegalDotDotPrefixedEntriesAreExtracted 是端到端复现：
// 修复前 Extract 返回 "zip slip" 且 dst 整个被 RemoveAll（dst_exists=false）。
func TestExtractZip_LegalDotDotPrefixedEntriesAreExtracted(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "dotdot.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"..foo.txt|foo",
		"..cache/x.txt|bar",
		"bin/hello.txt|hi",
	})

	err := Extract(zipPath, dstDir, "zip", 0)
	if err != nil {
		// 修复前正是这条路径：一个合法条目让整个 dst 被 RemoveAll。
		t.Fatalf("合法归档被拒绝（err=%v）；dst 存在=%v", err, dirExists(dstDir))
	}
	// 目标目录必须仍然存在 —— 缺陷的破坏面正是「整个目标目录被删掉」。
	if !dirExists(dstDir) {
		t.Fatalf("P3-5 复现：合法条目让整个目标目录 %s 被 RemoveAll 删除", dstDir)
	}
	for name, want := range map[string]string{
		"..foo.txt":     "foo",
		"..cache/x.txt": "bar",
		"bin/hello.txt": "hi",
	} {
		got, rerr := os.ReadFile(filepath.Join(dstDir, name))
		if rerr != nil {
			t.Errorf("合法条目 %q 未被解压：%v", name, rerr)
			continue
		}
		if string(got) != want {
			t.Errorf("条目 %q 内容 = %q, want %q", name, got, want)
		}
	}
	// 合法条目不得逃逸出 dst。
	if _, serr := os.Stat(filepath.Join(tmpDir, "..foo.txt")); serr == nil {
		t.Errorf("条目 %q 被写到了 dst 之外（%s）", "..foo.txt", tmpDir)
	}
}

// TestExtractTarGz_LegalDotDotPrefixedEntriesAreExtracted 覆盖 tar 侧同一
// 判据（isSafePath 是两条写入路径共享的安全门）。
func TestExtractTarGz_LegalDotDotPrefixedEntriesAreExtracted(t *testing.T) {
	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "dotdot.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tarPath, []string{
		"..foo.txt|foo",
		"..cache/x.txt|bar",
	})

	if err := Extract(tarPath, dstDir, "tar.gz", 0); err != nil {
		t.Fatalf("tar 侧合法归档被拒绝（err=%v）；dst 存在=%v", err, dirExists(dstDir))
	}
	if !dirExists(dstDir) {
		t.Fatalf("P3-5 复现（tar）：合法条目让整个目标目录 %s 被 RemoveAll 删除", dstDir)
	}
	for name, want := range map[string]string{"..foo.txt": "foo", "..cache/x.txt": "bar"} {
		got, rerr := os.ReadFile(filepath.Join(dstDir, name))
		if rerr != nil {
			t.Errorf("合法条目 %q 未被解压：%v", name, rerr)
			continue
		}
		if string(got) != want {
			t.Errorf("条目 %q 内容 = %q, want %q", name, got, want)
		}
	}
}

// TestExtractZip_RealZipSlipStillRejected 是 P3-5 的安全侧双向断言：
// 真实的 `../escape.txt` 必须仍然被拒绝，且文件绝不落到 dst 之外。
func TestExtractZip_RealZipSlipStillRejected(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "evil.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createMaliciousZip(t, zipPath, "../escape.txt")

	err := Extract(zipPath, dstDir, "zip", 0)
	if err == nil {
		t.Fatal("真实 zip-slip 条目未被拒绝（安全门被放宽）")
	}
	if !strings.Contains(err.Error(), "zip slip") {
		t.Errorf("错误信息未说明 zip slip 性质：%v", err)
	}
	escaped := filepath.Join(tmpDir, "escape.txt")
	if _, serr := os.Stat(escaped); serr == nil {
		t.Fatalf("zip-slip 成功：%s 被写出 dst 之外", escaped)
	}
}

// TestExtractTarGz_RealTarSlipStillRejected 是 tar 侧的反向断言。
func TestExtractTarGz_RealTarSlipStillRejected(t *testing.T) {
	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "evil.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tarPath, []string{"../escape.txt|pwned"})

	err := Extract(tarPath, dstDir, "tar.gz", 0)
	if err == nil {
		t.Fatal("真实 tar-slip 条目未被拒绝（安全门被放宽）")
	}
	if !strings.Contains(err.Error(), "tar slip") {
		t.Errorf("错误信息未说明 tar slip 性质：%v", err)
	}
	if _, serr := os.Stat(filepath.Join(tmpDir, "escape.txt")); serr == nil {
		t.Fatalf("tar-slip 成功：文件被写出 dst 之外")
	}
}

// dirExists 报告路径是否存在且为目录（缺陷断言「整个目标目录被删掉」需要它）。
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
