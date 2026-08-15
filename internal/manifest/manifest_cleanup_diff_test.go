package manifest

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Spec 028 Phase B3：语义不变性门禁。
// 背景：13 个 manifest 曾因 PowerShell 编码事故导致注释 mojibake + 行合并，
// name/gui_binary 字段被吞入注释。重写注释时功能性字段必须逐字节保持
// 不变——本测试用关键字段快照锁定语义（TC-B5），并用全量加载验证
// （TC-B6）与 vscode GUI 契约断言（TC-B7）。

// cleanupTargetFiles 是 Spec 028 清理的 13 个 manifest。
var cleanupTargetFiles = []string{
	"claude-code", "clion", "codex", "cursor", "copilot-cli", "goland",
	"jetbrains-idea-community", "opencode", "pycharm-community",
	"rubymine", "vscode", "webstorm", "zed",
}

// snapshotEntry 锁定每个清理文件的语义指纹（2026-08-09 清理前提取，
// 清理后必须完全一致——这些字段不允许随注释重写而变化）。
type snapshotEntry struct {
	file        string // 文件名 stem（也是 name 字段值）
	category    string
	displayName string
	latest      string
	guiBinary   string // "" = 未声明
	versionCnt  int
	firstURL    string
	firstSHA    string
}

var cleanupSnapshots = []snapshotEntry{
	{"claude-code", "development_tools", "Claude Code", "2.1.220", "", 2,
		"https://registry.npmjs.org/@anthropic-ai/claude-code-win32-x64/-/claude-code-win32-x64-2.1.220.tgz",
		"17b2a54acc5617cd4c5769ccdde86aa6d149dd44a96338ed9c0b027fda2e43f0"},
	{"clion", "development_tools", "CLion", "2026.2.0.1", "bin/clion64.exe", 2,
		"https://download.jetbrains.com/cpp/CLion-2026.2.0.1.exe",
		"1eb5d666a1f3985cd66a8136e4eb3c3b685ea49e5977977a4ec7c226f240d8ba"},
	{"codex", "development_tools", "Codex (OpenAI)", "0.146.0", "", 2,
		"https://registry.npmjs.org/@openai/codex/-/codex-0.146.0-win32-x64.tgz",
		"a7b0c568c1ee6ba6de7c419ae5ccf1473f229871caafed0205432654a2b8ec6f"},
	{"cursor", "development_tools", "Cursor", "3.14.7", "Cursor.exe", 1,
		"https://downloads.cursor.com/production/a758f2241ca99fecf380180b6cbdbbce0f1f42cf/win32/x64/system-setup/CursorSetup-x64-3.14.7.exe",
		"9db251bc79311a31c543cd2e3122f85621fff9755ef690e4b2fa47b3106410f5"},
	{"copilot-cli", "development_tools", "GitHub Copilot CLI", "1.0.77", "", 2,
		"https://registry.npmjs.org/@github/copilot-win32-x64/-/copilot-win32-x64-1.0.77.tgz",
		"e5151fe2fc18d4a26b3475ee06015ebc12d64a43079ff805a22b4a865f157c4f"},
	{"goland", "development_tools", "GoLand", "2026.2.0.1", "bin/goland64.exe", 2,
		"https://download.jetbrains.com/go/goland-2026.2.0.1.exe",
		"cbb1baa41c12bba7d973d2c614dfabea29ef03b2ca9ce84bb24b3f8c84dbf13e"},
	{"jetbrains-idea-community", "development_tools", "JetBrains IntelliJ IDEA Community", "2025.3", "bin/idea64.exe", 2,
		"https://download.jetbrains.com/idea/idea-2025.3.exe",
		"d7f0310423078c6a3b136c8a04090c1b6d316c2104dc2502d99167feb7ecc1fc"},
	{"opencode", "development_tools", "OpenCode", "1.18.11", "", 2,
		"https://registry.npmjs.org/opencode-windows-x64/-/opencode-windows-x64-1.18.11.tgz",
		"7ded0e81ad1504b1ddff13b40affabcecc8ea34a752324b0fd62601dffa2d7d2"},
	{"pycharm-community", "development_tools", "PyCharm Community", "2025.3", "bin/pycharm64.exe", 2,
		"https://download.jetbrains.com/python/pycharm-2025.3.exe",
		"b1e9a363d5e9a4c59f91610e214646291dfd8b77a02b9688a3a291be33557550"},
	{"rubymine", "development_tools", "RubyMine", "2026.2", "bin/rubymine64.exe", 2,
		"https://download.jetbrains.com/ruby/RubyMine-2026.2.exe",
		"ace95949354cbd539bed8ebddff0ab45814c2c02143b81cda585cab0d0769bee"},
	{"vscode", "development_tools", "Visual Studio Code", "1.131.0", "Code.exe", 2,
		"https://update.code.visualstudio.com/1.131.0/win32-x64-archive/stable",
		"afcb7f2963cb69b8a251ee275b60da0b74bca70277c103deb029ca73c3a5381e"},
	{"webstorm", "development_tools", "WebStorm", "2026.2.0.1", "bin/webstorm64.exe", 2,
		"https://download.jetbrains.com/webstorm/WebStorm-2026.2.0.1.exe",
		"7530b5c01734633e5049ba3fbd2664282febee823f32b967a1e11cc02e71cd41"},
	{"zed", "development_tools", "Zed", "1.13.2", "zed.exe", 2,
		"https://github.com/zed-industries/zed/releases/download/v1.13.2/Zed-x86_64.exe",
		"f9e73b28ed1d202832dc2ff1e5df1be46297d16ac7aa1762f230f7c9995fd5b3"},
}

func loadCleanupTool(t *testing.T, stem string) *Tool {
	t.Helper()
	b := readManifestBytes(t, stem+".yaml")
	var tool Tool
	if err := yaml.Unmarshal(b, &tool); err != nil {
		t.Fatalf("parse %s: %v", stem, err)
	}
	return &tool
}

// TestManifestCleanup_SemanticFingerprint (TC-B5)
// 13 个清理文件的关键字段与清理前快照完全一致。
func TestManifestCleanup_SemanticFingerprint(t *testing.T) {
	for _, snap := range cleanupSnapshots {
		tool := loadCleanupTool(t, snap.file)
		if tool.Name != snap.file {
			t.Errorf("%s: name = %q, want %q", snap.file, tool.Name, snap.file)
		}
		if tool.Category != snap.category {
			t.Errorf("%s: category = %q, want %q", snap.file, tool.Category, snap.category)
		}
		if tool.DisplayName != snap.displayName {
			t.Errorf("%s: display_name = %q, want %q", snap.file, tool.DisplayName, snap.displayName)
		}
		if tool.Latest != snap.latest {
			t.Errorf("%s: latest = %q, want %q", snap.file, tool.Latest, snap.latest)
		}
		if tool.GUIBinary != snap.guiBinary {
			t.Errorf("%s: gui_binary = %q, want %q", snap.file, tool.GUIBinary, snap.guiBinary)
		}
		if len(tool.VersionList) != snap.versionCnt {
			t.Errorf("%s: versions = %d, want %d", snap.file, len(tool.VersionList), snap.versionCnt)
		}
		if len(tool.VersionList) == 0 {
			continue
		}
		first := tool.VersionList[0]
		if first.Version != snap.latest || len(first.Archives) == 0 {
			t.Errorf("%s: first version %q (archives=%d) does not match snapshot latest %q", snap.file, first.Version, len(first.Archives), snap.latest)
			continue
		}
		if first.Archives[0].URL != snap.firstURL {
			t.Errorf("%s: first archive URL = %q, want %q", snap.file, first.Archives[0].URL, snap.firstURL)
		}
		if first.Archives[0].SHA256 != snap.firstSHA {
			t.Errorf("%s: first archive SHA256 = %q, want %q", snap.file, first.Archives[0].SHA256, snap.firstSHA)
		}
	}
}

// TestManifestCleanup_AllLoad (TC-B6)
// 全部 manifest（含 13 个清理文件）经 loader 加载无错误。
func TestManifestCleanup_AllLoad(t *testing.T) {
	dir, ok := repoManifestDir()
	if !ok {
		t.Fatal("repo manifests dir not found")
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	for _, stem := range cleanupTargetFiles {
		if _, ok := cat.Tool(stem); !ok {
			t.Errorf("catalog missing tool %q after cleanup", stem)
		}
	}
}

// TestManifestCleanup_VSCodeGUIBinary (TC-B7)
// vscode 恢复的 gui_binary 字段必须存在（Spec 028 功能修复：字段曾因
// 注释行合并被吞，导致 GUI 启动报「未声明有效的 gui_binary」）。
func TestManifestCleanup_VSCodeGUIBinary(t *testing.T) {
	tool := loadCleanupTool(t, "vscode")
	if tool.GUIBinary != "Code.exe" {
		t.Fatalf("vscode gui_binary = %q, want Code.exe", tool.GUIBinary)
	}
	hasGUI := false
	for _, f := range tool.EffectiveFormFactor() {
		if f == "gui" {
			hasGUI = true
		}
	}
	if !hasGUI {
		t.Fatal("vscode form_factor should include gui")
	}
	if len(tool.Shims) == 0 {
		t.Fatal("vscode should declare shims")
	}
	// GUIBinary 是相对 install 根的相对路径（app_launch.go 契约）。
	if strings.Contains(tool.GUIBinary, "\\") || filepath.IsAbs(tool.GUIBinary) || tool.GUIBinary == "" {
		t.Errorf("vscode gui_binary %q must be a non-empty relative path", tool.GUIBinary)
	}
}
