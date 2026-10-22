package util

import (
	"strings"
	"testing"
)

// 第十一轮审计（Round 10 报告待办 9）：`SafeName` 放行**纯点/纯空格**名字。
//
// 缺陷现场：`sanitize.go` 的尾随点/空格检查写成
//
//	trimmed := strings.TrimRight(s, ". ")
//	if trimmed != s && trimmed != "" { reject }
//
// 那个 `trimmed != ""` 是致命的：**全点/全空格**名字（`"..."`、`"...."`、
// `"   "`、`". ."`）经 TrimRight 后恰好是空串，于是被这个条件**放行**。
// 代码注释写的是「Allow if the entire name is dots/spaces (caught by "." check
// above)」—— 但上面的检查只拦 `"."` 与 `".."` 两个**精确**值，`"..."` 不在其中，
// 该假设不成立。实测（修复前）：
//
//	SafeName("...")  = "..."  err=<nil>   ← 应为错误
//	SafeName("....") = "...." err=<nil>
//	SafeName("   ")  = "   "  err=<nil>
//	SafeName(". .")  = ". ."  err=<nil>
//
// 危害：Windows 会把纯点/空格名静默折叠成**空名**或与其它名字合并
// （Round 10 已实测 `bin.` 与 `bin` 折叠为同一目录项）。`SafeName` 是工具名、
// 版本号、归档 format 与插件名的**共享入口**（约 23 个调用点），放行这类名字会让
// 安装目录互相串味。

// TestSafeName_RejectsPureDotAndSpaceNames 是核心守卫。
func TestSafeName_RejectsPureDotAndSpaceNames(t *testing.T) {
	for _, s := range []string{
		"...",
		"....",
		".....",
		"   ",
		" ",
		". .",
		".. ..",
		". ..",
		" \t ",
	} {
		if got, err := SafeName(s); err == nil {
			t.Errorf("SafeName(%q) = %q err=nil —— 纯点/空格名必须被拒绝"+
				"（Windows 会折叠成空名或与其它名字合并）", s, got)
		}
	}
}

// 反向断言：**不是**纯点/空格的合法名字绝不能因此被误拒。
//
// 特别是 `..foo` / `...x` 这类「以点开头但不是纯点」的名字 —— Round 10 已确立
// 它们是**合法**的（extractor 曾把它们误判为 zip-slip 并删掉整个目标目录）。
func TestSafeName_AcceptsDottedButNotPureNames(t *testing.T) {
	for _, s := range []string{
		"go",
		"bunx",
		"python3",
		"tool-name",
		"...x",    // 以点开头，但不是纯点
		"..foo",   // 同上
		".hidden", // 普通隐藏文件风格
		"a.b",     // 正常带点
		"a.b.c",   // 多段
		"1.0.0",   // 版本号
		"2026.2.0.1",
	} {
		if _, err := SafeName(s); err != nil {
			t.Errorf("SafeName(%q) 被误拒：%v —— 合法名字不得因纯点规则被拦", s, err)
		}
	}
}

// 保留设备名：**只有整体相等**才是保留名（与 Round 10 在 extractor 侧的实测校准
// 保持一致）。带扩展名的 `con.txt` / `nul.txt` / `lpt9.txt` 是**合法真实文件** ——
// 本机实测（`os.WriteFile` + `os.ReadDir` 目录枚举）它们可正常读写且出现在目录里。
//
// 旧实现「剥掉第一个 `.` 之前的部分再比对」是**误报源**：`con.txt.bak`、`aux.x.y`
// 这类完全正常的名字会被判非法，从而把合法工具名/版本号/format 拒之门外。
func TestSafeName_ReservedNamesRequireExactMatch(t *testing.T) {
	// 整体相等 → 必须拒绝。
	for _, s := range []string{
		"CON", "con", "Con",
		"PRN", "AUX", "NUL", "nul",
		"COM1", "com9", "LPT1", "lpt9",
	} {
		if _, err := SafeName(s); err == nil {
			t.Errorf("SafeName(%q) err=nil —— 完整保留设备名必须被拒绝", s)
		}
	}
	// 带扩展名/后缀 → 必须放行（本机实测均为合法真实文件）。
	for _, s := range []string{
		"con.txt", "nul.txt", "aux.x.y", "con.txt.bak", "lpt9.txt",
		"console", "nullable", "con.bak",
	} {
		if _, err := SafeName(s); err != nil {
			t.Errorf("SafeName(%q) 被误拒：%v —— 本机实测它是合法真实文件"+
				"（保留名要求整体相等，扩展名不参与判定）", s, err)
		}
	}
}

// 既有契约不得回归：精确的 `"."` / `".."`、路径分隔符、盘符、空名仍必须拒绝。
func TestSafeName_ExistingRejectionsStillHold(t *testing.T) {
	for _, s := range []string{
		"", ".", "..", "a/b", `a\b`, "C:evil", "bad\x00name", "trailing. ", "trailing ",
	} {
		if _, err := SafeName(s); err == nil {
			t.Errorf("SafeName(%q) err=nil —— 既有拒绝契约回归", s)
		}
	}
}

// 纯点/空格的判定必须是「整个名字都由点与空格组成」，而不是「以点开头」。
func TestSafeName_PureNamePredicateBoundary(t *testing.T) {
	// 直接钉住语义：TrimRight 后为空 ⇔ 整个名字都是点/空格。
	for _, s := range []string{"...", "   ", ". .", "...."} {
		if strings.TrimRight(s, ". ") != "" {
			t.Fatalf("测试前提错误：%q 应为纯点/空格", s)
		}
		if _, err := SafeName(s); err == nil {
			t.Errorf("SafeName(%q) 必须拒绝（纯点/空格）", s)
		}
	}
	for _, s := range []string{"...x", "a b", "a.b"} {
		if strings.TrimRight(s, ". ") == "" {
			t.Fatalf("测试前提错误：%q 不是纯点/空格", s)
		}
		if _, err := SafeName(s); err != nil {
			t.Errorf("SafeName(%q) 不应拒绝：%v", s, err)
		}
	}
	// 尾随点仍必须被拒绝（既有契约），与纯点规则是两条独立规则。
	if _, err := SafeName("x..."); err == nil {
		t.Error(`SafeName("x...") err=nil —— 尾随点必须被拒绝（既有契约）`)
	}
}
