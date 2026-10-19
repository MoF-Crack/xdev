package util

import "testing"

// 第十轮审计 P2-3 守卫：`CompareSemver` 必须支持 **4 段及以上**版本。
//
// 缺陷现场：Round 9 修好了 `internal/resolver` 的同一缺陷（那边新增 `extra []int`），
// 但 `util.CompareSemver` 是**第二套排序**，未被覆盖 —— 它只比较前 3 段，于是
// `2026.2.0.1` 被静默截断成 `[2026 2 0]`：
//
//	util.CompareSemver("2026.2.0.1", "2026.2") = 0   ← 审计实测
//
// 而真实语料里就有 `2026.2.0.1`、`2025.2.6.3`（四段构建号）。
// 后果：已装 `2026.2`、manifest latest 为 `2026.2.0.1` 时，所有走本函数的
// 「是否有更新」门**判不出更新**，用户看到「已是最新」，四段构建**永远装不上**。
//
// 这正是 Round 8/9 反复出现的「只修了一个调用点」形态的又一处。

// TestCompareSemver_FourSegmentVersions 是核心矩阵。
func TestCompareSemver_FourSegmentVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		why  string
	}{
		// 审计实测的那一条：4 段 > 3 段（缺失段视为 0）。
		{"2026.2.0.1", "2026.2", 1, "审计实测：此前返回 0（被截断）"},
		{"2026.2", "2026.2.0.1", -1, "对称"},
		{"2026.2.0.1", "2026.2.0.0", 1, "第 4 段参与比较"},
		{"2026.2.0.0", "2026.2.0.1", -1, "对称"},
		{"2026.2.0.1", "2026.2.0", 1, "缺失段视为 0"},
		{"2026.2.0", "2026.2.0.1", -1, "对称"},

		// 真实语料里的四段版本。
		{"2025.2.6.3", "2025.2.6.2", 1, "真实语料 2025.2.6.3"},
		{"2025.2.6.2", "2025.2.6.3", -1, "对称"},
		{"2025.2.6.3", "2025.2.6", 1, "4 段 > 3 段"},
		{"2025.2.6.3", "2025.2.7", -1, "第 3 段优先于第 4 段"},

		// 缺失段补齐语义必须与 resolver 一致。
		{"1.2.3.4", "1.2.3", 1, "4 段 > 3 段"},
		{"1.2.3", "1.2.3.0", 0, "缺失段视为 0 → 相等"},
		{"1.2.3.0", "1.2.3", 0, "对称"},
		{"1.2", "1.2.0.0", 0, "2 段 == 4 段全零"},
		{"1.2.0.0", "1.2", 0, "对称"},

		// 5 段。
		{"1.2.3.4.5", "1.2.3.4", 1, "5 段 > 4 段"},
		{"1.2.3.4", "1.2.3.4.5", -1, "对称"},
		{"1.2.3.4.5", "1.2.3.4.6", -1, "第 5 段参与比较"},
		{"1.2.3.4.5", "1.2.3.5", -1, "高位段优先"},

		// 4 段 + 预发布后缀：段数比较优先，然后才是预发布。
		{"2026.2.0.1", "2026.2.0.1-rc.1", 1, "正式版 > 预发布"},
		{"2026.2.0.1-rc.1", "2026.2.0.1", -1, "对称"},
		{"2026.2.0.1-rc.2", "2026.2.0.1-rc.1", 1, "预发布内数字比较"},
		{"2026.2.0.1", "2026.2.0.2-rc.1", -1, "段数优先于预发布"},

		// build 元数据仍被忽略。
		{"2026.2.0.1+build", "2026.2.0.1", 0, "build 元数据忽略"},
		{"2026.2.0.1+build1", "2026.2.0.1+build2", 0, "build 元数据忽略"},

		// 3 段以内既有行为不得回归。
		{"1.2.3", "1.2.4", -1, "既有 3 段行为"},
		{"2.0.0", "1.9.9", 1, "既有 3 段行为"},
		{"1.0.0", "1.0.0-rc.1", 1, "既有预发布语义"},
		{"v1.0.0", "1.0.0", 0, "v 前缀"},
	}
	for _, c := range cases {
		if got := CompareSemver(c.a, c.b); got != c.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d (%s)", c.a, c.b, got, c.want, c.why)
		}
	}
}

// 4 段版本的**排序**必须单调：这是「挑 latest」的实际用法。
func TestCompareSemver_FourSegmentSortingIsMonotonic(t *testing.T) {
	versions := []string{
		"2026.2", "2026.2.0.1", "2026.2.0", "2026.2.1", "2026.2.0.2",
		"2025.2.6.3", "2026.3", "2026.2.0.10", "2026.2.0.9",
	}
	// 插入排序，用 CompareSemver 作为唯一判据。
	sorted := append([]string(nil), versions...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && CompareSemver(sorted[j-1], sorted[j]) > 0; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	// 断言整体单调不减。
	for i := 1; i < len(sorted); i++ {
		if CompareSemver(sorted[i-1], sorted[i]) > 0 {
			t.Errorf("排序后仍非单调: %q > %q (全序 %v)", sorted[i-1], sorted[i], sorted)
		}
	}
	// 最大的必须是 2026.3（第 2 段优先）。
	last := sorted[len(sorted)-1]
	if last != "2026.3" {
		t.Errorf("最大版本 = %q, want 2026.3（全序 %v）", last, sorted)
	}
	// 4 段必须排在对应的 3 段之后。
	if CompareSemver("2026.2.0.1", "2026.2") <= 0 {
		t.Errorf("2026.2.0.1 应 > 2026.2")
	}
}

// `ParseSemverParts` 的历史签名（[3]int）保持不变，但**不得**再是排序的依据。
func TestParseSemverParts_StillReturnsThreeForCompatibility(t *testing.T) {
	got := ParseSemverParts("2026.2.0.1")
	want := [3]int{2026, 2, 0}
	if got != want {
		t.Errorf("ParseSemverParts(2026.2.0.1) = %v, want %v（签名兼容，只返回前 3 段）", got, want)
	}
	// 关键：即使 ParseSemverParts 相等，CompareSemver 也必须区分。
	if ParseSemverParts("2026.2.0.1") == ParseSemverParts("2026.2.0.0") {
		if CompareSemver("2026.2.0.1", "2026.2.0.0") == 0 {
			t.Error("ParseSemverParts 相等时 CompareSemver 仍必须能区分第 4 段")
		}
	}
}

// 饱和累加（票 39 / P2-23）在**任意段**上都必须保持 —— 超长数字段不得回绕。
func TestCompareSemver_SaturationHoldsForExtraSegments(t *testing.T) {
	huge := "1.2.3." + repeatDigits(30)
	// 30 个 9 远超 int 上限：必须饱和而不是回绕。
	if got := CompareSemver(huge, "1.2.3.1"); got <= 0 {
		t.Errorf("超长第 4 段应饱和为极大值（> 1），got %d", got)
	}
	if got := CompareSemver("1.2.3.1", huge); got >= 0 {
		t.Errorf("对称：1 应 < 饱和值，got %d", got)
	}
	// 两个都超长 → 相等（都饱和到同一上限），不得因回绕而乱序。
	if got := CompareSemver(huge, "1.2.3."+repeatDigits(31)); got != 0 {
		t.Errorf("两个饱和值应相等，got %d", got)
	}
}

func repeatDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '9'
	}
	return string(b)
}
