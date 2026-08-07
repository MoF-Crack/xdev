package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	javapkg "github.com/xmdemt/xdev/internal/plugins/java"
)

// loadTestCatalog 从 dir 加载 catalog，出错即失败测试。
func loadTestCatalog(t *testing.T, dir string) *manifest.Catalog {
	t.Helper()
	catalog, err := manifest.LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return catalog
}

// copyManifest 将仓库 manifests/ 下的 src 复制到 dir 并命名为 name。
func copyManifest(t *testing.T, dir, src, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "manifests", src))
	if err != nil {
		t.Fatalf("read %s manifest: %v", src, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("write %s manifest: %v", name, err)
	}
}

func TestRegisterAllIncludesCangjie(t *testing.T) {
	// 临时目录仅含 cangjie.yaml，遍历 catalog 也应注册它。
	dir := t.TempDir()
	copyManifest(t, dir, "cangjie.yaml", "cangjie.yaml")

	catalog := loadTestCatalog(t, dir)
	if _, ok := catalog.Tool("cangjie"); !ok {
		t.Fatal("cangjie tool not found in catalog — check manifest loading")
	}

	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	if _, ok := r.Get("cangjie"); !ok {
		t.Fatal("cangjie not registered — catalog iteration must register every tool")
	}
}

func TestRegisterAllIncludesExistingTools(t *testing.T) {
	// 回归：现有工具（go/node）仍注册为 Generic 插件。
	dir := t.TempDir()
	copyManifest(t, dir, "go.yaml", "go.yaml")
	copyManifest(t, dir, "node.yaml", "node.yaml")

	catalog := loadTestCatalog(t, dir)
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	for _, name := range []string{"go", "node"} {
		p, ok := r.Get(name)
		if !ok {
			t.Fatalf("%s not registered — regression", name)
		}
		if _, isGeneric := p.(*plugin.Generic); !isGeneric {
			t.Fatalf("%s should be a Generic plugin, got %T", name, p)
		}
	}
}

func TestRegisterAllRegistersNewManifestAutomatically(t *testing.T) {
	// 新增 manifest（如 Spec 023 的 ide/agent/harness）无需改代码即自动注册。
	dir := t.TempDir()
	// 附带一个现有工具，确认混合目录下两者都注册。
	copyManifest(t, dir, "go.yaml", "go.yaml")

	newManifest := `name: demo-agent
display_name: Demo Agent
description: Spec 023 harness agent tool.
homepage: https://example.com/
category: agent
shims:
    - demo-agent
versions:
    - version: 1.0.0
      stable: true
      release: "2026-08-02"
      archives:
        - os: any
          arch: any
          format: zip
          url: https://example.com/demo-agent-1.0.0.zip
          sha256: "0000000000000000000000000000000000000000000000000000000000000000"
          size: 1024
`
	if err := os.WriteFile(filepath.Join(dir, "demo-agent.yaml"), []byte(newManifest), 0o644); err != nil {
		t.Fatalf("write demo-agent manifest: %v", err)
	}

	catalog := loadTestCatalog(t, dir)
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	p, ok := r.Get("demo-agent")
	if !ok {
		t.Fatal("demo-agent not registered — new manifests must be picked up automatically")
	}
	if _, isGeneric := p.(*plugin.Generic); !isGeneric {
		t.Fatalf("demo-agent should be a Generic plugin, got %T", p)
	}
	if _, ok := r.Get("go"); !ok {
		t.Fatal("go not registered alongside new manifest — regression")
	}
}

func TestRegisterAllJavaUsesDedicatedPlugin(t *testing.T) {
	// java 有专用插件（固定 Temurin 资源解析），不能被 Generic 覆盖。
	dir := t.TempDir()
	copyManifest(t, dir, "java.yaml", "java.yaml")

	catalog := loadTestCatalog(t, dir)
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	p, ok := r.Get("java")
	if !ok {
		t.Fatal("java not registered — regression")
	}
	if _, isGeneric := p.(*plugin.Generic); isGeneric {
		t.Fatal("java must use its dedicated plugin, not Generic")
	}
	if _, isJava := p.(*javapkg.Plugin); !isJava {
		t.Fatalf("java should be *java.Plugin, got %T", p)
	}
}

func TestRegisterAllSkipsIndexYaml(t *testing.T) {
	// manifests/index.yaml 是目录索引而非工具，遍历 catalog 时必须跳过，
	// 否则 xdev available 会出现空的 "index" 条目。
	dir := t.TempDir()
	copyManifest(t, dir, "index.yaml", "index.yaml")
	copyManifest(t, dir, "go.yaml", "go.yaml")

	catalog := loadTestCatalog(t, dir)
	if _, ok := catalog.Tool("index"); !ok {
		t.Fatal("precondition: index.yaml should be parsed into the catalog")
	}

	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	if _, ok := r.Get("index"); ok {
		t.Fatal("index must NOT be registered — it is a catalog index file, not a tool")
	}
	if _, ok := r.Get("go"); !ok {
		t.Fatal("go not registered alongside index.yaml — regression")
	}
}

func TestRegisterAllRealCatalog(t *testing.T) {
	// 集成回归：用仓库真实 manifests/ 目录注册，现有工具全部在、
	// java 用专用插件、index.yaml 被跳过。
	//
	// 环境隔离（B2-A5-01）：把 XMDEV_HOME 指向空临时目录，避免扫描到
	// 用户真实 ~/.xdev/plugins 下的 Lua/vfox 插件——否则精确断言会因
	// 环境差异而失败。
	t.Setenv("XMDEV_HOME", t.TempDir())

	catalog := loadTestCatalog(t, filepath.Join("..", "..", "manifests"))
	r := plugin.NewRegistry()
	RegisterAll(r, catalog)

	// 精确断言：注册集合必须与 catalog 中可注册工具集合完全一致
	// （含 java 专用插件、排除 index.yaml 与非工具条目）。
	var want []string
	for _, t := range catalog.Tools() {
		if t.Name == "index" || !isRegisterableTool(t) {
			continue
		}
		want = append(want, t.Name)
	}
	got := r.Names()
	if len(got) != len(want) {
		t.Fatalf("registry count = %d, want %d (names: %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("registry[%d] = %q, want %q (exact catalog-derived set)", i, got[i], want[i])
		}
	}

	// java 用专用插件，其余全部 Generic。
	for _, name := range want {
		p, ok := r.Get(name)
		if !ok {
			t.Errorf("%s missing from registry", name)
			continue
		}
		if name == "java" {
			if _, isJava := p.(*javapkg.Plugin); !isJava {
				t.Errorf("java should use its dedicated plugin, got %T", p)
			}
			continue
		}
		if _, isGeneric := p.(*plugin.Generic); !isGeneric {
			t.Errorf("%s should be a Generic plugin, got %T", name, p)
		}
	}

	if _, ok := r.Get("index"); ok {
		t.Error("index must NOT be registered — it is a catalog index file, not a tool")
	}
}
