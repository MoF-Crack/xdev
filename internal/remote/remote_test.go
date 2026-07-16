package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
// DiscoverVersions — JSON with object list (name/id fields)
// ---------------------------------------------------------------------------

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
