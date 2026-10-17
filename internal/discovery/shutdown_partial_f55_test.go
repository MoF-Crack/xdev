package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

// 第七轮审计 F-55 守卫：Shutdown 不得把「已正常跑完」的嗅探标成 partial。
//
// 根因：旧实现无条件 `cache.Partial = true`，而 sniffAllWithForce 在嗅探本体
// 结束时已按 ctx.Err() 正确判定过 Partial 并保存了缓存（见 scheduler.go 中
// `Partial: ctx.Err() != nil`）。于是交互入口在 startup/interval 嗅探**成功
// 完成后**正常退出，会把一次完整嗅探覆写成 partial，使 `xdev sniff` 摘要
// 无条件多打一个 partial，误导排障。
//
// 本用例把后台嗅探做成「立即正常完成」，然后调用 Shutdown，断言缓存里的
// Partial 仍为 false（或保持既有值），而不是被无条件置 true。
func TestShutdown_CompletedSniffIsNotMarkedPartial_F55(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	// 空 catalog → 嗅探无需网络即可立即、干净地跑完。
	s.loadCatalog = func(string) (*manifest.Catalog, error) {
		return &manifest.Catalog{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.OnStartup(ctx)

	// 等后台嗅探真正结束（它自己会写缓存并把 Partial 置为 false）。
	select {
	case <-s.startupDone:
	case <-time.After(10 * time.Second):
		t.Fatal("background sniff never finished")
	}

	// 嗅探已正常完成 → Shutdown 不应改写 Partial。
	s.Shutdown()

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if cache == nil {
		t.Fatal("LoadCache returned nil after a completed sniff")
	}
	if cache.Partial {
		t.Error("F-55 复现：Shutdown marked an already-completed sniff as Partial=true " +
			"(a complete sniff must not be reported as partial)")
	}
}

// 对照用例：真正**在飞行中被中断**的嗅探仍必须标 partial（防止过度修正）。
func TestShutdown_InFlightSniffStillMarkedPartial(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	started := make(chan struct{})
	release := make(chan struct{})
	s.loadCatalog = func(string) (*manifest.Catalog, error) {
		close(started)
		<-release // 按住，使嗅探在 Shutdown 时仍在飞行
		return &manifest.Catalog{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.OnStartup(ctx)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("background sniff never started")
	}

	// 嗅探仍被按住 → 这里的中断属于「真的没跑完」。
	s.Shutdown()

	// 放行后台嗅探，并等它真正收尾——否则 goroutine 会在测试返回后继续写
	// 缓存文件，导致 t.TempDir() 的 RemoveAll 清理失败（目录非空）。
	close(release)
	select {
	case <-s.startupDone:
	case <-time.After(10 * time.Second):
		t.Fatal("background sniff did not finish after release")
	}

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if cache == nil {
		t.Fatal("LoadCache returned nil")
	}
	if !cache.Partial {
		t.Error("an in-flight (cancelled) sniff must still be marked Partial=true")
	}
}
