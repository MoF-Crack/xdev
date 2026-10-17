// net_chain_guard_test.go — bugx round 7 守卫测试（remote 版本发现链）：
//
//	F-15 : Link 分页无环检测、无迭代上限 → 同主机自环导致无限翻页
//	F-16 : 304 且缓存缺失时重发完全相同的请求 → 无限循环
//	F-19 : discoverHTML 在 2 MiB 处静默截断，版本无声丢失
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
// F-15 — 同主机自环的 Link 分页
// ---------------------------------------------------------------------------

// TestF15_SameHostPaginationLoop_Stops：上游把 rel="next" 指回自己（同主机
// 自环）。修复前循环无 visited 集合、无迭代上限 → 请求数无界增长
// （PoC 里 30 次限流后仍未停止）。
func TestF15_SameHostPaginationLoop_Stops(t *testing.T) {
	resetGitHubCache(t)

	var mu sync.Mutex
	requests := 0
	body, _ := json.Marshal([]map[string]string{{"tag_name": "v1.0.0"}})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 50 {
			// 兜底：万一修复没生效，让用例快速失败而不是挂死。
			t.Errorf("pagination loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// 自环：next 指回本页（同主机、同 URL）。
		w.Header().Set("Link", `<https://api.github.com/repos/test/repo-loop/releases?per_page=100>; rel="next"`)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     "test/repo-loop",
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
		// 环被识别后应正常返回已取到的版本，而不是报错。
		if err != nil {
			t.Errorf("DiscoverVersions with a self-loop Link: unexpected error: %v", err)
			return
		}
		if len(versions) != 1 || versions[0] != "1.0.0" {
			t.Errorf("versions = %v, want [1.0.0]", versions)
		}
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on a self-looping Link header (F-15: infinite pagination)")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests > 3 {
		t.Errorf("self-loop Link caused %d requests; a visited-set guard must stop after the first repeat", requests)
	}
}

// TestF15_PaginationCycleAtoB_Stops：A → B → A 的环也必须被识别。
//
// 注意 A 必须与循环起点**逐字符相同**：分页起点是
// `.../releases?per_page=100`（不带 page 参数），所以 B 的 rel="next" 要指回
// 这个精确 URL，才构成真正的文本环（visited 按 URL 字符串判等）。
func TestF15_PaginationCycleAtoB_Stops(t *testing.T) {
	resetGitHubCache(t)

	const repo = "test/repo-cycle"
	// 循环起点：与 fetchGitHubPages 构造的 apiURL 逐字符一致。
	const startURL = "https://api.github.com/repos/" + repo + "/releases?per_page=100"

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 50 {
			t.Errorf("pagination loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			// B → A：指回精确的循环起点。
			w.Header().Set("Link", `<`+startURL+`>; rel="next"`)
			_, _ = w.Write([]byte(`[{"tag_name":"v1.1.0"}]`))
			return
		}
		// A → B
		w.Header().Set("Link", `<https://api.github.com/repos/`+repo+`/releases?per_page=100&page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"tag_name":"v1.2.0"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     repo,
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
		if err != nil {
			t.Errorf("DiscoverVersions with an A→B→A cycle: unexpected error: %v", err)
			return
		}
		// 两页各一个版本，环被截断后不应重复累加。
		if len(versions) != 2 {
			t.Errorf("versions = %v, want exactly the 2 pages' versions (no cycle duplication)", versions)
		}
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on an A→B→A Link cycle (F-15)")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests > 3 {
		t.Errorf("A→B→A cycle caused %d requests, want ≤ 3 (each page fetched once + the repeat detected)", requests)
	}
}

// TestF15_HardPageCap：即使每一页都是**新的**同主机 URL（无法靠 visited
// 判环，例如上游无限生成 page=N），也必须有一个硬上限兜底，不能无限翻页。
func TestF15_HardPageCap(t *testing.T) {
	resetGitHubCache(t)

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// 永远有下一页，且每页 URL 都不同（visited 集合抓不住）。
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		var n int
		_, _ = fmt.Sscanf(page, "%d", &n)
		w.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/repos/test/repo-cap/releases?per_page=100&page=%d>; rel="next"`, n+1))
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = &http.Client{Transport: &redirectTransport{server.URL}, Timeout: 10 * time.Second}
	defer func() { httpClient = origClient }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = DiscoverVersions(context.Background(), manifest.RemoteSource{
			Type:    "github",
			URL:     "test/repo-cap",
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on an unbounded page sequence (F-15: no hard iteration cap)")
	}

	mu.Lock()
	defer mu.Unlock()
	if requests > maxGitHubPages+1 {
		t.Errorf("unbounded pagination issued %d requests, want ≤ %d (hard cap)", requests, maxGitHubPages+1)
	}
}

// ---------------------------------------------------------------------------
// F-16 — 304 且缓存缺失
// ---------------------------------------------------------------------------

// TestF16_304WithEvaporatedCache_NoInfiniteLoop：服务端对任何请求都无条件回
// 304。修复前 `if !ok { continue }` 重入循环时请求逐字节相同 → 无限循环
// （PoC：30 次限流后仍未停止）。缓存蒸发是异常状态，最终必须明确报错。
//
// P2-8 后本用例的**请求计数断言**已更新：F-16 修复时把这条路径换成了硬错误，
// 依据是「continue 重入循环时 ifNoneMatch 仍从同一个缺失的缓存取 → 发出的请求
// 与上一次逐字节相同」。该断言不成立 —— ifNoneMatch 是循环体内的局部变量，
// continue 会重新执行 cacheGet，缓存已被删除 → ifNoneMatch = ""，第二次是
// **不带 If-None-Match 的普通 GET**（服务端若正常应答 200 即自愈完成）。
// 现在恢复重试并封顶 **1 次**：F-16 的红线（必须终止 + 必须报错 + 不得无界重发）
// 全部保留，只是允许一次自愈尝试。请求数上限因此从 1 放宽到 2。
func TestF16_304WithEvaporatedCache_NoInfiniteLoop(t *testing.T) {
	resetGitHubCache(t)

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n > 50 {
			t.Errorf("304 loop ran away: %d requests", n)
			http.Error(w, "runaway", http.StatusTooManyRequests)
			return
		}
		// 无条件 304（不依赖 If-None-Match），且缓存里没有对应条目。
		w.WriteHeader(http.StatusNotModified)
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
			URL:     "test/repo-304",
			Pattern: `v?(\d+\.\d+\.\d+)$`,
		})
		// 304 且无缓存 → 必须报错（而不是静默重试或返回空列表）。
		if err == nil {
			t.Error("304 with an evaporated cache must surface an error, got nil")
		}
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DiscoverVersions did not terminate on an unconditional 304 (F-16: identical request loop)")
	}

	mu.Lock()
	defer mu.Unlock()
	// P2-8：1 次自愈重试 + 1 次最终判定 = 最多 2 次；修复前是无界重发。
	if requests > 2 {
		t.Errorf("unconditional 304 issued %d identical requests, want ≤ 2 (one self-healing retry, then give up)", requests)
	}
}

// ---------------------------------------------------------------------------
// F-19 — discoverHTML 静默截断
// ---------------------------------------------------------------------------

// TestF19_HTMLOverLimit_ReportsTruncation：HTML 索引页超过 2 MiB 时不得静默
// 截断（PoC：4.25 MB 页面 + 目标版本在 2 MiB 之后 → versions=[] err=nil）。
// 修复后必须给出明确错误（同包 readLimitedBody 的既有语义）。
func TestF19_HTMLOverLimit_ReportsTruncation(t *testing.T) {
	origLimit := htmlBodyLimit
	htmlBodyLimit = 1024 // 便于构造：>1 KiB 即触发
	defer func() { htmlBodyLimit = origLimit }()

	// 页面里有一个版本链接，但整体超过上限（真实场景是它落在截断点之后）。
	padding := strings.Repeat("<!-- filler -->", 200) // ~3 KiB
	page := "<html><body>" + padding + `<a href="/dl/9.9.9/">9.9.9</a></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     server.URL,
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err == nil {
		t.Fatalf("silently truncated HTML page accepted: got %d versions (page=%d bytes, limit=%d) — F-19",
			len(versions), len(page), htmlBodyLimit)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v, want it to mention truncation", err)
	}
}

// TestF19_HTMLWithinLimit_StillWorks 是对照组：不超限的页面必须照常解析。
func TestF19_HTMLWithinLimit_StillWorks(t *testing.T) {
	page := `<html><body><a href="/dl/1.2.3/">1.2.3</a></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     server.URL,
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions html: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.2.3" {
		t.Errorf("versions = %v, want [1.2.3]", versions)
	}
}
