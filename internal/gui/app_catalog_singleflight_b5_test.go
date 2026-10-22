// app_catalog_singleflight_b5_test.go — 审计修复 Batch 5（票 39 / P2-07）守卫：
// GetLatestVersionForTool 先读内存快照 a.getCat()，快照 miss 时的磁盘加载
// 必须单飞（并发调用者共享同一次 LoadCatalog，而不是各读一遍磁盘 + 各解析
// 一遍 YAML）。
//
// 宿主安全：全部用例经 t.Setenv("XMDEV_HOME", tmp) + newTestAppWithDirs(tmp)
// 把 home/cache/manifests 全部锚在 t.TempDir() 内，绝不触碰真实 xdev home。
//
// 并发说明：本机 go test -race 不可用（MinGW 8.1.0 与 Go 1.26 race 运行时不
// 兼容），因此用「单飞窗口内的显式并发构造 + 加载次数/在途峰值计数」替代。
package gui

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

// writeLatestFixture 写一个带显式 latest 的 manifest，返回 manifest 目录。
func writeLatestFixture(t *testing.T, tmp, name, latest string) string {
	t.Helper()
	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\n" +
		"display_name: " + name + "\n" +
		"latest: \"" + latest + "\"\n" +
		"versions:\n" +
		"  - version: \"" + latest + "\"\n" +
		"    stable: true\n"
	if err := os.WriteFile(filepath.Join(manifestDir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestDir
}

// newB5App 构造一个 home/manifests 都锚在临时目录的 App。
func newB5App(t *testing.T, tmp string) *App {
	t.Helper()
	t.Setenv("XMDEV_HOME", tmp) // 宿主安全：任何配置解析都落在 tmp
	return newTestAppWithDirs(t, tmp)
}

// TestGetLatestVersionForTool_SnapshotHitSkipsDiskLoad（P2-07 第一半）：
// 内存快照命中时必须零磁盘加载；快照中的未知工具才走单飞落盘一次。
func TestGetLatestVersionForTool_SnapshotHitSkipsDiskLoad(t *testing.T) {
	tmp := t.TempDir()
	app := newB5App(t, tmp)
	manifestDir := writeLatestFixture(t, tmp, "b5tool", "1.2.3")
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadCatalog(); err != nil {
		t.Fatalf("reloadCatalog: %v", err)
	}
	if app.getCat() == nil {
		t.Fatal("precondition: catalog snapshot must be loaded")
	}

	var loads int32
	orig := loadCatalogFn
	loadCatalogFn = func(dir string) (*manifest.Catalog, error) {
		atomic.AddInt32(&loads, 1)
		return orig(dir)
	}
	defer func() { loadCatalogFn = orig }()

	if got := app.GetLatestVersionForTool("b5tool"); got != "1.2.3" {
		t.Errorf("latest = %q, want 1.2.3 from the snapshot", got)
	}
	if n := atomic.LoadInt32(&loads); n != 0 {
		t.Errorf("snapshot hit performed %d disk catalog load(s), want 0 — 读点必须先查 a.getCat() (P2-07)", n)
	}

	// 快照里没有的工具 → 单飞落盘一次（结果仍为空，但只加载一次磁盘）。
	if got := app.GetLatestVersionForTool("b5-missing"); got != "" {
		t.Errorf("unknown tool latest = %q, want empty", got)
	}
	if n := atomic.LoadInt32(&loads); n != 1 {
		t.Errorf("unknown tool performed %d disk catalog load(s), want 1 (P2-07 miss 路径)", n)
	}
}

// TestGetLatestVersionForTool_ConcurrentMissSharesSingleLoad（P2-07 第二半）：
// cat == nil 的启动窗口里 N 个并发调用者必须只触发一次 LoadCatalog，
// 且任一时刻在途加载不超过 1 个。
//
// 构造（确定性，不依赖调度时序）：
//  1. 领飞者的加载被 release 按住 —— 此时单飞槽已登记；
//  2. 每个 worker 一旦成为等待者就经 catalogSharedWaitHook 上报，
//     测试**等齐 workers 个上报**（而不是睡固定时长）后才放行领飞者；
//  3. 因此 workers+1 个调用者必然共享同一次加载：loads 必须为 1。
//
// 修复前的兜底红：没有单飞时不存在「等待者」分支，hook 一次都不会触发，
// 下面的等待退化为超时，随后 loads 断言给出确定性的强红
// （每个调用者各自 LoadCatalog → loads = workers+1）。
func TestGetLatestVersionForTool_ConcurrentMissSharesSingleLoad(t *testing.T) {
	tmp := t.TempDir()
	app := newB5App(t, tmp)
	manifestDir := writeLatestFixture(t, tmp, "b5tool", "9.9.9")
	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ManifestDir = manifestDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 模拟"启动后、首次 reloadCatalog 之前"的窗口：快照为 nil。
	app.catMu.Lock()
	app.cat = nil
	app.catMu.Unlock()

	var loads, inFlight, maxInFlight int32
	release := make(chan struct{})
	orig := loadCatalogFn
	loadCatalogFn = func(dir string) (*manifest.Catalog, error) {
		atomic.AddInt32(&loads, 1)
		if cur := atomic.AddInt32(&inFlight, 1); cur > atomic.LoadInt32(&maxInFlight) {
			atomic.StoreInt32(&maxInFlight, cur)
		}
		defer atomic.AddInt32(&inFlight, -1)
		if atomic.LoadInt32(&loads) == 1 {
			<-release // 按住领飞者，制造确定的单飞窗口
		}
		return orig(dir)
	}
	defer func() { loadCatalogFn = orig }()

	const workers = 8
	waited := make(chan struct{}, workers)
	catalogSharedWaitHook = func() { waited <- struct{}{} }
	defer func() { catalogSharedWaitHook = nil }()

	leaderResult := make(chan string, 1)
	go func() { leaderResult <- app.GetLatestVersionForTool("b5tool") }()

	// 等领飞者真正进入加载（单飞槽已登记）。
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&loads) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("leader never entered LoadCatalog")
		}
		time.Sleep(time.Millisecond)
	}

	results := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = app.GetLatestVersionForTool("b5tool")
		}(i)
	}

	// 确定性屏障：等齐所有 worker「已加入在途加载」的上报再放行。
	parked := 0
	parkTimeout := time.After(5 * time.Second)
parkLoop:
	for parked < workers {
		select {
		case <-waited:
			parked++
		case <-parkTimeout:
			// 单飞缺失（修复前）时不会有等待者上报：交给下面的 loads 断言判红。
			t.Logf("no singleflight waiter observed after %d/%d workers — falling through to the load-count assertion", parked, workers)
			break parkLoop
		}
	}
	close(release)

	leader := <-leaderResult
	wg.Wait()

	if n := atomic.LoadInt32(&loads); n != 1 {
		t.Errorf("%d concurrent callers triggered %d LoadCatalog calls, want 1 — 缺少单飞，磁盘/YAML 解析被放大 (P2-07)", workers+1, n)
	}
	if peak := atomic.LoadInt32(&maxInFlight); peak > 1 {
		t.Errorf("peak concurrent catalog loads = %d, want ≤ 1 (P2-07)", peak)
	}
	if leader != "9.9.9" {
		t.Errorf("leader latest = %q, want 9.9.9", leader)
	}
	for i, v := range results {
		if v != "9.9.9" {
			t.Errorf("waiter %d latest = %q, want 9.9.9 (等待者必须复用同一次加载结果)", i, v)
		}
	}
}
