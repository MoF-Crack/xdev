package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第七轮审计 F-29 守卫：更新 latest 必须**保住用户手写注释**。
//
// 缺陷现场：`xdev refresh` 用 manifest.MarshalTool 整文件重写来更新 latest，
// 把用户手写 manifest 里的全部注释抹掉（与票 17 已确立的「文本级外科写」
// 原则冲突，见 UpdateArchiveSHA256InFile 的整段论证）。
func TestSetLatestInLines_PreservesComments(t *testing.T) {
	src := []byte(`# 这是用户手写的说明注释
# 第二行注释
name: demo
# 关于 latest 的说明
latest: 1.0.0
versions:
  # 1.0 是最初的版本
  - version: 1.0.0
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/demo-1.0.0.zip
  # 2.0 加了新特性
  - version: 2.0.0
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/demo-2.0.0.zip
`)

	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected a change when latest differs")
	}
	s := string(out)
	if !strings.Contains(s, "latest: 2.0.0") {
		t.Errorf("latest was not updated:\n%s", s)
	}
	// 关键断言：注释必须全部存活。
	for _, want := range []string{
		"# 这是用户手写的说明注释",
		"# 第二行注释",
		"# 关于 latest 的说明",
		"# 1.0 是最初的版本",
		"# 2.0 加了新特性",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("F-29 复现：注释被抹掉: %q\n---\n%s", want, s)
		}
	}
	// 行尾注释也要保留。
	srcWithTrailing := []byte("name: demo\nlatest: 1.0.0  # 当前稳定版\n")
	out2, _ := SetLatestInLines(srcWithTrailing, "2.0.0")
	if !strings.Contains(string(out2), "latest: 2.0.0  # 当前稳定版") {
		t.Errorf("trailing comment lost:\n%s", string(out2))
	}
}

// 幂等：目标值与现值相同时不产生改动（避免无谓写盘）。
func TestSetLatestInLines_NoChangeWhenAlreadyCurrent(t *testing.T) {
	src := []byte("name: demo\nlatest: 2.0.0\n")
	if _, changed := SetLatestInLines(src, "2.0.0"); changed {
		t.Error("expected no change when latest already equals the target")
	}
}

// 不得误改**嵌套**的 latest（只有顶层字段才是工具的默认版本）。
func TestSetLatestInLines_IgnoresIndentedLatest(t *testing.T) {
	src := []byte("name: demo\nlatest: 1.0.0\nversions:\n  - version: 1.0.0\n    latest: 9.9.9\n")
	out, changed := SetLatestInLines(src, "2.0.0")
	if !changed {
		t.Fatal("expected change")
	}
	s := string(out)
	if !strings.Contains(s, "latest: 2.0.0") {
		t.Errorf("top-level latest not updated:\n%s", s)
	}
	if !strings.Contains(s, "    latest: 9.9.9") {
		t.Errorf("nested latest must be untouched:\n%s", s)
	}
}

// UpdateLatestInFile：真实文件上的端到端（含锁与原子写）。
func TestUpdateLatestInFile_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "demo.yaml")
	original := "# keep me\nname: demo\nlatest: 1.0.0\n"
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
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
	if !strings.Contains(string(got), "# keep me") {
		t.Errorf("comment lost on disk:\n%s", string(got))
	}
	if !strings.Contains(string(got), "latest: 2.0.0") {
		t.Errorf("latest not updated on disk:\n%s", string(got))
	}
}
