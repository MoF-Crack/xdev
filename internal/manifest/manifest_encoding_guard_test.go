package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Spec 028 Phase B：manifest 编码守卫。
//
// 背景：PowerShell Set-Content 曾以 ANSI/GBK 重写 manifest，产生 UTF-8 烘焙的
// GBK mojibake（注释行合并 + 不可映射字节丢失成 '?'）。这些守卫防止复发：
//   - TC-B1: 所有 manifests/*.yaml 必须合法 UTF-8 且不含 U+FFFD
//   - TC-B2: 不得含 GBK mojibake 标记字符
//   - TC-B3: Tool manifest 必须显式声明 name 且等于文件名 stem
//     （消除对 loader filename fallback 的隐性依赖）
//   - TC-B4: form_factor 含 gui 且声明 shims 的工具必须声明 gui_binary
//     （vscode 曾因注释行合并丢失该字段；opencode 为文档化例外）

// gbkMojibakeMarkers 是 GBK 误读常见中文技术词汇后的特征字符集（实测
// 2026-08-09 损坏的 13 个 manifest 的并集）。这些字符在正常中文技术注释中
// 几乎不会出现，因此作为 mojibake 的可靠指纹。
const gbkMojibakeMarkers = "鈥鍘瀹淇锛銆鏈鏃绾鍣甯鐩閿垎钖灏鎸爣鍙鑴鍦浠涔娌圭敤鎵绔浜"

// nonToolManifests 是 manifests/ 下非 Tool manifest 的文件（如目录索引），
// 不参与 name/gui_binary 结构守卫（TC-B3/B4），但仍参与编码守卫（TC-B1/B2）。
var nonToolManifests = map[string]bool{
	"index.yaml": true,
	// package-managers.yaml 是包管理器定义（internal/pm 加载），
	// 顶层键是 package_managers 而非单个工具的 name，不是 Tool manifest。
	// 它有自己的一致性守卫：internal/pm.TestBuiltinMatchesYAML。
	"package-managers.yaml": true,
}

// guiBinaryExceptions 是 form_factor 含 gui 但有意不声明 gui_binary 的工具
// （GUI 二进制布局待核实，注释已说明；GUI 启动会得到明确的
// 「未声明有效的 gui_binary」错误而非误启动）。
var guiBinaryExceptions = map[string]string{
	"opencode": "GUI 桌面应用安装布局待实施期核实，manifest 注释已说明不设置 gui_binary",
}

func repoManifestDirT(t *testing.T) string {
	t.Helper()
	dir, ok := repoManifestDir()
	if !ok {
		t.Fatal("repo manifests dir not found")
	}
	return dir
}

func manifestFileNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(repoManifestDirT(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no manifests found")
	}
	return names
}

func readManifestBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoManifestDirT(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// TestManifestEncodingGuard_ValidUTF8 (TC-B1)
func TestManifestEncodingGuard_ValidUTF8(t *testing.T) {
	for _, name := range manifestFileNames(t) {
		b := readManifestBytes(t, name)
		if !utf8.Valid(b) {
			t.Errorf("%s: not valid UTF-8", name)
		}
		if strings.ContainsRune(string(b), '\uFFFD') {
			t.Errorf("%s: contains U+FFFD replacement character", name)
		}
	}
}

// TestManifestEncodingGuard_NoMojibake (TC-B2)
func TestManifestEncodingGuard_NoMojibake(t *testing.T) {
	for _, name := range manifestFileNames(t) {
		content := string(readManifestBytes(t, name))
		count := 0
		for _, r := range gbkMojibakeMarkers {
			count += strings.Count(content, string(r))
		}
		if count > 0 {
			t.Errorf("%s: %d GBK mojibake marker rune(s) — rewrite comments as clean UTF-8 Chinese", name, count)
		}
	}
}

// TestManifestEncodingGuard_ExplicitName (TC-B3)
func TestManifestEncodingGuard_ExplicitName(t *testing.T) {
	for _, name := range manifestFileNames(t) {
		if nonToolManifests[name] {
			continue
		}
		var tool Tool
		if err := yaml.Unmarshal(readManifestBytes(t, name), &tool); err != nil {
			t.Errorf("%s: yaml parse: %v", name, err)
			continue
		}
		want := strings.TrimSuffix(name, ".yaml")
		if tool.Name == "" {
			t.Errorf("%s: missing explicit name field (currently relying on loader filename fallback)", name)
		} else if tool.Name != want {
			t.Errorf("%s: name = %q, want %q (must match filename stem)", name, tool.Name, want)
		}
	}
}

// TestManifestEncodingGuard_GUIBinaryForGUIForm (TC-B4)
func TestManifestEncodingGuard_GUIBinaryForGUIForm(t *testing.T) {
	for _, name := range manifestFileNames(t) {
		if nonToolManifests[name] {
			continue
		}
		var tool Tool
		if err := yaml.Unmarshal(readManifestBytes(t, name), &tool); err != nil {
			continue // TC-B3 reports parse errors
		}
		hasGUI := false
		for _, f := range tool.EffectiveFormFactor() {
			if f == "gui" {
				hasGUI = true
			}
		}
		if !hasGUI || len(tool.Shims) == 0 || tool.GUIBinary != "" {
			continue
		}
		stem := strings.TrimSuffix(name, ".yaml")
		if reason, exempt := guiBinaryExceptions[stem]; exempt {
			t.Logf("%s: gui_binary absent (documented exception: %s)", name, reason)
			continue
		}
		t.Errorf("%s: form_factor includes gui with shims declared but gui_binary is missing", name)
	}
}
