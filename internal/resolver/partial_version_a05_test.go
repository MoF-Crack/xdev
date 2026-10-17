package resolver

import "testing"

// 第八轮审计 A-05 守卫：部分版本（`3.12`）必须解析为「3.12.x 中最高者」，
// 而不是当作精确版本。
//
// 缺陷现场：`Parse` 先判 `isExactSemver`，而 `parseSemver` 接受 2 段（`3.12`）
// → 返回 KindExact → 精确匹配 "3.12" 失败并报 "version 3.12 not found"。
// 于是 `isPartial` / `resolveRange` 的 "x.y" 分支是**完全死代码**。
//
// 契约来源：README.md 与 docs/installation-guide.md 都把
// `xdev install python@3.12` 当首要示例；resolver.go 的注释也承诺
// `"3.12" -> any 3.12.x`。
//
// 范围说明：`isPartial` 的定义是「恰好两段纯数字」（`3.12`），因此本用例只覆盖
// 该契约。单段 `3` 与带 `v` 前缀的 `v3.12` 不在 isPartial 的定义内（属**另议**的
// 扩展范围，本次不擅自放宽语义）。
func TestParse_PartialVersionIsRangeNotExact(t *testing.T) {
	for _, in := range []string{"3.12", "1.0", " 3.12 "} {
		spec, err := Parse(in)
		if err != nil {
			t.Errorf("A-05 复现：Parse(%q) 报错 %v —— 部分版本应被接受", in, err)
			continue
		}
		if spec.Kind != KindRange {
			t.Errorf("Parse(%q).Kind = %v, want KindRange（部分版本必须走范围解析）", in, spec.Kind)
		}
	}
}

// 端到端：`3.12` 必须解析到 3.12.x 里最高的那个（而非报 not found）。
func TestResolve_PartialVersionPicksHighestPatch(t *testing.T) {
	available := []string{"3.10.0", "3.12.1", "3.12.7", "3.13.0"}

	spec, err := Parse("3.12")
	if err != nil {
		t.Fatalf("Parse(3.12): %v", err)
	}
	got, err := Resolve(spec, available, "", "")
	if err != nil {
		t.Fatalf("A-05 复现：Resolve(3.12) 失败: %v", err)
	}
	if got != "3.12.7" {
		t.Errorf("Resolve(3.12) = %q, want 3.12.7（3.12.x 中最高者）", got)
	}
}

// 对照：完整三段版本仍必须走精确匹配（不得因为 A-05 的修复而变成范围）。
func TestParse_ExactSemverStillExact(t *testing.T) {
	for _, in := range []string{"3.12.7", "1.0.0-rc.1", "2.0.0+build"} {
		spec, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) 报错: %v", in, err)
			continue
		}
		if spec.Kind != KindExact {
			t.Errorf("Parse(%q).Kind = %v, want KindExact", in, spec.Kind)
		}
	}
}

// 对照：显式约束语法不受影响。
func TestParse_ExplicitConstraintsUnchanged(t *testing.T) {
	for _, in := range []string{"^3.12", "~3.12", ">=3.12", "*"} {
		spec, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) 报错: %v", in, err)
			continue
		}
		if spec.Kind != KindRange {
			t.Errorf("Parse(%q).Kind = %v, want KindRange", in, spec.Kind)
		}
	}
}
