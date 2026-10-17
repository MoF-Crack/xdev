package cli

// F-28（P0）：`xdev list <plugin> --remote` 对 Lua 插件 nil 解引用 panic。
//
// 根因链：internal/plugin 的 luaBasePlugin.Manifest() 恒返回 nil（Lua 插件没有静态
// manifest），而 listTool 直接写 `p.Manifest().Versions()` —— 对 nil *manifest.Tool
// 调用指针接收者方法会在 `range t.VersionList` 处 panic。
//
// 本用例用**真实** Lua 插件（经 loadCatalogAndRegistry → RegisterAll →
// registerUserPlugins 加载）走完整路径，而不是只驱动一个桩函数。
//
// 宿主安全：injectHome 把 XMDEV_HOME 与全局 runCtx 都钉到 t.TempDir()，
// 不触碰真实 xdev home。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListTool_Remote_WithLuaPluginDoesNotPanic 修复前必然 panic（P0）。
func TestListTool_Remote_WithLuaPluginDoesNotPanic(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)

	pluginsDir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// probeLuaPlugin 的 available() 返回空表：既无静态 manifest，
	// 也解析不出 "latest" —— 这是 Lua 插件的最坏形态。
	if err := os.WriteFile(filepath.Join(pluginsDir, "nilm.lua"),
		[]byte(probeLuaPlugin("nilm", "")), 0o644); err != nil {
		t.Fatal(err)
	}

	readStdout := captureStdout(t)
	err := listTool("nilm", filepath.Join(home, "install"), filepath.Join(home, "current"), true)
	out := readStdout()

	if err != nil {
		t.Fatalf("listTool(--remote) 对无静态 manifest 的 Lua 插件必须优雅降级，得到错误: %v", err)
	}
	if !strings.Contains(out, "static version list") {
		t.Errorf("必须明确告知「插件不提供静态版本列表」而不是静默空表; output=%q", out)
	}
}

// TestListTool_Remote_WithManifestPluginStillListsVersions 正例守卫：
// 有静态 manifest 的插件（Generic）必须照旧逐版本渲染，不能被降级分支吞掉。
func TestListTool_Remote_WithManifestPluginStillListsVersions(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)

	manifestsDir := filepath.Join(home, "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: demo\n" +
		"display_name: Demo\n" +
		"category: build\n" +
		"shims: [demo]\n" +
		"latest: \"2.0.0\"\n" +
		"versions:\n" +
		"  - version: \"2.0.0\"\n" +
		"  - version: \"1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(manifestsDir, "demo.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	readStdout := captureStdout(t)
	err := listTool("demo", filepath.Join(home, "install"), filepath.Join(home, "current"), true)
	out := readStdout()

	if err != nil {
		t.Fatalf("listTool(--remote) 对静态 manifest 插件失败: %v", err)
	}
	for _, want := range []string{"2.0.0", "1.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少版本 %s; output=%q", want, out)
		}
	}
	if strings.Contains(out, "static version list") {
		t.Errorf("有静态 manifest 的插件不得走降级分支; output=%q", out)
	}
}
