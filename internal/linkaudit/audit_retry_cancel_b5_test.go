// audit_retry_cancel_b5_test.go — 审计修复 Batch 5（票 39 / P2-21）守卫：
// 瞬时错误重试循环里的 ctx.Done() 必须真正终止重试循环。
//
// 观察手段：Request.Clock 是结果最终化的唯一时钟来源（resultFrom 每次都取一次
// CheckedAt）。修复前 select 里写的是裸 break——只跳出 select，循环继续，于是
// 已取消的上下文还会被多跑两轮重试（每轮都会 inspect + 重新最终化结果）。
// 计数最终化次数即可分辨"循环真的停了"与"只是跳出了 select"，且不依赖
// http.Client 在已取消上下文上是否真的调用 transport（实现细节）。
package linkaudit_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/linkaudit"
)

func TestAuditRetryLoopStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var transportCalls int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		// 首个请求（首轮的 HEAD）内取消上下文：后续 select 必然立刻看到 Done。
		if atomic.AddInt32(&transportCalls, 1) == 1 {
			cancel()
		}
		return nil, context.DeadlineExceeded // 瞬时错误 → 进入重试循环
	})}

	var finalizations int32
	clock := func() time.Time {
		atomic.AddInt32(&finalizations, 1)
		return fixedClock()
	}

	results, err := linkaudit.Audit(ctx, linkaudit.Request{
		Mode:              linkaudit.ModeProbe,
		Client:            client,
		AllowHTTPForTests: true,
		Clock:             clock,
		Concurrency:       1,
		Entries:           []linkaudit.Entry{{Tool: "tool", Role: "official", URL: "http://127.0.0.1:9/file.zip"}},
	})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].FailureReason != linkaudit.StatusTransientError {
		t.Fatalf("FailureReason = %q, want %q (precondition: transient → retry loop entered)",
			results[0].FailureReason, linkaudit.StatusTransientError)
	}

	// 期望最终化次数：首轮 1 次 + retry=0 的立即重试 1 次 = 2。
	// 修复前 = 4（retry=1/2 各多一次：break 只跳出 select）。
	if got := atomic.LoadInt32(&finalizations); got > 2 {
		t.Errorf("results finalized %d times after ctx cancellation, want ≤ 2 — the retry loop kept running instead of stopping (P2-21)", got)
	}
}
