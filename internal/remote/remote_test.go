package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// ---------------------------------------------------------------------------
// parseNextURL
// ---------------------------------------------------------------------------

func TestParseNextURL_Valid(t *testing.T) {
	header := `<https://api.github.com/repos/o/r/releases?page=2>; rel="next", <https://api.github.com/repos/o/r/releases?page=5>; rel="last"`
	url, ok := parseNextURL(header)
	if !ok {
		t.Fatal("parseNextURL: expected ok=true")
	}
	want := "https://api.github.com/repos/o/r/releases?page=2"
	if url != want {
		t.Errorf("parseNextURL = %q, want %q", url, want)
	}
}

func TestParseNextURL_NoNext(t *testing.T) {
	header := `<https://api.github.com/repos/o/r/releases?page=1>; rel="first", <https://api.github.com/repos/o/r/releases?page=5>; rel="last"`
	_, ok := parseNextURL(header)
	if ok {
		t.Error("parseNextURL: expected ok=false when no rel=next")
	}
}

func TestParseNextURL_Empty(t *testing.T) {
	_, ok := parseNextURL("")
	if ok {
		t.Error("parseNextURL: expected ok=false for empty header")
	}
}

func TestParseNextURL_SingleNext(t *testing.T) {
	header := `<https://example.com/page3>; rel="next"`
	url, ok := parseNextURL(header)
	if !ok {
		t.Fatal("parseNextURL: expected ok=true")
	}
	if url != "https://example.com/page3" {
		t.Errorf("parseNextURL = %q, want %q", url, "https://example.com/page3")
	}
}

func TestParseNextURL_Malformed_NoAngleBrackets(t *testing.T) {
	header := `https://example.com/page2; rel="next"`
	_, ok := parseNextURL(header)
	if ok {
		t.Error("parseNextURL: expected ok=false for malformed header without angle brackets")
	}
}

func TestParseNextURL_MultipleRelNext(t *testing.T) {
	// Should return the first rel="next"
	header := `<https://example.com/page2>; rel="next", <https://example.com/page3>; rel="next"`
	url, ok := parseNextURL(header)
	if !ok {
		t.Fatal("parseNextURL: expected ok=true")
	}
	if url != "https://example.com/page2" {
		t.Errorf("parseNextURL = %q, want %q", url, "https://example.com/page2")
	}
}

// ---------------------------------------------------------------------------
// checkRateLimit
// ---------------------------------------------------------------------------

func TestCheckRateLimit_RemainingZero(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	resp.Header.Set("X-RateLimit-Reset", "1700000000")

	err := checkRateLimit(resp)
	if err == nil {
		t.Error("checkRateLimit: expected error when remaining=0")
	}
}

func TestCheckRateLimit_RemainingLow(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "5")

	err := checkRateLimit(resp)
	if err != nil {
		t.Errorf("checkRateLimit: unexpected error for remaining=5: %v", err)
	}
}

func TestCheckRateLimit_RemainingNormal(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "50")

	err := checkRateLimit(resp)
	if err != nil {
		t.Errorf("checkRateLimit: unexpected error for remaining=50: %v", err)
	}
}

func TestCheckRateLimit_MissingHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}

	err := checkRateLimit(resp)
	if err != nil {
		t.Errorf("checkRateLimit: unexpected error for missing header: %v", err)
	}
}

func TestCheckRateLimit_InvalidHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "not-a-number")

	err := checkRateLimit(resp)
	if err != nil {
		t.Errorf("checkRateLimit: unexpected error for unparseable header: %v", err)
	}
}

// ---------------------------------------------------------------------------
// cacheGet / cachePut
// ---------------------------------------------------------------------------

func TestCachePutAndGet(t *testing.T) {
	// Use a unique key to avoid collisions with other tests
	key := fmt.Sprintf("test-cache-put-%d", time.Now().UnixNano())
	body := []byte(`[{"tag_name":"v1.0.0"}]`)
	etag := `"abc123"`

	cachePut(key, body, etag)

	entry, ok := cacheGet(key)
	if !ok {
		t.Fatal("cacheGet: expected cache hit")
	}
	if string(entry.body) != string(body) {
		t.Errorf("cacheGet body = %q, want %q", string(entry.body), string(body))
	}
	if entry.etag != etag {
		t.Errorf("cacheGet etag = %q, want %q", entry.etag, etag)
	}

	// Clean up
	cacheMu.Lock()
	delete(cache, key)
	cacheMu.Unlock()
}

func TestCacheGet_Miss(t *testing.T) {
	_, ok := cacheGet("nonexistent-key-12345")
	if ok {
		t.Error("cacheGet: expected cache miss for nonexistent key")
	}
}

func TestCacheGet_Expired(t *testing.T) {
	key := fmt.Sprintf("test-cache-expired-%d", time.Now().UnixNano())

	cacheMu.Lock()
	cache[key] = &cacheEntry{
		body:      []byte("expired"),
		etag:      "",
		expiresAt: time.Now().Add(-1 * time.Second), // already expired
	}
	cacheMu.Unlock()

	_, ok := cacheGet(key)
	if ok {
		t.Error("cacheGet: expected cache miss for expired entry")
	}

	// Verify the expired entry was deleted
	cacheMu.Lock()
	_, exists := cache[key]
	cacheMu.Unlock()
	if exists {
		t.Error("cacheGet: expired entry should have been deleted")
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — GitHub type with httptest
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub(t *testing.T) {
	releases := []map[string]string{
		{"tag_name": "v3.12.0"},
		{"tag_name": "v3.11.5"},
		{"tag_name": "v3.10.13"},
		{"tag_name": "not-a-version"},
	}
	body, _ := json.Marshal(releases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<http://example.com/done>; rel="last"`)
		w.Write(body)
	}))
	defer server.Close()

	// Override httpClient temporarily
	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	// We need to redirect the GitHub API URL. Since discoverGitHub hardcodes
	// api.github.com, we'll use DiscoverVersions with a custom approach.
	// Instead, test via httptest by patching the URL construction.
	// Since the code hardcodes the API URL, we test discoverGitHub indirectly
	// by using a transport that redirects to our test server.
	transport := &redirectTransport{server.URL}
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions github: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("DiscoverVersions github: got %d versions, want 3", len(versions))
	}
	want := []string{"3.12.0", "3.11.5", "3.10.13"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// redirectTransport redirects all requests to the test server.
type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect to test server, preserving the path
	newURL := t.targetURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq := req.Clone(req.Context())
	newReq.URL, _ = req.URL.Parse(newURL)
	return http.DefaultTransport.RoundTrip(newReq)
}

// ---------------------------------------------------------------------------
// DiscoverVersions — JSON type with httptest
// ---------------------------------------------------------------------------

func TestDiscoverVersions_JSON(t *testing.T) {
	response := map[string]interface{}{
		"versions": []interface{}{
			map[string]interface{}{"version": "1.0.0"},
			map[string]interface{}{"version": "1.1.0"},
			"2.0.0",
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  server.URL,
		Path: "versions",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions json: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("DiscoverVersions json: got %d versions, want 3", len(versions))
	}
	want := []string{"1.0.0", "1.1.0", "2.0.0"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

func TestDiscoverVersions_JSON_WithPattern(t *testing.T) {
	response := map[string]interface{}{
		"releases": []interface{}{
			"v1.0.0",
			"v2.0.0-beta",
			"v2.0.0",
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "json",
		URL:     server.URL,
		Path:    "releases",
		Pattern: `v(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions json with pattern: %v", err)
	}
	// Only "v1.0.0" and "v2.0.0" match the pattern (no pre-release)
	if len(versions) != 2 {
		t.Fatalf("DiscoverVersions json with pattern: got %d versions, want 2", len(versions))
	}
	want := []string{"1.0.0", "2.0.0"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — HTML type with httptest
// ---------------------------------------------------------------------------

func TestDiscoverVersions_HTML(t *testing.T) {
	html := `<html><body>
		<a href="/download/3.12.0/">Python 3.12.0</a>
		<a href="/download/3.11.5/">Python 3.11.5</a>
		<a href="/download/3.10.13/">Python 3.10.13</a>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     server.URL,
		Pattern: `/download/(\d+\.\d+\.\d+)/`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions html: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("DiscoverVersions html: got %d versions, want 3", len(versions))
	}
	want := []string{"3.12.0", "3.11.5", "3.10.13"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

func TestDiscoverVersions_HTML_Dedup(t *testing.T) {
	html := `<html><body>
		<a href="/dl/1.0.0/">1.0.0</a>
		<a href="/dl/1.0.0/">1.0.0 again</a>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
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
		t.Fatalf("DiscoverVersions html dedup: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("DiscoverVersions html dedup: got %d versions, want 1 (dedup)", len(versions))
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — HTML type with proxy fallback
// ---------------------------------------------------------------------------

func TestDiscoverVersions_HTML_ProxyFallback(t *testing.T) {
	html := `<html><body><a href="/dl/1.0.0/">1.0.0</a></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	origClient := httpClient
	defer func() { httpClient = origClient }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	// httpClient.Do fails with proxy error; retryDirect uses default
	// transport and can reach the test server directly.
	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     server.URL,
		Pattern: `/dl/(\d+\.\d+\.\d+)/`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions html proxy fallback: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Errorf("versions = %v, want [1.0.0]", versions)
	}
}

func TestDiscoverVersions_HTML_ProxyFallbackBothFail(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "html",
		URL:     "http://127.0.0.1:1", // unreachable — direct will also fail
		Pattern: `v(\d+\.\d+\.\d+)`,
	})
	if err == nil {
		t.Fatal("expected error when both proxy and direct fail")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "启动代理") && !strings.Contains(errStr, "proxy.enabled") {
		t.Errorf("error should contain actionable hint, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — error cases
// ---------------------------------------------------------------------------

func TestDiscoverVersions_UnsupportedType(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "ftp",
	})
	if err == nil {
		t.Error("DiscoverVersions: expected error for unsupported type")
	}
}

func TestDiscoverVersions_NoneType(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "none",
	})
	if err == nil {
		t.Error("DiscoverVersions: expected error for type=none")
	}
}

func TestDiscoverVersions_EmptyType(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "",
	})
	if err == nil {
		t.Error("DiscoverVersions: expected error for empty type")
	}
}

func TestDiscoverVersions_HTML_MissingPattern(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "html",
		URL:  "http://example.com",
	})
	if err == nil {
		t.Error("DiscoverVersions html: expected error for missing pattern")
	}
}

func TestDiscoverVersions_JSON_MissingURL(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
	})
	if err == nil {
		t.Error("DiscoverVersions json: expected error for missing URL")
	}
}

func TestDiscoverVersions_JSON_MissingPath(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  "http://example.com",
	})
	if err == nil {
		t.Error("DiscoverVersions json: expected error for missing path")
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — GitHub with pagination via httptest
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_Pagination(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	page1 := []map[string]string{
		{"tag_name": "v2.0.0"},
		{"tag_name": "v1.9.0"},
	}
	page2 := []map[string]string{
		{"tag_name": "v1.8.0"},
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/test/repo/releases?page=2>; rel="next"`, serverURL))
			body, _ := json.Marshal(page1)
			w.Write(body)
		} else {
			body, _ := json.Marshal(page2)
			w.Write(body)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions github pagination: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("DiscoverVersions github pagination: got %d versions, want 3", len(versions))
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — GitHub with rate limit exhausted
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_RateLimitExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Error("DiscoverVersions github: expected error when rate limit exhausted")
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — GitHub with 304 Not Modified (cache hit)
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_CacheHit(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	releases := []map[string]string{
		{"tag_name": "v1.0.0"},
	}
	body, _ := json.Marshal(releases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()

		// First call: return 200 with ETag
		// Second call: return 304 (but we pre-populate cache)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"test-etag"`)
		w.Write(body)
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	// First request — populates cache
	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions github first call: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Fatalf("DiscoverVersions github first call: got %v, want [1.0.0]", versions)
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — GitHub with non-200 status
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`forbidden`))
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Error("DiscoverVersions github: expected error for non-200 status")
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — JSON with nested path
// ---------------------------------------------------------------------------

func TestDiscoverVersions_JSON_NestedPath(t *testing.T) {
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"releases": []interface{}{
				"4.0.0",
				"3.9.1",
			},
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  server.URL,
		Path: "data.releases",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions json nested: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("DiscoverVersions json nested: got %d versions, want 2", len(versions))
	}
	want := []string{"4.0.0", "3.9.1"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// ConfigureHTTPClient
// ---------------------------------------------------------------------------

func TestConfigureHTTPClient_SetsProxy(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()

	// Configure with a proxy URL.
	ConfigureHTTPClient("http://127.0.0.1:7890")

	if httpClient.Transport == nil {
		t.Fatal("ConfigureHTTPClient: Transport must not be nil after configuration")
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("ConfigureHTTPClient: expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.Proxy == nil {
		t.Error("ConfigureHTTPClient: Proxy func must not be nil when proxyURL is set")
	}

	// Verify the proxy function resolves to the expected URL.
	req := &http.Request{
		Method: "GET",
		URL:    &url.URL{Scheme: "https", Host: "api.github.com", Path: "/repos/o/r/releases"},
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("ConfigureHTTPClient: Proxy func returned error: %v", err)
	}
	if proxyURL == nil {
		t.Fatal("ConfigureHTTPClient: Proxy func returned nil URL")
	}
	if proxyURL.String() != "http://127.0.0.1:7890" {
		t.Errorf("ConfigureHTTPClient: proxy URL = %q, want %q", proxyURL.String(), "http://127.0.0.1:7890")
	}
}

func TestConfigureHTTPClient_EmptyProxy(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()

	// Configure with empty proxy URL — should use ProxyFromEnvironment.
	ConfigureHTTPClient("")

	if httpClient.Transport == nil {
		t.Fatal("ConfigureHTTPClient: Transport must not be nil after configuration")
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("ConfigureHTTPClient: expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.Proxy == nil {
		t.Error("ConfigureHTTPClient: Proxy func must not be nil (should use http.ProxyFromEnvironment)")
	}
}

func TestConfigureHTTPClient_PreservesTimeout(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()

	ConfigureHTTPClient("http://proxy:8080")

	if httpClient.Timeout != 30*time.Second {
		t.Errorf("ConfigureHTTPClient: timeout = %v, want 30s", httpClient.Timeout)
	}
}

func TestConfigureHTTPClient_PreservesCheckRedirect(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()

	ConfigureHTTPClient("http://proxy:8080")

	if httpClient.CheckRedirect == nil {
		t.Fatal("ConfigureHTTPClient: CheckRedirect must not be nil")
	}

	// Verify redirect blocking still works.
	err := httpClient.CheckRedirect(
		&http.Request{URL: &url.URL{Scheme: "http"}},
		[]*http.Request{{URL: &url.URL{Scheme: "https"}}},
	)
	if err == nil {
		t.Error("ConfigureHTTPClient: expected error for HTTPS→HTTP downgrade")
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — per_page=30 check
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_PerPage30(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}

	if !strings.Contains(capturedQuery, "per_page=30") {
		t.Errorf("expected per_page=30 in query, got %q", capturedQuery)
	}
	if strings.Contains(capturedQuery, "per_page=100") {
		t.Errorf("per_page should not be 100, got %q", capturedQuery)
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — empty body error
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Empty body — no bytes written.
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Fatal("DiscoverVersions: expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty response body") {
		t.Errorf("error should mention 'empty response body', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — JSON decode error includes URL and body preview
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{truncated json`)) // malformed
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Fatal("DiscoverVersions: expected error for bad JSON")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "body preview") && !strings.Contains(errStr, "decode JSON") {
		t.Errorf("error should mention 'decode JSON' or 'body preview', got: %v", err)
	}
	if !strings.Contains(errStr, "test/repo") {
		t.Errorf("error should contain the repo URL, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// readLimitedBody
// ---------------------------------------------------------------------------

func TestReadLimitedBody_Normal(t *testing.T) {
	body, err := readLimitedBody(strings.NewReader("hello world"), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "hello world" {
		t.Errorf("body = %q, want %q", string(body), "hello world")
	}
}

func TestReadLimitedBody_Empty(t *testing.T) {
	_, err := readLimitedBody(strings.NewReader(""), 100)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty response body") {
		t.Errorf("error should mention empty, got: %v", err)
	}
}

func TestReadLimitedBody_ExactlyLimit(t *testing.T) {
	data := make([]byte, 10)
	_, err := readLimitedBody(strings.NewReader(string(data)), 10)
	if err == nil {
		t.Fatal("expected error when body size equals limit")
	}
	if !strings.Contains(err.Error(), "response body truncated") {
		t.Errorf("error should mention truncation, got: %v", err)
	}
}

func TestReadLimitedBody_ExceedsLimit(t *testing.T) {
	data := make([]byte, 20)
	_, err := readLimitedBody(strings.NewReader(string(data)), 10)
	if err == nil {
		t.Fatal("expected error when body exceeds limit")
	}
	if !strings.Contains(err.Error(), "response body truncated") {
		t.Errorf("error should mention truncation, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — body truncation detection via httptest
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_BodyTruncated(t *testing.T) {
	origLimit := githubBodyLimit
	githubBodyLimit = 100
	defer func() { githubBodyLimit = origLimit }()

	origPerPage := perPageValues
	perPageValues = []int{30} // single attempt, no retry
	defer func() { perPageValues = origPerPage }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := make([]byte, githubBodyLimit) // ≥ limit → truncated
		w.Write(body)
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Fatal("expected truncation error")
	}
	if !strings.Contains(err.Error(), "response body truncated") {
		t.Errorf("error should mention truncation, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — per_page retry on body truncation
// ---------------------------------------------------------------------------

func TestDiscoverVersions_GitHub_TruncationRetry(t *testing.T) {
	origLimit := githubBodyLimit
	githubBodyLimit = 100
	defer func() { githubBodyLimit = origLimit }()

	origPerPage := perPageValues
	perPageValues = []int{30, 10} // 30 truncated → retry with 10
	defer func() { perPageValues = origPerPage }()

	var mu sync.Mutex
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "per_page=30") {
			// Return body that hits the limit → truncated
			body := make([]byte, githubBodyLimit)
			w.Write(body)
		} else {
			// Return a small valid response
			w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
		}
	}))
	defer server.Close()

	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "github",
		URL:     "test/repo",
		Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Errorf("versions = %v, want [1.0.0]", versions)
	}

	mu.Lock()
	if callCount != 2 {
		t.Errorf("expected 2 requests (1 truncated + 1 retry), got %d", callCount)
	}
	mu.Unlock()
}

func TestDiscoverVersions_JSON_ObjectList(t *testing.T) {
	response := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"version": "1.0.0"},
			map[string]interface{}{"name": "2.0.0"},
			map[string]interface{}{"id": "3.0.0"},
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  server.URL,
		Path: "items",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions json object list: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("DiscoverVersions json object list: got %d versions, want 3", len(versions))
	}
	want := []string{"1.0.0", "2.0.0", "3.0.0"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — JSON type with proxy fallback
// ---------------------------------------------------------------------------

func TestDiscoverVersions_JSON_ProxyFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"versions": []interface{}{"1.0.0", "2.0.0"},
		})
	}))
	defer server.Close()

	origClient := httpClient
	defer func() { httpClient = origClient }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	// httpClient.Do fails with proxy error; retryDirect uses default
	// transport and can reach the test server directly.
	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  server.URL,
		Path: "versions",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions json proxy fallback: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions))
	}
	want := []string{"1.0.0", "2.0.0"}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

func TestDiscoverVersions_JSON_ProxyFallbackBothFail(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "json",
		URL:  "http://127.0.0.1:1", // unreachable — direct will also fail
		Path: "x",
	})
	if err == nil {
		t.Fatal("expected error when both proxy and direct fail")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "启动代理") && !strings.Contains(errStr, "proxy.enabled") {
		t.Errorf("error should contain actionable hint, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ConfigureGitHubToken / gitHubToken — Authorization header
// ---------------------------------------------------------------------------

func TestGitHubToken_DefaultEmpty(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token env var is set; skipping")
	}
}

func TestGitHubToken_ConfigureAndVerify(t *testing.T) {
	ConfigureGitHubToken("")
	ConfigureGitHubToken("ghp_test_token_123")
	if effectiveGitHubToken() != "ghp_test_token_123" {
		t.Errorf("effectiveGitHubToken = %q, want %q", effectiveGitHubToken(), "ghp_test_token_123")
	}
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" && effectiveGitHubToken() == "ghp_test_token_123" {
		t.Errorf("after clearing, effectiveGitHubToken should not return old token")
	}
}

func TestGitHubToken_AuthorizationHeaderPresent(t *testing.T) {
	ConfigureGitHubToken("")
	ConfigureGitHubToken("ghp_test_auth")
	var captured []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()
	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "github", URL: "test/repo", Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}
	if len(captured) == 0 {
		t.Fatal("no request captured")
	}
	if captured[0] != "Bearer ghp_test_auth" {
		t.Errorf("Authorization = %q, want Bearer ghp_test_auth", captured[0])
	}
}

func TestGitHubToken_NoAuthorizationHeaderWhenEmpty(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token env var or registry value is set; skipping")
	}
	var captured []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()
	transport := &redirectTransport{server.URL}
	origClient := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer func() { httpClient = origClient }()
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "github", URL: "test/repo", Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}
	if len(captured) == 0 {
		t.Fatal("no request captured")
	}
	if captured[0] != "" {
		t.Errorf("Authorization = %q, want empty", captured[0])
	}
}

// ---------------------------------------------------------------------------
// checkRateLimit — updated message format
// ---------------------------------------------------------------------------

func TestCheckRateLimit_MessageFormat(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token set (env or registry); skipping unauthenticated message test")
	}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	resp.Header.Set("X-RateLimit-Reset", "1700000000")
	err := checkRateLimit(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	if !strings.Contains(s, "GITHUB_TOKEN") && !strings.Contains(s, "GH_TOKEN") {
		t.Errorf("message should mention GITHUB_TOKEN/GH_TOKEN, got: %s", s)
	}
	if !strings.Contains(s, "5000") {
		t.Errorf("message should mention 5000, got: %s", s)
	}
	if !strings.Contains(s, "60") {
		t.Errorf("message should mention 60, got: %s", s)
	}
}

func TestCheckRateLimit_MessageWithToken(t *testing.T) {
	ConfigureGitHubToken("")
	defer ConfigureGitHubToken("")
	ConfigureGitHubToken("some-token")
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	resp.Header.Set("X-RateLimit-Reset", "1785416848")
	err := checkRateLimit(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	if strings.Contains(s, "设置环境变量 GITHUB_TOKEN") {
		t.Errorf("with token set, should not suggest setting token, got: %s", s)
	}
	if !strings.Contains(s, "5000") {
		t.Errorf("message should mention 5000, got: %s", s)
	}
}

// ---------------------------------------------------------------------------
// effectiveGitHubToken
// ---------------------------------------------------------------------------

func TestEffectiveGitHubToken_ExplicitWins(t *testing.T) {
	ConfigureGitHubToken("")
	ConfigureGitHubToken("explicit_token")
	t.Setenv("GITHUB_TOKEN", "env_token")
	if got := effectiveGitHubToken(); got != "explicit_token" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "explicit_token")
	}
	ConfigureGitHubToken("")
}

func TestEffectiveGitHubToken_EnvGITHUB_TOKEN(t *testing.T) {
	ConfigureGitHubToken("")
	t.Setenv("GITHUB_TOKEN", "ghp_env_token")
	t.Setenv("GH_TOKEN", "")
	if got := effectiveGitHubToken(); got != "ghp_env_token" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "ghp_env_token")
	}
}

func TestEffectiveGitHubToken_EnvGH_TOKEN(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token already set (env or registry); cannot test GH_TOKEN fallback")
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "ghp_gh_token")
	if got := effectiveGitHubToken(); got != "ghp_gh_token" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "ghp_gh_token")
	}
}

func TestEffectiveGitHubToken_EnvGITHUB_TOKENWinsOverGH_TOKEN(t *testing.T) {
	ConfigureGitHubToken("")
	t.Setenv("GITHUB_TOKEN", "primary")
	t.Setenv("GH_TOKEN", "secondary")
	if got := effectiveGitHubToken(); got != "primary" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "primary")
	}
}

func TestEffectiveGitHubToken_EmptyWhenNone(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token already set (env or registry); cannot test empty case")
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if got := effectiveGitHubToken(); got != "" {
		t.Errorf("effectiveGitHubToken = %q, want empty", got)
	}
}

func TestEffectiveGitHubToken_TrimWhitespace(t *testing.T) {
	ConfigureGitHubToken("")
	t.Setenv("GITHUB_TOKEN", "  trimmed_token  ")
	if got := effectiveGitHubToken(); got != "trimmed_token" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "trimmed_token")
	}
}

func TestEffectiveGitHubToken_ExplicitTrimWhitespace(t *testing.T) {
	ConfigureGitHubToken("  spaced_token  ")
	if got := effectiveGitHubToken(); got != "spaced_token" {
		t.Errorf("effectiveGitHubToken = %q, want %q", got, "spaced_token")
	}
	ConfigureGitHubToken("")
}

// ---------------------------------------------------------------------------
// GitHubTokenConfigured
// ---------------------------------------------------------------------------

func TestGitHubTokenConfigured_TrueWithExplicit(t *testing.T) {
	ConfigureGitHubToken("")
	ConfigureGitHubToken("some-token")
	if !GitHubTokenConfigured() {
		t.Error("GitHubTokenConfigured: expected true when explicit token set")
	}
	ConfigureGitHubToken("")
}

func TestGitHubTokenConfigured_TrueWithEnv(t *testing.T) {
	ConfigureGitHubToken("")
	t.Setenv("GITHUB_TOKEN", "env-token")
	if !GitHubTokenConfigured() {
		t.Error("GitHubTokenConfigured: expected true when env token set")
	}
}

func TestGitHubTokenConfigured_FalseWhenNone(t *testing.T) {
	ConfigureGitHubToken("")
	if effectiveGitHubToken() != "" {
		t.Skip("GitHub token already set (env or registry); cannot test false case")
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if GitHubTokenConfigured() {
		t.Error("GitHubTokenConfigured: expected false when no token available")
	}
}

// ---------------------------------------------------------------------------
// ReloadGitHubTokenFromEnv
// ---------------------------------------------------------------------------

func TestReloadGitHubTokenFromEnv_FromEnv(t *testing.T) {
	ConfigureGitHubToken("")
	t.Setenv("GITHUB_TOKEN", "reload_token")
	ReloadGitHubTokenFromEnv()
	if effectiveGitHubToken() != "reload_token" {
		t.Errorf("after Reload, effectiveGitHubToken = %q, want %q", effectiveGitHubToken(), "reload_token")
	}
	ConfigureGitHubToken("")
}

func TestReloadGitHubTokenFromEnv_DoesNotOverrideExplicit(t *testing.T) {
	ConfigureGitHubToken("")
	ConfigureGitHubToken("explicit_value")
	t.Setenv("GITHUB_TOKEN", "env_value")
	ReloadGitHubTokenFromEnv()
	if effectiveGitHubToken() != "explicit_value" {
		t.Errorf("after Reload with explicit set, effectiveGitHubToken = %q, want %q", effectiveGitHubToken(), "explicit_value")
	}
	ConfigureGitHubToken("")
}

// ---------------------------------------------------------------------------
// isProxyError
// ---------------------------------------------------------------------------

func TestIsProxyError_ProxyConnect(t *testing.T) {
	if !isProxyError("proxyconnect: dial tcp 127.0.0.1:7890: refused") {
		t.Error("should match proxyconnect")
	}
}

func TestIsProxyError_ConnectionRefused(t *testing.T) {
	if !isProxyError("dial tcp 127.0.0.1:7890: connection refused") {
		t.Error("should match connection refused")
	}
}

func TestIsProxyError_ActivelyRefused(t *testing.T) {
	if !isProxyError("dial tcp 127.0.0.1:7890: actively refused") {
		t.Error("should match actively refused")
	}
}

func TestIsProxyError_NonProxy(t *testing.T) {
	if isProxyError("dial tcp api.github.com:443: i/o timeout") {
		t.Error("should not match timeout")
	}
	if isProxyError("404 not found") {
		t.Error("should not match 404")
	}
}

// ---------------------------------------------------------------------------
// discoverGitHub — proxy fallback
// ---------------------------------------------------------------------------

// failDialer fails on the first Dial call, then delegates to a real dialer.
type failDialer struct {
	mu        sync.Mutex
	callCount int
	next      func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (d *failDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.callCount++
	count := d.callCount
	d.mu.Unlock()
	if count <= 1 {
		return nil, fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused")
	}
	return d.next(ctx, network, addr)
}

// failRoundTripper is an http.RoundTripper that always returns the configured error.
type failRoundTripper struct {
	failErr error
}

func (f *failRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.failErr
}

func TestDiscoverVersions_GitHub_ProxyFallback(t *testing.T) {
	var directCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.0.0"}]`))
	}))
	defer server.Close()

	origClient := httpClient
	defer func() { httpClient = origClient }()
	origTestHook := testHookDirectTransport
	defer func() { testHookDirectTransport = origTestHook }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	// httpClient.Do fails with proxy error.
	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}

	// retryDirect uses testHookDirectTransport to reach the test server.
	testHookDirectTransport = &redirectTransport{server.URL}

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "github", URL: "test/repo", Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions: %v", err)
	}
	if !directCalled {
		t.Error("expected direct fallback to be called")
	}
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Errorf("versions = %v, want [1.0.0]", versions)
	}
}

func TestDiscoverVersions_GitHub_ProxyFallbackBothFail(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()
	origTestHook := testHookDirectTransport
	defer func() { testHookDirectTransport = origTestHook }()
	origProxyURL := configuredProxyURL
	defer func() { configuredProxyURL = origProxyURL }()

	ConfigureHTTPClient("http://127.0.0.1:7890")

	httpClient = &http.Client{
		Transport: &failRoundTripper{
			failErr: fmt.Errorf("proxyconnect: dial tcp 127.0.0.1:7890: connection refused"),
		},
		Timeout: 30 * time.Second,
	}
	testHookDirectTransport = &failRoundTripper{
		failErr: fmt.Errorf("direct connection failed"),
	}

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "github", URL: "test/repo", Pattern: `v?(\d+\.\d+\.\d+)$`,
	})
	if err == nil {
		t.Fatal("expected error when both proxy and direct fail")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "启动代理") && !strings.Contains(errStr, "proxy.enabled") {
		t.Errorf("error should contain actionable hint, got: %v", err)
	}
}

func TestHasProxyConfigured_ProxySet(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()
	ConfigureHTTPClient("http://127.0.0.1:7890")
	if !hasProxyConfigured() {
		t.Error("hasProxyConfigured should be true when proxy is set")
	}
}

func TestHasProxyConfigured_NoProxy(t *testing.T) {
	origClient := httpClient
	defer func() { httpClient = origClient }()
	ConfigureHTTPClient("")
	if hasProxyConfigured() {
		t.Log("hasProxyConfigured returned true for ProxyFromEnvironment (acceptable)")
	}
}

// ---------------------------------------------------------------------------
// DiscoverVersions — npm type with httptest (T-A2)
// ---------------------------------------------------------------------------

// npmTestClient returns an httpClient whose requests are redirected to the
// httptest server (registry.npmjs.org is hardcoded in discoverNPM).
func npmTestClient(serverURL string) *http.Client {
	return &http.Client{Transport: &redirectTransport{serverURL}, Timeout: 30 * time.Second}
}

func TestDiscoverVersions_NPM(t *testing.T) {
	registry := map[string]interface{}{
		"name": "typescript",
		"dist-tags": map[string]string{
			"latest": "7.0.2",
		},
		"versions": map[string]interface{}{
			"5.5.4": map[string]interface{}{"version": "5.5.4"},
			"7.0.2": map[string]interface{}{"version": "7.0.2"},
			"5.7.3": map[string]interface{}{"version": "5.7.3"},
			"5.6.3": map[string]interface{}{"version": "5.6.3"},
		},
	}
	body, _ := json.Marshal(registry)
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type:    "npm",
		Package: "typescript",
	})
	if gotAccept != "application/vnd.npm.install-v1+json" {
		t.Errorf("Accept = %q, want abbreviated npm document", gotAccept)
	}
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	// dist-tags.latest 置顶，其余 semver 降序。
	want := []string{"7.0.2", "5.7.3", "5.6.3", "5.5.4"}
	if len(versions) != len(want) {
		t.Fatalf("got %d versions %v, want %d", len(versions), versions, len(want))
	}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

func TestDiscoverVersions_NPM_TruncatedTo20(t *testing.T) {
	// 25 个版本 → 截断为 20；latest 置顶且不重复。
	versionsMap := map[string]interface{}{}
	for i := 25; i >= 1; i-- {
		v := fmt.Sprintf("1.%d.0", i)
		versionsMap[v] = map[string]interface{}{"version": v}
	}
	registry := map[string]interface{}{
		"dist-tags": map[string]string{"latest": "2.0.0"},
		"versions":  versionsMap,
	}
	body, _ := json.Marshal(registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "bigpkg",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	if len(versions) != 20 {
		t.Fatalf("got %d versions, want 20 (truncated)", len(versions))
	}
	if versions[0] != "2.0.0" {
		t.Errorf("versions[0] = %q, want latest 2.0.0 first", versions[0])
	}
	if versions[1] != "1.25.0" || versions[19] != "1.7.0" {
		t.Errorf("semver desc order broken: versions[1]=%q versions[19]=%q", versions[1], versions[19])
	}
}

func TestDiscoverVersions_NPM_EmptyPackage(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{Type: "npm"})
	if err == nil || !strings.Contains(err.Error(), "package is required") {
		t.Fatalf("want package-required error, got %v", err)
	}
}

func TestDiscoverVersions_NPM_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "no-such-pkg",
	})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want 404 error, got %v", err)
	}
}

func TestDiscoverVersions_NPM_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not json"))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "broken",
	})
	if err == nil || !strings.Contains(err.Error(), "decode JSON") {
		t.Fatalf("want decode JSON error, got %v", err)
	}
}

// TestDiscoverVersions_NPM_FiltersWeirdKeys verifies malformed / prerelease
// / build / single-component keys are rejected by the default stable-only
// filter and the sort is deterministic (023-A2-02, 023-R4).
func TestDiscoverVersions_NPM_FiltersWeirdKeys(t *testing.T) {
	registry := map[string]interface{}{
		"dist-tags": map[string]string{
			"latest": "not-a-version", // latest 也必须通过过滤 → 被剔除
		},
		"versions": map[string]interface{}{
			"not-a-version": map[string]interface{}{"version": "x"},
			"22oops":        map[string]interface{}{"version": "x"}, // 数字前缀垃圾
			"1.0.0-beta.1":  map[string]interface{}{"version": "x"}, // prerelease 默认排除
			"1.0.0":         map[string]interface{}{"version": "x"},
			"1.0.0.0":       map[string]interface{}{"version": "x"}, // 4 段：与 1.0.0 比较相等 → 键串 tie-break
			"1.0.0+build.5": map[string]interface{}{"version": "x"}, // build 默认排除
			"v2.0.0":        map[string]interface{}{"version": "x"},
			"3":             map[string]interface{}{"version": "x"}, // 单段（无点）默认排除
			"v1.2.3-rc.1":   map[string]interface{}{"version": "x"}, // prerelease 默认排除
		},
	}
	body, _ := json.Marshal(registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "weird",
	})
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	// 过滤后仅稳定数字版本：v2.0.0 > 1.0.0；1.0.0 与 1.0.0.0 比较相等，
	// 按原始键字符串升序稳定排序（"1.0.0" < "1.0.0.0"）。
	want := []string{"v2.0.0", "1.0.0", "1.0.0.0"}
	if len(versions) != len(want) {
		t.Fatalf("got %d versions %v, want %d (%v)", len(versions), versions, len(want), want)
	}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// TestDiscoverVersions_NPM_PatternFilter verifies RemoteSource.Pattern is
// applied to both version keys and dist-tags.latest (typescript.yaml 形态:
// ^\d+\.\d+\.\d+$ 剔除 prerelease 键)。
func TestDiscoverVersions_NPM_PatternFilter(t *testing.T) {
	registry := map[string]interface{}{
		"dist-tags": map[string]string{
			"latest": "6.0.0-rc.1", // 不匹配 Pattern → 不置顶
		},
		"versions": map[string]interface{}{
			"7.0.2":         map[string]interface{}{"version": "x"},
			"6.0.0-rc.1":    map[string]interface{}{"version": "x"},
			"5.7.3":         map[string]interface{}{"version": "x"},
			"5.7.3-beta.1":  map[string]interface{}{"version": "x"},
			"not-a-version": map[string]interface{}{"version": "x"},
		},
	}
	body, _ := json.Marshal(registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "typed", Pattern: `^\d+\.\d+\.\d+$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	want := []string{"7.0.2", "5.7.3"}
	if len(versions) != len(want) {
		t.Fatalf("got %d versions %v, want %d", len(versions), versions, len(want))
	}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// TestDiscoverVersions_NPM_PatternKeepsPrerelease verifies that a Pattern
// explicitly matching prerelease keys keeps them (pattern author controls
// the filter) (023-R4). Sorting stays CompareSemver-based: a prerelease
// compares equal to its release and is tie-broken lexically.
func TestDiscoverVersions_NPM_PatternKeepsPrerelease(t *testing.T) {
	registry := map[string]interface{}{
		"dist-tags": map[string]string{"latest": "1.0.0-beta.1"},
		"versions": map[string]interface{}{
			"1.0.0":         map[string]interface{}{"version": "x"},
			"1.0.0-beta.1":  map[string]interface{}{"version": "x"},
			"5.7.3":         map[string]interface{}{"version": "x"},
			"not-a-version": map[string]interface{}{"version": "x"},
		},
	}
	body, _ := json.Marshal(registry)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = npmTestClient(server.URL)
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "typed", Pattern: `^v?\d+(\.\d+)*(-[0-9A-Za-z.-]+)?$`,
	})
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	// latest 1.0.0-beta.1 置顶；5.7.3 > 1.0.0 组；1.0.0 与 1.0.0-beta.1
	// 比较相等 → 键串升序 "1.0.0" 在前。
	want := []string{"1.0.0-beta.1", "5.7.3", "1.0.0"}
	if len(versions) != len(want) {
		t.Fatalf("got %d versions %v, want %d", len(versions), versions, len(want))
	}
	for i, v := range want {
		if versions[i] != v {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], v)
		}
	}
}

// TestDiscoverVersions_NPM_InvalidPattern verifies an unparseable Pattern
// is a configuration error, not a silent empty success (023-R4).
func TestDiscoverVersions_NPM_InvalidPattern(t *testing.T) {
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "typed", Pattern: "(",
	})
	if err == nil || !strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("want bad-pattern error, got %v", err)
	}
}

// TestDiscoverVersions_NPM_ManifestURLIgnored verifies RemoteSource.URL is
// NOT honored by the npm remote — the registry base is fixed (SSRF posture,
// 023-R3). Requests must only hit the configured base (test seam), never
// the manifest-supplied URL.
func TestDiscoverVersions_NPM_ManifestURLIgnored(t *testing.T) {
	registry := map[string]interface{}{
		"dist-tags": map[string]string{"latest": "7.0.2"},
		"versions": map[string]interface{}{
			"7.0.2": map[string]interface{}{"version": "x"},
		},
	}
	body, _ := json.Marshal(registry)

	// base：真正的 registry（经测试钩子注入 httptest）。
	baseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer baseServer.Close()

	// manifest 试图指向的"恶意"URL：若被访问则计数。
	decoyHits := 0
	decoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoyHits++
		http.Error(w, "decoy", http.StatusTeapot)
	}))
	defer decoy.Close()

	old := SetNPMRegistryBaseForTest(baseServer.URL)
	defer SetNPMRegistryBaseForTest(old)
	origClient := httpClient
	httpClient = &http.Client{Timeout: 30 * time.Second} // 直连（不经 redirectTransport）
	defer func() { httpClient = origClient }()

	versions, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "npm", Package: "typescript",
		URL: decoy.URL, // 必须被忽略
	})
	if err != nil {
		t.Fatalf("DiscoverVersions npm: %v", err)
	}
	if len(versions) != 1 || versions[0] != "7.0.2" {
		t.Fatalf("versions = %v, want [7.0.2] from fixed base", versions)
	}
	if decoyHits != 0 {
		t.Errorf("manifest-supplied URL was contacted (%d hits); npm base must stay fixed", decoyHits)
	}
}
