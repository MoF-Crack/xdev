// index_test.go — 票 18 / P2-15：index.yaml 生成与解析的单元测试。
package manifest

import (
	"strings"
	"testing"
	"time"
)

func idxTool(name, display, cat string) *Tool {
	return &Tool{Name: name, DisplayName: display, Category: cat}
}

// TestBuildIndexYAML_GroupsSortsAndDates：生成规则必须确定性——
// 分类按规范顺序、分类内按名字排序、日期取传入值（可重现）。
func TestBuildIndexYAML_GroupsSortsAndDates(t *testing.T) {
	tools := []*Tool{
		idxTool("zed", "Zed", "development_tools"),
		idxTool("go", "Go", "language"),
		idxTool("claude-code", "Claude Code", "development_tools"),
		idxTool("python", "Python", "language"),
		idxTool("uv", "UV", "package-manager"),
	}
	data, err := BuildIndexYAML(tools, time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, `generated: "2026-09-13"`) {
		t.Errorf("missing/incorrect generated date:\n%s", out)
	}
	// language 必须排在 development_tools 之前（规范顺序），且各自内部按名字升序。
	iLang := strings.Index(out, "- name: language")
	iDev := strings.Index(out, "- name: development_tools")
	iPkg := strings.Index(out, "- name: package-manager")
	if !(iLang < iPkg && iPkg < iDev) {
		t.Errorf("category order wrong: language=%d package-manager=%d development_tools=%d", iLang, iPkg, iDev)
	}
	if strings.Index(out, "- name: go\n") > strings.Index(out, "- name: python\n") {
		t.Errorf("tools are not sorted inside a category:\n%s", out)
	}
	if strings.Index(out, "- name: claude-code\n") > strings.Index(out, "- name: zed\n") {
		t.Errorf("development_tools not sorted:\n%s", out)
	}
	if !strings.Contains(out, "display_name: Development Tools") {
		t.Errorf("unknown-but-known category display name missing:\n%s", out)
	}
}

// TestBuildIndexYAML_EmptyCategoriesPreserved：已知分类即使没有工具也保留空分组，
// 以免重新生成时抹掉手工维护的占位（原 index.yaml 末尾的 system 就是这种）。
func TestBuildIndexYAML_EmptyCategoriesPreserved(t *testing.T) {
	data, err := BuildIndexYAML([]*Tool{idxTool("go", "Go", "language")}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "- name: system") || !strings.Contains(out, "tools: []") {
		t.Errorf("empty known categories must survive regeneration:\n%s", out)
	}
	idx, err := ParseIndexYAML(data)
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if got := idx.ManifestNames(); len(got) != 1 || got[0] != "go.yaml" {
		t.Errorf("ManifestNames = %v, want [go.yaml]", got)
	}
}

// TestBuildIndexYAML_UnknownCategoryAndDisplayFallback：未知分类按名称追加在后，
// 展示名回退为「下划线转空格 + 首字母大写」。
func TestBuildIndexYAML_UnknownCategoryAndDisplayFallback(t *testing.T) {
	data, err := BuildIndexYAML([]*Tool{idxTool("mytool", "My Tool", "cloud_native")}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "- name: cloud_native") {
		t.Errorf("unknown category dropped:\n%s", out)
	}
	if !strings.Contains(out, "display_name: Cloud Native") {
		t.Errorf("display fallback wrong:\n%s", out)
	}
	iSys := strings.Index(out, "- name: system")
	iUnknown := strings.Index(out, "- name: cloud_native")
	if !(iSys < iUnknown) {
		t.Errorf("unknown categories must be appended after the canonical order (system=%d unknown=%d)", iSys, iUnknown)
	}
}

// TestBuildIndexYAML_SkipsNamelessAndDefaults：无 name 的条目（非工具 YAML）被跳过，
// 空 display_name 回退为 tool 名，空 category 归入 system。
func TestBuildIndexYAML_SkipsNamelessAndDefaults(t *testing.T) {
	data, err := BuildIndexYAML([]*Tool{
		nil,
		{Name: ""},
		{Name: "bare"},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndexYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	names := idx.ManifestNames()
	if len(names) != 1 || names[0] != "bare.yaml" {
		t.Fatalf("ManifestNames = %v, want [bare.yaml]", names)
	}
	found := false
	for _, c := range idx.Categories {
		for _, tool := range c.Tools {
			if tool.Name == "bare" {
				if tool.DisplayName != "bare" {
					t.Errorf("display_name = %q, want fallback to the tool name", tool.DisplayName)
				}
				if c.Name != "system" {
					t.Errorf("category = %q, want system fallback", c.Name)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("bare tool missing from index:\n%s", data)
	}
}

// TestParseIndexYAML_RejectsGarbage：损坏的索引必须报错（守卫据此把漂移变成红）。
func TestParseIndexYAML_RejectsGarbage(t *testing.T) {
	if _, err := ParseIndexYAML([]byte("categories: [\n")); err == nil {
		t.Error("malformed YAML must fail to parse")
	}
}
