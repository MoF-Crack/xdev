package manifest

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// 守卫测试 28-1（审计 D-06 / P1-11）：loadMergedCatalog 的磁盘单文件
// 读失败 / 解析失败分支必须"保留容错语义（跳过该条）但发出可见告警"。
//
// 修复前：两个分支都是裸 `return nil`，坏 manifest 完全不可见——用户只
// 看到某个工具从 available 里消失，没有任何线索。
// 修复后：仍然只跳过这一条（不影响其余文件与嵌入基线），但 stderr 上必须
// 出现 [manifest] WARNING: skip unparseable <file>: <err>。

// TestLoadMergedCatalog_BadDiskManifestWarnsAndKeepsEmbeddedBase 覆盖解析
// 失败分支：坏 YAML 被跳过 + 告警可见 + 嵌入基线与同目录好文件都仍生效。
func TestLoadMergedCatalog_BadDiskManifestWarnsAndKeepsEmbeddedBase(t *testing.T) {
	oldFS := embeddedFS
	defer func() { embeddedFS = oldFS }()
	InitEmbedded(fstest.MapFS{
		"embtool.yaml": &fstest.MapFile{Data: []byte(`name: embtool
display_name: Embedded Base
category: language
versions:
  - version: "1.0.0"
`)},
	})

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	dir := t.TempDir()
	// 坏 YAML：flow sequence 未闭合 -> yaml.Unmarshal 必然报错。
	bad := "name: broken\ndisplay_name: Broken\ncategory: language\nversions: [oops\n  - version: \"1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	// 同一个目录里的好文件必须照常加载（证明容错语义没被改成整体失败）。
	good := `name: disktool
display_name: Disk Tool
category: language
versions:
  - version: "2.0.0"
`
	if err := os.WriteFile(filepath.Join(dir, "disktool.yaml"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	// 磁盘覆盖内置工具：只允许追加新版本，其余字段仍是嵌入优先（N-20 语义）。
	override := `name: embtool
display_name: Disk Override Must Not Win
category: language
versions:
  - version: "9.9.9"
`
	if err := os.WriteFile(filepath.Join(dir, "embtool.yaml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog must tolerate one bad manifest, got error: %v", err)
	}

	// 1) 坏的那条被跳过。
	if _, ok := cat.Tool("broken"); ok {
		t.Errorf("unparseable manifest must be skipped, but 'broken' is in catalog: %v", toolNames(cat))
	}
	// 2) 容错：同目录好文件照常加载。
	if _, ok := cat.Tool("disktool"); !ok {
		t.Errorf("good disk manifest missing (tolerance must be per-file): %v", toolNames(cat))
	}
	// 3) 嵌入基线仍生效。
	emb, ok := cat.Tool("embtool")
	if !ok {
		t.Fatalf("embedded baseline tool missing: %v", toolNames(cat))
	}
	if emb.DisplayName != "Embedded Base" {
		t.Errorf("DisplayName = %q, want embedded 'Embedded Base' (embedded-first, N-20)", emb.DisplayName)
	}
	got := map[string]bool{}
	for _, v := range emb.VersionList {
		got[v.Version] = true
	}
	if !got["1.0.0"] || !got["9.9.9"] {
		t.Errorf("merged versions = %v, want embedded 1.0.0 + appended disk 9.9.9", emb.VersionList)
	}
	// 4) 告警必须真的发出（这是 D-06 的核心：可见性）。
	out := buf.String()
	if !strings.Contains(out, "WARNING: skip unparseable broken.yaml") {
		t.Errorf("missing D-06 warning in log output:\n%s", out)
	}
}

// TestLoadMergedCatalog_UnreadableDiskManifestWarns 覆盖读失败分支。
//
// 构造手法：ReadDir 能列出、但 ReadFile 打不开的条目——悬空符号链接
// （名字是 .yaml，目标不存在）。Windows 上创建符号链接需要开发者模式或
// 管理员权限，失败即跳过（该分支在 Linux CI 上被覆盖）。
func TestLoadMergedCatalog_UnreadableDiskManifestWarns(t *testing.T) {
	oldFS := embeddedFS
	defer func() { embeddedFS = oldFS }()
	InitEmbedded(fstest.MapFS{
		"embtool.yaml": &fstest.MapFile{Data: []byte(`name: embtool
display_name: Embedded Base
category: language
versions:
  - version: "1.0.0"
`)},
	})

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	dir := t.TempDir()
	link := filepath.Join(dir, "dangling.yaml")
	if err := os.Symlink(filepath.Join(dir, "no-such-target.yaml"), link); err != nil {
		t.Skipf("dangling symlink unavailable on this host (Windows without Developer Mode?): %v", err)
	}
	// 前置确认：这个条目确实"列得出、读不了"，否则测不到目标分支。
	if _, err := os.ReadFile(link); err == nil {
		t.Fatalf("test precondition failed: %s is readable", link)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("unreadable manifest must not fail the whole load: %v", err)
	}
	if _, ok := cat.Tool("embtool"); !ok {
		t.Errorf("embedded baseline tool missing: %v", toolNames(cat))
	}
	out := buf.String()
	if !strings.Contains(out, "WARNING: skip unreadable dangling.yaml") {
		t.Errorf("missing D-06 warning for unreadable file in log output:\n%s", out)
	}
}
