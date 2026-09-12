package resolver

import (
	"strings"
	"testing"
)

// 守卫测试 28-2（审计 D-09 / P1-14，方案 B）：KindExact 的双侧 v 前缀匹配。
//
// 背景：预填/自定义工具的版本串可能带 "v" 前缀（remote.Pattern 捕获组
// 直接落在 VersionList 里，如 v3.12.0），而内置清单的 available 列表是
// 不带前缀的 3.12.0（反之亦有：用户输入 3.12.0，available 是 v3.12.0）。
// 修复前 KindExact 只做原串全等 -> 这类解析全部失败（工具"装不上"）。
// 修复后：先原串全等（零回归），miss 后双侧 TrimPrefix("v") 比较。
//
// 关键约束：返回值必须是 available 里的**原始字符串**（调用方据此拼安装
// 目录），绝不返回规范化后的形态。

func TestResolveExact_VPrefixBothDirections(t *testing.T) {
	cases := []struct {
		name  string
		input string
		avail []string
		want  string
	}{
		{
			name:  "input has v, available plain (builtin manifest, core D-09 case)",
			input: "v3.12.0",
			avail: []string{"3.10.0", "3.12.0"},
			want:  "3.12.0",
		},
		{
			name:  "input plain, available has v (custom tool prefill)",
			input: "3.12.0",
			avail: []string{"3.10.0", "v3.12.0"},
			want:  "v3.12.0",
		},
		{
			name:  "exact raw-string hit still wins (no regression, v on both sides)",
			input: "v1.2.3",
			avail: []string{"v1.2.3"},
			want:  "v1.2.3",
		},
		{
			name:  "exact hit has priority over v-normalised hit",
			input: "3.12.0",
			avail: []string{"v3.12.0", "3.12.0"},
			want:  "3.12.0",
		},
		{
			name:  "plain on both sides unchanged (original behaviour)",
			input: "3.11.0",
			avail: []string{"3.10.0", "3.11.0", "3.12.0"},
			want:  "3.11.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if spec.Kind != KindExact {
				t.Fatalf("Parse(%q).Kind = %d, want KindExact", tc.input, spec.Kind)
			}
			got, err := Resolve(spec, tc.avail, "", "")
			if err != nil {
				t.Fatalf("Resolve(Parse(%q), %v) error: %v", tc.input, tc.avail, err)
			}
			if got != tc.want {
				t.Errorf("Resolve(Parse(%q), %v) = %q, want %q (original string from available)", tc.input, tc.avail, got, tc.want)
			}
			// 返回的必须是 available 里的原始元素，而不是任何规范化形态。
			inList := false
			for _, a := range tc.avail {
				if a == got {
					inList = true
					break
				}
			}
			if !inList {
				t.Errorf("Resolve returned %q which is not one of the available entries %v (available must NOT be normalised)", got, tc.avail)
			}
		})
	}
}

// TestResolveExact_VPrefixStillMissesRealMismatch：去 v 比较不能变成"模糊
// 匹配"——版本号不同仍必须报错（否则 KindExact 会静默装错版本）。
func TestResolveExact_VPrefixStillMissesRealMismatch(t *testing.T) {
	for _, tc := range []struct {
		input string
		avail []string
	}{
		{"v3.13.0", []string{"3.12.0"}},
		{"3.13.0", []string{"v3.12.0"}},
		{"v3.12.1", []string{"3.12.0"}},
	} {
		spec, err := Parse(tc.input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.input, err)
		}
		got, err := Resolve(spec, tc.avail, "", "")
		if err == nil {
			t.Errorf("Resolve(Parse(%q), %v) = %q, want error (v-stripping must not fuzzy-match)", tc.input, tc.avail, got)
			continue
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Resolve(Parse(%q), %v) error = %v, want 'not found'", tc.input, tc.avail, err)
		}
	}
}

// TestResolveRange_VPrefixNotRegressed：Range 走 parseSemver（本来就剥 v），
// 返回值同样必须是 available 的原始串——确认 D-09 的改动没有波及 Range。
func TestResolveRange_VPrefixNotRegressed(t *testing.T) {
	avail := []string{"v3.12.1", "v3.13.0"}
	for _, tc := range []struct{ constraint, want string }{
		{"^3.12", "v3.13.0"}, // ^3.12 = >=3.12.0 <4.0.0 -> 最高匹配 v3.13.0
		{"~3.12", "v3.12.1"}, // ~3.12 = >=3.12.0 <3.13.0 -> v3.12.1
	} {
		spec, err := Parse(tc.constraint)
		if err != nil {
			t.Fatalf("Parse(%s): %v", tc.constraint, err)
		}
		got, err := Resolve(spec, avail, "", "")
		if err != nil {
			t.Fatalf("Resolve(%s) error: %v", tc.constraint, err)
		}
		if got != tc.want {
			t.Errorf("Resolve(%s, %v) = %q, want %q (original string from available)", tc.constraint, avail, got, tc.want)
		}
	}
}
