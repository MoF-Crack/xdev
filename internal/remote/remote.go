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

// jsonBodyLimit caps how much of a JSON version-discovery response is decoded
// (票 16 / P2-15). A malicious or broken remote could otherwise stream an
// unbounded body, which the decoder would buffer in memory (OOM / hang).
const jsonBodyLimit = 16 << 20 // 16 MiB

// htmlBodyLimit 是 HTML 版本索引页的读取上限。
//
// F-19：修复前 discoverHTML 用裸 io.ReadAll(io.LimitReader(...))——LimitReader
// 到限即返回 io.EOF，ReadAll 视为**成功**，于是页面在 2 MiB 处被静默截断，
// 落在截断点之后的版本无声丢失（用户看到「没有新版本」且无任何线索）。改用
// 同包 readLimitedBody（GitHub 路径已在用）做显式截断检测。
// Var（非 const）以便测试调小上限构造超限页面。
var htmlBodyLimit = 2 << 20 // 2 MiB

// maxGitHubPages 是 GitHub 分页的硬迭代上限。
//
// F-15：修复前循环既无 visited 集合也无迭代计数，上游（或被劫持的响应）给出
// 同主机自环的 Link 头时无限翻页——烧光未认证的 60 req/h 限额，allReleases
// 无界增长直至 OOM。visited 集合抓不住「每页 URL 都不同」的无限序列，故再
// 加一道硬上限兜底。100 页 × 100 条/页 = 10000 个 release，远超任何真实仓库
// 的版本数量（也是 perPageValues 上限 100 时的自然界）。
const maxGitHubPages = 100

// perPageValues defines the per_page values to try when the response body is
// truncated. Starting with 100 (GitHub API maximum), falling back to 50, then 30.
var perPageValues = []int{100, 50, 30}

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

// EffectiveGitHubToken 返回当前生效的 GitHub API token（显式配置 >
// GITHUB_TOKEN > Windows 用户环境 > GH_TOKEN）。供 internal/update 等
// 其他包复用同一 token 解析，避免弱化子集重复（Spec 034 审计）。
func EffectiveGitHubToken() string {
	return effectiveGitHubToken()
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

// init 只做一件事：给单例 transport 装上**只读**的代理判定分发器。
// 其余初始化刻意留空——effectiveGitHubToken 每次现读环境变量，无需在包
// 初始化时捕获 token。
func init() {
	httpTransport.Proxy = func(req *http.Request) (*url.URL, error) {
		httpTransportMu.Lock()
		fn := currentProxyFn
		httpTransportMu.Unlock()
		return fn(req)
	}
}

// configuredProxyURL stores the last proxy URL passed to ConfigureHTTPClient,
// used in error messages when proxy connection fails.
var configuredProxyURL string

// httpTransport is the process-wide transport reused by every
// ConfigureHTTPClient call (P2-30 / N-08).
//
// 修复前每次调用都新建一个 *http.Transport 再挂到 httpClient.Transport：
// 被替换掉的旧 transport 的空闲连接池（socket + 后台读 goroutine）没有任何
// 人调用 CloseIdleConnections，只能等 GC 终结器回收。GUI 每次改代理/重载配置
// 都会调用本函数，于是连接与句柄随调用次数一路累积。改为单例后，无论调用
// 多少次，进程里只有这一个 transport。
//
// 字段在 init 里装好之后**永不改写**：代理口径的切换走 currentProxyFn，
// 这样请求路径（RoundTrip 会读 transport 字段）与配置路径之间不产生数据
// 竞争——直接原地改写 transport.Proxy 会与并发请求构成竞争。
var httpTransport = &http.Transport{}

// httpTransportMu 保护 currentProxyFn 的读写（配置侧写、请求侧读）。
var httpTransportMu sync.Mutex

// currentProxyFn 是当前生效的代理判定，默认 http.ProxyFromEnvironment。
// 通过 httpTransportMu 读写（见 httpTransport 注释）。
var currentProxyFn = http.ProxyFromEnvironment

// proxyUserinfoPattern 匹配 URL 的 userinfo 段（`//user:pass@`），用于日志脱敏。
var proxyUserinfoPattern = regexp.MustCompile(`//[^/@\s]*@`)

// redactProxyCredentials 把日志文案里的 `user:pass@` 替换为 `<redacted>@`。
//
// 这里刻意不用 util.SanitizeURL：它在 url.Parse 失败时原样返回输入，而
// 「URL 畸形到解析失败」恰恰是最需要脱敏的场景（错误文本里带着原始 URL）。
// 与 internal/cli/config.go 的同名函数同款实现——跨包复用会引入 cli→remote
// 方向的反向依赖，故在 remote 包内就地保留一份（P2-16 后半）。
func redactProxyCredentials(msg string) string {
	return proxyUserinfoPattern.ReplaceAllString(msg, "//<redacted>@")
}

// ConfigureHTTPClient sets up the HTTP client's proxy transport.
// When proxyURL is non-empty, it is used as the HTTP proxy for all
// requests (the remote package only makes HTTPS requests to GitHub
// APIs); when empty, http.ProxyFromEnvironment is used instead.
// Call this at startup after loading configuration.
//
// P2-30（N-08）：不再每次新建 transport（见 httpTransport 注释）；每次
// 重新配置后调用 CloseIdleConnections——代理口径变了以后，池里残留的空闲
// 连接可能仍指向旧代理/旧直连目标，复用它们会绕过新配置。在途请求不受影响
// （它们持有的连接仍归自己，这与 net/http 的语义一致）。
func ConfigureHTTPClient(proxyURL string) {
	configuredProxyURL = proxyURL
	httpTransportMu.Lock()
	if proxyURL == "" {
		currentProxyFn = http.ProxyFromEnvironment
	} else if parsed, err := url.Parse(proxyURL); err != nil {
		// P2-16（后半）：解析失败必须告警——修复前这里静默忽略，用户以为
		// 代理已生效，实际每个请求都在直连（配置错误被吞掉，只表现为"变慢/
		// 连不上"）。语义不变：与"新建裸 transport"等价，Proxy 为 nil = 直连。
		// url.Parse 的错误文本会原样回显含凭据的 URL，故先脱敏再进日志。
		log.Printf("[remote] WARNING: invalid proxy URL, falling back to direct connection: %s",
			redactProxyCredentials(err.Error()))
		currentProxyFn = func(*http.Request) (*url.URL, error) { return nil, nil }
	} else {
		currentProxyFn = http.ProxyURL(parsed)
	}
	httpTransportMu.Unlock()
	httpTransport.CloseIdleConnections()
	httpClient.Transport = httpTransport
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
	// nextURL 是该页响应 Link 头里的 rel="next" 目标（无下一页则为空）。
	// P2-08 / C-11：304 响应不携带 Link 头，所以分页游标必须随缓存一起
	// 保存——否则命中缓存的每一次翻页都会在第一页之后断掉，第二页及更后
	// 的版本永远发现不到。
	nextURL string
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
// nextURL 是该页的下一页链接（Link 头 rel="next"，无下一页传 ""），
// 供 304 命中时恢复分页游标（P2-08 / C-11）。
func cachePut(key string, body []byte, etag string, nextURL string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[key] = &cacheEntry{
		body:      body,
		etag:      etag,
		expiresAt: time.Now().Add(cacheTTL),
		nextURL:   nextURL,
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

// sameHostURL reports whether raw targets the same host (scheme+host)
// as the original URL. Used to validate Link-header pagination targets:
// a foreign host in the Link header must never be followed while the
// request still carries the GitHub Bearer token — the token would be
// sent to the attacker's server.
func sameHostURL(original, next string) bool {
	ou, err := url.Parse(original)
	if err != nil {
		return false
	}
	nu, err := url.Parse(next)
	if err != nil {
		return false
	}
	return ou.Scheme == nu.Scheme && ou.Host == nu.Host
}

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
// Pagination loop detection (P3-9)
// ---------------------------------------------------------------------------

// paginationKey 把分页 URL 规范化成环检测用的键：
// scheme + host + path + **排序后**的查询参数（含重复键按值排序）。
//
// P3-9：修复前 visited 直接以原始 URL 字符串为键，上游只要把查询串参数顺序
// 换一换（`?per_page=100&page=2` vs `?page=2&per_page=100`）再附一个无关参数，
// 每次 hop 都是「新 URL」→ 环检测 0 次命中，只能靠 maxGitHubPages 硬上限终止，
// 一次嗅探就烧掉 100 次请求（未认证限额 60/h）。规范化后这些变体折叠成同一个键。
//
// 无法解析的 URL 原样返回：宁可漏判（还有硬上限兜底）也不要让键碰撞把两个
// 真正不同的页合并。
func paginationKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.Host)
	b.WriteString(u.Path)
	for i, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			if i == 0 && b.Len() > 0 {
				b.WriteByte('?')
			} else {
				b.WriteByte('&')
			}
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

// paginationPageNumber 返回 URL 的 page 查询参数（缺省为 0 = 未指定）。
//
// P3-9 的第二道守卫：即便某个上游用完全不同的查询串拼出同一页，
// page 参数也必须**单调递增** —— 环检测之外再兜一层。
func paginationPageNumber(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(u.Query().Get("page"))
	if err != nil {
		return 0
	}
	return n
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

// urlInTextPattern 匹配任意 URL（含被引号/括号包裹的情形）。
//
// F-18：net/http 的错误文本把 URL 包在双引号里
// （`Get "https://host/path?token=X": dial tcp …`），按 strings.Fields 切词再
// 判 HasPrefix(field, "https://") 判假 → 整段 URL（含查询串凭据）原样保留。
// 这里对整段文本做正则替换，不再依赖字段边界。
var urlInTextPattern = regexp.MustCompile(`https?://[^\s"'<>()\[\],;]+`)

// sanitizedReason 去除错误消息中的 URL 凭据/查询参数并清理控制字符（F-15），
// 用于把底层错误安全地回显给用户。
func sanitizedReason(reason string) string {
	reason = urlInTextPattern.ReplaceAllStringFunc(reason, util.SanitizeURL)
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

// directTransport 是 retryDirect 用的包级单例直连 transport（P2-11）。
//
// 修复前每次代理回退都新建 &http.Transport{}：每个被丢弃的 transport 都
// 带着自己的空闲连接池（socket + 后台读 goroutine），而 retryDirect 从不
// 关闭它们——代理不稳定时会话里会反复回退，连接/句柄一路累积到 GC 才回收。
// 单例后无论回退多少次都只有一个直连池（同时保留 keep-alive 收益；这也是
// 没有选"N 次响应后 CloseIdleConnections"方案的原因：那样每次回退都拆掉
// 连接池，直连路径退化为每请求一次 TCP+TLS 握手）。
var (
	directTransportOnce sync.Once
	directTransport     http.RoundTripper
)

// directTransportForRetry 返回直连（显式无代理）transport。
//
// testHookDirectTransport 优先——remote_test.go 的
// TestDiscoverVersions_GitHub_ProxyFallback / _ProxyFallbackBothFail 靠它把
// "直连"引到 httptest 服务器；P2-11 明确要求保留该检查，漏掉这两个测试即红。
func directTransportForRetry() http.RoundTripper {
	if hook := testHookDirectTransport; hook != nil {
		return hook
	}
	directTransportOnce.Do(func() {
		directTransport = &http.Transport{
			Proxy: nil, // explicitly no proxy
		}
	})
	return directTransport
}

// retryDirect clones the request and sends it using a default transport
// (no proxy). Used as a fallback when the proxy connection fails.
//
// 每次调用仍新建 *http.Client：client 自身不持有连接池（池在 transport
// 上，见 directTransport），因此这不是泄漏点，也保证 Timeout/CheckRedirect
// 每次取当前值。
func retryDirect(ctx context.Context, originalReq *http.Request) (*http.Response, error) {
	directClient := &http.Client{
		Timeout:       30 * time.Second,
		Transport:     directTransportForRetry(),
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
		log.Printf("[remote] ⚠️ Request failed, retrying with direct connection: %s", sanitizedReason(err.Error()))
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
		return discoverGitHub(ctx, r, "")
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

// DiscoverVersionsWithFloor 在 GitHub 远端上以 floor（已知最新版本）提前终止
// 翻页；其他 remote 类型委托 DiscoverVersions（floor 仅 github 有语义）。
// 由 github sniffer 传入 manifest 的已知最新版本，大幅减少请求数。
func DiscoverVersionsWithFloor(ctx context.Context, r manifest.RemoteSource, floor string) ([]string, error) {
	if r.Type == "github" {
		return discoverGitHub(ctx, r, floor)
	}
	return DiscoverVersions(ctx, r)
}

// npmRegistryBase is the fixed npm registry endpoint used for version
// discovery. It is intentionally NOT user-controllable via the manifest
// (SSRF posture — a manifest must never make xdev GET arbitrary hosts);
// only the exported test-only setter may change it.
var npmRegistryBase = "https://registry.npmjs.org"

// npmFallbackBase 是版本发现的主 registry 不可达时的镜像（国内可达）。
// 与 npmRegistryBase 同理，固定常量、非 manifest 可控；测试可用 setter 覆盖。
var npmFallbackBase = "https://registry.npmmirror.com"

// SetNPMRegistryBaseForTest overrides the npm registry base URL for tests
// and returns the previous value (caller should restore it with defer).
// 仅供测试使用（discovery 包测试注入 httptest 服务器用）。
func SetNPMRegistryBaseForTest(url string) string {
	old := npmRegistryBase
	npmRegistryBase = strings.TrimSuffix(url, "/")
	return old
}

// SetNPMFallbackBaseForTest overrides the npm fallback registry base URL
// for tests and returns the previous value (restore with defer).
func SetNPMFallbackBaseForTest(url string) string {
	old := npmFallbackBase
	npmFallbackBase = strings.TrimSuffix(url, "/")
	return old
}

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
// 主 registry（npmjs）失败（超时/网络/5xx）时 fallback 到 npmFallbackBase
// （npmmirror，国内可达）——版本发现不再被单一 registry 不可达卡死。
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

	// 主 registry 用短超时：npmjs 在部分地区不可达，12s 无响应立刻
	// fallback，避免空等完整 30s client timeout。
	primaryCtx, cancelPrimary := context.WithTimeout(ctx, npmPrimaryTimeout)
	versions, err := fetchNPMVersions(primaryCtx, npmRegistryBase, pkg, accept)
	cancelPrimary()
	if err == nil {
		return versions, nil
	}
	log.Printf("[remote] npm registry %s failed (%v), falling back to %s", npmRegistryBase, err, npmFallbackBase)
	versions, fbErr := fetchNPMVersions(ctx, npmFallbackBase, pkg, accept)
	if fbErr != nil {
		return nil, fmt.Errorf("npm remote: %v (fallback %s also failed: %v)", err, npmFallbackBase, fbErr)
	}
	return versions, nil
}

// npmPrimaryTimeout 主 registry 的短超时（见 discoverNPM）。
const npmPrimaryTimeout = 12 * time.Second

// fetchNPMVersions 从指定 registry base 抓取包元数据并返回版本列表
// （请求 + 解析；失败返回错误，由 discoverNPM 决定是否 fallback）。
//
// 解析用流式 JSON：只提取 dist-tags 与 versions 的 key，不将完整响应
// 读入内存（opencode-ai 等大包在部分镜像返回全量文档可超过 16 MiB）。
func fetchNPMVersions(ctx context.Context, base, pkg string, accept func(string) bool) ([]string, error) {
	// 固定 registry 基址（不经 manifest 控制）。
	u := base + "/" + url.PathEscape(pkg)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("npm remote: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("npm remote: fetch %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("npm remote: registry returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	distTags, versionKeys, err := decodeNPMKeys(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("npm remote: decode JSON from %s: %w", u, err)
	}

	// 版本键集合：先过滤，忽略畸形键。
	versions := make([]string, 0, len(versionKeys))
	for _, v := range versionKeys {
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
	latest := strings.TrimSpace(distTags["latest"])
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

// decodeNPMKeys 流式解析 npm registry 包元数据，提取 dist-tags 与
// versions 的对象 key（不保留 value），内存与响应体积解耦。
func decodeNPMKeys(r io.Reader) (map[string]string, []string, error) {
	distTags := map[string]string{}
	var versionKeys []string

	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("top-level value is %v, want object", tok)
	}
	for dec.More() {
		fieldTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		field, ok := fieldTok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected field token %v", fieldTok)
		}
		switch field {
		case "dist-tags":
			if err := dec.Decode(&distTags); err != nil {
				return nil, nil, err
			}
		case "versions":
			valTok, err := dec.Token()
			if err != nil {
				return nil, nil, err
			}
			if valTok != json.Delim('{') {
				// versions 不是对象（异常文档）：整体跳过。
				if err := skipJSONValue(dec); err != nil {
					return nil, nil, err
				}
				continue
			}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, nil, err
				}
				if k, ok := keyTok.(string); ok {
					versionKeys = append(versionKeys, k)
				}
				if err := skipJSONValue(dec); err != nil {
					return nil, nil, err
				}
			}
			if _, err := dec.Token(); err != nil { // 收尾 '}'
				return nil, nil, err
			}
		default:
			if err := skipJSONValue(dec); err != nil {
				return nil, nil, err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // 收尾顶层 '}'
		return nil, nil, err
	}
	return distTags, versionKeys, nil
}

// skipJSONValue 用 Token 遍历跳过当前 JSON 值（json.Decoder 无 Skip 方法）。
func skipJSONValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok {
		return nil // 标量值：一个 token 完成
	}
	if d != '{' && d != '[' {
		return nil
	}
	depth := 1
	for depth > 0 {
		t2, err := dec.Token()
		if err != nil {
			return err
		}
		if d2, ok := t2.(json.Delim); ok {
			switch d2 {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
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
//
// floor（已知最新版本，可空）用于提前终止：GitHub releases 按发布时间降序，
// 若当前页所有可提取版本都 ≤ floor，后续页只会更旧 → 停止翻页。
// floor 只丢弃 ≤ floor 的旧条目，绝不会丢失比 floor 更新的版本。
func fetchGitHubPages(ctx context.Context, ownerRepo string, re *regexp.Regexp, perPage int, floor string) ([]string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", ownerRepo, perPage)

	var allReleases []struct {
		TagName string `json:"tag_name"`
	}

	// Follow paginated responses.
	//
	// F-15：两道守卫同时存在——
	//   - visited：抓同主机自环 / A→B→A 环（Link 头指回已取过的页）；
	//   - maxGitHubPages：兜底抓「每页 URL 都不同」的无限序列（visited 抓不住）。
	// 任一命中都 break（保留已取到的版本）而不是报错：已取页面的数据是好的，
	// 环只是上游的异常。
	//
	// P3-9：visited 的键改用**规范化**形式（scheme+host+path + 排序后的查询
	// 参数），并单独对 page 参数做单调性检查。修复前按原始字符串判等 ——
	// 每次 hop 只把查询串参数顺序换一换（`?per_page=100&page=2&mark=N`，
	// 逻辑上永远是同一页）就能完全绕过环检测，只能靠 maxGitHubPages 硬上限
	// 终止：100 次请求，未认证 GitHub 限额 60/h → 一次嗅探就耗尽配额（H9）。
	currentURL := apiURL
	visited := map[string]bool{paginationKey(currentURL): true}
	lastPage := paginationPageNumber(currentURL)
	pages := 0
	// revalidations 限制「304 且缓存蒸发」的自愈重试次数（P2-8：最多 1 次）。
	revalidations := 0
	for {
		pages++
		if pages > maxGitHubPages {
			log.Printf("[remote] WARNING: GitHub pagination exceeded the hard cap of %d pages — stopping (F-15)", maxGitHubPages)
			break
		}

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
			// P1-4：ownerRepo 是 manifest 可控的，它被拼进 apiURL 后可能带上
			// 凭据/查询串；网络错误文本里还会内嵌整段 URL。两处都要脱敏。
			return nil, fmt.Errorf("github remote: fetch %s: %w",
				util.SanitizeURL(currentURL), errors.New(sanitizedReason(err.Error())))
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
				// P2-8（F-16 回归）：缓存蒸发时**重试一次**而不是硬报错。
				//
				// F-16 的修复注释断言「continue 重入循环时 ifNoneMatch 仍从同一个
				// 缺失的缓存取 → 发出的请求与上一次逐字节相同」——这个断言不成立：
				// ifNoneMatch 是循环体**内**的局部变量，continue 会重新执行
				// cacheGet，缓存已被删除 → ifNoneMatch = ""，第二次是**不带
				// If-None-Match 的普通 GET**，服务端回 200，发现流程正常完成。
				// 修复把这条自愈路径换成了硬错误（H5），用户看到的是无法恢复的
				// 失败而不是透明重试。
				//
				// 现在恢复重试并加**一次**计数器：既保留自愈，又满足 F-16 的
				// 初衷（防止服务端无条件回 304 时无限重发同一个请求）。
				if revalidations < 1 {
					revalidations++
					log.Printf("[remote] WARNING: 304 Not Modified for %s but the cached response is gone (expired or cleared) — retrying once without If-None-Match",
						util.SanitizeURL(currentURL))
					continue
				}
				return nil, fmt.Errorf("github remote: server returned 304 Not Modified for %s but no cached response is available (cache expired or was cleared) — cannot revalidate",
					util.SanitizeURL(currentURL))
			}
			var pageReleases []struct {
				TagName string `json:"tag_name"`
			}
			if err := json.Unmarshal(cached.body, &pageReleases); err != nil {
				return nil, fmt.Errorf("github remote: decode cached JSON from %s: %w", util.SanitizeURL(currentURL), err)
			}
			allReleases = append(allReleases, pageReleases...)
			// P2-08 / C-11：304 响应**没有** Link 头（RFC 9110：304 只回
			// 缓存相关头），所以下一页游标只能来自缓存条目——由产生该条目的
			// 那次 200 响应写入（写入前已过 M-1 同主机校验，见下）。
			// 修复前这里直接 break：命中缓存的第二次调用永远停在第一页，
			// 第二页及更后页的版本发现不到（表现为"sniff 结果变少"）。
			nextURL := cached.nextURL
			if nextURL == "" {
				break // 该页本就没有下一页
			}
			// F-15：环检测——游标指回已取过的页就停，别无限翻页。
			// P3-9：按规范化键判等（参数顺序/无关参数不再能绕过）。
			if visited[paginationKey(nextURL)] {
				log.Printf("[remote] WARNING: GitHub pagination loop detected at %s (page already fetched) — stopping (F-15)", util.SanitizeURL(nextURL))
				break
			}
			visited[paginationKey(nextURL)] = true
			currentURL = nextURL
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, fmt.Errorf("github remote: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		bodyBytes, err := readLimitedBody(resp.Body, githubBodyLimit)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("github remote: read body from %s: %w", util.SanitizeURL(currentURL), err)
		}

		// Store in cache with ETag, together with this page's next-page cursor
		// (P2-08 / C-11: a 304 carries no Link header, so the cursor must be
		// cached alongside the body).
		//
		// Security (M-1 fix): the Link header is attacker-influenceable
		// (compromised upstream, MITM, or a configured proxy) and the next
		// request still carries the Bearer token — never follow a pagination
		// URL that leaves the host the loop started on (api.github.com in
		// production, the test server in tests). Validating here is the
		// single gate: only same-host cursors are ever cached, hence only
		// same-host cursors can be replayed on a 304 hit.
		etag := resp.Header.Get("ETag")
		nextURL, hasNext := parseNextURL(resp.Header.Get("Link"))
		if hasNext && !sameHostURL(apiURL, nextURL) {
			log.Printf("[remote] WARNING: ignoring cross-host pagination URL in Link header: %s", util.SanitizeURL(nextURL))
			hasNext, nextURL = false, ""
		}
		cachePut(currentURL, bodyBytes, etag, nextURL)

		var pageReleases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(bodyBytes, &pageReleases); err != nil {
			preview := string(bodyBytes)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return nil, fmt.Errorf("github remote: decode JSON from %s (body preview: %q): %w", util.SanitizeURL(currentURL), preview, err)
		}
		allReleases = append(allReleases, pageReleases...)

		// 提前终止：floor 非空且本页全部可提取版本 ≤ floor → 后续页只会更旧。
		if floor != "" && pageAllAtOrBelow(pageReleases, re, floor) {
			break
		}

		// Check for next page (cursor parsed and host-validated above).
		if !hasNext {
			break
		}
		// F-15：环检测——同一主机内 Link 头指回已取过的页（自环或 A→B→A）
		// 时停止翻页，避免无界请求与 allReleases 无界增长。
		// P3-9：按规范化键判等 + page 参数单调性检查（双重守卫）。
		nextKey := paginationKey(nextURL)
		nextPage := paginationPageNumber(nextURL)
		if visited[nextKey] {
			log.Printf("[remote] WARNING: GitHub pagination loop detected at %s (page already fetched) — stopping (F-15)", util.SanitizeURL(nextURL))
			break
		}
		if lastPage > 0 && nextPage > 0 && nextPage <= lastPage {
			log.Printf("[remote] WARNING: GitHub pagination page number did not advance (%d → %d) at %s — stopping (P3-9)", lastPage, nextPage, util.SanitizeURL(nextURL))
			break
		}
		visited[nextKey] = true
		if nextPage > 0 {
			lastPage = nextPage
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

// pageAllAtOrBelow 报告页内全部可提取版本都 ≤ floor。
// 防御：页内没有任何可提取版本时返回 false（继续翻页，避免误停）。
func pageAllAtOrBelow(page []struct {
	TagName string `json:"tag_name"`
}, re *regexp.Regexp, floor string) bool {
	found := 0
	for _, rel := range page {
		m := re.FindStringSubmatch(rel.TagName)
		if len(m) < 2 {
			continue
		}
		found++
		if util.CompareSemver(m[1], floor) > 0 {
			return false
		}
	}
	return found > 0
}

// discoverGitHub uses the GitHub Releases API to list tag names and
// extracts versions using the Pattern regex.
// It supports:
//   - TTL cache (5 min) with ETag / If-None-Match
//   - Pagination via the Link header（floor 提前终止，见 fetchGitHubPages）
//   - Rate-limit awareness (X-RateLimit-Remaining)
//   - Automatic retry with smaller per_page on body truncation
func discoverGitHub(ctx context.Context, r manifest.RemoteSource, floor string) ([]string, error) {
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
		versions, err := fetchGitHubPages(ctx, r.URL, re, perPage, floor)
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
	//
	// P1-4：修复前这里用 %q 把 r.URL **原样**拼进用户可见错误 —— userinfo 与
	// ?token=SECRET 全部泄漏（会进 GUI 错误弹窗、CLI stderr，并可能被复制进
	// issue/日志）。F-18 的 sanitizedReason 只作用于 doWithProxyFallback 返回的
	// 网络错误，这两条主路径完全没有脱敏。现在统一走 util.SanitizeURL +
	// sanitizedReason 二次脱敏（后者同时覆盖 url.Parse 错误文本里内嵌的原始 URL）。
	if err := util.IsSafeExternalURL(r.URL); err != nil {
		return nil, fmt.Errorf("json remote: unsafe URL %q: %w",
			util.SanitizeURL(r.URL), errors.New(sanitizedReason(err.Error())))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("json remote: build request: %w", err)
	}

	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("json remote: fetch %s: %w",
			util.SanitizeURL(r.URL), errors.New(sanitizedReason(err.Error())))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("json remote: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Decode into generic structure for path traversal.
	// 票 16（P2-15）：限额读取 —— 超过 jsonBodyLimit 的响应在解码阶段即以
	// unexpected EOF 报错，而不是先无界缓冲整个 body 再解析。
	var root interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonBodyLimit)).Decode(&root); err != nil {
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
	//
	// P1-4：同 discoverJSON —— `unsafe URL %q` 与 `fetch %s` 都不得原样回显
	// 带凭据的 r.URL（userinfo / 查询串）。
	if err := util.IsSafeExternalURL(r.URL); err != nil {
		return nil, fmt.Errorf("html remote: unsafe URL %q: %w",
			util.SanitizeURL(r.URL), errors.New(sanitizedReason(err.Error())))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("html remote: build request: %w", err)
	}

	resp, err := doWithProxyFallback(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("html remote: fetch %s: %w",
			util.SanitizeURL(r.URL), errors.New(sanitizedReason(err.Error())))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("html remote: returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// F-19：修复前是裸 io.ReadAll(io.LimitReader(resp.Body, 2<<20))——LimitReader
	// 到限即返回 io.EOF，ReadAll 视为成功，页面被静默截断、落在截断点之后的
	// 版本无声丢失。改用同包 readLimitedBody（GitHub 路径已在用）做显式检测：
	// 读满上限即判定截断并报错，让用户看到可诊断的失败而不是「没有新版本」。
	//
	// 注意 readLimitedBody 对**空 body** 也报错：HTML 索引页为空同样是异常
	// （没有可解析的版本来源），报错比静默返回空列表更有用。
	body, err := readLimitedBody(resp.Body, htmlBodyLimit)
	if err != nil {
		if strings.Contains(err.Error(), "truncated") {
			return nil, fmt.Errorf("html remote: %s exceeded the %d-byte limit for %s — the version list may be incomplete; raise the limit or use a more specific index page: %w",
				sanitizedReason(err.Error()), htmlBodyLimit, util.SanitizeURL(r.URL), err)
		}
		return nil, fmt.Errorf("html remote: read body from %s: %w", util.SanitizeURL(r.URL), err)
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
