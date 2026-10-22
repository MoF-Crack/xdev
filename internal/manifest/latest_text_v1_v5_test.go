package manifest

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 第八轮审计 V1–V5 的永久守卫（由审计探针转化而来）。
//
// 这些缺陷**全部出在第七轮 F-29 引入的文本级 latest 改写器上**，是同一段代码
// 的多轮打磨：V1 插入粘连、V2 等价键写法不识别、V3 无标量转义、V4 丢引号风格、
// V5 indexDisplayName 按字节切分破坏非 ASCII。

// V1：插入路径在前导块末行无换行时不得粘连（否则 latest 键根本不存在）。
func TestSetLatestInLines_InsertNeverGluesOntoPreviousLine(t *testing.T) {
	for _, in := range []string{"# only a comment", "# a\n# b", "---", "---\n# t"} {
		out, changed := SetLatestInLines([]byte(in), "2.0.0")
		var m struct {
			Latest string `yaml:"latest"`
		}
		err := yaml.Unmarshal(out, &m)
		if err != nil {
			t.Errorf("in=%q → 产出非法 YAML: %v (out=%q)", in, err, string(out))
			continue
		}
		if changed && m.Latest != "2.0.0" {
			t.Errorf("V1 复现：changed=true 但 latest=%q（out=%q）——新键与上一行粘连", m.Latest, string(out))
		}
	}
}

// V2：所有等价的键写法都必须被识别（否则产出重复键 → 文件损坏）。
func TestSetLatestInLines_RecognisesEquivalentKeySpellings(t *testing.T) {
	for _, in := range []string{
		"name: demo\nlatest : 1.0.0\n",
		"name: demo\n\"latest\": 1.0.0\n",
		"name: demo\n'latest': 1.0.0\n",
		"name: demo\nlatest  : 1.0.0\n",
		"\ufefflatest: 1.0.0\nname: demo\n",
	} {
		out, changed := SetLatestInLines([]byte(in), "2.0.0")
		if !changed {
			t.Errorf("in=%q 未被识别为已有 latest（应就地改写而非插入）", in)
			continue
		}
		var m struct {
			Latest string `yaml:"latest"`
		}
		if err := yaml.Unmarshal(out, &m); err != nil {
			t.Errorf("V2 复现：in=%q 改写后 YAML 非法（重复键）：%v (out=%q)", in, err, string(out))
			continue
		}
		if m.Latest != "2.0.0" {
			t.Errorf("in=%q → latest=%q, want 2.0.0 (out=%q)", in, m.Latest, string(out))
		}
	}
}

// V3：特殊字符的值必须被安全引用，不得产出非法 YAML。
func TestSetLatestInLines_EscapesSpecialScalars(t *testing.T) {
	// 注意：空串不是合法目标值 —— SetLatestInLines 对 newLatest=="" 直接返回
	// (src, false)，调用方（refresh）也不会传空值，故不在本用例范围内。
	for _, v := range []string{"a: b", "*glob", "- dash", "[bracket", "\"quote", "1.0 #beta", "yes", "~", " lead", "trail "} {
		in := []byte("name: demo\nlatest: 1.0.0\n")
		out, _ := SetLatestInLines(in, v)
		var m struct {
			Latest string `yaml:"latest"`
		}
		if err := yaml.Unmarshal(out, &m); err != nil {
			t.Errorf("V3 复现：值 %q 产出非法 YAML: %v (out=%q)", v, err, string(out))
			continue
		}
		// 读回必须仍是**字符串**且内容一致（防止 "yes"/"1.0" 被解析成 bool/number）。
		if m.Latest != v {
			t.Errorf("值 %q 读回为 %q（类型或内容被改变）(out=%q)", v, m.Latest, string(out))
		}
	}
}

// V4：原有引号风格必须被沿用。
func TestSetLatestInLines_PreservesQuoteStyle(t *testing.T) {
	out, _ := SetLatestInLines([]byte("name: demo\nlatest: \"1.0.0\"\n"), "2.0.0")
	if !strings.Contains(string(out), `latest: "2.0.0"`) {
		t.Errorf("双引号风格丢失：out=%q", string(out))
	}
	out2, _ := SetLatestInLines([]byte("name: demo\nlatest: '1.0.0'\n"), "2.0.0")
	if !strings.Contains(string(out2), "latest: '2.0.0'") {
		t.Errorf("单引号风格丢失：out=%q", string(out2))
	}
}

// V5：indexDisplayName 必须按 rune 处理非 ASCII 分类名。
func TestIndexDisplayName_HandlesNonASCII(t *testing.T) {
	for _, s := range []string{"工具", "über-build", "Ωmega", "日本語ツール"} {
		got := indexDisplayName(s)
		if strings.ContainsRune(got, '\uFFFD') {
			t.Errorf("V5 复现：indexDisplayName(%q) = %q 含替换字符（按字节切分破坏了 UTF-8）", s, got)
		}
		if !isValidUTF8Str(got) {
			t.Errorf("indexDisplayName(%q) = %q 不是合法 UTF-8", s, got)
		}
	}
	// ASCII 行为不变（首字母大写 + 下划线转空格）。
	if got := indexDisplayName("development_tools"); got != "Development Tools" {
		t.Errorf("indexDisplayName(development_tools) = %q, want Development Tools", got)
	}
}

func isValidUTF8Str(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}
