// round8_guard_test.go — bugx round 8 守卫测试（remote 版本发现链）：
//
//	P1-4 : 带凭据的原始 URL 直接进入用户可见错误链（html/json 两条主路径）
//	P2-8 : F-16 回归 —— 304 缓存蒸发把「可自愈重试」变成了硬错误
//	P3-9 : visited 不做 URL 规范化，排列查询串即可绕过环检测
//
// 并发说明同 remote_b5_guard_test.go：本机 go test -race 不可用，故用显式
// 计数与「请求数上限」做判据。
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// ---------------------------------------------------------------------------
// P1-4 — 用户可见错误链中的 URL 凭据
// ---------------------------------------------------------------------------

// TestP1_4_HTMLUnsafeURLErrorRedactsCredentials：SSRF 校验失败时
// `unsafe URL %q` 原样拼入 r.URL —— userinfo 与 ?token=SECRET 全部泄漏。
func TestP1_4_HTMLUnsafeURLErrorRedactsCredentials(t *testing.T) {
	const secret = "SECRET-r8-html"
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     "http://user:pass@169.254.169.254/list?token=" + secret + "&page=1",
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err == nil {
		t.Fatal("expected an unsafe-URL rejection")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("html error chain leaks the query credential: %s", msg)
	}
	if strings.Contains(msg, "user:pass") {
		t.Errorf("html error chain leaks URL userinfo: %s", msg)
	}
	if !strings.Contains(msg, "unsafe URL") {
		t.Errorf("error = %v, want it to still name the unsafe URL", err)
	}
}

// TestP1_4_JSONUnsafeURLErrorRedactsCredentials：json 路径同款泄漏。
func TestP1_4_JSONUnsafeURLErrorRedactsCredentials(t *testing.T) {
	const secret = "SECRET-r8-json"
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  "http://user:pass@169.254.169.254/list?token=" + secret + "&page=1",
		Path: "versions",
	})
	if err == nil {
		t.Fatal("expected an unsafe-URL rejection")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("json error chain leaks the query credential: %s", msg)
	}
	if strings.Contains(msg, "user:pass") {
		t.Errorf("json error chain leaks URL userinfo: %s", msg)
	}
	if !strings.Contains(msg, "unsafe URL") {
		t.Errorf("error = %v, want it to still name the unsafe URL", err)
	}
}

// TestP1_4_HTMLFetchErrorRedactsCredentials：网络失败路径
// `html remote: fetch %s: %w` 同样不得原样回显 r.URL。
//
// 用一个必然失败的本地端口（127.0.0.1 被 IsSafeExternalURL 放行，因 localhost
// 允许 http）制造真实的 fetch 失败。
func TestP1_4_HTMLFetchErrorRedactsCredentials(t *testing.T) {
	const secret = "SECRET-r8-fetch"
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     "http://127.0.0.1:1/list?token=" + secret + "&user=alice",
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err == nil {
		t.Fatal("expected the fetch to fail against a closed port")
	}
	if msg := err.Error(); strings.Contains(msg, secret) {
		t.Errorf("html fetch error leaks the query credential: %s", msg)
	}
}

// TestP1_4_JSONFetchErrorRedactsCredentials：json 路径同款。
func TestP1_4_JSONFetchErrorRedactsCredentials(t *testing.T) {
	const secret = "SECRET-r8-jsonfetch"
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  "http://127.0.0.1:1/list?token=" + secret,
		Path: "versions",
	})
	if err == nil {
		t.Fatal("expected the fetch to fail against a closed port")
	}
	if msg := err.Error(); strings.Contains(msg, secret) {
		t.Errorf("json fetch error leaks the query credential: %s", msg)
	}
}

// TestP1_4_Control_PlainURLStillVisible 是对照组：无凭据的 URL 必须仍然
// 完整出现在错误里（脱敏不能把可诊断性一起删掉）。
func TestP1_4_Control_PlainURLStillVisible(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     "http://user:pass@127.0.0.1:1/index.html",
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err == nil {
		t.Fatal("expected the fetch to fail")
	}
	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("error = %v, want the URL path to remain diagnosable", err)
	}
}

// TestP1_4_GitHubFetchErrorRedactsCredentials：github 路径的 `fetch %s` 同样
// 原样拼 apiURL（ownerRepo 来自 manifest），必须脱敏。
func TestP1_4_GitHubFetchErrorRedactsCredentials(t *testing.T) {
	resetGitHubCache(t)

	const secret = "SECRET-r8-gh"
	origClient := httpClient
	httpClient = &http.Client{
		Transport: &failRoundTripper{failErr: fmt.Errorf(
			`Get "https://api.github.com/repos/o/r?token=%s": dial tcp: connection refused`, secret)},
		Timeout: 5 * time.Second,
	}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "o/r?token=" + secret,
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Fatal("expected the fetch to fail")
	}
	if msg := err.Error(); strings.Contains(msg, secret) {
		t.Errorf("github fetch error leaks credentials: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// P2-8 — 304 缓存蒸发：最多重试 1 次（自愈），不得变成硬错误
// ---------------------------------------------------------------------------

// TestP2_8_304EvaporatedCache_SelfHealsOnce：第一次请求带 If-None-Match
// （缓存条目被并发清掉后仍回了 304），第二次是**不带 If-None-Match 的普通
// GET**（ifNoneMatch 是循环体内的局部变量，重入时会重新 cacheGet → 空），
// 服务端回 200 → 发现流程正常完成。
//
// 修复前这里直接 return 硬错误："304 ... but no cached response is available"。
func TestP2_8_304EvaporatedCache_SelfHealsOnce(t *testing.T) {
	resetGitHubCache(t)

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// 第一跳：无条件 304（模拟缓存条目在请求与响应之间蒸发）。
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// 第二跳：普通 GET（无 If-None-Match）→ 正常 200。
		_, _ = w.Write([]byte(`[{"tag_name":"v9.9.9"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo-304-heal",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("a 304 with an evaporated cache must self-heal with one plain retry, got: %v", err)
	}
	if len(versions) != 1 || versions[0] != "9.9.9" {
		t.Errorf("versions = %v, want [9.9.9]", versions)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Errorf("requests = %d, want exactly 2 (one 304 + one self-healing plain GET)", requests)
	}
}

// TestP2_8_304AlwaysEvaporated_GivesUpAfterOneRetry：服务端对**任何**请求都
// 无条件回 304。自愈重试必须封顶 1 次，然后以明确错误终止（F-16 的初衷：
// 不能无限重发同一个请求）。
func TestP2_8_304AlwaysEvaporated_GivesUpAfterOneRetry(t *testing.T) {
	resetGitHubCache(t)

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 20 {
			t.Errorf("304 loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     "test/repo-304-always",
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on an unconditional 304 (P2-8: retry cap missing)")
	}
	if err == nil {
		t.Error("an always-304 server must eventually surface an error, got nil")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Errorf("unconditional 304 issued %d requests, want exactly 2 (1 retry cap)", requests)
	}
}

// ---------------------------------------------------------------------------
// P3-9 — visited 必须做 URL 规范化
// ---------------------------------------------------------------------------

// TestP3_9_PermutedQueryStringLoopDetected：每次 hop 都生成逻辑上同一页、但
// 查询参数**顺序**不同的 URL。修复前 visited 按原始字符串判等 → 0 次命中，
// 只能靠 maxGitHubPages 硬上限终止（烧掉 100 次请求）。
func TestP3_9_PermutedQueryStringLoopDetected(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-permute"
	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 30 {
			t.Errorf("permuted-query loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// 逻辑上永远是同一页（per_page=100, page=2），只是参数顺序不同。
		var link string
		if n%2 == 0 {
			link = fmt.Sprintf(`<https://api.github.com/repos/%s/releases?page=2&per_page=100>; rel="next"`, repo)
		} else {
			link = fmt.Sprintf(`<https://api.github.com/repos/%s/releases?per_page=100&page=2>; rel="next"`, repo)
		}
		w.Header().Set("Link", link)
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     repo,
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
		if err != nil {
			t.Errorf("DiscoverVersions with a permuted-query loop: unexpected error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on a permuted-query loop (P3-9)")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests > 3 {
		t.Errorf("permuted-query loop caused %d requests; a normalized visited key must stop after the first repeat", requests)
	}
}

// TestP3_9_NonMonotonicPageStops：审计 H9 的精确形状 —— 每次 hop 追加一个
// **取值不断变化**的无关参数（`?per_page=100&page=2&mark=…&i=N`），因此仅靠
// 查询串规范化无法折叠成同一个键。第二道守卫（page 参数必须单调递增）必须兜住：
// page 停在 2 不再前进 → 停止翻页，而不是烧掉 100 次请求。
func TestP3_9_NonMonotonicPageStops(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-nonmono"
	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 30 {
			t.Errorf("non-monotonic page loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// page 恒为 2，但 mark/i 每次都变 → 规范化键各不相同。
		w.Header().Set("Link", fmt.Sprintf(
			`<https://api.github.com/repos/%s/releases?per_page=100&page=2&mark=%d&i=%d>; rel="next"`, repo, n, n))
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     repo,
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		}); err != nil {
			t.Errorf("DiscoverVersions with a non-monotonic page sequence: unexpected error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on a non-monotonic page sequence (P3-9)")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests > 3 {
		t.Errorf("non-monotonic page sequence caused %d requests; the page-monotonicity guard must stop it (audit H9: 100 requests burned one hourly quota)", requests)
	}
}

// TestP3_9_Control_DistinctPagesStillPaginate 是对照组：真正不同的页
// （page=2、page=3）必须继续翻，规范化不能把正常分页误判成环。
func TestP3_9_Control_DistinctPagesStillPaginate(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-permute-ok"
	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			w.Header().Set("Link", `<https://api.github.com/repos/`+repo+`/releases?per_page=100&page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"tag_name":"v3.0.0"}]`))
		case "2":
			// 参数顺序刻意打乱：仍是一个**新的**页。
			w.Header().Set("Link", `<https://api.github.com/repos/`+repo+`/releases?page=3&per_page=100>; rel="next"`)
			_, _ = w.Write([]byte(`[{"tag_name":"v2.0.0"}]`))
		default:
			_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
		}
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     repo,
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("versions = %v, want 3 (normal pagination must not be mistaken for a loop)", versions)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 3 {
		t.Errorf("requests = %d, want 3", requests)
	}
}

// jsonBodyFor 构造一个最小的合法 JSON 版本列表响应。
func jsonBodyFor(versions ...string) []byte {
	b, _ := json.Marshal(map[string]any{"versions": versions})
	return b
}
