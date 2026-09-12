package update

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// ---------------------------------------------------------------------------
// 票 32（P1-25 / E-06、P1-26 / E-07）：自更新自愈组
//
// 全部用例在 t.TempDir() 内用假二进制构造，绝不触碰真实仓库 build/bin
// 或运行中的 xdev 二进制。
// ---------------------------------------------------------------------------

// TC-32-1a restoreFromBackup：活二进制缺失 + .old 完好 → 自愈恢复。
func TestRestoreFromBackup_HealsMissingExe(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "xdev.exe")
	backup := current + ".old"
	writeFile(t, backup, "OLD-BINARY")

	healed, err := restoreFromBackup(current)
	if err != nil {
		t.Fatalf("restoreFromBackup: %v", err)
	}
	if !healed {
		t.Fatal("healed = false, want true（currentExe 缺失 + .old 完好）")
	}
	if got := readFile(t, current); got != "OLD-BINARY" {
		t.Errorf("currentExe content = %q, want OLD-BINARY（崩溃恢复）", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf(".old 应已被重命名回 currentExe，stat err=%v", err)
	}
}

// TC-32-1b restoreFromBackup 的非触发分支：活二进制在位 / 无备份 / 备份是目录。
func TestRestoreFromBackup_NonTriggeringCases(t *testing.T) {
	// 活二进制在位：不动，且不消耗备份。
	dir := t.TempDir()
	current := filepath.Join(dir, "xdev.exe")
	writeFile(t, current, "LIVE")
	writeFile(t, current+".old", "OLD")
	if healed, err := restoreFromBackup(current); err != nil || healed {
		t.Errorf("live exe: healed=%v err=%v, want false/nil", healed, err)
	}
	if got := readFile(t, current); got != "LIVE" {
		t.Errorf("live exe overwritten: %q", got)
	}

	// 两者都缺失：无操作、无错误。
	missing := filepath.Join(t.TempDir(), "xdev.exe")
	if healed, err := restoreFromBackup(missing); err != nil || healed {
		t.Errorf("both missing: healed=%v err=%v, want false/nil", healed, err)
	}

	// .old 是目录：绝不删除/重命名。
	dirCase := t.TempDir()
	cur := filepath.Join(dirCase, "xdev.exe")
	if err := os.MkdirAll(cur+".old", 0o755); err != nil {
		t.Fatal(err)
	}
	if healed, err := restoreFromBackup(cur); err != nil || healed {
		t.Errorf("dir backup: healed=%v err=%v, want false/nil", healed, err)
	}
	if fi, err := os.Stat(cur + ".old"); err != nil || !fi.IsDir() {
		t.Errorf(".old 目录被动过: fi=%v err=%v", fi, err)
	}
}

// TC-32-1c 崩溃恢复场景（票面 32-1）：currentExe 缺失 + .old 完好，且新二进制
// 不可用（下载失败路径）→ 二进制必须仍然可用（回滚到旧副本），不得丢失。
//
// 修复前：先 os.Remove(backup)（删掉唯一可恢复副本）→ rename 失败 →
// copyAndReplace 打开不存在的 newBinary 失败 → currentExe 永久缺失。
func TestReplaceBinaryWindows_InterruptedUpdateKeepsRecoverableBinary(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "xdev.exe")
	writeFile(t, current+".old", "OLD-BINARY")
	missingNew := filepath.Join(dir, "never-downloaded.exe")

	if err := replaceBinaryWindows(current, missingNew); err == nil {
		t.Fatal("replaceBinaryWindows with missing new binary = nil, want error")
	}
	got, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("currentExe 丢失（自愈失败）: %v", err)
	}
	if string(got) != "OLD-BINARY" {
		t.Errorf("currentExe content = %q, want OLD-BINARY", string(got))
	}
}

// TC-32-1d 崩溃恢复 + 新二进制可用 → 正常替换（自愈后继续走原流程）。
func TestReplaceBinaryWindows_RecoversThenReplaces(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "xdev.exe")
	newBin := filepath.Join(dir, "incoming.exe")
	writeFile(t, current+".old", "OLD-BINARY")
	writeFile(t, newBin, "NEW-BINARY")

	if err := replaceBinaryWindows(current, newBin); err != nil {
		t.Fatalf("replaceBinaryWindows: %v", err)
	}
	if got := readFile(t, current); got != "NEW-BINARY" {
		t.Errorf("currentExe content = %q, want NEW-BINARY", got)
	}
	if _, err := os.Stat(newBin); !os.IsNotExist(err) {
		t.Errorf("new binary 应已被 rename 消耗, stat err=%v", err)
	}
}

// TC-32-1e copyAndReplace 走「写 <.exe>.new → rename 覆盖」：陈旧 .new 被清理，
// 成功后无中间文件残留，currentExe 原内容绝不半截。
func TestCopyAndReplace_StagingThenRename(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "xdev.exe")
	newBin := filepath.Join(dir, "incoming.exe")
	writeFile(t, current, "OLD-BINARY")
	writeFile(t, current+".new", "STALE-JUNK")
	writeFile(t, newBin, "NEW-BINARY")

	if err := copyAndReplace(current, newBin); err != nil {
		t.Fatalf("copyAndReplace: %v", err)
	}
	if got := readFile(t, current); got != "NEW-BINARY" {
		t.Errorf("currentExe content = %q, want NEW-BINARY", got)
	}
	if _, err := os.Stat(current + ".new"); !os.IsNotExist(err) {
		t.Errorf("残留 .new 中间文件, stat err=%v", err)
	}
}

// TC-32-1f 源码守卫：copyAndReplace 不得再以 O_TRUNC 直写 currentExe
// （半截损坏的来源），必须经 currentExe+".new" → rename 发布。
func TestCopyAndReplace_NoDirectTruncate(t *testing.T) {
	data, err := os.ReadFile("update.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if regexp.MustCompile(`os\.OpenFile\(currentExe`).MatchString(src) {
		t.Error("update.go 仍直接以 os.OpenFile(currentExe, ...) 覆盖写——必须改为写 currentExe+\".new\" 再 rename")
	}
	if !strings.Contains(src, `currentExe + ".new"`) {
		t.Error("update.go 缺少 currentExe+\".new\" 暂存文件（E-06：写 .new → rename 覆盖）")
	}
	if !strings.Contains(src, `os.Remove(currentExe + ".new")`) {
		t.Error("update.go 缺少陈旧 .new 的启动清理（E-06：启动时清理上次中断留下的半成品）")
	}
}

// TC-32-2a replaceCopyAtomic：SHA-256 不符 → 删除临时文件与已损坏副本
// （下次 EnsureSelfInstalled 重拷自愈），绝不发布未校验内容。
func TestReplaceCopyAtomic_HashMismatchDeletesDamagedCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "xdev.exe")
	writeFile(t, src, "NEW-BINARY-CONTENT")
	writeFile(t, dst, "DAMAGED-HALF") // 半截损坏副本

	err := replaceCopyAtomic(src, dst, strings.Repeat("0", 64)) // 故意错误的期望哈希
	if err == nil {
		t.Fatal("replaceCopyAtomic with wrong hash = nil, want error")
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("损坏副本未被删除（EnsureSelfInstalled 无法重拷自愈）: stat err=%v", statErr)
	}
	assertNoTempResidue(t, dir)
}

// TC-32-2b replaceCopyAtomic 成功路径：发布校验通过的副本、权限跟随源文件、
// 无中间文件残留。
func TestReplaceCopyAtomic_SuccessPublishesVerifiedCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "xdev.exe")
	writeFile(t, src, "NEW-BINARY-CONTENT")
	writeFile(t, dst, "OLD-BINARY-CONTENT")

	want, err := util.FileSHA256(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceCopyAtomic(src, dst, want); err != nil {
		t.Fatalf("replaceCopyAtomic: %v", err)
	}
	if got := readFile(t, dst); got != "NEW-BINARY-CONTENT" {
		t.Errorf("dst content = %q, want NEW-BINARY-CONTENT", got)
	}
	assertNoTempResidue(t, dir)
}

// TC-32-2c 端到端（integration，半截副本 + 旧版本探测）→ 副本被原子替换修复。
func TestReplaceStaleCLICopy_HealsHalfWrittenCopy(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	copyPath := filepath.Join(home, staleFixtureName(t))
	writeFile(t, current, "NEW-BINARY-CONTENT")
	writeFile(t, copyPath, "HALF") // 上次非原子拷贝留下的半截文件

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("0.2.0")); err != nil {
		t.Fatalf("ReplaceStaleCLICopyWithProbe: %v", err)
	}
	if got := readFile(t, copyPath); got != "NEW-BINARY-CONTENT" {
		t.Errorf("copy content = %q, want NEW-BINARY-CONTENT（半截副本必须被修复）", got)
	}
	h1, _ := util.FileSHA256(current)
	h2, _ := util.FileSHA256(copyPath)
	if h1 != h2 {
		t.Errorf("hash mismatch after heal: %s vs %s", h1, h2)
	}
	assertNoTempResidue(t, home)
}

// assertNoTempResidue 断言目录内没有临时文件残留。
func assertNoTempResidue(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("临时文件残留: %s", e.Name())
		}
	}
}
