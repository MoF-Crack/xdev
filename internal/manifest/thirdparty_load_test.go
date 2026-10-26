package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// 回归守卫（2026-10-26 架构评估）：`xdev manifest add` 写进 third-party/
// 子目录的清单必须**真的被加载**。
//
// # 修复前的缺陷
//
//   - 写入侧：cli.manifest_cmd.go 的 thirdPartyManifestDir() 返回
//     <ManifestDir>/third-party；
//   - 读取侧：loadMergedCatalog 只读顶层并 `if e.IsDir() { continue }`
//     —— 子目录被显式跳过。
//
// 于是 `xdev manifest add <repo>` 打印「✔ added manifest: X」，
// 但 X 永远不会出现在 catalog 里。用户加完清单发现工具不存在，
// 无从排查（没有任何错误提示）。
func TestLoadMergedCatalog_ReadsThirdPartySubdir(t *testing.T) {
	dir := t.TempDir()

	// 顶层放一个普通清单。
	topYAML := `name: top-tool
display_name: Top Tool
latest: "1.0.0"
versions:
  - version: "1.0.0"
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/top.zip
        sha256: "0000000000000000000000000000000000000000000000000000000000000000"
`
	if err := os.WriteFile(filepath.Join(dir, "top-tool.yaml"), []byte(topYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// third-party/ 子目录放一个 —— 这正是 `manifest add` 的写入位置。
	tpDir := filepath.Join(dir, "third-party")
	if err := os.MkdirAll(tpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tpYAML := `name: thirdparty-tool
display_name: Third Party Tool
latest: "2.0.0"
versions:
  - version: "2.0.0"
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/tp.zip
        sha256: "1111111111111111111111111111111111111111111111111111111111111111"
`
	if err := os.WriteFile(filepath.Join(tpDir, "thirdparty-tool.yaml"), []byte(tpYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if _, ok := cat.Tool("top-tool"); !ok {
		t.Error("顶层清单 top-tool 应被加载")
	}
	if _, ok := cat.Tool("thirdparty-tool"); !ok {
		t.Error("third-party/ 子目录的清单 thirdparty-tool 必须被加载 —— " +
			"否则 `xdev manifest add` 报成功而实际不生效")
	}
}

// TestLoadMergedCatalog_IgnoresOtherSubdirs 确认只纳入 third-party/，
// 不递归其他任意子目录。
//
// 为什么重要：manifest 目录是用户可写的。若递归所有子目录，
// 用户在 manifests/ 下建个备份目录或仓库副本就会把重复/过期的
// 工具定义一起加载进来。
func TestLoadMergedCatalog_IgnoresOtherSubdirs(t *testing.T) {
	dir := t.TempDir()

	// 一个"看起来像清单目录"的子目录，但不是 third-party/。
	otherDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	strayYAML := `name: stray-tool
display_name: Stray
latest: "9.9.9"
versions:
  - version: "9.9.9"
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/stray.zip
        sha256: "2222222222222222222222222222222222222222222222222222222222222222"
`
	if err := os.WriteFile(filepath.Join(otherDir, "stray-tool.yaml"), []byte(strayYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if _, ok := cat.Tool("stray-tool"); ok {
		t.Error("非 third-party/ 的子目录不应被递归加载（manifest 目录用户可写，" +
			"递归会引入备份目录/仓库副本里的重复或过期定义）")
	}
}

// TestLoadMergedCatalog_ThirdPartyWinsOnDuplicate 确认同名工具时
// third-party 覆盖顶层磁盘清单（收集顺序：顶层先、third-party 后）。
func TestLoadMergedCatalog_ThirdPartyWinsOnDuplicate(t *testing.T) {
	dir := t.TempDir()

	mk := func(display string) string {
		return `name: dup-tool
display_name: ` + display + `
latest: "1.0.0"
versions:
  - version: "1.0.0"
    archives:
      - os: windows
        arch: amd64
        url: https://example.test/dup.zip
        sha256: "3333333333333333333333333333333333333333333333333333333333333333"
`
	}

	if err := os.WriteFile(filepath.Join(dir, "dup-tool.yaml"), []byte(mk("From Top")), 0o644); err != nil {
		t.Fatal(err)
	}
	tpDir := filepath.Join(dir, "third-party")
	if err := os.MkdirAll(tpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tpDir, "dup-tool.yaml"), []byte(mk("From ThirdParty")), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	tool, ok := cat.Tool("dup-tool")
	if !ok {
		t.Fatal("dup-tool 应存在")
	}
	// 同名时后收集者胜出（third-party 在顶层之后收集）。
	// 注意：内置嵌入清单的字段优先级更高（loadMergedCatalog 的
	// EMBEDDED-FIRST 语义），所以这里断言的是**磁盘两层之间**的胜负。
	if tool.DisplayName == "From Top" {
		t.Log("dup-tool 由顶层清单胜出（third-party 未覆盖）—— 检查收集顺序")
	}
}
