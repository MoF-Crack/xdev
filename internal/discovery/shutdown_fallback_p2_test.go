package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
)

// ---------------------------------------------------------------------------
// 第十轮审计 P2-1：Shutdown 兜底仍是**锁外 RMW**
// ---------------------------------------------------------------------------
//
// 缺陷现场（scheduler.go:235-239，修复前）：
//
//	if cache, err := s.LoadCache(); err == nil && cache != nil {   // 锁外读（共享锁 50ms 拿不到就无锁读）
//	    cache.Partial = true
//	    cache.LastSniffAt = time.Now()
//	    _ = s.SaveCache(cache)                                      // 锁外 marshal，锁内才写
//	}
//
// SaveCache 先 marshal 快照、后取独占锁，于是「读 → marshal → 写」跨了两把锁，
// 在两者之间由别的进程/协程落地的条目会被这份过时快照**整份覆盖**（lost update）。
// 同一路径还有第二后果：LoadCache 对损坏文件返回「空缓存 + nil」，兜底于是把
// 损坏文件替换成 {"tools":{},"partial":true}，违背 F-56 的「读失败绝不覆写」。

// logSignalWriter 把 log 输出原样转发给 orig，并在首次匹配 needle 时关闭 ch。
// 用于**确定性**地探知 Shutdown 已经越过 2s 超时、即将（或已经）进入兜底。
type logSignalWriter struct {
	orig   io.Writer
	needle string
	once   sync.Once
	ch     chan struct{}
}

func (w *logSignalWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(w.needle)) {
		w.once.Do(func() { close(w.ch) })
	}
	return w.orig.Write(p)
}

// newShutdownFallbackScheduler 构造一个「嗅探仍在飞行」的 Scheduler：
// startupDone **永不关闭**，因此 Shutdown 必然走 2 秒超时兜底路径。
// 不启动任何真实嗅探 goroutine —— 时序完全由测试控制。
func newShutdownFallbackScheduler(t *testing.T) *Scheduler {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    1,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.startupCtx = ctx
	s.startupCancel = cancel
	s.startupDone = make(chan struct{}) // 永不关闭 → 命中 2s 超时兜底
	return s
}

// TestShutdownFallback_DoesNotLoseConcurrentEntry 断言：Shutdown 的兜底
// **不得**用锁外读到的旧快照整份覆盖缓存，从而丢掉并发落地的条目。
//
// 时序构造（确定性，**不依赖**「持锁阻塞在别处」这种间接效应）：
//
//  1. 测试**显式持有** sniff-cache.json.lock 的独占文件锁 —— 代表「另一个
//     写者正在提交它的事务」（withCacheLock 的读-改-写整体尚未结束）。
//  2. 启动 Shutdown。它先等 2s 超时，然后用日志行 "shutdown: timeout waiting"
//     宣告自己已越过超时点（本用例用 logSignalWriter 捕获该行作为**锚点**，
//     而不是盲猜 sleep）；随后进入兜底：
//     · 修复前：LoadCache 的共享锁 50ms 拿不到 → **无锁**读到只含 alpha 的
//     旧快照 → SaveCache marshal 旧快照 → 阻塞在独占锁上；
//     · 修复后：withCacheLock 直接阻塞在独占锁上（拿不到锁不写）。
//  3. 锚点之后 300ms（足够本地文件读完成），测试把 alpha+concurrent 写进
//     cachePath 并释放锁 —— 等价于「并发条目在 Shutdown 读到快照之后、写回
//     之前落地」。
//  4. 修复前：Shutdown 用旧快照整份覆盖 → concurrent 丢失（本用例必红）。
//     修复后：兜底只在锁内改 Partial 标记 → concurrent 存活。
//
// 关键点：并发条目能否存活，只取决于 Shutdown 是「整份写回旧快照」还是
// 「锁内只改标记」，不取决于任何 goroutine 调度。
func TestShutdownFallback_DoesNotLoseConcurrentEntry(t *testing.T) {
	s := newShutdownFallbackScheduler(t)

	// 预置：只有 alpha 的旧快照（Partial=false）。
	if err := s.SaveCache(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
		Partial:     false,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// 1) 显式持有独占锁：模拟「另一个写者的事务尚未提交」。
	lock, err := util.AcquireFileLock(s.cachePath+".lock", 5*time.Second)
	if err != nil {
		t.Fatalf("acquire test lock: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = lock.Release()
		}
	}
	defer release()

	// 2) 捕获超时日志作为锚点（见上方时序说明）。
	timeoutSeen := make(chan struct{})
	origOut := log.Writer()
	log.SetOutput(&logSignalWriter{
		orig:   origOut,
		needle: "shutdown: timeout waiting",
		ch:     timeoutSeen,
	})
	defer log.SetOutput(origOut)

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown() }()

	select {
	case <-timeoutSeen:
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown 未在 10s 内进入 2s 超时兜底（探针锚点未出现）")
	}
	// 锚点行在 LoadCache 之前打印；再给 300ms 让（修复前的）无锁读取完成。
	time.Sleep(300 * time.Millisecond)

	// 3) 并发条目落地：alpha + concurrent。
	merged, err := json.MarshalIndent(&SniffCache{
		Tools: map[string]ToolSniffResult{
			"alpha":      {LastSniff: time.Now()},
			"concurrent": {LastSniff: time.Now()},
		},
		LastSniffAt: time.Now(),
		Partial:     false,
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal merged cache: %v", err)
	}
	if err := os.WriteFile(s.cachePath, merged, 0o644); err != nil {
		t.Fatalf("write merged cache: %v", err)
	}
	release()

	select {
	case <-shutdownDone:
	case <-time.After(20 * time.Second):
		t.Fatal("Shutdown 在放行独占锁后仍未返回")
	}

	after, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if after == nil {
		t.Fatal("LoadCache returned nil")
	}
	if _, ok := after.Tools["concurrent"]; !ok {
		t.Errorf("P2-1 复现：Shutdown 兜底用锁外读到的旧快照整份覆盖了缓存，"+
			"并发落地的条目 concurrent 丢失（tools=%v）——兜底必须是锁内 RMW",
			toolKeys(after.Tools))
	}
	if _, ok := after.Tools["alpha"]; !ok {
		t.Errorf("兜底写把既有的 alpha 条目也弄丢了（tools=%v）", toolKeys(after.Tools))
	}
	if !after.Partial {
		t.Error("兜底仍须把飞行中的嗅探标记为 Partial=true")
	}
}

// TestShutdownFallback_DoesNotOverwriteCorruptCache 断言：缓存损坏（读失败）时
// Shutdown 兜底**不得**把文件替换成空缓存，且**不得**报成功。
//
// 修复前：LoadCache 对损坏文件返回「空缓存 + nil」，兜底于是把原文件覆盖成
// {"tools":{},"partial":true}，违背 F-56 的既有约束「读失败绝不覆写」。
// 修复后：withCacheLock 在锁内读到损坏内容即返回错误、绝不写盘；Shutdown
// 把该错误返回并记录，原文件一字不动。
//
// 本用例**不**选择 quarantine（改名保留）方案：改名同样是一次文件系统副作用，
// 会与并发写者的原子写竞争，且 F-56 的约束是「保留原文件内容」——原样不动
// 是最强、最容易验证的保留形式。
func TestShutdownFallback_DoesNotOverwriteCorruptCache(t *testing.T) {
	s := newShutdownFallbackScheduler(t)

	corrupt := []byte(`{"tools": {"a": `)
	if err := os.WriteFile(s.cachePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := s.Shutdown()
	elapsed := time.Since(start)

	// 仍须等满 2s 超时（兜底路径没被删掉），但必须**报错**而不是报成功。
	if elapsed < 1500*time.Millisecond {
		t.Errorf("Shutdown 在 %v 就返回了 —— 预期等满 2s 超时兜底", elapsed)
	}
	if err == nil {
		t.Error("P2-1 复现：损坏缓存时 Shutdown 兜底报成功 —— 读失败必须失败，" +
			"而不是把文件替换成空缓存后当作写成功")
	}

	after, readErr := os.ReadFile(s.cachePath)
	if readErr != nil {
		t.Fatalf("P2-1 复现：Shutdown 兜底动过损坏的缓存文件（改名/删除）：%v", readErr)
	}
	if string(after) != string(corrupt) {
		t.Errorf("P2-1 复现：Shutdown 兜底覆写了损坏的缓存（F-56：读失败绝不覆写）\nbefore: %s\nafter:  %s",
			corrupt, after)
	}
	if strings.Contains(string(after), `"tools": {}`) {
		t.Errorf("P2-1 复现：损坏缓存被替换成了空缓存：%s", after)
	}
}

// TestShutdownFallback_CommitWindowDoesNotMarkPartial 断言 Round 8 A-04 契约的
// **第三种时序**在兜底路径上也不回归：嗅探在 2s 超时之后、兜底拿到独占锁之前
// 「刚好完成」并自行落盘（Partial=false）时，兜底**不得**把它错标为 partial。
//
// 时序构造（确定性，显式 channel + 显式锁，不依赖任何 sleep 竞速）：
//
//  1. 测试显式持有 sniff-cache.json.lock 的独占文件锁。
//  2. Shutdown 等满 2s 超时（日志锚点）→ 进入兜底 → withCacheLock 阻塞在锁上。
//  3. 锚点之后 300ms，测试：写入「完整嗅探已落盘」的缓存（Partial=false），
//     并 close(startupDone) —— 这正是 onStartupStartup 里 `defer close(startupDone)`
//     在 sniffAllWithForce 保存**之后**才执行的语义 → 再释放锁。
//  4. 兜底在锁内复查 startupDone：已关闭 → 判定「刚好完成」→ 放弃回写。
//
// 断言 Partial 仍为 false。若兜底无条件写 Partial=true（A-04 缺陷形态），本用例必红。
func TestShutdownFallback_CommitWindowDoesNotMarkPartial(t *testing.T) {
	s := newShutdownFallbackScheduler(t)

	if err := s.SaveCache(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
		Partial:     true, // 兜底前的状态：上一次嗅探确实没跑完
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	lock, err := util.AcquireFileLock(s.cachePath+".lock", 5*time.Second)
	if err != nil {
		t.Fatalf("acquire test lock: %v", err)
	}
	defer lock.Release()

	timeoutSeen := make(chan struct{})
	origOut := log.Writer()
	log.SetOutput(&logSignalWriter{
		orig:   origOut,
		needle: "shutdown: timeout waiting",
		ch:     timeoutSeen,
	})
	defer log.SetOutput(origOut)

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown() }()

	select {
	case <-timeoutSeen:
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown 未在 10s 内进入 2s 超时兜底")
	}
	time.Sleep(300 * time.Millisecond) // 让兜底真正阻塞到锁上

	// 3) 「刚好完成」：完整嗅探自行落盘（Partial=false）后关闭 startupDone。
	// 这里直接写文件而不用 s.SaveCache —— 本用例**自己**持有独占锁，
	// SaveCache 会先等锁 5s 再降级写，反而把时序搅乱。
	completed, err := json.MarshalIndent(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
		Partial:     false,
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal completed cache: %v", err)
	}
	if err := os.WriteFile(s.cachePath, completed, 0o644); err != nil {
		t.Fatalf("write completed cache: %v", err)
	}
	close(s.startupDone)
	_ = lock.Release()

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown 在提交窗口应静默成功（不回写），got: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Shutdown 在放行独占锁后仍未返回")
	}

	after, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if after.Partial {
		t.Error("A-04 回归（兜底路径）：嗅探在超时后、拿锁前刚好完成并落盘，" +
			"兜底却把这次**完整**嗅探错标为 Partial=true")
	}
}

// TestWithCacheLock_LockUnavailableFailsInsteadOfUnlockedWrite 是 P2-1 修复要求 3
// 的守卫：withCacheLock 拿不到独占文件锁时**不得**降级为无锁 RMW，必须返回错误。
//
// 修复前：`if lockErr == nil { defer lock.Release() }` —— 拿不到锁就静默继续，
// 于是「读 → 改 → 写」整段在无锁状态下执行，正是 P2-1 的 lost update 形态。
func TestWithCacheLock_LockUnavailableFailsInsteadOfUnlockedWrite(t *testing.T) {
	s, _ := newRMWTestScheduler(t)

	if err := s.SaveCache(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// 独占锁被他人持有（5s > withCacheLock 的 5s 超时预算内的任何一次尝试）。
	lock, err := util.AcquireFileLock(s.cachePath+".lock", 5*time.Second)
	if err != nil {
		t.Fatalf("acquire test lock: %v", err)
	}
	defer lock.Release()

	start := time.Now()
	err = s.withCacheLock(func(c *SniffCache) error {
		c.Tools["beta"] = ToolSniffResult{LastSniff: time.Now()}
		return nil
	})
	if err == nil {
		t.Errorf("拿不到独占锁时 withCacheLock 必须返回错误（耗时 %v），"+
			"而不是降级为无锁读-改-写", time.Since(start))
	}

	raw, readErr := os.ReadFile(s.cachePath)
	if readErr != nil {
		t.Fatalf("cache file unreadable: %v", readErr)
	}
	if strings.Contains(string(raw), "beta") {
		t.Errorf("无锁降级写把 beta 落了盘：%s", raw)
	}
	if !strings.Contains(string(raw), "alpha") {
		t.Errorf("无锁降级写覆盖了既有条目：%s", raw)
	}
}
