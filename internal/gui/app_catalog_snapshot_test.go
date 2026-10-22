package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// ──────────────────────────────────────────────────────
// 票 24 守卫测试（P1-16 / P2-07 / P1-01 相关）：
//   CT2 GetDebugInfo 在 cat == nil 时不 panic（红线）
//   CT3 GetAvailableVersions 在 cat == nil 时不 panic
//   CT1 替代：本机无 -race → 用「并发交错 + 多轮」构造 catalog 换指针与读方竞争
// ──────────────────────────────────────────────────────

// mustNotPanic 执行 fn 并把 panic 转成可读的红线失败信息。
func mustNotPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("红线（票 24）：%s 不应 panic，实际 panic: %v", what, r)
		}
	}()
	fn()
}

// TestGetDebugInfo_NilCatalog_NoPanic（CT2，红线）：
// GetDebugInfo 是最常用的排障入口，而 catalog 未加载/加载失败时 a.cat 恰为 nil。
// 修复前直接 a.cat.Tools() → nil pointer dereference panic，把排障路径本身
// 变成崩溃点；修复后经 getCat()（RLock）+ nil 门返回零值，且仍报告配置路径。
func TestGetDebugInfo_NilCatalog_NoPanic(t *testing.T) {
	app := newTestApp(t)

	// 构造 cat == nil 的真实状态（catalog 未加载）。
	app.catMu.Lock()
	app.cat = nil
	app.catMu.Unlock()

	var info DebugInfo
	mustNotPanic(t, "GetDebugInfo() 且 cat == nil", func() {
		info = app.GetDebugInfo()
	})

	if info.CatalogCount != 0 {
		t.Errorf("CatalogCount = %d, want 0", info.CatalogCount)
	}
	if len(info.CatalogNames) != 0 {
		t.Errorf("CatalogNames = %v, want 空", info.CatalogNames)
	}
	if info.RegistryCount != 0 {
		t.Errorf("RegistryCount = %d, want 0", info.RegistryCount)
	}
	// 排障入口在 catalog 缺失时仍必须给出真实配置路径。
	if info.XMDevDir == "" || info.CacheDir == "" || info.ManifestDir == "" {
		t.Errorf("排障信息缺少配置路径: %+v", info)
	}
	t.Logf("cat == nil 时 GetDebugInfo 正常返回: catalogCount=%d registryCount=%d xmdevDir=%s",
		info.CatalogCount, info.RegistryCount, info.XMDevDir)
}

// TestGetAvailableVersions_NilCatalog_NoPanic（CT3）：
// 与 CT2 同型：版本列表也是排障时首先打开的入口，cat == nil 时必须返回
// nil 而不是 panic。
func TestGetAvailableVersions_NilCatalog_NoPanic(t *testing.T) {
	app := newTestApp(t)
	app.catMu.Lock()
	app.cat = nil
	app.catMu.Unlock()

	var got []VersionInfo
	mustNotPanic(t, "GetAvailableVersions() 且 cat == nil", func() {
		got = app.GetAvailableVersions("python")
	})
	if got != nil {
		t.Errorf("cat == nil 时应返回 nil，实际 %v", got)
	}

	// 同型路径：toolManifest（app_dependencies.go 的 catalog 读点）也必须安全。
	mustNotPanic(t, "toolManifest() 且 cat == nil", func() {
		if m := app.toolManifest("python"); m != nil {
			t.Errorf("cat == nil 且 registry 无该工具时应返回 nil，实际 %+v", m)
		}
	})
	t.Log("cat == nil 时 GetAvailableVersions / toolManifest 均返回空值，无 panic")
}

// writeCatalogFixture 写入两个 manifest 文件，返回 manifest 目录。
func writeCatalogFixture(t *testing.T, tmp string) string {
	t.Helper()
	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"name: ctalpha\ndisplay_name: CT Alpha\nversions:\n  - version: \"1.0.0\"\n",
		"name: ctbeta\ndisplay_name: CT Beta\nversions:\n  - version: \"2.0.0\"\n",
	} {
		name := strings.Fields(body)[1] + ".yaml"
		if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return manifestDir
}

// TestCatalogSnapshot_ConcurrentReloadAndReads（CT1 替代，本机无 -race）：
// 并发交错执行 reloadCatalog（真实的 catMu 写路径换指针）与 nil 窗口注入，
// 同时多个读方反复调用 3 个改走 getCat() 的读点。判据：
//   - 不 panic（nil 门必须覆盖并发窗口）
//   - 结果自洽：读到的工具名/版本永远取自某一次完整 catalog，而不是混合值
//
// 说明：`-race` 在本机不可用（MinGW 8.1.0 与 Go 1.26 race 运行时不兼容），
// 因此改用「多轮交错 + 精确自洽断言」作为等价门；真正的数据竞争检测留待
// 有 race 运行时的环境执行。
func TestCatalogSnapshot_ConcurrentReloadAndReads(t *testing.T) {
	tmp := t.TempDir()
	manifestDir := writeCatalogFixture(t, tmp)

	app := newTestApp(t)
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadCatalog(); err != nil {
		t.Fatalf("初次 reloadCatalog 失败: %v", err)
	}
	saved := app.getCat()
	if saved == nil {
		t.Fatal("reloadCatalog 后 cat 仍为 nil")
	}

	const rounds = 30
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
	)
	fail := func(format string, args ...any) {
		mu.Lock()
		errs = append(errs, fmt.Sprintf(format, args...))
		mu.Unlock()
	}

	stop := make(chan struct{})

	// 读方 1：GetDebugInfo（原 a.cat.Tools() 读点）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			info := app.GetDebugInfo()
			names := strings.Join(info.CatalogNames, ",")
			switch names {
			case "", "ctalpha", "ctbeta", "ctalpha,ctbeta":
			default:
				fail("GetDebugInfo 读到混合 catalog 名称: %q", names)
				return
			}
			if info.CatalogCount != len(info.CatalogNames) {
				fail("CatalogCount=%d 与 CatalogNames=%v 不自洽", info.CatalogCount, info.CatalogNames)
				return
			}
		}
	}()

	// 读方 2：GetAvailableVersions（原 a.cat.Tool() 读点）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, v := range app.GetAvailableVersions("ctalpha") {
				if v.Version != "1.0.0" {
					fail("GetAvailableVersions(ctalpha) 读到非本工具版本 %q", v.Version)
					return
				}
			}
		}
	}()

	// 读方 3：toolManifest（app_dependencies.go 的 catalog 读点）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if m := app.toolManifest("ctbeta"); m != nil && m.Name != "ctbeta" {
				fail("toolManifest(ctbeta) 返回了 %q 的 manifest", m.Name)
				return
			}
		}
	}()

	// 写方 1：真实写路径 reloadCatalog 反复换指针
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; i < rounds; i++ {
			if err := app.reloadCatalog(); err != nil {
				fail("reloadCatalog 第 %d 轮失败: %v", i, err)
				return
			}
		}
	}()

	// 写方 2：注入 cat == nil 窗口（catalog 未加载/加载失败的等价状态）
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; i < rounds; i++ {
			app.catMu.Lock()
			if i%2 == 0 {
				app.cat = nil
			} else {
				app.cat = saved
			}
			app.catMu.Unlock()
		}
	}()

	writerWG.Wait()
	close(stop)
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("并发交错期间发现 %d 个问题（示例: %s）", len(errs), errs[0])
	}

	// 收尾：恢复完整 catalog，读点必须报告全部 2 个工具。
	if err := app.reloadCatalog(); err != nil {
		t.Fatalf("收尾 reloadCatalog 失败: %v", err)
	}
	info := app.GetDebugInfo()
	if info.CatalogCount != 2 || len(info.CatalogNames) != 2 {
		t.Fatalf("收尾状态不自洽: count=%d names=%v", info.CatalogCount, info.CatalogNames)
	}
	t.Logf("并发 %d 轮换指针 + nil 窗口注入后无 panic、无混合读；收尾 catalog=%v", rounds*2, info.CatalogNames)
}
