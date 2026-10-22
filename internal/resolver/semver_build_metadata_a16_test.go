// semver_build_metadata_a16_test.go — 第九轮审计 A-16（P2）复现守卫。
//
// 缺陷原形（resolver.go parseSemver）：先按 '-' 切 pre-release，再按 '+' 去
// build metadata。对 "1.0.0-rc.1+build"：
//
//	第 1 步（切 '-'）: s="1.0.0", pre="rc.1+build"
//	第 2 步（切 '+'）: 只作用于 s="1.0.0" —— '+' 已不在主版本段里
//
// 于是 pre-release 残留 "+build"。实测
// compare("1.0.0-rc.1+build","1.0.0-rc.1") = 1，
// 而 SemVer 2.0.0 §10 要求 build metadata **不参与**优先级比较 → 应为 0；
// 同仓库的 util.CompareSemver 给出的正是 0，两者结论相反。
//
// 修复：按 SemVer 先剥离 "+build"，再切 "-pre"（调整两步顺序）。
package resolver

import (
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// A16SemverCase 是「parseSemver 结论必须与 util.CompareSemver 一致」的用例表。
type A16SemverCase struct {
	a, b string
	want int // SemVer 语义下的期望（与 util.CompareSemver 一致）
}

func a16Cases() []A16SemverCase {
	return []A16SemverCase{
		// build metadata 不参与比较（SemVer 2.0.0 §10）
		{"1.0.0-rc.1+build", "1.0.0-rc.1", 0},
		{"1.0.0+build", "1.0.0", 0},
		{"1.0.0+build.1", "1.0.0+build.2", 0},
		{"1.2.3-alpha+build", "1.2.3-alpha+other", 0},
		{"v1.0.0+meta", "1.0.0", 0},
		// build metadata 与 pre-release 同时存在时，pre-release 仍决定优先级
		{"1.0.0-rc.2+build", "1.0.0-rc.1+build", 1},
		{"1.0.0-alpha+build", "1.0.0-beta+build", -1},
		{"1.0.0-rc.1+build", "1.0.0+build", -1}, // 有 pre-release < 无
		{"1.0.0+build", "1.0.0-rc.1", 1},
		// 纯 pre-release 优先级不得回归
		{"1.0.0-rc.2", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.1.2", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},
		{"1.0.0-rc.1", "1.0.0", -1},
		// 主版本号比较不受影响
		{"2.0.0+build", "1.9.9", 1},
		{"1.0.0", "1.0.0", 0},
	}
}

// TestParseSemver_BuildMetadataStrippedBeforePreRelease 是本项的红→绿守卫：
// 对每一对，parseSemver 的比较结论必须与 util.CompareSemver 一致。
func TestParseSemver_BuildMetadataStrippedBeforePreRelease(t *testing.T) {
	for _, tc := range a16Cases() {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			pa, err := parseSemver(tc.a)
			if err != nil {
				t.Fatalf("parseSemver(%q): %v", tc.a, err)
			}
			pb, err := parseSemver(tc.b)
			if err != nil {
				t.Fatalf("parseSemver(%q): %v", tc.b, err)
			}
			got := pa.compare(pb)

			if want := util.CompareSemver(tc.a, tc.b); want != tc.want {
				t.Fatalf("前置条件漂移：util.CompareSemver(%q,%q) = %d, 用例表期望 %d", tc.a, tc.b, want, tc.want)
			}
			if got != tc.want {
				t.Errorf("compare(parseSemver(%q), parseSemver(%q)) = %d, want %d（build metadata 必须不参与比较）",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestParseSemver_PreReleaseHasNoBuildMetadataResidue：直接断言 pre 字段
// 不含 '+' —— 这是缺陷最直观的形态（"rc.1+build" 粘在 pre-release 上）。
func TestParseSemver_PreReleaseHasNoBuildMetadataResidue(t *testing.T) {
	v, err := parseSemver("1.0.0-rc.1+build")
	if err != nil {
		t.Fatalf("parseSemver: %v", err)
	}
	if v.pre != "rc.1" {
		t.Errorf("pre = %q, want %q（build metadata 必须被剥离）", v.pre, "rc.1")
	}
	if v.major != 1 || v.minor != 0 || v.patch != 0 {
		t.Errorf("major.minor.patch = %d.%d.%d, want 1.0.0", v.major, v.minor, v.patch)
	}

	v, err = parseSemver("1.0.0+build")
	if err != nil {
		t.Fatalf("parseSemver: %v", err)
	}
	if v.pre != "" {
		t.Errorf("1.0.0+build 的 pre = %q, want \"\"", v.pre)
	}
}

// TestResolveRange_BuildMetadataDoesNotChangeSelection：行为级守卫 ——
// 同一批 available 里同时存在带/不带 build metadata 的同一版本时，范围
// 解析结果必须与「build metadata 被忽略」的语义一致（不因残留 '+' 而把
// pre-release 版本错误地排到正式版之上）。
func TestResolveRange_BuildMetadataDoesNotChangeSelection(t *testing.T) {
	avail := []string{"1.0.0-rc.1", "1.0.0-rc.1+build"}
	spec, err := Parse("^1.0.0-rc.1")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := Resolve(spec, avail, "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 两个字符串语义等价；排序必须稳定且二者都不会被判为「高于 1.0.0」。
	for _, v := range avail {
		pa, _ := parseSemver(v)
		pb, _ := parseSemver("1.0.0")
		if pa.compare(pb) >= 0 {
			t.Errorf("%q 与 1.0.0 比较 = %d，pre-release 必须低于正式版（A-16 残留 '+' 会让它反转）", v, pa.compare(pb))
		}
	}
	if got != "1.0.0-rc.1" && got != "1.0.0-rc.1+build" {
		t.Errorf("Resolve = %q, want one of the two equivalent rc.1 entries", got)
	}
}
