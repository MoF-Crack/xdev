// app_custom_tools_b5_guard_test.go 守卫票 29 的自定义工具一侧：
//
//   - 29-1（B-05 / P1-18 + N-13）AddCustomTool 三重存在性门：
//     ① catalog 命中（内置/已加载 manifest）② cfg.CustomTools 已含同名
//     ③ 磁盘同名 yaml 已存在（未进 catalog）；以及 cfg.Save 失败后的
//     回滚无残留（manifest 不在 + cfg 未变）。
//   - 29-3（B-07 / P1-20）三构造点一致：npm 自定义工具的落盘 manifest
//     必须含 remote.package，且该 remote 段能独立再次发现版本
//     （sniff / 预览 / 安装链路可用——漏持久化块时这里就报
//     "npm remote: package is required"）。
//
// 所有用例只用临时目录 + httptest，不触网、不写真实注册表/用户环境。
package gui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// b5MustReadFile 读文件并在失败时终止（helper 前缀 b5 避免与并行票的
// 测试 helper 重名）。
func b5MustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// b5HTMLVersionServer 提供固定的 HTML 版本链接页（版本发现用，不触网）。
func b5HTMLVersionServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
			<a href="tool-2.0.0.zip">2.0.0</a>
			<a href="tool-1.0.0.zip">1.0.0</a>
		</body></html>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ─────────────────────────────────────────────────────────────────────
// 29-1 三重门
// ─────────────────────────────────────────────────────────────────────

// TestAddCustomTool_DuplicateCatalogName_Rejected：门①——与 catalog 内的
// 工具（内置嵌入 manifest 或已加载的第三方 manifest）同名必须拒绝。
// 不修则本次生成的 manifest 覆盖同名内置 manifest，且其 Discovered 版本
// 会并进内置版本表。
func TestAddCustomTool_DuplicateCatalogName_Rejected(t *testing.T) {
	tmp := t.TempDir()
	manifestDir := writeCatalogFixture(t, tmp) // ctalpha / ctbeta

	app := newTestApp(t)
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadCatalog(); err != nil {
		t.Fatalf("reloadCatalog: %v", err)
	}
	if _, ok := app.getCat().Tool("ctalpha"); !ok {
		t.Fatal("前置条件失败：catalog 内应有 ctalpha")
	}

	before := b5MustReadFile(t, filepath.Join(manifestDir, "ctalpha.yaml"))

	// 版本源用可用的 httptest 服务：修复前该调用**能成功**（于是覆盖掉既有
	// manifest），红灯因此来自「重复被接受」而不是「网络不可达」。
	srv := b5HTMLVersionServer(t)
	result := app.AddCustomTool("ctalpha", "CT Alpha", srv.URL, "html", `(\d+\.\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatalf("与 catalog 内既有工具同名必须拒绝（B-05 门①），实际成功: %+v", result)
	}
	if !strings.Contains(result.Message, "catalog") {
		t.Errorf("错误信息应点明 catalog 重名，实际: %q", result.Message)
	}
	if got := b5MustReadFile(t, filepath.Join(manifestDir, "ctalpha.yaml")); got != before {
		t.Errorf("门①必须在写盘之前命中：既有 manifest 被改写\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if n := len(app.getCfg().CustomTools); n != 0 {
		t.Errorf("门①命中后不应写入 config，CustomTools=%d", n)
	}
}

// TestAddCustomTool_DuplicateConfigName_Rejected：门②——cfg.CustomTools 已含
// 同名（磁盘 yaml 可能已被手工删除）必须拒绝。旧代码（N-13）无条件 append，
// 会在 config 里留下两条同名记录。
func TestAddCustomTool_DuplicateConfigName_Rejected(t *testing.T) {
	tmp := t.TempDir()
	app := newTestAppWithDirs(t, tmp)

	app.cfgMu.Lock()
	app.cfg.CustomTools = []config.CustomToolConfig{{
		Name:        "cfgtool",
		DisplayName: "CFG Tool",
		SourceURL:   "https://example.com",
		SourceType:  "html",
	}}
	app.cfgMu.Unlock()

	srv := b5HTMLVersionServer(t)
	result := app.AddCustomTool("cfgtool", "CFG Tool", srv.URL, "html", `(\d+\.\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatalf("config 内已注册同名工具必须拒绝（B-05 门②），实际成功: %+v", result)
	}
	if !strings.Contains(result.Message, "config") {
		t.Errorf("错误信息应点明 config 重名，实际: %q", result.Message)
	}
	if _, err := os.Stat(filepath.Join(tmp, "manifests", "cfgtool.yaml")); !os.IsNotExist(err) {
		t.Errorf("门②命中后不应写盘，stat err=%v", err)
	}
	if n := len(app.getCfg().CustomTools); n != 1 {
		t.Errorf("config 记录数 = %d, want 1（旧行为 N-13 会变成 2）", n)
	}
}

// TestAddCustomTool_ExistingManifestOnDisk_Rejected：门③——磁盘已有同名 yaml
// 但未进 catalog（坏 yaml 被 loader 跳过 / 第三方手工放置）必须拒绝。
// 这条门同时把「回滚只删本次新建的文件」变成平凡真值。
func TestAddCustomTool_ExistingManifestOnDisk_Rejected(t *testing.T) {
	tmp := t.TempDir()
	manifestDir := writeCatalogFixture(t, tmp)

	// 关键：**不**调用 reloadCatalog —— catalog 里看不到 ctalpha，
	// 只有磁盘上有（门①因此不命中，命中门③）。
	app := newTestApp(t)
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.getCat().Tool("ctalpha"); ok {
		t.Fatal("前置条件失败：catalog 不应包含 ctalpha")
	}

	before := b5MustReadFile(t, filepath.Join(manifestDir, "ctalpha.yaml"))

	srv := b5HTMLVersionServer(t)
	result := app.AddCustomTool("ctalpha", "CT Alpha", srv.URL, "html", `(\d+\.\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatalf("磁盘已有同名 manifest 必须拒绝（B-05 门③），实际成功: %+v", result)
	}
	if !strings.Contains(result.Message, "on disk") {
		t.Errorf("错误信息应点明磁盘 manifest 已存在，实际: %q", result.Message)
	}
	if got := b5MustReadFile(t, filepath.Join(manifestDir, "ctalpha.yaml")); got != before {
		t.Errorf("门③命中后既有 manifest 不得被覆盖\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if n := len(app.getCfg().CustomTools); n != 0 {
		t.Errorf("门③命中后不应写入 config，CustomTools=%d", n)
	}
}

// TestAddCustomTool_SaveFailure_RollsBackManifest：cfg.Save 失败必须回滚
// ——磁盘不留「config 里没有」的孤儿 manifest（否则门③会让用户被自己的
// 失败操作永久卡住），内存 config 也不留本次记录。
func TestAddCustomTool_SaveFailure_RollsBackManifest(t *testing.T) {
	tmp := t.TempDir()
	manifests := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifests, 0o755); err != nil {
		t.Fatal(err)
	}
	srv := b5HTMLVersionServer(t)

	app := newTestAppWithDirs(t, tmp)

	// 让 cfg.Save() 确定性失败：XMDevDir 的父路径是普通文件 → MkdirAll 失败。
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.cfg.Core.XMDevDir = filepath.Join(blocker, "sub")

	result := app.AddCustomTool("rollbacktool", "Rollback Tool", srv.URL, "html", `(\d+\.\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatalf("cfg.Save 失败时 AddCustomTool 必须失败，实际: %+v", result)
	}
	if !strings.Contains(result.Message, "save config") {
		t.Errorf("错误信息应来自 save config，实际: %q", result.Message)
	}

	manifestPath := filepath.Join(manifests, "rollbacktool.yaml")
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("回滚后 manifest 必须不存在（B-05 回滚），stat err=%v", err)
	}
	if n := len(app.getCfg().CustomTools); n != 0 {
		t.Errorf("Save 失败后 in-memory CustomTools 未回滚：%d", n)
	}
}

// ─────────────────────────────────────────────────────────────────────
// 29-3 三构造点一致（B-07）
// ─────────────────────────────────────────────────────────────────────

// TestAddCustomTool_NPMPackage_ThreeConstructionPoints：npm 自定义工具的
// 三个 RemoteSource 构造点（Preview 发现 / AddCustomTool 发现 / 落盘
// manifest 持久化块）必须一致地填上 Package。
func TestAddCustomTool_NPMPackage_ThreeConstructionPoints(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 假 npm registry（discoverNPM 的 base 固定，用测试 setter 注入）。
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo-pkg" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.1.0"},` +
			`"versions":{"2.1.0":{},"2.0.0":{},"1.0.0":{}}}`))
	}))
	defer registry.Close()
	prevBase := remote.SetNPMRegistryBaseForTest(registry.URL)
	defer remote.SetNPMRegistryBaseForTest(prevBase)

	app := newTestAppWithDirs(t, tmp)
	sourceURL := registry.URL + "/demo-pkg"

	// 构造点①：Preview 的发现链路。旧代码 Package 恒空 →
	// "npm remote: package is required" → 预览永远返回空列表。
	preview := app.PreviewCustomToolVersions(sourceURL, "npm", "")
	if !preview.Success {
		t.Fatalf("PreviewCustomToolVersions(npm) 失败（B-07 构造点①）: %s", preview.Message)
	}
	if len(preview.Versions) == 0 {
		t.Fatal("PreviewCustomToolVersions(npm) 未发现任何版本（B-07 构造点①）")
	}

	// 构造点②：AddCustomTool 的发现链路（结果里带版本即证明发现成功）。
	result := app.AddCustomTool("demopkg", "Demo Pkg", sourceURL, "npm", "", "", "")
	if !result.Success {
		t.Fatalf("AddCustomTool(npm) 失败（B-07 构造点②）: %s", result.Message)
	}
	if len(result.Versions) == 0 {
		t.Fatal("AddCustomTool(npm) 未返回版本（B-07 构造点②）")
	}

	// 构造点③：落盘 manifest 的 remote 段。
	manifestPath := filepath.Join(tmp, "manifests", "demopkg.yaml")
	raw := b5MustReadFile(t, manifestPath)
	var persisted manifest.Tool
	if err := yaml.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatalf("解析落盘 manifest 失败: %v\n%s", err, raw)
	}
	if persisted.Remote.Type != "npm" {
		t.Errorf("落盘 remote.type = %q, want \"npm\"\n%s", persisted.Remote.Type, raw)
	}
	if persisted.Remote.Package != "demo-pkg" {
		t.Fatalf("落盘 manifest 的 remote.package = %q, want \"demo-pkg\"（B-07 构造点③ 未同步）\n%s",
			persisted.Remote.Package, raw)
	}

	// 三构造点一致的**可观测判据**：把落盘的 remote 段直接送回发现链路 ——
	// 这正是 sniff / mergeIntoManifest / 预览在工具落盘后的行为。持久化块
	// 漏字段时这里会报 "npm remote: package is required"（死功能只修一半）。
	again, err := remote.DiscoverVersions(context.Background(), persisted.Remote)
	if err != nil {
		t.Fatalf("用落盘 remote 段重新发现版本失败（B-07 半修复）: %v\n%s", err, raw)
	}
	if len(again) == 0 {
		t.Fatalf("用落盘 remote 段重新发现返回空版本\n%s", raw)
	}

	// 安装链路读的是 catalog 视图，同样必须带上 package。
	cat, err := manifest.LoadCatalog(filepath.Join(tmp, "manifests"))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	loaded, ok := cat.Tool("demopkg")
	if !ok {
		t.Fatalf("落盘 manifest 未被 catalog 加载\n%s", raw)
	}
	if loaded.Remote.Package != "demo-pkg" {
		t.Errorf("catalog 视图的 remote.package = %q, want \"demo-pkg\"", loaded.Remote.Package)
	}

	// 非 npm 类型不得凭空产生 package（字段一致性是双向的）。
	if got := npmPackageFromSource("https://example.com/tool", "html"); got != "" {
		t.Errorf("html 类型的 npmPackageFromSource = %q, want \"\"", got)
	}
}
