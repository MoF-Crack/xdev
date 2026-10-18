//go:build windows

// cleanup_userpath_empty_anchor_a13_test.go — 第九轮审计 A-13（P2）复现守卫。
//
// 缺陷原形：cleanupUserPathLocked 用
//
//	homeClean := strings.ToLower(filepath.Clean(home))
//	shimDirClean := strings.ToLower(filepath.Clean(shimDir))
//
// 做 home 锚定。filepath.Clean("") 返回 "."，因此 home/shimDir 为空时锚点
// 退化为 "."：
//   - `normalized == homeClean` 对 PATH 里的 `.` 条目为真 → **删除用户 PATH
//     里与 xdev 无关的 `.` 条目**；
//   - shim 模式的前插逻辑认为 `.` 不在列表里 → 前插一个**空条目**
//     （`;...`，语义上等价于 CWD）。
//
// 实测：`"…;.;C:\Tools;…"` → `";…;C:\Tools;…"`（`.` 被删、空条目前插）。
//
// 本守卫断言：home/shimDir 为空时 PATH **逐字节不变**、changed=false。
package env

import (
	"strings"
	"testing"
)

// TestCleanupUserPath_EmptyAnchorsLeavePathUntouched 是本项的红→绿守卫。
func TestCleanupUserPath_EmptyAnchorsLeavePathUntouched(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	// 刻意包含 "." 条目（缺陷原形会被删掉）与空条目（前插逻辑会产生）。
	initial := strings.Join([]string{
		`C:\Windows\System32`,
		".",
		`C:\Tools`,
		"",
		`D:\Env\jdk17\bin`,
	}, ";")
	if err := WriteUserPath(initial); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath("", "", "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath(empty anchors): %v", err)
	}
	if changed {
		t.Error("home/shimDir 为空时不得报告 changed=true（无锚点 ⇒ 无从判定 xdev 归属）")
	}

	got, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	if got != initial {
		t.Errorf("空锚点下 PATH 必须逐字节不变（A-13：\".\" 条目被误删 / 前插空条目）:\n want=%q\n got =%q", initial, got)
	}
	if !strings.Contains(";"+got+";", ";.;") {
		t.Errorf("无关的 \".\" 条目必须存活: PATH=%q", got)
	}
}

// TestCleanupUserPath_EmptyHomeNonEmptyShimStillUntouched：只给了 shimDir
// 但没给 home 时同样不得动 PATH —— shim 前插会把「没有 home 的调用」变成
// 一次盲写（"." 锚点仍然命中 `normalized == homeClean`）。
func TestCleanupUserPath_EmptyHomeNonEmptyShimStillUntouched(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	initial := `C:\Windows\System32;.;C:\Tools`
	if err := WriteUserPath(initial); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath("", `E:\XDev\shims`, "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath(empty home): %v", err)
	}
	if changed {
		t.Error("home 为空时不得报告 changed=true")
	}
	if got, _ := ReadUserPath(); got != initial {
		t.Errorf("home 为空时 PATH 必须逐字节不变:\n want=%q\n got =%q", initial, got)
	}
}

// TestCleanupUserPath_EmptyShimDirDoesNotInjectEmptyEntry：home 非空、shimDir
// 为空时同样保守退出——修复前的缺陷形态是 shimDirClean="." 使前插分支判定
// 「shim 不在 PATH 里」，于是前插一个**空条目**（`;C:\...`，Windows 解释为
// CWD，属安全隐患）。本守卫断言 PATH 逐字节不变、且不出现空条目。
func TestCleanupUserPath_EmptyShimDirDoesNotInjectEmptyEntry(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	home := `E:\XDev`
	initial := `C:\Windows\System32;E:\XDev\install\go\1.22`
	if err := WriteUserPath(initial); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	changed, err := CleanupUserPath(home, "", "shim")
	if err != nil {
		t.Fatalf("CleanupUserPath(empty shimDir): %v", err)
	}
	if changed {
		t.Error("shimDir 为空时不得报告 changed=true")
	}

	got, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	if got != initial {
		t.Errorf("shimDir 为空时 PATH 必须逐字节不变（A-13：前插空条目等价于 CWD）:\n want=%q\n got =%q", initial, got)
	}
	if strings.HasPrefix(got, ";") {
		t.Errorf("shimDir 为空时不得前插空条目（等价于 CWD）: PATH=%q", got)
	}
	for _, p := range strings.Split(got, ";") {
		if p == "" {
			t.Errorf("shimDir 为空时 PATH 不得出现空条目: PATH=%q", got)
		}
	}
}
