// archive_installable_test.go — 票 18 / P2-29：可安装性判定与 LoadCatalog 标记。
//
// 背景：manifest 里可以存在「有 os/arch、没有 url/format」的占位条目（实测 mingw
// 三个版本全部如此）：`xdev available` 照常展示，用户点安装才失败。本文件锁定
// 判定语义与加载期标记（Tool.Installable）。
package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArchive_Installable(t *testing.T) {
	cases := []struct {
		name string
		a    Archive
		want bool
	}{
		{"url+format", Archive{URL: "https://x/y.zip", Format: "zip"}, true},
		{"missing url", Archive{Format: "zip"}, false},
		{"missing format", Archive{URL: "https://x/y"}, false},
		{"whitespace only", Archive{URL: "  ", Format: "  "}, false},
		{"npm kind needs no url", Archive{InstallKind: InstallKindNPM}, true},
		{"npm kind ignores missing format", Archive{InstallKind: InstallKindNPM, URL: "https://x"}, true},
		{"executable requires url+format", Archive{InstallKind: InstallKindExecutable, Format: "exe"}, false},
		{"script requires url+format", Archive{InstallKind: InstallKindScript, URL: "https://x", Format: "sh"}, true},
		{"unknown kind falls back to archive", Archive{InstallKind: "wat", URL: "https://x", Format: "zip"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Installable(); got != tc.want {
				t.Errorf("Installable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVersion_Installable_PlatformMatching(t *testing.T) {
	v := Version{Version: "1.0.0", Archives: []Archive{
		{OS: "windows", Arch: "amd64", URL: "https://x/w.zip", Format: "zip"},
		{OS: "any", Arch: "any", URL: "https://x/a.tar.gz", Format: "tar.gz"},
	}}
	if !v.Installable("windows", "amd64") {
		t.Error("windows/amd64 must be installable")
	}
	if !v.Installable("linux", "arm64") {
		t.Error("any/any archive must match every platform")
	}

	placeholder := Version{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64"}}}
	if placeholder.Installable("windows", "amd64") {
		t.Error("an archive without url/format must not count as installable (mingw placeholder shape)")
	}

	crossPlatform := Version{Version: "1.0.0", Archives: []Archive{
		{OS: "darwin", Arch: "arm64", URL: "https://x/m.pkg", Format: "pkg", InstallKind: InstallKindExecutable},
	}}
	if crossPlatform.Installable("linux", "amd64") {
		t.Error("a darwin-only version must not be installable on linux/amd64")
	}
	if !crossPlatform.Installable("darwin", "arm64") {
		t.Error("darwin/arm64 must be installable")
	}
}

func TestTool_HasInstallableVersion_And_HasCompleteArchive(t *testing.T) {
	placeholderOnly := &Tool{Name: "mingw", VersionList: []Version{
		{Version: "16.1.0", Archives: []Archive{{OS: "windows", Arch: "amd64"}}},
	}}
	if placeholderOnly.HasCompleteArchive() {
		t.Error("HasCompleteArchive must be false for a pure placeholder tool (platform-agnostic question)")
	}
	if placeholderOnly.HasInstallableVersion("windows", "amd64") {
		t.Error("HasInstallableVersion must be false when no archive is complete")
	}

	windowsOnly := &Tool{Name: "winapp", VersionList: []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://x/a.zip", Format: "zip"}}},
	}}
	if !windowsOnly.HasCompleteArchive() {
		t.Error("HasCompleteArchive must be true even for a windows-only tool (guard runs on Linux CI too)")
	}
	if !windowsOnly.HasInstallableVersion("windows", "amd64") {
		t.Error("windows/amd64 must be installable")
	}
	if windowsOnly.HasInstallableVersion("linux", "amd64") {
		t.Error("linux/amd64 must not be installable for a windows-only tool")
	}

	var nilTool *Tool
	if nilTool.HasCompleteArchive() || nilTool.HasInstallableVersion("windows", "amd64") {
		t.Error("nil tool must be safe")
	}
}

// TestLoadCatalog_MarksInstallability：LoadCatalog 必须给每个工具盖上派生标记，
// 且合并路径（内嵌+磁盘）也要覆盖——本用例走纯磁盘加载路径。
func TestLoadCatalog_MarksInstallability(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 用运行平台写 fixture：Installable 是“本机可安装”，不能用 windows-only
	// 归档去断言 macOS/Linux 上为 true（20261022 CI 实测）。
	write("good", "name: good\nversions:\n  - version: \"1.0.0\"\n    archives:\n"+
		"      - os: "+runtime.GOOS+"\n        arch: "+runtime.GOARCH+"\n        url: https://x/good.zip\n        format: zip\n")
	write("placeholder", "name: placeholder\nversions:\n  - version: \"1.0.0\"\n    archives:\n"+
		"      - os: windows\n        arch: amd64\n")

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	good, ok := cat.Tool("good")
	if !ok {
		t.Fatal("tool good missing")
	}
	if !good.Installable {
		t.Error("good.Installable = false, want true")
	}
	ph, ok := cat.Tool("placeholder")
	if !ok {
		t.Fatal("tool placeholder missing")
	}
	if ph.Installable {
		t.Error("placeholder.Installable = true —— 缺 url/format 的条目必须被标记为不可安装 (P2-29)")
	}
}
