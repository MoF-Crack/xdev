// archive_sha_text_test.go — 第三轮复核（修复后再复核）新增的守卫：
// 回填 sha256 **必须只改那一行**：注释、引号风格、缩进宽度、字段顺序、CRLF 行尾全部保留。
//
// 为什么必须有这条守卫：本批实测证明"parse → MarshalTool → 写回"会让 52 个 manifest 的
// 220 行注释归零、最坏文件仅 5% 行保留，使"重填哈希"的 PR 不可评审并毁掉文档注释。
package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 2 空格缩进风格 + 行内与顶部注释 + 混合引号
const sampleTwoSpace = `# 顶部说明：这条注释解释了为什么用该 URL（绝不能被回填抹掉）
name: sample2
display_name: Sample Two Space
category: build
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: linux
        arch: x86_64
        format: zip
        url: "https://example.com/a.zip"   # 行尾注释：也必须保留
        sha256: ""
        mirror_url: ""
        strip: 1
  - version: "2.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/b.zip
        sha256: ""
        strip: 0
`

const newHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSetArchiveSHA256InLines_PreservesEverything(t *testing.T) {
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(sampleTwoSpace), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v, want 1/[]", changed, missed)
	}
	got := string(out)

	// 1) 注释必须原样保留（含行尾注释）
	if !strings.Contains(got, "# 顶部说明：这条注释解释了为什么用该 URL（绝不能被回填抹掉）") {
		t.Error("顶部注释被抹掉")
	}
	if !strings.Contains(got, `url: "https://example.com/a.zip"   # 行尾注释：也必须保留`) {
		t.Error("行尾注释或引号风格被改动")
	}
	// 2) 只应改一行：行数不变、非目标行逐行一致
	if n1, n2 := len(strings.Split(sampleTwoSpace, "\n")), len(strings.Split(got, "\n")); n1 != n2 {
		t.Errorf("行数变化：%d → %d（应该只替换一行）", n1, n2)
	}
	inLines := strings.Split(sampleTwoSpace, "\n")
	outLines := strings.Split(got, "\n")
	diffs := 0
	for i := range inLines {
		if inLines[i] != outLines[i] {
			diffs++
		}
	}
	if diffs != 1 {
		t.Errorf("改动行数 = %d, want 1（只允许 sha256 那一行变）", diffs)
	}
	// 3) 目标字段已更新，且未误伤同名的其它版本/平台
	if !strings.Contains(got, "sha256: "+newHash) {
		t.Error("目标 sha256 未写入")
	}
	if strings.Count(got, `sha256: ""`) != 1 {
		t.Errorf("应只剩 1 处空 sha256（2.0.0/windows），实际 %d", strings.Count(got, `sha256: ""`))
	}
}

// 4 空格风格（仓库里 mingw/zig 等文件就是这个风格）同样只改一行。
func TestSetArchiveSHA256InLines_FourSpaceStyle(t *testing.T) {
	src := `name: sample4
versions:
    - version: 16.2.0
      stable: true
      archives:
        - os: windows
          arch: amd64
          format: 7z
          url: https://example.com/x.7z
          sha256: ""
          strip: 1
`
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "16.2.0", OS: "windows", Arch: "amd64", SHA256: newHash},
	})
	if err != nil || changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v err=%v", changed, missed, err)
	}
	if !strings.Contains(string(out), "          sha256: "+newHash) {
		t.Errorf("4 空格风格下缩进/值不对:\n%s", out)
	}
	inLines, outLines := strings.Split(src, "\n"), strings.Split(string(out), "\n")
	if len(inLines) != len(outLines) {
		t.Fatalf("行数变化 %d → %d", len(inLines), len(outLines))
	}
	for i := range inLines {
		if inLines[i] != outLines[i] && !strings.HasPrefix(strings.TrimSpace(inLines[i]), "sha256:") {
			t.Errorf("非目标行被改动（第 %d 行）: %q → %q", i+1, inLines[i], outLines[i])
		}
	}
}

// 缺失 sha256 行时必须**插入**（而不是报找不到），且不影响既有注释。
func TestSetArchiveSHA256InLines_InsertsWhenMissing(t *testing.T) {
	src := `name: inserttest
versions:
  - version: "1.0.0"
    archives:
      - os: linux
        arch: x86_64
        # 该条目历史上没有 sha256 行
        url: https://example.com/a.zip
        strip: 1
`
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil || changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v err=%v", changed, missed, err)
	}
	got := string(out)
	if !strings.Contains(got, "sha256: "+newHash) {
		t.Errorf("未插入 sha256:\n%s", got)
	}
	if !strings.Contains(got, "# 该条目历史上没有 sha256 行") {
		t.Error("插入时丢了注释")
	}
	// 插入在 url 之后，仍处在同一归档项内（缩进与 url 对齐）
	lines := strings.Split(got, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "sha256:") {
			if i == 0 || !strings.HasPrefix(lines[i-1], "        url:") {
				t.Errorf("插入位置不在 url 之后: prev=%q", lines[i-1])
			}
		}
	}
}

// 幂等：同一更新跑两次，第二次不应再有改动（防"每次 fill 都整篇重写"）。
func TestSetArchiveSHA256InLines_Idempotent(t *testing.T) {
	up := []ArchiveSHAUpdate{{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash}}
	once, changed, _, err := SetArchiveSHA256InLines([]byte(sampleTwoSpace), up)
	if err != nil || changed != 1 {
		t.Fatalf("first pass changed=%d err=%v", changed, err)
	}
	twice, changed2, missed2, err := SetArchiveSHA256InLines(once, up)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 != 0 || len(missed2) != 0 {
		t.Errorf("第二次应无改动，实际 changed=%d missed=%v", changed2, missed2)
	}
	if string(twice) != string(once) {
		t.Error("幂等性被破坏：内容发生变化")
	}
}

// CRLF 行尾必须保留（Windows 检出）。
func TestSetArchiveSHA256InLines_PreservesCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(sampleTwoSpace, "\n", "\r\n")
	out, changed, _, err := SetArchiveSHA256InLines([]byte(crlf), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	if strings.Count(string(out), "\r\n") != strings.Count(crlf, "\r\n") {
		t.Errorf("CRLF 数量变化：%d → %d", strings.Count(crlf, "\r\n"), strings.Count(string(out), "\r\n"))
	}
}

// 定位不到时必须**计入 missed**（而不是静默成功）。
func TestSetArchiveSHA256InLines_ReportsMisses(t *testing.T) {
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(sampleTwoSpace), []ArchiveSHAUpdate{
		{Version: "9.9.9", OS: "linux", Arch: "x86_64", SHA256: newHash},
		{Version: "1.0.0", OS: "darwin", Arch: "arm64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 || len(missed) != 2 {
		t.Fatalf("changed=%d missed=%d, want 0/2（定位不到必须报出来）", changed, len(missed))
	}
	if string(out) != sampleTwoSpace {
		t.Error("未命中时不得改动文件内容")
	}
}

// ---- 第四轮复核新增的边界用例（解析器自身的健壮性）----

// 版本列表之后的**其它顶层段**里若出现 `- os:` 形式的列表项，绝不能被当成目标。
// （加固前 verEnd 只认"下一个 - version:"，搜索范围会一路扩到文件末尾。）
func TestSetArchiveSHA256InLines_DoesNotLeakIntoLaterSections(t *testing.T) {
	src := `name: leaktest
versions:
  - version: "1.0.0"
    archives:
      - os: linux
        arch: x86_64
        format: zip
        url: https://example.com/a.zip
        sha256: ""
remote:
  targets:
    - os: linux
      arch: x86_64
      sha256: "SHOULD-NOT-BE-TOUCHED"
`
	// 目标版本 1.0.0 的归档项本身存在 → 正常更新，且 remote 段不得被改动
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil || changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v err=%v", changed, missed, err)
	}
	got := string(out)
	if !strings.Contains(got, `sha256: "SHOULD-NOT-BE-TOUCHED"`) {
		t.Error("顶层 remote 段被误改（verEnd 越界）")
	}
	if !strings.Contains(got, "sha256: "+newHash) {
		t.Error("目标归档项未更新")
	}

	// 反向：版本 2.0.0 不存在，且 remote 段有同 os/arch → 必须 missed，绝不能改 remote 段
	out2, changed2, missed2, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "2.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed2 != 0 || len(missed2) != 1 {
		t.Fatalf("changed=%d missed=%d, want 0/1（版本不存在时必须报 missed）", changed2, len(missed2))
	}
	if string(out2) != src {
		t.Error("未命中时文件被改动")
	}
}

// 关键区分点（加固前必红）：目标版本的归档项里**没有** linux/x86_64，而版本列表之后的
// 顶层段里有 → 加固前 verEnd 会扩到文件末尾，把这个"别处的同名条目"当成目标改掉；
// 加固后必须在版本块边界处停下并报 missed。
func TestSetArchiveSHA256InLines_NoCrossSectionSubstitution(t *testing.T) {
	src := `name: crosssection
versions:
  - version: "1.0.0"
    archives:
      - os: darwin
        arch: arm64
        format: zip
        url: https://example.com/mac.zip
        sha256: ""
remote:
  targets:
    - os: linux
      arch: x86_64
      sha256: "UNRELATED-SECTION"
`
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 || len(missed) != 1 {
		t.Fatalf("changed=%d missed=%v —— 不得用其它段的同名条目顶替（want 0/1）", changed, missed)
	}
	if string(out) != src {
		t.Error("跨段顶替：文件被改动")
	}
	if strings.Contains(string(out), newHash) {
		t.Error("哈希被写进了 remote 段（跨段顶替）")
	}
}

// 归档项内部的**嵌套映射**里的 url:/sha256: 不得被误认成目标字段。
// （加固前只看 TrimSpace 前缀，嵌套的 `sha256:` 会被当成目标行，把哈希写到更深缩进上，
//
//	而真正的字段仍是空 → 语义损坏且静默。）
func TestSetArchiveSHA256InLines_IgnoresNestedFields(t *testing.T) {
	src := `name: nested
versions:
  - version: "1.0.0"
    archives:
      - os: linux
        arch: x86_64
        format: tar.gz
        url: https://example.com/a.tar.gz
        sha256: ""
        hooks:
          post_install:
            url: https://nested.example/hook
            sha256: "NESTED-VALUE"
`
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil || changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v err=%v", changed, missed, err)
	}
	got := string(out)
	if !strings.Contains(got, `sha256: "NESTED-VALUE"`) {
		t.Error("嵌套字段被改动")
	}
	// 目标必须写在归档项自身缩进（8 空格）上
	if !strings.Contains(got, "\n        sha256: "+newHash) {
		t.Errorf("目标 sha256 未写在归档项自身缩进上:\n%s", got)
	}
	if strings.Contains(got, "            sha256: "+newHash) {
		t.Error("哈希被写到嵌套层级（12 空格缩进）")
	}
}

// 注释里出现 "version 1.0.0" / "- os: linux" 之类的文字不得被当结构匹配。
func TestSetArchiveSHA256InLines_IgnoresCommentedStructure(t *testing.T) {
	src := `# 说明：下面这个块历史上是 - version: "1.0.0" - os: linux / arch: x86_64 的写法
name: commenttraps
versions:
  - version: "1.0.0"
    archives:
      - os: darwin
        arch: arm64
        sha256: ""
      # - os: linux
      #   arch: x86_64
      #   sha256: "COMMENTED"
      - os: linux
        arch: x86_64
        sha256: ""
`
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil || changed != 1 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v err=%v", changed, missed, err)
	}
	got := string(out)
	if !strings.Contains(got, `#   sha256: "COMMENTED"`) {
		t.Error("被注释掉的整块结构被改动")
	}
	if strings.Count(got, "sha256: "+newHash) != 1 {
		t.Errorf("目标 sha256 出现次数不对:\n%s", got)
	}
}

// 版本值的三种写法（裸、双引号、单引号）都必须能定位。
func TestSetArchiveSHA256InLines_VersionQuoteStyles(t *testing.T) {
	for _, style := range []string{`1.0.0`, `"1.0.0"`, `'1.0.0'`} {
		src := "name: q\nversions:\n  - version: " + style + "\n    archives:\n" +
			"      - os: linux\n        arch: x86_64\n        sha256: \"\"\n"
		_, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
			{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
		})
		if err != nil || changed != 1 || len(missed) != 0 {
			t.Errorf("version 写法 %s 未命中：changed=%d missed=%v err=%v", style, changed, missed, err)
		}
	}
	// 前缀相近的版本不得被误匹配（1.0.0 vs 1.0.0-beta）
	src := "name: q\nversions:\n  - version: 1.0.0-beta\n    archives:\n" +
		"      - os: linux\n        arch: x86_64\n        sha256: \"\"\n"
	_, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 || len(missed) != 1 {
		t.Errorf("1.0.0 不应匹配 1.0.0-beta：changed=%d missed=%v", changed, missed)
	}
}

// "值本来就一样"的幂等情形不得被算作 missed（fill 用 missed 判定是否报失败）。
func TestSetArchiveSHA256InLines_AlreadyCorrectIsNotMissed(t *testing.T) {
	src := "name: same\nversions:\n  - version: \"1.0.0\"\n    archives:\n" +
		"      - os: linux\n        arch: x86_64\n        sha256: " + newHash + "\n"
	out, changed, missed, err := SetArchiveSHA256InLines([]byte(src), []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Errorf("changed=%d, want 0（已是目标值）", changed)
	}
	if len(missed) != 0 {
		t.Errorf("missed=%v, want 空（「值本来就一样」不是「找不到」，否则 fill 会误报失败）", missed)
	}
	if string(out) != src {
		t.Error("幂等情况不得改动内容")
	}
}

// 文件级入口：真实文件上验证"只改一行" + 原子写 + 加锁副作用（.lock 文件不残留内容语义）。
func TestUpdateArchiveSHA256InFile_MinimalDiffOnRealShape(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(p, []byte(sampleTwoSpace), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, missed, err := UpdateArchiveSHA256InFile(p, []ArchiveSHAUpdate{
		{Version: "1.0.0", OS: "linux", Arch: "x86_64", SHA256: newHash},
		{Version: "2.0.0", OS: "windows", Arch: "amd64", SHA256: newHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 || len(missed) != 0 {
		t.Fatalf("changed=%d missed=%v, want 2/[]", changed, missed)
	}
	after, _ := os.ReadFile(p)
	inLines := strings.Split(sampleTwoSpace, "\n")
	outLines := strings.Split(string(after), "\n")
	if len(inLines) != len(outLines) {
		t.Fatalf("行数变化 %d → %d", len(inLines), len(outLines))
	}
	diffs := 0
	for i := range inLines {
		if inLines[i] != outLines[i] {
			diffs++
		}
	}
	if diffs != 2 {
		t.Errorf("两次更新应只改 2 行，实际 %d", diffs)
	}
	if !strings.Contains(string(after), "# 顶部说明") {
		t.Error("文件级路径丢了注释")
	}
}
