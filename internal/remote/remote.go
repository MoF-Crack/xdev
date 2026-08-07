// Package remote discovers available tool versions from remote sources
// (GitHub Releases, JSON APIs, HTML pages).
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// githubBodyLimit is the maximum body size to read from a GitHub API response.
// 16 MiB covers repos like llvm/llvm-project (~2.3 MiB at per_page=30) with
// ample headroom. Var (not const) for testability.
var githubBodyLimit = 16 << 20 // 16 MiB

// perPageValues defines the per_page values to try when the response body is
// truncated. Starting with 30, falling back to 10, then 5.
var perPageValues = []int{30, 10, 5}

// explicitGitHubToken holds the token set via ConfigureGitHubToken or config.
// It takes priority over the environment variables GITHUB_TOKEN and GH_TOKEN.
var explicitGitHubToken string

// ConfigureGitHubToken sets the explicit GitHub API token.
// Pass an empty string to clear the explicit token (env will still be checked).
func ConfigureGitHubToken(token string) {
	explicitGitHubToken = strings.TrimSpace(token)
}

// effectiveGitHubToken returns the token to use for API authentication.
// Priority: explicit (ConfigureGitHubToken/config) > env (GITHUB_TOKEN, GH_TOKEN).
func effectiveGitHubToken() string {
	if explicitGitHubToken != "" {
		return explicitGitHubToken
	}
	return gitHubTokenFromEnv()
}

// GitHubTokenConfigured reports whether a GitHub API token is available
// (either explicitly configured or via environment variable).
func GitHubTokenConfigured() bool {
	return effectiveGitHubToken() != ""
}

// ReloadGitHubTokenFromEnv re-reads GITHUB_TOKEN and GH_TOKEN from the
// environment and sets the explicit token if it is currently empty.
// It does not override an already-configured explicit token.
func ReloadGitHubTokenFromEnv() {
	if explicitGitHubToken != "" {
		return
	}
	if t := gitHubTokenFromEnv(); t != "" {
		explicitGitHubToken = t
	}
}

// gitHubTokenFromEnv returns the first non-empty GitHub token found in the
// process environment (os.Getenv), falling back to Windows user-level
// environment variables stored in HKCU\Environment when running on Windows.
// This ensures GUI processes launched from Windows Explorer (which do not
// inherit newly-added User environment variables) can still find the token.
func gitHubTokenFromEnv() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	if t := windowsUserEnv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t
	}
	return windowsUserEnv("GH_TOKEN")
}

// httpClient is the default HTTP client with a reasonable timeout.
// Security (N-6): CheckRedirect blocks HTTPS→HTTP downgrade to
// prevent MITM on remote version discovery requests.
var httpClient = &http.Client{
	Timeout:       30 * time.Second,
	CheckRedirect: util.SecureRedirectCheck(10),
}

// init is intentionally empty — effectiveGitHubToken reads env each time,
// so there is no need to eagerly capture the env token at package init.
func init() {}

// configuredProxyURL stores the last proxy URL passed to ConfigureHTTPClient,
// used in error messages when proxy connection fails.
var configuredProxyURL string

// ConfigureHTTPClient sets up the HTTP client's proxy transport.
// When proxyURL is non-empty, it is used as the HTTP proxy for all
// requests (the remote package only makes HTTPS requests to GitHub
// APIs); when empty, http.ProxyFromEnvironment is used instead.
// Call this at startup after loading configuration.
func ConfigureHTTPClient(proxyURL string) {
	configuredProxyURL = proxyURL
	transport := &http.Transport{}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	httpClient.Transport = transport
}

// ---------------------------------------------------------------------------
// TTL Cache for GitHub API responses
// ---------------------------------------------------------------------------

const cacheTTL = 5 * time.Minute

// cacheEntry stores the HTTP response body bytes and the ETag header.
type cacheEntry struct {
	body      []byte
	etag      string
	expiresAt time.Time
}

var (
	cacheMu sync.RWMutex
	cache   = map[string]*cacheEntry{}
)

// cacheGet returns the cached entry for key if it exists and has not expired.
func cacheGet(key string) (*cacheEntry, bool) {
	cacheMu.RLock()
	e, ok := cache[key]
	if !ok {
		cacheMu.RUnlock()
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		cacheMu.RUnlock()
		// Upgrade to write lock to delete expired entry
		cacheMu.Lock()
		// Re-check after acquiring write lock (another goroutine may have already deleted it)
		if e2, ok2 := cache[key]; ok2 && time.Now().After(e2.expiresAt) {
			delete(cache, key)
		}
		cacheMu.Unlock()
		return nil, false
	}
	cacheMu.RUnlock()
	return e, true
}

// cachePut stores an entry in the cache with the standard TTL.
func cachePut(key string, body []byte, etag string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[key] = &cacheEntry{
		body:      body,
		etag:      etag,
		expiresAt: time.Now().Add(cacheTTL),
	}
}

// ---------------------------------------------------------------------------
// GitHub API rate-limit helpers
// ---------------------------------------------------------------------------

// checkRateLimit inspects the X-RateLimit-Remaining header.  It returns an
// error when the remaining count is 0 and logs a warning when it drops below
// 10.  Missing or unparseable headers are silently ignored.
func checkRateLimit(resp *http.Response) error {
	v := resp.Header.Get("X-RateLimit-Remaining")
	if v == "" {
		return nil
	}
	remaining, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	if remaining == 0 {
		reset := resp.Header.Get("X-RateLimit-Reset")
		resetTime := "(unknown)"
		if secs, err := strconv.ParseInt(reset, 10, 64); err == nil {
			resetTime = time.Unix(secs, 0).Local().Format("2006-01-02 15:04:05")
		}
		var hint string
		if !GitHubTokenConfigured() {
			hint = "  · 未认证 GitHub API 限额仅约 60 次/小时，已耗尽。\n" +
				"  · 设置环境变量 GITHUB_TOKEN 或 GH_TOKEN 可提升至 5000 次/小时。\n" +
				"  · 等待到 " + resetTime + " 后重试，或关闭自动全量 sniff。"
		} else {
			hint = "  · 即使使用 Token，5000 次/小时限额已耗尽。\n" +
				"  · 等待到 " + resetTime + " 后重试。"
		}
		return fmt.Errorf("github remote: API rate limit exhausted (reset at %s)\n%s", reset, hint)
	}
	if remaining < 10 {
		log.Printf("[remote] ⚠️ GitHub API rate limit low: %d requests remaining", remaining)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Link-header pagination parser
// ---------------------------------------------------------------------------

// parseNextURL extracts the URL from the Link header with rel="next".
// Returns ("", false) if no next page exists.
func parseNextURL(linkHeader string) (string, bool) {
	// Link: <https://api.github.com/...?page=2>; rel="next", <...>; rel="last"
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		urlPart := strings.TrimSpace(segments[0])
		relPart := strings.TrimSpace(segments[1])
		if !strings.HasPrefix(urlPart, "<") || !strings.HasSuffix(urlPart, ">") {
			continue
		}
		urlPart = urlPart[1 : len(urlPart)-1]
		if strings.Contains(relPart, `rel="next"`) {
			return urlPart, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Proxy error helpers
// ---------------------------------------------------------------------------

// isProxyError returns true if the error message suggests a proxy connection
// failure (proxy refused, unreachable, or proxyconnect error). Thin alias
// over util.IsProxyError (Spec 021 D4) — string-based signature kept for
// existing call sites and tests.
func isProxyError(errMsg string) bool {
	return util.IsProxyError(errors.New(errMsg))
}

// sanitizedReason 去除错误消息中的 URL 凭据/查询参数并清理控制字符（F-15），
// 用于把底层错误安全地回显给用户。
func sanitizedReason(reason string) string {
	for _, field := range strings.Fields(reason) {
		if strings.HasPrefix(field, "https://") {
			reason = strings.ReplaceAll(reason, field, util.SanitizeURL(strings.TrimRight(field, ",;:)")))
		}
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, reason)
}

// hasProxyConfigured reports whether an explicit proxy was configured via
// ConfigureHTTPClient (non-empty proxyURL) by checking the stored URL.
func hasProxyConfigured() bool {
	return configuredProxyURL != ""
}

// testHookDirectTransport, when non-nil, is used as the transport for retryDirect
// instead of the default no-proxy transport. Used in tests to redirect direct
// connections to a local test server.
var testHookDirectTransport http.RoundTripper

// retryDirect clones the request and sends it using a default transport
// (no proxy). Used as a fallback when the proxy connection fails.
func retryDirect(ctx context.Context, originalReq *http.Request) (*http.Response, error) {
	directTransport := testHookDirectTransport
	if directTransport == nil {
		directTransport = &http.Transport{
			Proxy: nil, // explicitly no proxy
		}
	}
	directClient := &http.Client{
		Timeout:       30 * time.Second,
		Transport:     directTransport,
		CheckRedirect: httpClient.CheckRedirect,
	}
	// Clone the request body (it should be nil for GET/HEAD, but handle
	// defensively).
	bodyCopy := io.NopCloser(strings.NewReader(""))
	if originalReq.Body != nil {
		bodyBytes, _ := io.ReadAll(originalReq.Body)
		bodyCopy = io.NopCloser(strings.NewReader(string(bodyBytes)))
	}
	clone, err := http.NewRequestWithContext(ctx, originalReq.Method, originalReq.URL.String(), bodyCopy)
	if err != nil {
		return nil, err
	}
	clone.Header = originalReq.Header.Clone()
	return directClient.Do(clone)
}

// doWithProxyFallback executes the request using httpClient. If the request fails
// and an explicit proxy is configured, it retries once using a direct (no-proxy)
// connection via retryDirect.
//
// When the error is a proxy connection failure and the direct fallback also fails,
// a Chinese actionable error is returned with a hint for the user. For non-proxy
// errors where the direct fallback fails, the original error is returned instead.
func doWithProxyFallback(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err == nil {
		return resp, nil
	}

	errMsg := err.Error()
	isProxyErr := isProxyError(errMsg)
	hasProxy := hasProxyConfigured()

	if hasProxy {
		log.Printf("[remote] ⚠️ Request failed, retrying with direct connection: %v", err)
		directResp, directErr := retryDirect(ctx, req)
		if directErr == nil {
			return directResp, nil
		}

		// Both proxy and direct failed.
		if isProxyErr {
			// Security (F-15): 错误消息中的代理地址去除 userinfo/query，防凭据泄露。
			proxyAddr := util.SanitizeURL(configuredProxyURL)
			if proxyAddr == "" {
				proxyAddr = "已配置的代理 (127.0.0.1:7890)"
			}
			return nil, fmt.Errorf("代理 %s 不可达 (%s)，且直连尝试也失败 (%s)；请启动代理服务或关闭 proxy.enabled",
				proxyAddr, sanitizedReason(errMsg), sanitizedReason(directErr.Error()))
		}
	}

	return nil, err
}

// ---------------------------------------------------------------------------
// DiscoverVersions
// ---------------------------------------------------------------------------

// DiscoverVersions fetches available versions from a remote source
// based on the RemoteSource config in the manifest.
// Returns a list of version strings (e.g. ["3.12.0", "3.11.5", "3.10.13"]).
func DiscoverVersions(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	switch r.Type {
	case "github":
		return discoverGitHub(ctx, r)
	case "json":
		return discoverJSON(ctx, r)
	case "html":
		return discoverHTML(ctx, r)
	case "npm":
		return discoverNPM(ctx, r)
	case "none", "":
		return nil, fmt.Errorf("remote discovery not configured (type=%q)", r.Type)
	default:
		return nil, fmt.Errorf("unsupported remote type %q", r.Type)
	}
}

// npmRegistryBase is the fixed npm registry endpoint used for version
// discovery. It is intentionally NOT user-controllable via the manifest
// (SSRF posture — a manifest must never make xdev GET arbitrary hosts);
// only the exported test-only setter may change it.
var npmRegistryBase = "https://registry.npmjs.org"

// SetNPMRegistryBaseForTest overrides the npm registry base URL for tests
// and returns the previous value (caller should restore it with defer).
// 仅供测试使用（discovery 包测试注入 httptest 服务器用）。
func SetNPMRegistryBaseForTest(url string) string {
	old := npmRegistryBase
	npmRegistryBase = strings.TrimSuffix(url, "/")
	return old
}

// npmBodyLimit caps the npm registry metadata response size (packages with
// many versions can be large). Var (not const) for testability.
var npmBodyLimit = 16 << 20 // 16 MiB

// maxNPMVersions caps the number of versions returned from the npm
// registry (semver 降序截断 20 个).
const maxNPMVersions = 20

// npmVersionPattern 是默认版本键过滤器：稳定数字版本（可选 "v" 前缀，
// 至少 major.minor 两段）。prerelease/build 版本默认排除——除非 manifest
// 显式提供匹配它们的 Pattern（Pattern 作者自行控制）。
var npmVersionPattern = regexp.MustCompile(`^v?\d+(\.\d+)+$`)

// discoverNPM fetches the npm registry metadata for r.Package and returns
// the version list: dist-tags.latest first, then the version keys sorted by
// semver descending, truncated to maxNPMVersions.
//
// 版本键先经过过滤（Pattern 或默认稳定数字形态），latest 同样过滤；
// 排序用 CompareSemver 降序，等价版本按原始键字符串稳定排序。
// 非法 Pattern 属于配置错误，直接返回错误（不静默空结果）。
func discoverNPM(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	pkg := strings.TrimSpace(r.Package)
	if pkg == "" {
		return nil, fmt.Errorf("npm remote: package is required")
	}

	// 编译 Pattern（若有）。
	var versionRe *regexp.Regexp
	if p := strings.TrimSpace(r.Pattern); p != "" {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("npm remote: bad pattern %q: %w", p, err)
		}
		versionRe = re
	}
	// 版本键过滤：有 Pattern 用 Pattern；否则默认稳定数字形态。
	accept := func(v string) bool {
		if versionRe != nil {
			return versionRe.MatchString(v)
		}
		return npmVersionPattern.MatchString(v)
	}

	// 固定 registry 基址（不经 manifest 控制）。
	u := npmRegistryBase + "/" + url.PathEscape(pkg)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("npm remote: build request: %w", err)
	}
	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("npm remote: fetch %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("npm remote: registry returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := readLimitedBody(resp.Body, npmBodyLimit)
	if err != nil {
		return nil, fmt.Errorf("npm remote: read body from %s: %w", u, err)
	}

	var meta struct {
		DistTags map[string]string          `json:"dist-tags"`
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("npm remote: decode JSON from %s (body preview: %q): %w", u, preview, err)
	}

	// 版本键集合：先过滤，忽略畸形键。
	versions := make([]string, 0, len(meta.Versions))
	for v := range meta.Versions {
		if accept(v) {
			versions = append(versions, v)
		}
	}
	// semver 降序；等价版本按原始键字符串升序（确定性排序）。
	sort.Slice(versions, func(i, j int) bool {
		if c := util.CompareSemver(versions[i], versions[j]); c != 0 {
			return c > 0
		}
		return versions[i] < versions[j]
	})

	// dist-tags.latest 置顶（同样需通过过滤）；与 versions 重复时不重复追加。
	latest := strings.TrimSpace(meta.DistTags["latest"])
	if latest != "" && accept(latest) {
		out := make([]string, 0, len(versions)+1)
		out = append(out, latest)
		for _, v := range versions {
			if v != latest {
				out = append(out, v)
			}
		}
		if len(out) > maxNPMVersions {
			out = out[:maxNPMVersions]
		}
		return out, nil
	}
	if len(versions) > maxNPMVersions {
		versions = versions[:maxNPMVersions]
	}
	return versions, nil
}

// readLimitedBody reads up to limit bytes from r and returns an error if
// the body is empty or appears to be truncated (read exactly limit bytes).
func readLimitedBody(r io.Reader, limit int) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	if len(body) == limit {
		return nil, fmt.Errorf("response body truncated at %d bytes", limit)
	}
	return body, nil
}

// fetchGitHubPages fetches all pages of GitHub releases for the given
// owner/repo using the specified per_page count, and extracts versions
// matching the compiled regex.
func fetchGitHubPages(ctx context.Context, ownerRepo string, re *regexp.Regexp, perPage int) ([]string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", ownerRepo, perPage)

	var allReleases []struct {
		TagName string `json:"tag_name"`
	}

	// Follow paginated responses.
	currentURL := apiURL
	for {
		// --- ETag / cache lookup ---
		var ifNoneMatch string
		if cached, ok := cacheGet(currentURL); ok {
			ifNoneMatch = cached.etag
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return nil, fmt.Errorf("github remote: build request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		if token := effectiveGitHubToken(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}

		resp, err := doWithProxyFallback(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("github remote: fetch %s: %w", currentURL, err)
		}

		// Rate-limit check (before consuming the body).
		if rlErr := checkRateLimit(resp); rlErr != nil {
			resp.Body.Close()
			return nil, rlErr
		}

		// 304 Not Modified → use cached body.
		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()
			cached, ok := cacheGet(currentURL)
			if !ok {
				// Cache evaporated between the lookup and now; retry without ETag.
				continue
			}
			var pageReleases []struct {
				TagName string `json:"tag_name"`
			}
			if err := json.Unmarshal(cached.body, &pageReleases); err != nil {
				return nil, fmt.Errorf("github remote: decode cached JSON from %s: %w", currentURL, err)
			}
			allReleases = append(allReleases, pageReleases...)
			// No pagination change on 304 – the Link header is not present.
			break
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, fmt.Errorf("github remote: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		bodyBytes, err := readLimitedBody(resp.Body, githubBodyLimit)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("github remote: read body from %s: %w", currentURL, err)
		}

		// Store in cache with ETag.
		etag := resp.Header.Get("ETag")
		cachePut(currentURL, bodyBytes, etag)

		var pageReleases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(bodyBytes, &pageReleases); err != nil {
			preview := string(bodyBytes)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return nil, fmt.Errorf("github remote: decode JSON from %s (body preview: %q): %w", currentURL, preview, err)
		}
		allReleases = append(allReleases, pageReleases...)

		// Check for next page.
		linkHeader := resp.Header.Get("Link")
		nextURL, hasNext := parseNextURL(linkHeader)
		if !hasNext {
			break
		}
		currentURL = nextURL
	}

	// Extract versions from accumulated releases.
	var versions []string
	for _, rel := range allReleases {
		m := re.FindStringSubmatch(rel.TagName)
		if len(m) >= 2 {
			versions = append(versions, m[1])
		}
	}
	return versions, nil
}

// discoverGitHub uses the GitHub Releases API to list tag names and
// extracts versions using the Pattern regex.
// It supports:
//   - TTL cache (5 min) with ETag / If-None-Match
//   - Pagination via the Link header
//   - Rate-limit awareness (X-RateLimit-Remaining)
//   - Automatic retry with smaller per_page on body truncation
func discoverGitHub(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	if r.URL == "" {
		return nil, fmt.Errorf("github remote: url (owner/repo) is required")
	}

	// Compile pattern once.
	pat := r.Pattern
	if pat == "" {
		pat = `v?(\d+\.\d+\.\d+)$`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("github remote: bad pattern %q: %w", pat, err)
	}

	var lastErr error
	for i, perPage := range perPageValues {
		versions, err := fetchGitHubPages(ctx, r.URL, re, perPage)
		if err == nil {
			return versions, nil
		}
		// Non-truncation errors are fatal.
		if !strings.Contains(err.Error(), "response body truncated") {
			return nil, err
		}
		lastErr = err
		if i+1 < len(perPageValues) {
			log.Printf("[remote] ⚠️ GitHub response truncated at %d bytes, retrying with per_page=%d", githubBodyLimit, perPageValues[i+1])
		}
	}
	return nil, fmt.Errorf("github remote: giving up after retries: %w", lastErr)
}

// discoverJSON HTTP-GETs r.URL, parses the JSON response and extracts
// versions from the dot-separated r.Path field path.
func discoverJSON(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	if r.URL == "" {
		return nil, fmt.Errorf("json remote: url is required")
	}
	if r.Path == "" {
		return nil, fmt.Errorf("json remote: path (dot-separated field) is required")
	}
	// Security (N-6): enforce HTTPS and block private/reserved IPs to
	// prevent SSRF via malicious third-party manifest RemoteSource URLs.
	if err := util.IsSafeExternalURL(r.URL); err != nil {
		return nil, fmt.Errorf("json remote: unsafe URL %q: %w", r.URL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("json remote: build request: %w", err)
	}

	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("json remote: fetch %s: %w", r.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("json remote: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Decode into generic structure for path traversal.
	var root interface{}
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, fmt.Errorf("json remote: decode JSON: %w", err)
	}

	node := root
	for _, key := range strings.Split(r.Path, ".") {
		m, ok := node.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json remote: path %q: expected object at segment %q", r.Path, key)
		}
		node, ok = m[key]
		if !ok {
			return nil, fmt.Errorf("json remote: path %q: key %q not found", r.Path, key)
		}
	}

	// The value at the path should be a list of strings or a list of objects.
	var versions []string
	switch v := node.(type) {
	case []interface{}:
		for _, item := range v {
			switch it := item.(type) {
			case string:
				versions = append(versions, it)
			case map[string]interface{}:
				// Try common field names: "version", "name", "id"
				for _, field := range []string{"version", "name", "id"} {
					if s, ok := it[field].(string); ok {
						versions = append(versions, s)
						break
					}
				}
			}
		}
	default:
		return nil, fmt.Errorf("json remote: path %q: expected array, got %T", r.Path, node)
	}

	// Apply pattern filter if provided.
	if r.Pattern != "" {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("json remote: bad pattern %q: %w", r.Pattern, err)
		}
		var filtered []string
		for _, ver := range versions {
			if re.MatchString(ver) {
				m := re.FindStringSubmatch(ver)
				if len(m) >= 2 {
					filtered = append(filtered, m[1])
				} else {
					filtered = append(filtered, ver)
				}
			}
		}
		versions = filtered
	}

	return versions, nil
}

// discoverHTML HTTP-GETs r.URL, parses the HTML and extracts versions
// using the r.Pattern regex.
func discoverHTML(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	if r.URL == "" {
		return nil, fmt.Errorf("html remote: url is required")
	}
	if r.Pattern == "" {
		return nil, fmt.Errorf("html remote: pattern is required")
	}
	// Security (N-6): enforce HTTPS and block private/reserved IPs to
	// prevent SSRF via malicious third-party manifest RemoteSource URLs.
	if err := util.IsSafeExternalURL(r.URL); err != nil {
		return nil, fmt.Errorf("html remote: unsafe URL %q: %w", r.URL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("html remote: build request: %w", err)
	}

	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("html remote: fetch %s: %w", r.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("html remote: returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MiB limit
	if err != nil {
		return nil, fmt.Errorf("html remote: read body: %w", err)
	}

	re, err := regexp.Compile(r.Pattern)
	if err != nil {
		return nil, fmt.Errorf("html remote: bad pattern %q: %w", r.Pattern, err)
	}

	matches := re.FindAllStringSubmatch(string(body), -1)
	seen := map[string]bool{}
	var versions []string
	for _, m := range matches {
		if len(m) >= 2 && !seen[m[1]] {
			seen[m[1]] = true
			versions = append(versions, m[1])
		}
	}
	return versions, nil
}
