package manifest

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// 第九轮审计 P2-2 守卫：`yamlScalarFor` 必须转义控制字符。
//
// 缺陷现场：`needsYAMLQuoting` 对 `\n`/`\r`/`\t` 返回 true（于是给标量加双引号），
// 但 `yamlScalarFor` 只转义 `\` 与 `"` —— 拼出来的是 `"a<真实换行>b"`。YAML 双引号
// 标量跨行会被**折叠成空格**：文件合法、值变了。实测：
//
//	latest = "1.0.0\n2.0.0"  → 解析回来是 "1.0.0 2.0.0"
//	display_name = "a\nb"    → 从两行变一行
//
// 其它 `< 0x20` 控制字符更糟：旧实现裸写 NUL → `yaml: control characters are not allowed`
// （产出**不可解析**的文件）。这条是 Round 9 A-17 的残留（A-17 只保证了 `:`/`#` 等
// 指示符的往返，漏了控制字符）。
func TestYAMLScalarFor_ControlCharsRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"newline", "1.0.0\n2.0.0"},
		{"CRLF", "x\r\ny"},
		{"CR only", "x\ry"},
		{"tab", "a\tb"},
		{"NUL", "1.0.0\x00x"},
		{"bell", "a\x07b"},
		{"DEL", "a\x7fb"},
		{"mixed", "a\nb\r\nc\td\x01e"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 三种原值形态都不得丢失/改变内容。
			for _, oldRaw := range []string{"1.0.0", "'1.0.0'", `"1.0.0"`} {
				scalar := yamlScalarFor(tc.value, oldRaw)

				// 产出必须是**单行**（不得含真实换行 —— 那会被 YAML 折叠成空格）。
				if strings.ContainsAny(scalar, "\n\r") {
					t.Errorf("oldRaw=%q: 产出含真实换行，YAML 会把它折叠成空格: %q", oldRaw, scalar)
				}

				// 必须能被真实 YAML 解析器读回，且值逐字节相同。
				doc := "v: " + scalar + "\n"
				var out struct {
					V string `yaml:"v"`
				}
				if err := yaml.Unmarshal([]byte(doc), &out); err != nil {
					t.Errorf("oldRaw=%q: 产出无法被 YAML 解析: %v (scalar=%q)", oldRaw, err, scalar)
					continue
				}
				if out.V != tc.value {
					t.Errorf("oldRaw=%q: 往返丢内容 —— 得到 %q, want %q (scalar=%q)",
						oldRaw, out.V, tc.value, scalar)
				}
			}
		})
	}
}

// 回归：普通标量的形态不得因 P2-2 的改动而变化（该裸写仍裸写、该引号仍引号）。
func TestYAMLScalarFor_PlainValuesUnchangedByControlCharFix(t *testing.T) {
	cases := []struct {
		value, oldRaw, want string
	}{
		{"1.0.0", "1.0.0", "1.0.0"},
		{"1.0.0", "'1.0.0'", "'1.0.0'"},
		{`1.0.0`, `"1.0.0"`, `"1.0.0"`},
		// 单引号原值 + 含单引号的新值：按 YAML 规则双写。
		{"a'b", "'x'", "'a''b'"},
		// 双引号原值 + 含反斜杠的新值：必须转义反斜杠。
		{`a\b`, `"x"`, `"a\\b"`},
	}
	for _, tc := range cases {
		if got := yamlScalarFor(tc.value, tc.oldRaw); got != tc.want {
			t.Errorf("yamlScalarFor(%q, %q) = %q, want %q", tc.value, tc.oldRaw, got, tc.want)
		}
	}
}

// 单引号原值 + 含控制字符的新值：必须切到双引号形态（单引号标量不允许控制字符）。
func TestYAMLScalarFor_SingleQuotedFallsBackToDoubleQuoted(t *testing.T) {
	got := yamlScalarFor("a\nb", "'x'")
	if !strings.HasPrefix(got, `"`) {
		t.Errorf("单引号原值遇到控制字符时应改用双引号形态, got %q", got)
	}
	var out struct {
		V string `yaml:"v"`
	}
	if err := yaml.Unmarshal([]byte("v: "+got+"\n"), &out); err != nil {
		t.Fatalf("产出无法解析: %v (scalar=%q)", err, got)
	}
	if out.V != "a\nb" {
		t.Errorf("往返丢内容: got %q, want %q", out.V, "a\nb")
	}
}

// index 侧：display_name 含换行必须往返（A-17 的消费点）。
func TestBuildIndexYAML_ControlCharsInDisplayNameRoundTrip(t *testing.T) {
	tools := []*Tool{{
		Name:        "demo",
		DisplayName: "a\nb",
		Category:    "misc",
	}}
	data, err := BuildIndexYAML(tools, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("BuildIndexYAML: %v", err)
	}
	parsed, err := ParseIndexYAML(data)
	if err != nil {
		t.Fatalf("产出无法被 ParseIndexYAML 读回: %v\n%s", err, string(data))
	}
	var got string
	for _, cat := range parsed.Categories {
		for _, tool := range cat.Tools {
			if tool.Name == "demo" {
				got = tool.DisplayName
			}
		}
	}
	if got != "a\nb" {
		t.Errorf("display_name 往返丢内容: got %q, want %q\nyaml:\n%s", got, "a\nb", string(data))
	}
}
