package tui

import (
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// nilManifestPlugin 模拟 Lua/vfox 插件：Manifest() 恒返回 nil
// （internal/plugin/lua_plugin.go:49），但 ResolveVersion 可用。
// 嵌入 plugin.Generic 以满足完整接口，只覆写 Manifest 与 ResolveVersion。
type nilManifestPlugin struct {
	*plugin.Generic
	resolved string
}

func (p *nilManifestPlugin) Manifest() *manifest.Tool { return nil }

func (p *nilManifestPlugin) ResolveVersion(string) (string, error) {
	return p.resolved, nil
}

// 第七轮审计 F-28（TUI 侧）：fetchRemoteVersions 对 Manifest() 返回 nil 的插件
// 必须不 panic。
//
// 缺陷现场：旧代码直接 `p.Manifest().Versions()`，而 Lua/vfox 插件的 Manifest()
// 恒返回 nil → nil 解引用 panic。任何装了 Lua 插件的机器，在 TUI 里选中该工具按
// r 刷新远程版本即崩溃（与 CLI 侧 `xdev list <plugin> --remote` 是同一 P0 形态）。
func TestFetchRemoteVersions_NilManifestPluginDoesNotPanic(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   t.TempDir(),
			InstallDir: t.TempDir(),
			ShimDir:    t.TempDir(),
		},
	}
	reg := plugin.NewRegistry()
	reg.Register(&nilManifestPlugin{
		Generic: plugin.NewGeneric(&manifest.Tool{
			Name: "luatool", Category: "language", Shims: []string{"luatool"},
		}),
		resolved: "2.0.0",
	})

	m := NewModel(cfg, &manifest.Catalog{}, reg)
	m.applyCategoryFilter()

	cmd := m.fetchRemoteVersions()
	if cmd == nil {
		t.Fatal("fetchRemoteVersions returned nil cmd for a non-empty tool list")
	}

	// 执行 cmd —— 旧实现在这里 panic。
	msg := cmd()
	rv, ok := msg.(RemoteVersionsMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want RemoteVersionsMsg", msg)
	}
	if rv.Tool != "luatool" {
		t.Errorf("Tool = %q, want luatool", rv.Tool)
	}
	// nil manifest → 回落到 ResolveVersion("latest") 的单个版本，而不是崩溃。
	if len(rv.Versions) != 1 || rv.Versions[0] != "2.0.0" {
		t.Errorf("Versions = %v, want [2.0.0] (fallback to ResolveVersion)", rv.Versions)
	}
}

// 对照：Manifest() 非 nil 的普通插件仍走静态版本列表（防「修成只认 ResolveVersion」）。
func TestFetchRemoteVersions_ManifestPluginStillListsStaticVersions(t *testing.T) {
	m := newTUITestModelWithTools(t, "aaa")
	cmd := m.fetchRemoteVersions()
	if cmd == nil {
		t.Fatal("fetchRemoteVersions returned nil cmd")
	}
	rv, ok := cmd().(RemoteVersionsMsg)
	if !ok {
		t.Fatal("cmd() did not return RemoteVersionsMsg")
	}
	if len(rv.Versions) != 1 || rv.Versions[0] != "1.0.0" {
		t.Errorf("Versions = %v, want [1.0.0]", rv.Versions)
	}
}
