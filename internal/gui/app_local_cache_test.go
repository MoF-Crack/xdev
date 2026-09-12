package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/env"
)

// ──────────────────────────────────────────────────────
// 票 22 守卫测试（P1-16 / D-08）：
//   1. saveLocalCache 原子化（AtomicWriteFile + 进程内互斥排队写）
//   2. 刷新上提（worker 不再刷新；批量迁移每批恰刷新一次）
//
// 本机 `go test -race` 不可用（唯一 MinGW 8.1.0 与 Go 1.26 race 运行时不兼容），
// 因此并发用例全部用「goroutine + WaitGroup 交错 + 多轮循环」构造，
// 并以「可观察的撕裂产物 / 精确计数」作为判据（见各测试注释）。
// ──────────────────────────────────────────────────────

// localCacheSnapshot 构造一份足够大的本地工具缓存快照（多工具场景）。
// 单条记录故意较长（长 Path），把「非原子写」的截断窗口放大到可稳定观测。
func localCacheSnapshot(marker, n int) map[string]localInfo {
	m := make(map[string]localInfo, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool-%d-%03d", marker, i)
		m[name] = localInfo{
			Version: fmt.Sprintf("1.0.%d", i),
			Path:    strings.Repeat(`D:\very\long\path\segment\`, 6) + name,
			Source:  "local",
		}
	}
	return m
}

// TestSaveLocalCache_ConcurrentTools_Atomic（SC1，红线）：
// 多工具并发（goroutine + WaitGroup）写缓存，期间另有并发读方持续读取该文件。
//
// 修复前：saveLocalCache 用 os.WriteFile 直接 O_TRUNC 目标文件 —— 读方会读到
// 0 字节或半截 JSON（json.Unmarshal 失败），即「缓存撕裂」；进程若在写中途
// 退出则磁盘上永久留下不可解析的缓存，下次启动本地工具状态整体丢失。
// 修复后：util.AtomicWriteFile（同目录临时文件 + rename）+ 互斥排队，
// 读方只会看到「旧的完整内容」或「新的完整内容」。
func TestSaveLocalCache_ConcurrentTools_Atomic(t *testing.T) {
	app := newTestApp(t)
	cachePath := filepath.Join(app.getCfg().Core.CacheDir, "tool_scan_cache.json")

	// 与 refreshLocalCache 相同的写入时序：先换 localMap，再落盘。
	writeSnapshot := func(marker int) {
		app.localMu.Lock()
		app.localMap = localCacheSnapshot(marker, 200)
		app.localMu.Unlock()
		app.saveLocalCache()
	}
	writeSnapshot(0)

	var (
		stop      = make(chan struct{})
		readerWG  sync.WaitGroup
		readMu    sync.Mutex
		torn      []string
		valid     int
		transient int
	)

	// readCache 读取缓存文件。
	//
	// Windows 细节（实测观察）：原子写用 MoveFileEx 替换目标文件的那一瞬间
	//（以及紧随其后的实时杀毒扫描），对同名文件的 CreateFile 会短暂返回
	// ERROR_SHARING_VIOLATION（"being used by another process"）。
	// 这是「瞬态占用」而非「内容撕裂」，因此允许有限重试（上限约 50ms）；
	// 但「文件不存在」或重试后仍打不开 → 直接判红线。
	readCache := func() ([]byte, error, bool) {
		var lastErr error
		for attempt := 0; attempt < 50; attempt++ {
			data, err := os.ReadFile(cachePath)
			if err == nil {
				return data, nil, attempt > 0
			}
			lastErr = err
			if os.IsNotExist(err) {
				return nil, err, false
			}
			time.Sleep(time.Millisecond)
		}
		return nil, lastErr, false
	}

	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err, retried := readCache()
			if err != nil {
				readMu.Lock()
				torn = append(torn, "读文件失败: "+err.Error())
				readMu.Unlock()
				continue
			}
			var got map[string]localInfo
			if err := json.Unmarshal(data, &got); err != nil {
				readMu.Lock()
				torn = append(torn, fmt.Sprintf("读到撕裂内容 (%d 字节): %v", len(data), err))
				readMu.Unlock()
				continue
			}
			readMu.Lock()
			valid++
			if retried {
				transient++
			}
			readMu.Unlock()
			runtime.Gosched()
		}
	}()

	const writers, rounds = 8, 40
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				writeSnapshot(w*1000 + r + 1)
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	readerWG.Wait()

	if len(torn) > 0 {
		t.Fatalf("红线（SC1）：并发写期间读方观察到 %d 次非完整缓存内容（示例: %s）——写入不是原子的",
			len(torn), torn[0])
	}
	if valid == 0 {
		t.Fatal("测试无效：读方未能成功读取任何一次完整缓存（未真正压到写窗口）")
	}
	t.Logf("并发写 %d 次，读方成功读取完整缓存 %d 次（其中 %d 次首读命中重命名瞬态占用、重试后成功），撕裂 0 次",
		writers*rounds, valid, transient)

	// 最终状态：最后一次写入必须完整，且包含全部最新工具条目。
	const finalMarker = 4242
	writeSnapshot(finalMarker)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("读取最终缓存失败: %v", err)
	}
	var final map[string]localInfo
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("最终缓存不可解析（红线）: %v\n内容前 200 字节: %q", err, firstBytes(data, 200))
	}
	if len(final) != 200 {
		t.Fatalf("最终缓存条目数 = %d, want 200（最新状态被覆盖/丢失）", len(final))
	}
	key := fmt.Sprintf("tool-%d-%03d", finalMarker, 7)
	if _, ok := final[key]; !ok {
		t.Fatalf("最终缓存缺少最新状态 %q", key)
	}

	// 原子写的中间产物不得残留（.tmp 已被 rename）。
	if _, err := os.Stat(cachePath + ".tmp"); err == nil {
		t.Errorf("原子写留下了临时文件残留: %s", cachePath+".tmp")
	}
}

func firstBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}

// TestMigrateAllLocalTools_RefreshOncePerBatch（SC2）：
// 批量迁移 N 个工具必须「每批恰好刷新一次」本地缓存；单工具迁移仍刷新一次。
//
// 修复前：worker（migrateLocalToolByJunction / migrateLocalToolByMove）内部
// 各自 refreshAndSaveLocalCache()，批量 3 个工具会刷新 4 次
// （3 次 worker + 1 次批量尾部）——N 个工具退化成 N 次全盘 hostscan + N 次落盘。
// 修复后：worker 不刷新，批量尾部 1 次、单工具入口 1 次。
//
// seam 说明（最小侵入、不改生产行为）：
//   - scanLocalToolsForMigration：默认 env.ScanLocalTools（生产值）；测试替换为
//     临时目录中的假工具集，避免触碰真实主机（否则会真的把用户 PATH 上的工具
//     迁移进临时目录、并写用户环境变量）。
//   - localCacheRefreshHook：仅用于计数，生产恒为 nil。
func TestMigrateAllLocalTools_RefreshOncePerBatch(t *testing.T) {
	home := t.TempDir()    // xdev 主目录（install/current/cache 都在这里）
	srcRoot := t.TempDir() // 迁移源必须是 xdev 主目录之外的目录
	app := newTestAppWithDirs(t, home)

	mkTools := func(names ...string) []env.LocalTool {
		out := make([]env.LocalTool, 0, len(names))
		for _, n := range names {
			dir := filepath.Join(srcRoot, n)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("创建假工具目录失败: %v", err)
			}
			// 预置版本号：避免迁移路径调用外部进程探测版本。
			out = append(out, env.LocalTool{Name: n, Path: dir, Version: "1.0.0"})
		}
		return out
	}

	batch := mkTools("faketool-a", "faketool-b", "faketool-c")

	origScan := scanLocalToolsForMigration
	scanLocalToolsForMigration = func(string) []env.LocalTool { return batch }
	defer func() { scanLocalToolsForMigration = origScan }()

	origHook := localCacheRefreshHook
	refreshes := 0
	localCacheRefreshHook = func() { refreshes++ }
	defer func() { localCacheRefreshHook = origHook }()

	results := app.MigrateAllLocalTools("junction")
	if len(results) != len(batch) {
		t.Fatalf("结果数 = %d, want %d", len(results), len(batch))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Fatalf("工具 %s 迁移未成功: status=%s reason=%s err=%s", r.Name, r.Status, r.Reason, r.Error)
		}
	}
	if refreshes != 1 {
		t.Fatalf("红线（SC2）：批量迁移 %d 个工具应恰刷新 1 次本地缓存，实际 %d 次（worker 内刷新未上提）",
			len(batch), refreshes)
	}
	t.Logf("批量迁移 %d 个工具：刷新 %d 次（期望 1）", len(batch), refreshes)

	// 回归：单工具迁移（走公开入口 MigrateLocalToolByJunction）仍刷新一次。
	single := mkTools("faketool-d")[0]
	body, err := json.Marshal(single)
	if err != nil {
		t.Fatal(err)
	}
	refreshes = 0
	if err := app.MigrateLocalToolByJunction(string(body)); err != nil {
		t.Fatalf("单工具迁移失败: %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("红线（SC2）：单工具迁移应恰刷新 1 次本地缓存，实际 %d 次", refreshes)
	}
	t.Logf("单工具迁移：刷新 %d 次（期望 1）", refreshes)
}
