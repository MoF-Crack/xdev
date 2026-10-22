package discovery

import (
	"os"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// newRMWTestScheduler 构造一个只用于缓存 RMW 用例的 Scheduler（不触网）。
func newRMWTestScheduler(t *testing.T) (*Scheduler, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "manual",
		Interval:       "24h",
		Concurrency:    1,
		RateLimitPerHr: 60,
	}
	return NewScheduler(cfg, dir, dir), dir
}

// 第七轮审计 F-53 守卫：限流必须把**窗口**（ResetAt）写进缓存。
//
// 缺陷现场：RateLimitState.ResetAt 全仓只有读取点（NewScheduler 里用它判断
// 「窗口是否仍活跃」），生产代码从不赋值。于是 403/rate-limit 之后缓存里
// remaining=0 但 reset_at 为零值 → 下一个进程走「无活跃窗口」救援分支保留满桶
// → specs/011 FR-007 承诺的「到 reset 前不再重试」从未生效，令牌桶限额无法
// 跨进程累计（重启即重置）。
func TestMarkRateLimited_PersistsResetWindow(t *testing.T) {
	s, _ := newRMWTestScheduler(t)

	s.mu.Lock()
	err := s.markRateLimitedLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("markRateLimitedLocked: %v", err)
	}

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if cache == nil {
		t.Fatal("LoadCache returned nil")
	}
	if cache.RateLimit.ResetAt.IsZero() {
		t.Error("F-53 复现：限流后 ResetAt 仍为零值 —— 跨进程限流窗口形同虚设")
	}
	if !cache.RateLimit.ResetAt.After(time.Now()) {
		t.Errorf("ResetAt = %v, want a future instant", cache.RateLimit.ResetAt)
	}
	if cache.RateLimit.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0 after a rate limit", cache.RateLimit.Remaining)
	}
}

// 第七轮审计 F-56 守卫：并发写同一缓存时不得丢更新。
//
// 缺陷现场：旧实现是 LoadCache（共享锁）→ 改内存 → SaveCache（独占锁），
// 两把锁之间没有覆盖整个 RMW 周期，两个进程/goroutine 会基于同一快照写回，
// 后写者覆盖前者的条目。这里用多 goroutine 并发 upsert **不同** key，
// 断言最终缓存里**每一条**都在（无丢失）。
func TestUpdateCacheEntry_ConcurrentWritesDoNotLoseEntries(t *testing.T) {
	s, _ := newRMWTestScheduler(t)

	const n = 12
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			name := "tool" + string(rune('a'+i))
			s.mu.Lock()
			err := s.updateCacheEntryLocked(name, ToolSniffResult{LastSniff: time.Now()})
			s.mu.Unlock()
			done <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Errorf("updateCacheEntryLocked: %v", err)
		}
	}

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	missing := []string{}
	for i := 0; i < n; i++ {
		name := "tool" + string(rune('a'+i))
		if _, ok := cache.Tools[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("F-56 复现：并发 RMW 丢了 %d 条更新：%v（读-改-写未在同一把独占锁内）",
			len(missing), missing)
	}
}

// F-56 加固守卫：读失败时**绝不**写空缓存。
//
// withCacheLock 旧写法把「文件不存在」与「读取/解析失败」都当成 nil 并从空缓存
// 开始，然后照样写回——一次瞬时读失败就把整份缓存清空。这正是 F-56 要消灭的
// 「丢更新」形态。现在损坏的缓存必须让事务失败并保留原文件。
func TestWithCacheLock_CorruptCacheIsNotOverwritten(t *testing.T) {
	s, _ := newRMWTestScheduler(t)

	corrupt := []byte(`{"tools": {"a": `)
	if err := os.WriteFile(s.cachePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	err := s.updateCacheEntryLocked("b", ToolSniffResult{LastSniff: time.Now()})
	s.mu.Unlock()
	if err == nil {
		t.Error("F-56 加固：损坏缓存的 RMW 必须失败，而不是从空缓存重建")
	}

	after, readErr := os.ReadFile(s.cachePath)
	if readErr != nil {
		t.Fatalf("cache file disappeared: %v", readErr)
	}
	if string(after) != string(corrupt) {
		t.Errorf("F-56 加固：损坏的缓存被覆写了\nbefore: %s\nafter:  %s", corrupt, after)
	}
}

// F-56 加固守卫：文件不存在（首次运行）必须正常成功，不能因为加固而误报。
func TestWithCacheLock_MissingCacheSucceeds(t *testing.T) {
	s, _ := newRMWTestScheduler(t)

	s.mu.Lock()
	err := s.updateCacheEntryLocked("first", ToolSniffResult{LastSniff: time.Now()})
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("first-ever write must succeed, got: %v", err)
	}
	cache, err := s.LoadCache()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Tools["first"]; !ok {
		t.Error("first entry missing after a successful initial write")
	}
}
