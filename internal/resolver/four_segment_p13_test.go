package resolver

import "testing"

// 第九轮审计 P1-3 守卫：4 段版本（真实语料）必须能解析、能精确匹配、能按范围选中。
//
// 缺陷现场：`parseSemver` 用 `SplitN(s, ".", 3)`，于是 "2026.2.0.1" 的第 3 段变成
// "0.1"、Atoi 失败 → `unrecognised version spec`。后果（审计实测）：
//
//	RESOLVE clion@"latest"     -> "2026.2.0.1"  err=<nil>
//	RESOLVE clion@"2026.2.0.1" -> ""            err=unrecognised version spec
//	RESOLVE clion@"^2026"      -> "2026.2"      ← 静默降级到较旧版本
//
// 而 `filterVersions` 会**静默丢弃**解析不了的条目，所以范围解析永远选不到 4 段
// 构建 —— 与 R8-2 同一失败形态（「以为在更新，其实没推上去」）。
//
// 真实语料：clion/goland/webstorm 的 latest 就是 "2026.2.0.1"；
// jetbrains-idea-community "2025.2.6.3"、pycharm-community "2025.2.6.2"。
func TestParse_AcceptsFourSegmentVersions(t *testing.T) {
	for _, spec := range []string{
		"2026.2.0.1",
		"2025.2.6.3",
		"2025.2.6.2",
		"1.2.3.4",
		"1.2.3.4.5", // 任意段数
	} {
		v, err := Parse(spec)
		if err != nil {
			t.Errorf("P1-3 复现：Parse(%q) 失败: %v", spec, err)
			continue
		}
		if v.Kind != KindExact {
			t.Errorf("Parse(%q).Kind = %v, want KindExact", spec, v.Kind)
		}
	}
}

// 精确匹配：显式安装 4 段版本必须成功（这是审计里最直接的失败形态）。
func TestResolve_ExactFourSegmentVersion(t *testing.T) {
	// clion 语料的真实形状。
	available := []string{"2026.2.0.1", "2026.2", "2026.1.3.2"}

	spec, err := Parse("2026.2.0.1")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := Resolve(spec, available, "", "")
	if err != nil {
		t.Fatalf("P1-3 复现：Resolve(\"2026.2.0.1\") 失败: %v", err)
	}
	if got != "2026.2.0.1" {
		t.Errorf("Resolve(\"2026.2.0.1\") = %q, want 2026.2.0.1", got)
	}
}

// 范围解析：`^2026`/`~2026.2`/`>=2026` 必须能选中 4 段构建（而不是静默降级）。
func TestResolve_RangeSelectsFourSegmentBuild(t *testing.T) {
	available := []string{"2026.2.0.1", "2026.2", "2026.1.3.2"}

	cases := []struct {
		spec string
		want string
	}{
		{"^2026", "2026.2.0.1"},
		{"~2026.2", "2026.2.0.1"},
		{">=2026", "2026.2.0.1"},
		{"*", "2026.2.0.1"},
		{"2026.2", "2026.2.0.1"}, // 部分版本应匹配到同前缀里最新的
	}
	for _, tc := range cases {
		sp, perr := Parse(tc.spec)
		if perr != nil {
			t.Errorf("Parse(%q) 失败: %v", tc.spec, perr)
			continue
		}
		got, err := Resolve(sp, available, "", "")
		if err != nil {
			t.Errorf("Resolve(%q) 失败: %v", tc.spec, err)
			continue
		}
		if got != tc.want {
			t.Errorf("P1-3：Resolve(%q) = %q, want %q（4 段构建应被选中，而不是静默降级）",
				tc.spec, got, tc.want)
		}
	}
}

// 比较语义：缺失段视为 0（"2026.2" == "2026.2.0.0"），但 4 段更大者胜出。
func TestSemverCompare_FourSegmentOrdering(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.2.0.1", "2026.2", 1},  // 4 段构建 > 3 段
		{"2026.2", "2026.2.0.1", -1}, // 反向
		{"2026.2.0.0", "2026.2", 0},  // 缺失段视为 0
		{"2026.2.0.2", "2026.2.0.1", 1},
		{"2026.2.1.0", "2026.2.0.9", 1}, // patch 优先于第 4 段
		{"2026.3.0.0", "2026.2.0.9", 1}, // minor 优先
		{"2027.0.0.0", "2026.9.9.9", 1}, // major 优先
		{"1.2.3.4", "1.2.3.4", 0},
		{"1.0.0-rc.1", "1.0.0", -1},   // pre-release 仍低于正式版
		{"1.0.0.1", "1.0.0", 1},       // extra 属数值核心，高于同基线的正式版
		{"1.0.0-rc.1", "1.0.0.1", -1}, // pre-release 低于更高数值核心
	}
	for _, tc := range cases {
		a, errA := parseSemver(tc.a)
		if errA != nil {
			t.Errorf("parseSemver(%q): %v", tc.a, errA)
			continue
		}
		b, errB := parseSemver(tc.b)
		if errB != nil {
			t.Errorf("parseSemver(%q): %v", tc.b, errB)
			continue
		}
		if got := a.compare(b); got != tc.want {
			t.Errorf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// 回归：标准 3 段与 2 段的既有语义不得改变。
func TestParse_ThreeAndTwoSegmentSemanticsUnchanged(t *testing.T) {
	for _, spec := range []string{"1.2.3", "0.0.1", "2026.2.0"} {
		v, err := Parse(spec)
		if err != nil {
			t.Errorf("Parse(%q) 失败: %v", spec, err)
			continue
		}
		if v.Kind != KindExact {
			t.Errorf("Parse(%q).Kind = %v, want KindExact", spec, v.Kind)
		}
	}
	// 2 段仍是「部分版本」（A-05）。
	v, err := Parse("3.12")
	if err != nil {
		t.Fatalf("Parse(\"3.12\") 失败: %v", err)
	}
	if v.Kind != KindRange {
		t.Errorf("Parse(\"3.12\").Kind = %v, want KindRange（A-05 回归）", v.Kind)
	}
}
