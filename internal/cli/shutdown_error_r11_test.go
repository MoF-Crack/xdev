package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
)

// 第十一轮审计（Round 10 报告待办 6）：`ShutdownSniffScheduler` **不得吞掉** Shutdown 的错误。
//
// 背景链：
//   - Round 10 P2-1 把 `Scheduler.Shutdown()` 的签名改成了 `error`（读/写失败一律
//     上报，不再 `_ =` 吞掉），并加了守卫钉住 Scheduler 侧。
//   - 但 CLI 侧的调用方 `cli.ShutdownSniffScheduler()` 仍然忽略返回值 ——
//     `sniffScheduler.Shutdown()` 的结果被**丢弃**，于是「Shutdown 刷缓存失败」
//     在 CLI 侧依然静默。**底层修好了，调用方没接上** —— 与 Round 10 的
//     `saveState` 吞错、以及本轮修掉的 `SniffAll` 吞错是**同一形态的第三次出现**。
//
// 本守卫同时钉住三层：① 函数签名返回 error；② 真的把 Scheduler 的错误往外传；
// ③ main.go 侧呈现可见但**不改写退出码**（票 33 契约）。

// bareShutdownCall 匹配「裸语句调用 sniffScheduler.Shutdown()」——
// 即返回值被丢弃的形态（`sniffScheduler.Shutdown()` 单独成句）。
var bareShutdownCall = regexp.MustCompile(`(?m)^\s*sniffScheduler\.Shutdown\(\)\s*$`)

// TestShutdownSniffScheduler_NoSchedulerIsNil 是最基本的安全断言：
// 没有 scheduler（零参 GUI 路径）时必须安全返回 nil，不得 panic。
func TestShutdownSniffScheduler_NoSchedulerIsNil(t *testing.T) {
	prev := sniffScheduler
	t.Cleanup(func() { sniffScheduler = prev })

	sniffScheduler = nil
	if err := ShutdownSniffScheduler(); err != nil {
		t.Errorf("无 scheduler 时 ShutdownSniffScheduler() = %v, want nil", err)
	}
}

// TestShutdownSniffScheduler_PropagatesSchedulerError 是行为守卫：
// Scheduler.Shutdown() 失败时，错误必须**外传**到调用方（而不是只 log）。
//
// **时序构造（照搬 Scheduler 侧既有守卫 `shutdown_f55_a04_test.go` 的做法）**：
// `Shutdown` 的**第一个** select 会在 `startupDone` 已关闭时直接 `return nil`
// （「嗅探已自行落盘，无需回写」）。因此不能只 `NewScheduler` 就调 Shutdown ——
// 那样走的是早退分支、根本不碰磁盘（本用例初版就是这样，修复后仍假绿）。
//
// 必须手工搭出「startupDone 尚未关闭」的飞行中状态（不走 OnStartup，避免真实
// goroutine 与我们争抢 close），让 Shutdown 走完第一个 select 的 default → cancel
// → 停在第二个 select → 2s 超时 → 走**兜底写盘**。把 sniff-cache.json 做成**目录**
// 使该次写盘必失败，从而让 Shutdown 返回错误。
//
// 耗时约 2s（兜底窗口），与 Scheduler 侧既有守卫同量级。
func TestShutdownSniffScheduler_PropagatesSchedulerError(t *testing.T) {
	prev := sniffScheduler
	t.Cleanup(func() { sniffScheduler = prev })

	home := t.TempDir()
	cachePath := filepath.Join(home, "sniff-cache.json")
	// 缓存路径是**目录** → 兜底写盘必失败（MkdirAll/WriteFile 都失败）。
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    1,
		RateLimitPerHr: 60,
	}
	s := discovery.NewScheduler(cfg, home, t.TempDir())

	// 手工搭出「嗅探仍在飞行」的状态（startupDone 不关闭）→ 强制走 2s 兜底写盘。
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.SetStartupStateForTest(ctx, cancel, make(chan struct{}))

	sniffScheduler = s

	if err := ShutdownSniffScheduler(); err == nil {
		t.Errorf("待办 6 复现：Shutdown 兜底写盘失败（缓存路径是目录），" +
			"但 ShutdownSniffScheduler() 返回 nil —— 错误被吞掉了")
	}
}

// TestShutdownSniffScheduler_SourceGuard 是**源码级**守卫：确定性地钉住契约。
//
// 为什么需要源码守卫：上面那条行为断言依赖 Shutdown 的 2s 兜底时序，若日后实现
// 细节变化（例如兜底不再写盘）就可能「碰巧不报错」而假绿。源码守卫不受时序影响。
func TestShutdownSniffScheduler_SourceGuard(t *testing.T) {
	data, err := os.ReadFile("context.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	if !strings.Contains(src, "func ShutdownSniffScheduler() error {") {
		t.Error("ShutdownSniffScheduler 必须返回 error（Round 10 待办 6）")
	}
	if !strings.Contains(src, "if err := sniffScheduler.Shutdown(); err != nil {") {
		t.Error("必须检查 sniffScheduler.Shutdown() 的返回值并外传（不得吞掉）")
	}
	if bareShutdownCall.MatchString(src) {
		t.Error("不得以裸语句形式调用 sniffScheduler.Shutdown()（返回值会被吞掉）")
	}
}

// TestMainShutdownErrorIsSurfaced 钉住 main.go 侧的呈现：
// 收尾刷盘失败必须写 stderr（可见），且**不得**影响退出码。
//
// 票 33（P1-29 / E-10）确立的契约是「退出码原样反映子进程真实退出码」，
// 一次收尾缓存刷盘失败不该覆盖它 —— 否则 CI 会把「工具跑成功但缓存没刷上」
// 误判成命令失败。
func TestMainShutdownErrorIsSurfaced(t *testing.T) {
	data, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	marker := "if shutdownErr := cli.ShutdownSniffScheduler(); shutdownErr != nil {"
	idx := strings.Index(src, marker)
	if idx < 0 {
		t.Fatal("main.go 必须检查 ShutdownSniffScheduler() 的返回值（Round 10 待办 6）")
	}
	if !strings.Contains(src, "Fprintf(os.Stderr") {
		t.Error("收尾刷盘失败必须写到 stderr（可见）")
	}
	// 该分支内不得 os.Exit（会覆盖子进程真实退出码）。
	end := strings.Index(src[idx:], "\n\t}")
	if end < 0 {
		t.Fatal("无法定位 shutdown 错误分支的结尾")
	}
	if block := src[idx : idx+end]; strings.Contains(block, "os.Exit") {
		t.Error("收尾刷盘失败分支内不得 os.Exit（会覆盖票 33 的子进程退出码契约）")
	}
}
