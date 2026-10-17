package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 第八轮审计自纠守卫：SetLatestInLines 的三个边界缺陷。
//
// 这三个缺陷都是**第七轮 F-29 修复自身引入的**，由第八轮探针抓出：
//  1. CRLF 文件被改成混合行尾；
//  2. 追加到第 0 行会破坏以 `---` 开头的文档结构（新键落进独立文档）；
//  3. UTF-8 BOM 被挤到字节 0 之外。

// 缺陷 1：CRLF 文件必须保持 CRLF，不得出现 `\n` 与 `\r\n` 混合。
func TestSetLatestInLines_CRLFFileKeepsCRLF(t *testing.T) {
	src := []byte("# comment\r\nname: demo\r\nlatest: 1.0.0\r\nversions: []\r\n")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change")
	}
	got := string(out)
	if strings.Contains(got, "\r\r") {
		t.Errorf("produced a double CR: %q", got)
	}
	// 不得出现「裸 \n」（即前面不是 \r 的 \n）。
	for i := 0; i < len(got); i++ {
		if got[i] == '\n' && (i == 0 || got[i-1] != '\r') {
			t.Errorf("mixed line endings: bare \\n at offset %d in %q", i, got)
			break
		}
	}
	if !strings.Contains(got, "latest: 2.0.0\r\n") {
		t.Errorf("latest line lost its CRLF: %q", got)
	}
	// 其余内容逐字节不变。
	if !strings.Contains(got, "# comment\r\nname: demo\r\n") {
		t.Errorf("leading content altered: %q", got)
	}
}

// 缺陷 2：以 `---` 开头的文档，追加 latest 后必须仍在**同一个** YAML 文档里。
func TestSetLatestInLines_DocSeparatorStartStaysOneDocument(t *testing.T) {
	src := []byte("---\nname: demo\nversions: []\n")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change")
	}
	var v map[string]any
	if err := yaml.Unmarshal(out, &v); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%q", err, string(out))
	}
	if _, ok := v["name"]; !ok {
		t.Errorf("'name' unreachable — latest was inserted as a separate document: %q", string(out))
	}
	if got, ok := v["latest"]; !ok || got != "2.0.0" {
		t.Errorf("latest = %v (present=%v), want 2.0.0 in the first document: %q", got, ok, string(out))
	}
}

// 缺陷 3：带 UTF-8 BOM 的文件，BOM 必须仍在字节 0。
func TestSetLatestInLines_BOMStaysAtOffsetZero(t *testing.T) {
	src := []byte("\ufeffname: demo\nversions: []\n")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change")
	}
	if !strings.HasPrefix(string(out), "\ufeff") {
		t.Errorf("BOM no longer at offset 0: %q", string(out))
	}
	// 整份文件里 BOM 只能出现一次。
	if strings.Count(string(out), "\ufeff") != 1 {
		t.Errorf("BOM appears %d times: %q", strings.Count(string(out), "\ufeff"), string(out))
	}
	var v map[string]any
	if err := yaml.Unmarshal(out, &v); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	if _, ok := v["latest"]; !ok {
		t.Errorf("latest missing: %q", string(out))
	}
}

// 追加时新增行应沿用邻近行的行尾风格（CRLF 文件里的新行也是 CRLF）。
func TestSetLatestInLines_AppendedLineMatchesLineEndings(t *testing.T) {
	src := []byte("# note\r\nname: demo\r\nversions: []\r\n")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change")
	}
	got := string(out)
	if !strings.Contains(got, "latest: 2.0.0\r\n") {
		t.Errorf("appended line did not use CRLF: %q", got)
	}
}

// 无末尾换行的文件：改写后不得凭空多出换行（且仍是合法 YAML）。
func TestSetLatestInLines_NoTrailingNewlinePreserved(t *testing.T) {
	src := []byte("name: demo\nlatest: 1.0.0")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change")
	}
	got := string(out)
	if strings.HasSuffix(got, "\n") {
		t.Errorf("trailing newline was added to a file that had none: %q", got)
	}
	if got != "name: demo\nlatest: 2.0.0" {
		t.Errorf("got %q, want %q", got, "name: demo\nlatest: 2.0.0")
	}
}

// 引号包裹的值：幂等比较必须能识别 `latest: "2.0.0"` 已是目标值。
func TestSetLatestInLines_QuotedValueIsIdempotent(t *testing.T) {
	for _, src := range []string{
		"name: demo\nlatest: \"2.0.0\"\n",
		"name: demo\nlatest: '2.0.0'\n",
	} {
		if _, changed := SetLatestInLines([]byte(src), "2.0.0"); changed {
			t.Errorf("quoted value %q should be recognised as already current", src)
		}
	}
	// 但引号值不等于目标时仍要改写，且**沿用原有引号风格**（第八轮审计 V4：
	// 丢掉引号是格式回归，且 `"1.0"` 与 `1.0` 在 YAML 里类型不同）。
	out, changed := SetLatestInLines([]byte("name: demo\nlatest: \"1.0.0\"\n"), "2.0.0")
	if !changed {
		t.Fatal("expected a change for a different quoted value")
	}
	if !strings.Contains(string(out), `latest: "2.0.0"`) {
		t.Errorf("双引号风格未被沿用：got %q", string(out))
	}
	// 单引号同理。
	out2, _ := SetLatestInLines([]byte("name: demo\nlatest: '1.0.0'\n"), "2.0.0")
	if !strings.Contains(string(out2), "latest: '2.0.0'") {
		t.Errorf("单引号风格未被沿用：got %q", string(out2))
	}
	// 无引号的原值 + 不需要引号的新值 → 保持无引号（不做无谓的格式改动）。
	out3, _ := SetLatestInLines([]byte("name: demo\nlatest: 1.0.0\n"), "2.0.0")
	if !strings.Contains(string(out3), "latest: 2.0.0\n") {
		t.Errorf("裸值被无谓地加了引号：got %q", string(out3))
	}
}

// 真实文件端到端：CRLF 文件经 UpdateLatestInFile 后仍是 CRLF 且内容正确。
func TestUpdateLatestInFile_CRLFRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(p, []byte("# c\r\nname: demo\r\nlatest: 1.0.0\r\nversions: []\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := UpdateLatestInFile(p, "2.0.0")
	if err != nil {
		t.Fatalf("UpdateLatestInFile: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\r\r") {
		t.Errorf("double CR on disk: %q", string(got))
	}
	for i := 0; i < len(got); i++ {
		if got[i] == '\n' && (i == 0 || got[i-1] != '\r') {
			t.Errorf("mixed line endings on disk at offset %d: %q", i, string(got))
			break
		}
	}
	if !strings.Contains(string(got), "# c\r\nname: demo\r\nlatest: 2.0.0\r\n") {
		t.Errorf("disk content wrong: %q", string(got))
	}
}
