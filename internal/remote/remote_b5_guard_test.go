// remote_b5_guard_test.go — 审计修复 Batch 5（票 39）remote 侧守卫测试：
//
//	P2-08 / C-11 : 304 命中缓存时恢复分页游标（第二页不该丢）
//	P2-11        : retryDirect 复用包级单例 Transport（不再每次回退新建）
//	P2-30 / N-08 : ConfigureHTTPClient 不再累积 transport，且重配置时关闭空闲连接
//
// 并发说明：本机 go test -race 不可用（MinGW 8.1.0 与 Go 1.26 race 运行时不
// 兼容），因此并发相关的判据改用「平台无关的显式串行构造 + 连接/实例计数」，
// 而不是依赖竞态检测器。
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// resetGitHubCache 清空包级 TTL 缓存并登记恢复（缓存是进程级的，测试之间
// 会互相污染——它按完整 URL 做键，多个测试都用 api.github.com 的仓库路径）。
func resetGitHubCache(t *testing.T) {
	t.Helper()
	cacheMu.Lock()
	saved := cache
	cache = map[string]*cacheEntry{}
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		cache = saved
		cacheMu.Unlock()
	})
}

// ---------------------------------------------------------------------------
// P2-08 / C-11 — 304 命中缓存时恢复分页
// ---------------------------------------------------------------------------

// TestGitHubPagination_304RestoresCachedCursor：第一页命中 304 时，下一页
// 游标必须从缓存恢复并继续翻页。
//
// 修复前：304 分支直接 break（注释"the Link header is not present"），
// 于是"缓存命中"的那一轮只看得到第一页——版本列表凭空变少。
func TestGitHubPagination_304RestoresCachedCursor(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-b5-p208"
	page1 := []map[string]string{{"tag_name": "v2.0.0"}}
	page1Body, _ := json.Marshal(page1)
	page2 := []map[string]string{{"tag_name": "v1.8.0"}}
	page2Body, _ := json.Marshal(page2)

	var mu sync.Mutex
	page2Requests := 0
	page1Hits, page1NotModified := 0, 0
	page2NotModified := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			mu.Lock()
			page2Requests++
			mu.Unlock()
			if r.Header.Get("If-None-Match") == `"etag-page2"` {
				mu.Lock()
				page2NotModified++
				mu.Unlock()
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"etag-page2"`)
			w.Write(page2Body) // 最后一页：无 Link 头
			return
		}
		if r.Header.Get("If-None-Match") == `"etag-page1"` {
			// 真 GitHub 的 304 不带 Link 头——这正是 C-11 的根因。
			mu.Lock()
			page1NotModified++
			mu.Unlock()
			w.WriteHeader(http.StatusNotModified)
			return
		}
		mu.Lock()
		page1Hits++
		mu.Unlock()
		w.Header().Set("ETag", `"etag-page1"`)
		w.Header().Set("Link", `<https://api.github.com/repos/`+repo+`/releases?per_page=100&page=2>; rel="next"`)
		w.Write(page1Body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	src := manifest.RemoteSource{Type: "github", URL: repo, Pattern: `v?(\d+\.\d+\.\d+)$`}

	// 第一轮：200 两页 → 2 个版本，同时把第 1 页的游标写进缓存。
	first, err := DiscoverVersions(context.Background(), src)
	if err != nil {
		t.Fatalf("DiscoverVersions (cold): %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("cold run: got %v, want 2 versions (page1+page2)", first)
	}

	// 第二轮：两页都命中 304（缓存命中）——必须仍然拿到 2 个版本。
	second, err := DiscoverVersions(context.Background(), src)
	if err != nil {
		t.Fatalf("DiscoverVersions (cache hit): %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("cache-hit run: got %v, want 2 versions — 304 分支丢失了缓存中的下一页游标 (P2-08/C-11)", second)
	}

	mu.Lock()
	defer mu.Unlock()
	if page1NotModified == 0 {
		t.Errorf("precondition: page 1 was expected to be served from cache via 304 (hits=%d)", page1NotModified)
	}
	if page2NotModified == 0 {
		t.Errorf("page 2 was not revalidated → the cached next-page cursor was not followed (page2 requests=%d)", page2Requests)
	}
	if page1Hits != 1 {
		t.Errorf("page 1 full responses = %d, want 1 (second run must be a 304 hit)", page1Hits)
	}
}

// TestGitHubPagination_CrossHostCursorNeverCached：跨主机的 Link 头（M-1）
// 在缓存路径上同样不得被跟随——游标要么不入缓存，要么入缓存后仍被主机校验
// 拦住；两条路都不能让第二页被取到。
func TestGitHubPagination_CrossHostCursorNeverCached(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-b5-p208-xhost"
	var mu sync.Mutex
	page2Requests := 0
	body, _ := json.Marshal([]map[string]string{{"tag_name": "v2.0.0"}})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			mu.Lock()
			page2Requests++
			mu.Unlock()
			w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
			return
		}
		if r.Header.Get("If-None-Match") == `"etag-xh"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag-xh"`)
		// 恶意 Link：跳到外站（redirectTransport 也会把它带回本测试服务器，
		// 因此"page2Requests 是否增长"就是"是否跟随"的判据）。
		w.Header().Set("Link", `<https://evil.example/repos/`+repo+`/releases?per_page=100&page=2>; rel="next"`)
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	src := manifest.RemoteSource{Type: "github", URL: repo, Pattern: `v?(\d+\.\d+\.\d+)$`}
	for run := 0; run < 2; run++ { // 第二轮走 304 缓存路径
		versions, err := DiscoverVersions(context.Background(), src)
		if err != nil {
			t.Fatalf("DiscoverVersions run %d: %v", run, err)
		}
		if len(versions) != 1 {
			t.Fatalf("run %d: got %v, want exactly 1 version (cross-host page 2 must never be fetched)", run, versions)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if page2Requests != 0 {
		t.Errorf("cross-host cursor was followed (%d times) — M-1 regression on the cached-pagination path", page2Requests)
	}
}

// ---------------------------------------------------------------------------
// P2-11 — retryDirect 复用单例 Transport
// ---------------------------------------------------------------------------

// TestRetryDirect_ReusesSingleTransport：两次直连回退必须共用同一个连接池
// （同一个 TCP 连接），证明 transport 是包级单例。
//
// 修复前每次 retryDirect 都新建 &http.Transport{} → 每次回退一条新连接
// （服务端看到 2 条）；代理不稳定时会话里反复回退即连接累积。
func TestRetryDirect_ReusesSingleTransport(t *testing.T) {
	var mu sync.Mutex
	newConns := 0 // StateNew 事件总数 = 服务端看到的 TCP 连接数

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	server.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s != http.StateNew {
			return
		}
		mu.Lock()
		newConns++
		mu.Unlock()
	}
	server.Start()
	defer server.Close()

	origHook := testHookDirectTransport
	testHookDirectTransport = nil
	defer func() { testHookDirectTransport = origHook }()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := retryDirect(ctx, req)
		if err != nil {
			t.Fatalf("retryDirect #%d: %v", i, err)
		}
		if _, err := io.ReadAll(resp.Body); err != nil {
			resp.Body.Close()
			t.Fatalf("drain body #%d: %v", i, err)
		}
		resp.Body.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	// 连接建立次数必须为 1（keep-alive 复用）；修复前每次回退新建 transport → 3。
	if newConns != 1 {
		t.Errorf("retryDirect opened %d TCP connections across 3 calls, want 1 — the direct transport is not a singleton (P2-11)", newConns)
	}
}

// TestDirectTransportForRetry_HookWinsAndIsSingleton：测试钩子优先（既有
// remote_test.go:1767/1802 两处代理回退测试靠它），未设钩子时返回同一个
// 显式无代理的 transport 实例。
func TestDirectTransportForRetry_HookWinsAndIsSingleton(t *testing.T) {
	origHook := testHookDirectTransport
	defer func() { testHookDirectTransport = origHook }()

	hook := &failRoundTripper{failErr: fmt.Errorf("hook")}
	testHookDirectTransport = hook
	if got := directTransportForRetry(); got != http.RoundTripper(hook) {
		t.Fatalf("testHookDirectTransport not honored: got %T %v, want the injected hook", got, got)
	}

	testHookDirectTransport = nil
	first := directTransportForRetry()
	tr, ok := first.(*http.Transport)
	if !ok {
		t.Fatalf("direct transport = %T, want *http.Transport", first)
	}
	if tr.Proxy != nil {
		t.Error("direct transport must be explicitly proxy-less (Proxy == nil)")
	}
	for i := 0; i < 25; i++ {
		if got := directTransportForRetry(); got != first {
			t.Fatalf("call %d returned a different transport (%p vs %p) — transports accumulate per call (P2-11)", i, got, first)
		}
	}
}

// ---------------------------------------------------------------------------
// P2-30 / N-08 — ConfigureHTTPClient 不累积 transport + 关闭空闲连接
// ---------------------------------------------------------------------------

// TestConfigureHTTPClient_DoesNotAccumulateTransports：反复配置（GUI 每次
// 改代理/重载配置都会调用）必须始终复用同一个 transport 实例。
//
// 修复前每调用一次就 new 一个 *http.Transport 并替换 httpClient.Transport，
// 旧的一直没人 CloseIdleConnections → 空闲连接池累积。
func TestConfigureHTTPClient_DoesNotAccumulateTransports(t *testing.T) {
	origTransport := httpClient.Transport
	origProxyURL := configuredProxyURL
	defer func() { restoreHTTPClientState(origTransport, origProxyURL) }()

	ConfigureHTTPClient("http://127.0.0.1:7890")
	first := httpClient.Transport
	if first == nil {
		t.Fatal("ConfigureHTTPClient: transport must not be nil")
	}
	seen := map[http.RoundTripper]int{first: 1}
	for i := 0; i < 20; i++ {
		ConfigureHTTPClient(fmt.Sprintf("http://127.0.0.1:%d", 8000+i))
		seen[httpClient.Transport]++
	}
	if len(seen) != 1 {
		t.Fatalf("%d distinct transports created across 21 ConfigureHTTPClient calls, want 1 (P2-30/N-08)", len(seen))
	}
	if _, ok := httpClient.Transport.(*http.Transport); !ok {
		t.Fatalf("transport = %T, want *http.Transport", httpClient.Transport)
	}
	if got := httpClient.Transport.(*http.Transport); got != httpTransport {
		t.Error("httpClient.Transport is not the package-level singleton transport")
	}

	// 代理口径必须以最后一次调用为准（单例是原地更新，不能残留旧值）。
	ConfigureHTTPClient("http://127.0.0.1:9999")
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	proxyURL, err := httpClient.Transport.(*http.Transport).Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:9999" {
		t.Errorf("proxy = %v, want the last configured value http://127.0.0.1:9999", proxyURL)
	}
}

// TestConfigureHTTPClient_InvalidProxyIsDirect：非法代理 URL 的语义与修复前
// 等价（那时新建的裸 transport 没有 Proxy → 直连），且仍然复用同一个单例。
func TestConfigureHTTPClient_InvalidProxyIsDirect(t *testing.T) {
	origTransport := httpClient.Transport
	origProxyURL := configuredProxyURL
	defer func() { restoreHTTPClientState(origTransport, origProxyURL) }()

	// 本用例只关心 transport 复用与直连语义；WARNING 日志由
	// TestConfigureHTTPClient_InvalidProxyWarnsWithoutCredentials 断言。
	origWriter := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origWriter)

	ConfigureHTTPClient("http://[::1") // 未闭合的 IPv6 字面量 → url.Parse 失败
	before := httpClient.Transport
	ConfigureHTTPClient("http://[::1") // 再来一次：单例不累积
	if httpClient.Transport != before {
		t.Error("invalid proxy URL changed the transport instance — accumulation (P2-30/N-08)")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	proxyURL, err := before.(*http.Transport).Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if proxyURL != nil {
		t.Errorf("proxy = %v, want nil (direct) for an unparsable proxy URL", proxyURL)
	}
}

// TestConfigureHTTPClient_InvalidProxyWarnsWithoutCredentials（票 40 / P2-16 后半）：
// 无效代理 URL 必须留下 WARNING 日志（修复前静默忽略 → 用户以为代理生效、
// 实际一直直连），且日志不得回显 userinfo 里的凭据（url.Parse 的错误文本会
// 原样带上 URL），同时保持"解析失败 → 直连"的既有语义。
func TestConfigureHTTPClient_InvalidProxyWarnsWithoutCredentials(t *testing.T) {
	origTransport := httpClient.Transport
	origProxyURL := configuredProxyURL
	defer func() { restoreHTTPClientState(origTransport, origProxyURL) }()

	var buf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origWriter)

	const (
		user   = "alice"
		secret = "s3cr3t-p4ssw0rd"
	)
	// userinfo + 未闭合的 IPv6 字面量：url.Parse 失败，且错误文本里含原始 URL。
	ConfigureHTTPClient("http://" + user + ":" + secret + "@[::1")

	out := buf.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "invalid proxy URL") {
		t.Errorf("invalid proxy URL did not produce a WARNING log; log=%q", out)
	}
	if strings.Contains(out, secret) {
		t.Errorf("WARNING log leaks the proxy password; log=%q", out)
	}
	if strings.Contains(out, user+":") || strings.Contains(out, "@[::1") && !strings.Contains(out, "<redacted>@") {
		t.Errorf("WARNING log leaks proxy userinfo unredacted; log=%q", out)
	}
	if !strings.Contains(out, "<redacted>@") {
		t.Errorf("expected the userinfo段 to be redacted to <redacted>@; log=%q", out)
	}

	// 语义不变：解析失败 → 直连（Proxy 返回 nil），且仍复用同一单例。
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	proxyURL, err := httpClient.Transport.(*http.Transport).Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if proxyURL != nil {
		t.Errorf("proxy = %v, want nil (direct fallback) for an unparsable proxy URL", proxyURL)
	}
	if httpClient.Transport != httpTransport {
		t.Error("invalid proxy URL must still reuse the singleton transport")
	}
}

// TestConfigureHTTPClient_ClosesIdleConnections：重配置代理后，旧口径下
// 建立并留在池里的空闲连接必须被关闭（否则复用旧连接会绕过新代理配置）。
func TestConfigureHTTPClient_ClosesIdleConnections(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")

	var mu sync.Mutex
	live := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	server.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch s {
		case http.StateNew:
			live++
		case http.StateClosed, http.StateHijacked:
			live--
		}
	}
	server.Start()
	defer server.Close()

	origTransport := httpClient.Transport
	origProxyURL := configuredProxyURL
	defer func() { restoreHTTPClientState(origTransport, origProxyURL) }()

	ConfigureHTTPClient("") // 直连（127.0.0.1 本来也不经代理）
	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("GET test server: %v", err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		resp.Body.Close()
		t.Fatalf("drain body: %v", err)
	}
	resp.Body.Close()

	if !waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return live == 1 // 连接已回到 keep-alive 空闲池
	}) {
		t.Fatalf("precondition: expected 1 idle keep-alive connection, live=%d", liveCount(&mu, &live))
	}

	// 重配置（代理口径变化）→ 必须关闭空闲连接。
	ConfigureHTTPClient("http://127.0.0.1:7891")
	if !waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return live == 0
	}) {
		t.Errorf("idle connection survived reconfiguration (live=%d) — CloseIdleConnections semantics missing (P2-30/N-08)", liveCount(&mu, &live))
	}
}

// liveCount 是读取 live 的小助手（避免在判据函数外重复加锁）。
func liveCount(mu *sync.Mutex, live *int) int {
	mu.Lock()
	defer mu.Unlock()
	return *live
}

// waitFor 轮询 cond 直到为真或超时。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// restoreHTTPClientState 复原 httpClient/单例 transport 的进程级状态，
// 避免本文件的守卫测试污染同包其它测试（它们共享 httpClient 与 configuredProxyURL）。
func restoreHTTPClientState(origTransport http.RoundTripper, origProxyURL string) {
	httpTransportMu.Lock()
	currentProxyFn = http.ProxyFromEnvironment
	httpTransportMu.Unlock()
	httpTransport.CloseIdleConnections()
	httpClient.Transport = origTransport
	configuredProxyURL = origProxyURL
}
