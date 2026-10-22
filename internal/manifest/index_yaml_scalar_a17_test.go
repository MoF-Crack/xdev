// index_yaml_scalar_a17_test.go — 第九轮审计 A-17（P2）复现守卫。
//
// 缺陷原形（index.go:145,152）：BuildIndexYAML 用 fmt.Fprintf 直接把标量拼进
// YAML 文本（`display_name: %s` / `name: %s` / `manifest: %s`），完全不做转义。
// 任何含 YAML 指示符的值都会产出**非法 index.yaml**：
//
//	display_name: Demo: Runtime     → 映射歧义（解析报错）
//	display_name: - x               → 序列指示符（解析报错）
//	display_name: Foo #bar          → 注释吞掉后半段（静默丢字）
//
// 修复：复用同包第八轮新增的 needsYAMLQuoting / yamlScalarFor
// （internal/manifest/archive_sha_text.go），不复制第二份转义逻辑。
//
// 本守卫断言：这些值生成的 index.yaml 必须能被同包 ParseIndexYAML 往返读回，
// 且字段值逐字符相等。
package manifest

import (
	"testing"
	"time"
)

// a17DisplayCases 覆盖任务要求的全部形态：`:`、`#`、前导 `-`、引号、非 ASCII。
var a17DisplayCases = []struct {
	label   string
	display string
}{
	{"colon+space (map ambiguity)", "Demo: Runtime"},
	{"trailing colon", "Demo:"},
	{"comment hash", "Foo #bar"},
	{"leading dash", "- leading dash"},
	{"leading quote", `"quoted`},
	{"embedded double quote", `Say "hi"`},
	{"embedded single quote", "it's a tool"},
	{"non-ascii cjk", "工具链"},
	{"non-ascii accented", "über-build"},
	{"empty (falls back to name)", ""},
	{"boolean lookalike", "true"},
	{"numeric lookalike", "12345"},
	{"yaml indicator prefix", "@scope/tool"},
	{"flow indicator", "[bracket"},
}

// TestBuildIndexYAML_QuotesUnsafeScalars_RoundTrip 是本项的红→绿守卫。
func TestBuildIndexYAML_QuotesUnsafeScalars_RoundTrip(t *testing.T) {
	var tools []*Tool
	for i, tc := range a17DisplayCases {
		name := "a17-tool-" + string(rune('a'+i))
		tools = append(tools, &Tool{
			Name:        name,
			DisplayName: tc.display,
			Category:    "language",
		})
	}

	data, err := BuildIndexYAML(tools, time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildIndexYAML: %v", err)
	}

	idx, err := ParseIndexYAML(data)
	if err != nil {
		t.Fatalf("A-17：生成的 index.yaml 无法被同包解析器读回（标量未转义）: %v\n--- 生成内容 ---\n%s", err, data)
	}

	got := map[string]string{}
	for _, c := range idx.Categories {
		for _, tool := range c.Tools {
			got[tool.Name] = tool.DisplayName
		}
	}
	for i, tc := range a17DisplayCases {
		name := "a17-tool-" + string(rune('a'+i))
		want := tc.display
		if want == "" {
			want = name // 空 display_name 回退为工具名
		}
		gotV, ok := got[name]
		if !ok {
			t.Errorf("往返后工具 %s（display=%q，%s）丢失", name, tc.display, tc.label)
			continue
		}
		if gotV != want {
			t.Errorf("display_name 往返不一致（%s）: got %q, want %q", tc.label, gotV, want)
		}
	}
}

// TestBuildIndexYAML_QuotesUnsafeCategoryAndManifest：分类名与 manifest 文件名
// 走的是同一段 Fprintf 拼接（index.go:145/152），同类缺口一并覆盖。
func TestBuildIndexYAML_QuotesUnsafeCategoryAndManifest(t *testing.T) {
	cat := "cloud: native"
	data, err := BuildIndexYAML([]*Tool{
		{Name: "a17-cat", DisplayName: "A17 Cat", Category: cat},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("BuildIndexYAML: %v", err)
	}

	idx, err := ParseIndexYAML(data)
	if err != nil {
		t.Fatalf("A-17：含指示符的分类名产出非法 YAML: %v\n--- 生成内容 ---\n%s", err, data)
	}
	found := false
	for _, c := range idx.Categories {
		if c.Name == cat {
			found = true
		}
	}
	if !found {
		t.Errorf("分类名 %q 往返丢失或变形: %+v\n--- 生成内容 ---\n%s", cat, idx.Categories, data)
	}
}

// TestBuildIndexYAML_PlainScalarsStayUnquoted：防过度引用回归 —— 既有的
// 无歧义标量必须保持裸写（index_test.go 的文本断言依赖这一点）。
func TestBuildIndexYAML_PlainScalarsStayUnquoted(t *testing.T) {
	data, err := BuildIndexYAML([]*Tool{
		idxTool("go", "Go", "language"),
		idxTool("claude-code", "Claude Code", "development_tools"),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("BuildIndexYAML: %v", err)
	}
	out := string(data)
	for _, want := range []string{
		"- name: language\n",
		"- name: go\n",
		"display_name: Go\n",
		"display_name: Claude Code\n",
		"manifest: go.yaml\n",
	} {
		if !containsLine(out, want) {
			t.Errorf("无歧义标量必须保持裸写，缺少 %q:\n%s", want, out)
		}
	}
}

func containsLine(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
