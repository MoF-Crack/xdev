package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// 第八轮审计 A-04 守卫（确定性）：Shutdown 的**第二个** `<-s.startupDone` 分支
// 不得回写 Partial。
//
// 背景：第七轮 F-55 的初版修复只堵住了「进入 Shutdown 前就已完成」这一时序
// （第一个非阻塞 select）。但 sniffAllWithForce 在 :571 算出 Partial、到 :625
// 落盘之间存在真实窗口：若嗅探在这期间完成（它的 Partial 是按**活 context** 算的
// false），Shutdown 的第二个 select 会走 `<-s.startupDone` 分支，然后**无条件**
// 把 Partial 覆写成 true —— 同一个「完整嗅探被错标 partial」缺陷只是窗口变窄。
//
// 本用例直接、确定性地驱动该分支：手工构造一个「startupDone 尚未关闭」的
// Scheduler（不用 OnStartup，避免真实 goroutine 与我们争抢 close），让 Shutdown
// 走过第一个 select 的 default、发出 cancel、停在第二个 select；此时由测试关闭
// startupDone（等价于「嗅探恰好在此刻完成并自行落盘」），再断言 Partial 未被改写。
//
// 旧实现下该断言必红（Shutdown 会写 Partial=true）。
func TestShutdown_SecondSelectBranchDoesNotRewritePartial_A04(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	// 预置一份「完整嗅探」的缓存（Partial=false）——这就是嗅探自行落盘的结果。
	if err := s.SaveCache(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
		Partial:     false,
	}); err != nil {
		t.Fatal(err)
	}

	// 手工搭建 OnStartup 会建立的三个字段，但 startupDone **不**由 goroutine 关闭
	//（本用例自己控制关闭时机，从而确定性地命中第二个 select）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.startupCtx = ctx
	s.startupCancel = cancel
	s.startupDone = make(chan struct{})

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		s.Shutdown()
	}()

	// 给 Shutdown 足够时间走完第一个 select（default）→ cancel → 进入第二个 select。
	time.Sleep(200 * time.Millisecond)

	// 「嗅探恰好在此刻完成」：它已按活/死 context 自行落盘，随后关闭 startupDone。
	close(s.startupDone)

	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return after startupDone closed")
	}

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if cache == nil {
		t.Fatal("LoadCache returned nil")
	}
	if cache.Partial {
		t.Error("A-04 复现：Shutdown 的第二个 startupDone 分支把一次**完整**嗅探" +
			"（Partial 由 sniffAllWithForce 按 ctx.Err() 判定并落盘）覆写成了 Partial=true")
	}
}

// 对照：真正**超时**（嗅探仍在飞行、没能自行落盘）时，Shutdown 必须兜底写
// Partial=true —— 防止上面的修复把兜底能力一起删掉。
func TestShutdown_TimeoutStillWritesPartial(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	if err := s.SaveCache(&SniffCache{
		Tools:       map[string]ToolSniffResult{"alpha": {LastSniff: time.Now()}},
		LastSniffAt: time.Now(),
		Partial:     false,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.startupCtx = ctx
	s.startupCancel = cancel
	s.startupDone = make(chan struct{}) // 永不关闭 → 命中 2s 超时兜底

	start := time.Now()
	s.Shutdown()
	elapsed := time.Since(start)

	// 应等待约 2 秒超时（不是立刻返回）。
	if elapsed < 1500*time.Millisecond {
		t.Errorf("Shutdown returned after %v — expected it to wait for the 2s timeout", elapsed)
	}

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !cache.Partial {
		t.Error("嗅探仍在飞行（超时）时，Shutdown 必须兜底把结果标为 Partial=true")
	}
}
