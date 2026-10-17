// Package downloader fetches remote archives to disk with verification
// and resilience features.
//
// # Features
//
//   - Streaming writes (no full-file buffering in memory)
//   - SHA-256 verification after download (skipped when hash is all-zeros)
//   - HTTP Range header support for resuming partial downloads
//   - Retry on transient failures (5xx, network errors) with exponential backoff
//   - Fallback URLs: when the primary URL fails with 404 or other non-retryable
//     errors, each FallbackURL is tried in order with the same retry logic
//   - Mirror support: the installer layer rewrites URLs via the mirror table
//     before passing them to Download
//   - Proxy support: HTTP/HTTPS/SOCKS5 proxies via ProxyURL option
//   - Manifest signature verification (Ed25519) for supply-chain security
//   - Human-readable progress on stderr (or via OnProgress callback)
//   - Download audit logging (JSONL format) for debugging and forensics
//
// # Download Flow
//
//  1. If a partial download exists (from a previously interrupted attempt),
//     resume via HTTP Range headers
//  2. Stream the response body to a temporary file (dest + ".downloading")
//  3. Verify SHA-256 against ExpectedSHA (if provided and non-zero)
//  4. Verify manifest signature (if ManifestSignature is provided)
//  5. Rename temp file to final destination (atomic on most filesystems)
//  6. Log the download result to the audit log
//
// # Security
//
// SHA-256 hashes that are all zeros (e.g. "0000...") are treated as
// placeholders. When the signer package's root public key is a placeholder,
// a WARNING is printed to stderr and signature verification is disabled.
// In that degraded mode, the sidecar .sha256 TOFU (Trust On First Use)
// mechanism records the first download's hash for future verification.
// When a real root key IS embedded, all-zero and missing hashes on
// pinned built-in archives are rejected. Sniff-discovered versions set
// AllowPlaceholderTOFU so the first download can still establish a pin.
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/xmdemt/xdev/internal/signer"
	"github.com/xmdemt/xdev/internal/util"
)

// Options controls a single download.
type Options struct {
	URL          string
	Dest         string
	ExpectedSHA  string // empty = skip verification
	ProgressDesc string
	Client       *http.Client
	MaxRetries   int
	Timeout      time.Duration
	Quiet        bool
	OnProgress   func(downloaded, total int64, elapsed time.Duration)          // optional progress callback
	OnProgressEx func(downloaded, total int64, speed int64, eta time.Duration) // optional extended progress callback with speed/eta

	// OnStatus is called for non-byte status updates such as retry
	// attempts, fallback URL switches, and restarts caused by the server
	// ignoring a Range header. The message is human-readable (e.g.
	// "Attempt 2/3 for github.com"). Callers (e.g. the GUI) can surface
	// it as a phase/status line.
	OnStatus func(message string)
	Cookie   string // optional cookie header (e.g. "oraclelicense=accept-securebackup-cookie")

	// ManifestSignature is the Ed25519 signature for the manifest that
	// declares this download's SHA-256. When non-nil, the signature is
	// verified against the embedded root public key before the download
	// proceeds. If verification fails, the download is aborted.
	ManifestSignature []byte // JSON-encoded signer.ManifestSignature

	// IsThirdParty indicates this download is from a third-party manifest
	// (not from the embedded/built-in manifests). Third-party manifests
	// use a TOFU trust model when no signature is available, rather than
	// being rejected outright. If a third-party manifest carries a .yaml.sig
	// file, it is still verified against the embedded root public key.
	//
	// This implements the layered trust model:
	//   - Built-in manifests: signature required when root key is embedded
	//   - Third-party manifests: signature optional, TOFU fallback
	IsThirdParty bool

	// ProxyURL is the HTTP/HTTPS/SOCKS5 proxy URL for this download.
	// When non-empty, the download uses this proxy instead of direct connection.
	// Format: "http://host:port", "https://host:port", "socks5://host:port"
	ProxyURL string

	// FallbackURLs is a list of alternative URLs to try if the primary
	// URL fails with HTTP 404 or other non-retryable errors. Each URL
	// is tried in order with the same retry logic.
	FallbackURLs []string

	// URLShaMap maps individual download URLs to their expected SHA-256.
	// When a URL is found in this map, its SHA is used instead of ExpectedSHA.
	// This allows mirror URLs that serve different binaries (e.g. Temurin JDK
	// vs Oracle JDK) to have different expected hashes than the official URL.
	URLShaMap map[string]string

	// AllowPlaceholderTOFU permits an all-zero / missing SHA-256 on a
	// built-in manifest when a real root key is embedded. Used only for
	// sniff-discovered versions whose archive URL was synthesised after
	// the manifest was signed — the first download records a sidecar pin.
	AllowPlaceholderTOFU bool

	// ToolName is the tool being installed. It is compared against the
	// signature payload's tool field so a valid signature for tool A can
	// never authorise tool B (ticket 13 / N-02 cross-tool confusion).
	ToolName string

	// Version and OSArch identify the exact slot in the signed SHA-256
	// map (`m[version][os/arch]`) for this archive. Both pipelines set
	// them; a missing Version means no signed coverage can be proven
	// (verification fails closed).
	Version string
	OSArch  string

	// UnsignedVersion marks a manifest version that is legitimately NOT
	// covered by the signed hash map: sniff-discovered (Discovered) or
	// explicitly unverified versions, versions absent from a custom
	// manifest, and Lua/vfox plugins (nil manifest).
	//
	// When true, three gates are skipped — the L-10 all-zero/missing-hash
	// refusal, the signed-map membership check, and the per-URL coverage
	// filter — so the TOFU sidecar/manifest write-back path keeps working.
	//
	// Derivation MUST NOT include a hash condition (ticket 13): a
	// "hash is empty" term would flip the verdict after TOFU write-back
	// (hash becomes real) and re-installing the same version would be
	// misreported as hash tampering.
	UnsignedVersion bool
}

// Fallbacks 返回该下载的全部候选 URL（主 URL + 回退 URL）。
//
// F-12：.part 按 URL 分代，因此「清理这次下载可能留下的临时文件」必须覆盖
// 每一个候选 URL 的代，否则回退 URL 写入的 .part 会变成孤儿。
func (o Options) Fallbacks() []string {
	out := make([]string, 0, len(o.FallbackURLs)+1)
	if o.URL != "" {
		out = append(out, o.URL)
	}
	out = append(out, o.FallbackURLs...)
	return out
}

// Download fetches opts.URL to opts.Dest.
//
// It overwrites the destination atomically: writes go to "<dest>.part"
// first and only after a successful checksum (when ExpectedSHA is
// non-empty) does the file get renamed.
//
// If the primary URL fails with HTTP 404/403/410, FallbackURLs are
// tried in order before giving up.
func Download(ctx context.Context, opts Options) error {
	log.Printf("[download] Starting download: url=%s dest=%s", sanitizedURL(opts.URL), opts.Dest)

	// Verify manifest signature before download if present
	if len(opts.ManifestSignature) > 0 {
		if err := verifyManifestSignature(opts); err != nil {
			log.Printf("[download] ERROR: manifest signature verification failed: %v", err)
			return fmt.Errorf("manifest signature verification failed: %w", err)
		}
		log.Printf("[download] Manifest signature verified successfully")
	} else if signer.HasRootKey() && !opts.IsThirdParty {
		// Security (2.6): When a root public key is embedded, missing
		// signatures on built-in manifests are an error. This prevents
		// downgrade attacks where an attacker deletes .yaml.sig files
		// to bypass verification on the built-in tool set.
		// Third-party manifests (IsThirdParty=true) gracefully fall back
		// to TOFU — see the layered trust model in Options.IsThirdParty.
		return fmt.Errorf("manifest signature is missing for tool download — refusing to download without signature verification (root public key is embedded)")
	}

	if opts.URL == "" {
		return fmt.Errorf("empty URL")
	}
	// Security (H-7): Enforce HTTPS for all downloads to prevent MITM attacks.
	// HTTP downloads transmit archives in cleartext, allowing attackers to
	// replace the content even when SHA-256 verification is active (since
	// the hash itself could be intercepted if the manifest is also fetched
	// over HTTP).
	parsedURL, err := url.Parse(opts.URL)
	if err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("download URL must use HTTPS, got %q (insecure connection rejected)", parsedURL.Scheme)
	}
	if opts.Dest == "" {
		return fmt.Errorf("empty destination")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Dest), 0o755); err != nil {
		log.Printf("[download] ERROR: failed to create dest directory %s: %v", filepath.Dir(opts.Dest), err)
		return err
	}
	if opts.Client == nil {
		opts.Client = newHTTPClient(opts, false)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Minute
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	log.Printf("[download] Config: timeout=%v maxRetries=%d cookie=%v sha256=%v fallbacks=%d proxy=%v",
		opts.Timeout, opts.MaxRetries, opts.Cookie != "", opts.ExpectedSHA != "", len(opts.FallbackURLs), opts.ProxyURL != "")

	// Build the list of URLs to try: primary first, then fallbacks
	urls := []string{opts.URL}
	for _, fb := range opts.FallbackURLs {
		fbParsed, err := url.Parse(fb)
		if err != nil || fbParsed.Scheme != "https" {
			log.Printf("[download] WARNING: skipping non-HTTPS fallback URL: %s", fb)
			continue
		}
		urls = append(urls, fb)
	}

	// Ticket 13 (P0-02): per-URL signature coverage filter. Every candidate
	// URL's EFFECTIVE hash (URLShaMap[url] when present, else ExpectedSHA)
	// must be present in the signed map for THIS version. A mirror added
	// after signing — or an attacker-controlled mirror with its own
	// mirror_sha256 — is dropped before a single byte is fetched; the
	// official URL stays as the last candidate.
	//
	// Skipped entirely for legitimately unsigned versions (TOFU-discovered
	// or custom tools): after a TOFU write-back the hash is real but still
	// absent from the signed map, and filtering would evict every
	// candidate and break re-installation (ticket 13 / G6).
	//
	// Also gated on HasRootKey(): with no embedded root key the whole
	// signature chain is inert, and the pre-batch behaviour (no filtering)
	// must be preserved exactly — that is the release-form regression
	// anchor for this batch (ticket 13 / G5).
	if signer.HasRootKey() && len(opts.ManifestSignature) > 0 && !opts.UnsignedVersion &&
		opts.ExpectedSHA != "" && !util.IsAllZeroHash(opts.ExpectedSHA) {
		urls = filterSignatureCoveredURLs(opts, urls)
		if len(urls) == 0 {
			return fmt.Errorf("no download URL is covered by the manifest signature for %s@%s — refusing (mirror hash not in signed map)", opts.ToolName, opts.Version)
		}
		if urls[0] != opts.URL {
			log.Printf("[download] WARNING: primary URL is not signature-covered — falling through to %s", sanitizedURL(urls[0]))
		}
	}

	ok, failures, proxyFailed := tryURLList(ctx, opts, urls, true)
	if ok {
		// P2-5：成功收尾时清掉**其它候选 URL** 的代（主 URL 中途断流留下的
		// <dest>.<other-url-gen>.part 及其 .part.lock）。F-12 让每个候选 URL
		// 各有一份 .part，但修复前成功路径从不清理失败候选的代 —— dest 已经
		// 生成，目录里却永久残留孤儿 .part 与 .part.lock（H1/H15）。
		//
		// 复用 removePartialFiles（修复前它是死代码，全仓无调用方），传入
		// Fallbacks() 覆盖全部候选 URL + 历史未分代命名。注意成功的那一代
		// .part 已在 finalizeDownload 里被 rename 掉，删除是幂等的。
		if rmErr := removePartialFiles(opts.Dest, opts.Fallbacks()...); rmErr != nil {
			log.Printf("[download] WARNING: 成功下载后清理残留临时文件失败: %v", rmErr)
		}
		return nil
	}

	// Proxy fallback: when a proxy was configured (explicit ProxyURL or an
	// environment proxy via ProxyFromEnvironment) and a proxy connection
	// failure occurred, retry every URL once with a direct (no-proxy)
	// client. This makes a dead/misconfigured proxy fail fast instead of
	// burning MaxRetries per URL, and still succeeds when the target is
	// reachable directly.
	if opts.ProxyURL != "" || proxyFailed {
		notifyStatus(opts, "Proxy unreachable — retrying directly (no proxy)")
		log.Printf("[download] Proxy failed; retrying all URLs without a proxy")
		directOpts := opts
		directOpts.ProxyURL = ""
		directOpts.Client = newHTTPClient(opts, true)
		if ok, directFailures, _ := tryURLList(ctx, directOpts, urls, false); ok {
			return nil
		} else {
			// Actionable hint only when proxy-connection errors were
			// actually observed (dead/misconfigured proxy), not when the
			// proxy worked and the target simply 404'd.
			if opts.ProxyURL != "" && proxyFailed {
				// F-17：代理 URL 的 userinfo 是合法配置（认证代理），
				// 绝不能出现在用户可见的错误里。
				return fmt.Errorf("download failed: proxy %s unreachable (%s); direct also failed (%s); start the proxy service or disable proxy.enabled",
					redactProxyCredentials(opts.ProxyURL), strings.Join(failures, "; "), strings.Join(directFailures, "; "))
			}
			failures = append(failures, directFailures...)
		}
	}

	log.Printf("[download] ERROR: all URLs failed for %s", sanitizedURL(opts.URL))
	return fmt.Errorf("download failed: %s", strings.Join(failures, "; "))
}

// tryURLList attempts every URL in order with the given options. It returns
// ok=true as soon as one URL succeeds, otherwise the sanitized per-URL
// failures and whether any failure was proxy-related.
func tryURLList(ctx context.Context, opts Options, urls []string, notifyFallback bool) (ok bool, failures []string, proxyFailed bool) {
	for urlIdx, tryURL := range urls {
		if urlIdx > 0 && notifyFallback {
			log.Printf("[download] Trying fallback URL #%d: %s", urlIdx, sanitizedURL(tryURL))
			notifyStatus(opts, "Trying fallback URL #%d: %s", urlIdx, shortHost(tryURL))
		}
		timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		err := downloadWithRetries(timeoutCtx, opts, tryURL)
		cancel()
		if err == nil {
			return true, nil, false
		}
		if isProxyError(err) {
			proxyFailed = true
		}
		failures = append(failures, fmt.Sprintf("%s: %s", sanitizedURL(tryURL), sanitizedReason(err)))
		log.Printf("[download] URL #%d failed: %s", urlIdx, sanitizedReason(err))
	}
	return false, failures, proxyFailed
}

// newHTTPClient builds the http.Client used for a download. When forceDirect
// is true the transport's Proxy is explicitly nil, so environment proxy
// variables (HTTP_PROXY/HTTPS_PROXY) cannot re-trap the request. If
// opts.Client already carries a transport (e.g. a test client with custom
// TLS settings), its configuration is preserved and only the proxy is
// replaced.
func newHTTPClient(opts Options, forceDirect bool) *http.Client {
	var proxy func(*http.Request) (*url.URL, error)
	if !forceDirect {
		proxy = proxyFromOptions(opts)
	}
	var transport *http.Transport
	if opts.Client != nil {
		if existing, ok := opts.Client.Transport.(*http.Transport); ok && existing != nil {
			cloned := existing.Clone()
			cloned.Proxy = proxy
			transport = cloned
		}
	}
	if transport == nil {
		transport = &http.Transport{Proxy: proxy}
	}
	return &http.Client{
		Timeout:   30 * time.Minute,
		Transport: transport,
		// Security (H-8): Prevent HTTPS-to-HTTP redirect downgrades
		// (shared policy — util.SecureRedirectCheck).
		CheckRedirect: util.SecureRedirectCheck(10),
	}
}

// isProxyError reports whether the error indicates a proxy connection
// failure. Thin alias over util.IsProxyError (Spec 021 D4) — kept for
// local call sites and tests.
func isProxyError(err error) bool {
	return util.IsProxyError(err)
}

func sanitizedURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid URL"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// proxyUserinfoPattern 匹配 URL 的 userinfo 段（`//user:pass@`），用于脱敏。
var proxyUserinfoPattern = regexp.MustCompile(`//[^/@\s]*@`)

// redactProxyCredentials 把文案里的 `user:pass@` 替换为 `<redacted>@`。
//
// F-17：与 internal/remote 的 redactProxyCredentials 同款实现（票 40 / P2-16
// 后半只修了 remote 包，downloader 包遗漏）。刻意不复用 util.SanitizeURL：它在
// url.Parse 失败时原样返回输入，而「URL 畸形到解析失败」恰恰是最需要脱敏的
// 场景（url.Parse 的错误文本会带上含凭据的原始 URL）。也不为共享而改
// internal/util——该文件属其他 agent 的所有权。
func redactProxyCredentials(msg string) string {
	return proxyUserinfoPattern.ReplaceAllString(msg, "//<redacted>@")
}

// urlInTextPattern 匹配任意 URL（含被引号/括号包裹的情形）。
//
// F-18：net/http 的错误文本把 URL 包在双引号里
// （`Get "https://host/path?token=X": dial tcp …`），修复前按 strings.Fields
// 切词再判 HasPrefix(field, "https://") 判假 → 整段 URL（含查询串凭据）原样
// 保留。这里对整段文本做正则替换，不再依赖字段边界。
var urlInTextPattern = regexp.MustCompile(`https?://[^\s"'<>()\[\],;]+`)

// sanitizedReason 去除错误消息中的 URL 凭据/查询参数并清理控制字符，
// 用于把底层错误安全地回显给用户。
func sanitizedReason(err error) string {
	reason := urlInTextPattern.ReplaceAllStringFunc(err.Error(), sanitizedURL)
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, reason)
}

// notifyStatus invokes opts.OnStatus (if set) with a formatted message.
func notifyStatus(opts Options, format string, args ...any) {
	if opts.OnStatus == nil {
		return
	}
	opts.OnStatus(fmt.Sprintf(format, args...))
}

// shortHost extracts the hostname (e.g. "github.com") from a URL for
// use in human-readable status messages.
func shortHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

// ---------------------------------------------------------------------------
// .part path generation (F-12)
// ---------------------------------------------------------------------------

// partGeneration 是「按 URL 分代」的 .part 命名所用的短哈希长度
// （sha256(url) 的十六进制前 16 字符 = 64 bit，碰撞风险可忽略）。
const partGenerationLen = 16

// partPathFor 返回某个候选 URL 的 .part 路径：<dest>.<sha256(url)[:16]>.part。
//
// F-12（P0）：修复前 .part 只由 opts.Dest 决定，而 fromBytes 取该文件大小后
// **无条件**作为 Range 起点发给当前候选 URL。tryURLList 在主 URL 失败后继续用
// 同一 dest 尝试回退 URL → 主 URL 写了一半的字节被当成回退 URL 的前缀，落盘
// AAAA…BBBB 混合归档；无哈希(TOFU)通路上该文件被 rename 并写入 sidecar pin
// 永久固化。
//
// 按 URL 分代后，跨 URL 的残留落在不同文件里，天然不可能被当成前缀；同一 URL
// 的崩溃残留仍按原名续传（断点续传能力保留）。
//
// 注意：凡是要拼接 .part 的地方都必须走本函数或 partPathsFor，否则会留下
// 孤儿 .part 或删错文件（见 manager.go 的 Pause/Cancel 清理与辅助函数）。
func partPathFor(dest, rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return dest + "." + hex.EncodeToString(sum[:])[:partGenerationLen] + ".part"
}

// partPathsFor 返回一个下载（主 URL + 回退 URL + 历史命名）可能涉及的
// 全部 .part 路径，供清理逻辑（Pause/Cancel/退出清理）一次性删除。
//
// 末位刻意包含未分代的 <dest>.part：修复前的版本或更早的 xdev 留下的残留
// 必须仍能被清理，否则会在缓存目录里永久占位。
func partPathsFor(dest string, urls ...string) []string {
	paths := make([]string, 0, len(urls)+1)
	seen := make(map[string]bool, len(urls)+1)
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	for _, u := range urls {
		if u == "" {
			continue
		}
		add(partPathFor(dest, u))
	}
	add(dest + ".part")
	return paths
}

// removePartLockIfUnheld 尽力删除一个 .part.lock，但**只在能证明它没被别的
// 下载进程持有时**才删。
//
// 为什么不能无条件删：LockFileEx/flock 的排他锁是「按文件」的。如果另一个
// xdev 进程正持有 L，我们却把 L 从目录里删掉，它锁住的就变成一个已解除链接的
// inode；下一个进程会新建同名 L 并成功加锁 → 两个进程同时写同一个 .part
// （正是 P1-01 的互斥语义要防的事）。因此先用 0 超时试探性获取：拿得到说明
// 无人持有，释放后即可安全删除；拿不到说明有活跃下载，保持原样。
func removePartLockIfUnheld(lockPath string) error {
	if _, err := os.Stat(lockPath); err != nil {
		return nil // 不存在（或不可读）：无可清理
	}
	fl, err := util.AcquireFileLock(lockPath, 0)
	if err != nil {
		return nil // 被活跃下载持有 —— 绝不删除
	}
	_ = fl.Release()
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removePartResidueIfUnheld 删除一份 .part 残留及其 .part.lock，但同样**只在
// 能证明该 .part 没有被活跃下载持有时**才动手。
//
// 这一点对「成功收尾时清理失败候选的代」尤其重要：同一 dest 下，主 URL 与
// 回退 URL 各有独立的分代与锁，两个下载可以同时在跑（互斥锁是按分代的）。
// 无条件删会把**别人在途的 .part** 从目录里抽走（Windows 上删除会失败、
// Unix 上写进已解除链接的 inode），所以先试探锁：拿不到就原样保留。
func removePartResidueIfUnheld(partPath string) error {
	if _, err := os.Stat(partPath); err != nil {
		// 没有 .part：仍清掉可能残留的孤儿锁。
		return removePartLockIfUnheld(partPath + ".lock")
	}
	fl, err := util.AcquireFileLock(partPath+".lock", 0)
	if err != nil {
		return nil // 有活跃下载持有 —— 不动它的 .part
	}
	_ = fl.Release()

	var firstErr error
	if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if err := os.Remove(partPath + ".lock"); err != nil && !os.IsNotExist(err) && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// removePartialFiles 删除给定 dest/URL 组合的全部 .part 残留与 .part.lock
// （best-effort，不存在的路径不算错误；被活跃下载持有的分代保持原样）。
// 返回首个错误，供调用方记日志。
//
// P2-5：调用方是「本次下载成功收尾」——必须把**失败候选 URL** 留下的代与
// 锁一起清掉，否则 dest 已生成但目录里永久残留 <other-url>.part / .part.lock。
func removePartialFiles(dest string, urls ...string) error {
	var firstErr error
	for _, p := range partPathsFor(dest, urls...) {
		if err := removePartResidueIfUnheld(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// Range / Content-Range helpers (F-12b, F-13)
// ---------------------------------------------------------------------------

// contentRangePattern 匹配 RFC 9110 的 Content-Range：
// `bytes <start>-<end>/<total>`，也接受 unsatisfied 形式的 `bytes */<total>`。
var contentRangePattern = regexp.MustCompile(`^bytes\s+(?:(\d+)-(\d+)|\*)/(\d+|\*)$`)

// parseContentRange 解析 Content-Range，返回 (start, total, ok)。
// start 为 -1 表示 unsatisfied 形式（`bytes */total`）；total 为 -1 表示 `*`。
// 任何不符合 RFC 形状的值都返回 ok=false（调用方 fail closed）。
func parseContentRange(value string) (start, total int64, ok bool) {
	m := contentRangePattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return 0, 0, false
	}
	total = -1
	if m[3] != "*" {
		n, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total = n
	}
	if m[1] == "" {
		return -1, total, true // `bytes */total`：请求区间不满足
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return n, total, true
}

// completePartResidue 报告「这个 416 是否意味着本地 .part 已等于完整文件」。
//
// 判定刻意保守（fail closed）：
//   - 服务端未给 Content-Range：无从证明，返回 false（删 .part 让下次重来）；
//   - Content-Range 是 `bytes */<total>`：total 必须与本地长度一致；
//   - Content-Range 是普通 `bytes <start>-<end>/<total>`：必须满足
//     start == 本地长度且 end+1 == total（即请求的起点正好是文件末尾）。
//
// 不满足时返回 false，调用方删除 .part —— 宁可从零重下，也不接受一个来历
// 不明的「完整」文件（它可能只是本地截断的残缺品）。
//
// P1-2：**裸 416 不再是完整性的证据**。修复前这里有一条
// `if resp.ContentLength == 0 { return true }` 的退路，而 RFC 9110 允许 416
// 不带 Content-Range，Go 服务端写 WriteHeader(416) 且无 body 时 ContentLength
// 恰好就是 0 → 任意本地 .part（包括只有真实文件一半的截断残片）都被当作
// 「已完整」提升为正式归档，并在无哈希(TOFU)通路上被 pin 永久固化。
// 现在只有服务端明确给出 `bytes */total`（或 `bytes A-B/total`）且 total 与
// 本地长度一致时才走提升路径；否则删 .part，宁可让用户从零重下。
func completePartResidue(resp *http.Response, fromBytes int64) bool {
	start, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
	if !ok {
		return false
	}
	if total < 0 {
		return false // `bytes */ *` 或 `bytes 0-99/*`：无法证明完整性
	}
	if total != fromBytes {
		return false
	}
	// unsatisfied 形式（start == -1）只要 total 对上即可。
	return start <= 0 || start == fromBytes
}

// isNotFoundError reports whether the error indicates the resource was
// not found (HTTP 404, 403, 410), making it worthwhile to try fallback URLs.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 404") ||
		strings.Contains(msg, "HTTP 403") ||
		strings.Contains(msg, "HTTP 410")
}

// downloadWithRetries tries to download from a single URL with retries.
func downloadWithRetries(ctx context.Context, opts Options, tryURL string) error {
	urlOpts := opts
	urlOpts.URL = tryURL

	var lastErr error
	for attempt := 1; attempt <= opts.MaxRetries; attempt++ {
		log.Printf("[download] Attempt %d/%d for %s", attempt, opts.MaxRetries, sanitizedURL(tryURL))
		notifyStatus(opts, "Attempt %d/%d for %s", attempt, opts.MaxRetries, shortHost(tryURL))
		err := downloadOnce(ctx, urlOpts)
		if err == nil {
			log.Printf("[download] Download completed successfully: %s", opts.Dest)
			return nil
		}
		lastErr = err
		log.Printf("[download] Attempt %d failed: %v", attempt, err)
		// Don't retry on 404 — move to fallback immediately
		if isNotFoundError(err) {
			return err
		}
		// Fail fast on proxy errors: a dead/misconfigured proxy will not
		// recover within the retry window, so don't burn MaxRetries on it.
		// The caller (Download) retries the whole URL list directly once.
		if isProxyError(err) {
			return err
		}
		select {
		case <-time.After(time.Duration(attempt) * time.Second):
		case <-ctx.Done():
			log.Printf("[download] Context timeout exceeded")
			return ctx.Err()
		}
	}
	return fmt.Errorf("download failed after %d attempts: %w", opts.MaxRetries, lastErr)
}

func downloadOnce(ctx context.Context, opts Options) error {
	// F-12：.part 按候选 URL 分代（<dest>.<sha256(url)[:16]>.part）。同一 URL
	// 的崩溃残留仍被续传；**另一个 URL** 的残留落在别的文件里，永远不会被
	// 当成前缀拼成本 URL 的续传目标。
	partPath := partPathFor(opts.Dest, opts.URL)

	// Ticket 21 (audit-fixes / P1-01): exclusive lock on the partial file.
	// Two xdev processes sharing one cache destination (GUI + CLI, or a
	// batch item + a manual install) used to interleave Range requests and
	// writes into the same .part — both reported success while the archive
	// was a mix of two responses.
	//
	// The lock is stale-safe: AcquireFileLock uses OS byte-range locks
	// (LockFileEx/flock), released by the kernel when a holder dies — a
	// leftover .part.lock from a crashed process never blocks a takeover,
	// which is exactly what keeps crash-resume working (ticket 23
	// inventory conclusion).
	partLock, lockErr := util.AcquireFileLock(partPath+".lock", 1*time.Second)
	if lockErr != nil {
		return fmt.Errorf("another download is already in progress for %s (lock %s: %w) — retry once it finishes, or delete the .part/.part.lock files if no download is running", opts.ProgressDesc, partPath+".lock", lockErr)
	}
	defer partLock.Release()

	// Security (M-6): Open the .part file first, then stat the open handle
	// to avoid TOCTOU race between os.Stat and os.OpenFile.
	// Use O_EXCL for fresh downloads to prevent symlink attacks.
	var fromBytes int64
	flags := os.O_CREATE | os.O_WRONLY | os.O_EXCL
	f, openErr := os.OpenFile(partPath, flags, 0o644)
	if openErr != nil {
		// File may already exist — resume path. Security (M-4 fix):
		// os.OpenFile follows symlinks, so a planted <dest>.part symlink
		// would redirect the archive write to an arbitrary file. Only
		// resume when the existing .part is a regular file (lstat, not
		// stat); otherwise remove the impostor and start fresh with O_EXCL.
		// Ticket 21: this branch stays INSIDE the .part lock — the lock is
		// never treated as "another download in progress" for a crash
		// residue (that is the whole point of taking it over).
		if fi, stErr := os.Lstat(partPath); stErr == nil && fi.Mode().IsRegular() {
			flags = os.O_CREATE | os.O_WRONLY
			f, openErr = os.OpenFile(partPath, flags, 0o644)
		} else {
			_ = os.Remove(partPath)
			f, openErr = os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
		}
	}
	defer f.Close()
	if openErr != nil {
		log.Printf("[download] ERROR: failed to open partial file %s: %v", partPath, openErr)
		return openErr
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		fromBytes = fi.Size()
		log.Printf("[download] Resuming from %d bytes (partial file exists)", fromBytes)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", opts.URL, nil)
	if err != nil {
		log.Printf("[download] ERROR: failed to create request: %v", err)
		return err
	}
	if fromBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", fromBytes))
		log.Printf("[download] Set Range header: bytes=%d-", fromBytes)
	}
	req.Header.Set("User-Agent", "xdev/0.1")
	if opts.Cookie != "" {
		req.Header.Set("Cookie", opts.Cookie)
		log.Printf("[download] Set Cookie header for OTN license")
	}

	log.Printf("[download] Sending HTTP GET request to %s", sanitizedURL(opts.URL))
	resp, err := opts.Client.Do(req)
	if err != nil {
		log.Printf("[download] ERROR: HTTP request failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	log.Printf("[download] Response: status=%d content-length=%d", resp.StatusCode, resp.ContentLength)

	// F-13：416 Range Not Satisfiable 单独处理。它恰恰是「本地 .part 已等于
	// 完整文件」时服务器唯一合法的回答。修复前该分支按普通非 2xx 直接返回，
	// 且**不删 .part** → 下次安装读到同样大小、发同样越界的 Range、再吃 416，
	// 该 dest 永久不可恢复。
	//
	// 正确动作：把手上的 .part 当作完整文件走后续的校验 + rename 路径。
	// 但这只在「服务器确实说请求的区间越界（区间从 fromBytes 起）」时才成立，
	// 所以先确认本地确有内容、且（若服务端给了 Content-Range）声明的是
	// `bytes */total` 且 total 与本地长度一致。
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if fromBytes > 0 && completePartResidue(resp, fromBytes) {
			log.Printf("[download] HTTP 416 with a complete local .part (%d bytes) — verifying and promoting it instead of failing", fromBytes)
			notifyStatus(opts, "Server reported the partial file is already complete — verifying it")
			resp.Body.Close()
			// written=0：本次响应没有写入任何字节（完整内容已在 .part 里），
			// finalizeDownload 会把既有内容补喂给 hasher。
			return finalizeDownload(opts, partPath, f, sha256.New(), fromBytes, 0)
		}
		// 无法证明本地就是完整文件（无残留 / 服务端未确认 total）：删掉 .part
		// 让下次能从头重来，而不是把一个永不满足的 Range 留给下一次。
		log.Printf("[download] ERROR: HTTP 416 for %s (local .part=%d bytes) — discarding the partial file", sanitizedURL(opts.URL), fromBytes)
		f.Close()
		_ = os.Remove(partPath)
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, sanitizedURL(opts.URL))
	}

	if resp.StatusCode/100 != 2 {
		log.Printf("[download] ERROR: non-2xx status code %d for %s", resp.StatusCode, sanitizedURL(opts.URL))
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, sanitizedURL(opts.URL))
	}
	// If server ignored our range, restart from scratch.
	if fromBytes > 0 && resp.StatusCode == http.StatusOK {
		log.Printf("[download] Server did not honor Range header, restarting from scratch")
		notifyStatus(opts, "Server ignored Range header — restarting from scratch")
		fromBytes = 0
	}

	// P1-1 (a)：新鲜下载（未发 Range）绝不允许收到 206。
	//
	// F-12b 的 Content-Range 偏移校验被门控在 `fromBytes > 0 && 206`，于是
	// 「未发 Range 的请求收到 206 + bytes 0-499/1000」这条路径完全没有校验：
	// F-13b 只比 ContentLength（500 == 500）→ 500 字节残缺归档被 rename，并在
	// 无哈希(TOFU)通路上写入 sidecar pin 永久固化（后续安装比对的是同一个坏 pin，
	// 永远通过，只能手动删缓存才能恢复）。对 206 的合法前提是「我们确实请求了
	// 某个区间」，因此这里直接删 .part 并 fail closed。
	if fromBytes == 0 && resp.StatusCode == http.StatusPartialContent {
		log.Printf("[download] ERROR: server returned 206 Partial Content for a fresh request without Range (Content-Range=%q) — discarding",
			resp.Header.Get("Content-Range"))
		f.Close()
		_ = os.Remove(partPath)
		return fmt.Errorf("server returned 206 Partial Content with Content-Range %q for a request that asked for the whole file — the response is only part of the archive; discarding the partial file and retrying from scratch",
			resp.Header.Get("Content-Range"))
	}

	// F-12b：resume 时必须校验响应的 Content-Range 起始偏移 == fromBytes。
	// 修复前从不校验：服务端回 206 但起点是 0（或任何别的偏移）时，body 被
	// 写在偏移 fromBytes 处 → 错位文件；无哈希(TOFU)通路上还会被接受并写 pin。
	//
	// P1-1 (b)：除起点外还必须校验**组装后的总长度 == Content-Range 声明的
	// total**。只比起点抓不住「服务端只回部分区间但如实声明该区间的
	// Content-Length」（例：本地 500 字节 + `bytes 500-599/1000` + 如实
	// Content-Length: 100 → 组装出 600 字节就被接受并写 pin）。total 为 `*`
	// （parseContentRange 返回 -1）时无从证明完整性 —— 也 fail closed。
	if fromBytes > 0 && resp.StatusCode == http.StatusPartialContent {
		start, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start != fromBytes {
			log.Printf("[download] ERROR: Content-Range mismatch: server=%q requested-start=%d — discarding the partial file",
				resp.Header.Get("Content-Range"), fromBytes)
			f.Close()
			_ = os.Remove(partPath)
			return fmt.Errorf("server returned Content-Range %q which does not start at the requested offset %d for %s — discarding the partial file and retrying from scratch",
				resp.Header.Get("Content-Range"), fromBytes, sanitizedURL(opts.URL))
		}
		if total < 0 {
			log.Printf("[download] ERROR: Content-Range %q declares an unknown total — cannot prove the assembled archive is complete",
				resp.Header.Get("Content-Range"))
			f.Close()
			_ = os.Remove(partPath)
			return fmt.Errorf("server returned Content-Range %q with an unknown total for %s — cannot verify that the assembled archive is complete; discarding the partial file",
				resp.Header.Get("Content-Range"), sanitizedURL(opts.URL))
		}
		// 期望总长度 = 续传起点 + 本次响应应写入的字节数。响应自带
		// Content-Length 时用它；chunked（-1）时只能用 Content-Range 推出的
		// 区间长度（end-start+1），并在写完后再与实写字节数交叉校验。
		if total < fromBytes {
			log.Printf("[download] ERROR: Content-Range %q declares total=%d which is smaller than the local partial file (%d bytes) — discarding",
				resp.Header.Get("Content-Range"), total, fromBytes)
			f.Close()
			_ = os.Remove(partPath)
			return fmt.Errorf("server declared Content-Range total %d for %s but the local partial file already holds %d bytes — discarding the partial file and retrying from scratch",
				total, sanitizedURL(opts.URL), fromBytes)
		}
		// 响应区间长度必须正好补齐 total（否则组装出来的文件长度就对不上）。
		if resp.ContentLength >= 0 && fromBytes+resp.ContentLength != total {
			log.Printf("[download] ERROR: Content-Range total=%d but local=%d + Content-Length=%d = %d — discarding the partial file",
				total, fromBytes, resp.ContentLength, fromBytes+resp.ContentLength)
			f.Close()
			_ = os.Remove(partPath)
			return fmt.Errorf("server returned Content-Range %q for %s but the declared total %d does not match the assembled length %d (local %d + response %d) — discarding the partial file and retrying from scratch",
				resp.Header.Get("Content-Range"), sanitizedURL(opts.URL), total, fromBytes+resp.ContentLength, fromBytes, resp.ContentLength)
		}
	}

	// If resuming, seek to the end; if fresh, truncate any existing .part file.
	if fromBytes > 0 {
		if _, err := f.Seek(fromBytes, io.SeekStart); err != nil {
			f.Close()
			log.Printf("[download] ERROR: failed to seek to %d: %v", fromBytes, err)
			return err
		}
	} else {
		// Fresh download: truncate any stale content
		if err := f.Truncate(0); err != nil {
			f.Close()
			log.Printf("[download] ERROR: failed to truncate partial file: %v", err)
			return err
		}
	}

	hasher := sha256.New()

	// If resuming, feed the existing partial file content into the hasher
	// so that the final SHA-256 covers the entire file, not just the
	// resumed portion.
	if fromBytes > 0 {
		if pf, err := os.Open(partPath); err == nil {
			if _, err := io.Copy(hasher, pf); err != nil {
				log.Printf("[download] WARNING: failed to hash existing partial file: %v", err)
			} else {
				log.Printf("[download] Hashed %d bytes of existing partial file for SHA-256 continuity", fromBytes)
			}
			pf.Close()
		} else {
			log.Printf("[download] WARNING: failed to open partial file for hashing: %v", err)
		}
	}

	writer := &progressWriter{
		desc:         opts.ProgressDesc,
		total:        resp.ContentLength + fromBytes,
		quiet:        opts.Quiet,
		inner:        io.MultiWriter(f, hasher),
		current:      fromBytes,
		startTime:    time.Now(),
		onProgress:   opts.OnProgress,
		onProgressEx: opts.OnProgressEx,
		isTTY:        term.IsTerminal(os.Stderr.Fd()),
		lastBytes:    fromBytes,
	}
	log.Printf("[download] Writing to %s (total expected: %d bytes)", partPath, writer.total)
	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		f.Close()
		log.Printf("[download] ERROR: write failed after %d bytes: %v", written, err)
		return err
	}

	// F-13b：close-delimited（无 Content-Length、非 chunked）短 body 被当完整
	// 下载接受。服务端声明 total 却提前收尾时，io.Copy 以 nil 错误结束——
	// 必须自己比对字节数，否则残缺归档会被 rename 并写入 TOFU pin。
	if resp.ContentLength >= 0 && resp.ContentLength+fromBytes != written+fromBytes {
		f.Close()
		log.Printf("[download] ERROR: short body: wrote %d bytes, server declared %d", written+fromBytes, resp.ContentLength+fromBytes)
		_ = os.Remove(partPath)
		return fmt.Errorf("incomplete download for %s: wrote %d bytes but the server declared %d — discarding the partial file",
			sanitizedURL(opts.URL), written+fromBytes, resp.ContentLength+fromBytes)
	}

	// P1-1 (b) 收尾交叉校验：206 已声明的 total 必须等于实际组装出的总长度。
	// 上面只校验了「本地 + Content-Length」这一对，chunked（ContentLength == -1）
	// 的 206 响应绕过了它——这里用实写字节数再核一次，任何少于 total 的组装结果
	// 都删 .part 并 fail closed（残缺归档绝不能进 TOFU pin）。
	if resp.StatusCode == http.StatusPartialContent {
		if _, total, ok := parseContentRange(resp.Header.Get("Content-Range")); ok && total >= 0 {
			if assembled := fromBytes + written; assembled != total {
				f.Close()
				log.Printf("[download] ERROR: assembled length %d != Content-Range total %d (local=%d written=%d) — discarding",
					assembled, total, fromBytes, written)
				_ = os.Remove(partPath)
				return fmt.Errorf("incomplete download for %s: the assembled archive is %d bytes but the server declared a total of %d — discarding the partial file",
					sanitizedURL(opts.URL), assembled, total)
			}
		}
	}

	// P1-1 (c)：显式拒绝空下载。
	//
	// `200 + Content-Length: 0` 在修复前会通过 F-13b 的 `0 == 0` 判据 →
	// 0 字节文件被 rename 成正式归档，并写入空文件哈希
	// (e3b0c442…b855) 的 TOFU pin。真实归档不可能是 0 字节：这里删 .part
	// 并 fail closed。注意 chunked 响应（ContentLength == -1）不在此列——
	// 它由上面的实写字节数校验负责。
	if written == 0 && fromBytes == 0 {
		f.Close()
		log.Printf("[download] ERROR: empty download (0 bytes written) for %s — discarding", sanitizedURL(opts.URL))
		_ = os.Remove(partPath)
		return fmt.Errorf("empty download for %s: the server returned 0 bytes — refusing to promote an empty file to a complete archive",
			sanitizedURL(opts.URL))
	}
	log.Printf("[download] Write complete: %d bytes written", written+fromBytes)

	return finalizeDownload(opts, partPath, f, hasher, fromBytes, written)
}

// finalizeDownload 完成「关闭句柄 → SHA-256/TOFU 校验 → 原子 rename → 审计」
// 这条收尾链。它被两条路径复用：
//
//   - 正常路径：刚写完响应 body（written 为本次写入字节数）；
//   - F-13 的 416 恢复路径：本地 .part 已等于完整文件，没有可写内容
//     （written == 0），但 .part 的既有内容必须先喂进 hasher 才能校验。
//
// f 是打开的 .part 句柄（本函数负责关闭）；fromBytes 是续传起点（也是要
// 预先喂给 hasher 的既有字节数）。
func finalizeDownload(opts Options, partPath string, f *os.File, hasher hash.Hash, fromBytes, written int64) error {
	// 416 恢复路径下 fromBytes > 0 而没有任何写入：既有内容尚未进入 hasher
	// （正常路径在写之前已喂过），这里补上，保证 gotHash 覆盖整个文件。
	if written == 0 && fromBytes > 0 {
		if pf, err := os.Open(partPath); err == nil {
			if _, copyErr := io.Copy(hasher, pf); copyErr != nil {
				pf.Close()
				f.Close()
				log.Printf("[download] ERROR: failed to hash the complete partial file: %v", copyErr)
				return copyErr
			}
			pf.Close()
			log.Printf("[download] Hashed %d bytes of the complete partial file for SHA-256 continuity", fromBytes)
		} else {
			f.Close()
			log.Printf("[download] ERROR: failed to open the complete partial file for hashing: %v", err)
			return err
		}
	}
	f.Close()

	// SHA-256 verification.
	//
	// Security (C-4 + L-10 + 第六轮审计 F5): The sidecar .sha256 file uses a TOFU
	// (Trust On First Use) model — the first download's hash is recorded and used to
	// verify subsequent downloads. This is inherently weaker than a manifest-
	// provided hash because an attacker who can replace the archive can also
	// replace the sidecar. The following hardening measures are applied:
	//
	//   1. When a root public key is embedded (signer.HasRootKey()) AND the caller
	//      has NOT opted into placeholder-TOFU (AllowPlaceholderTOFU=false) AND the
	//      version is not a legitimately unsigned discovery (UnsignedVersion),
	//      downloads without a real SHA-256 hash are REJECTED.
	//      ⚠ 现状（过渡期）：两个生产调用方（installer.go / app_install.go）在
	//      哈希为空/占位时把 AllowPlaceholderTOFU 派生为 true——这正是 L-10 设计要
	//      拒绝的场景，即当前 keyed 构建对空哈希内置条目仍走 TOFU 兜底。这是
	//      票 17 数据线（163 空哈希待填充）完成前的**有意过渡策略**（否则 keyed
	//      发布版本直接拒装 17/52 工具）；数据线完成后调用方应改为 false，
	//      让下方门控真正生效（已在票 17 卡片登记为端态要求）。
	//   2. Sidecar files are written read-only (0o444) to raise the bar for
	//      casual tampering.
	//   3. First-use downloads print a prominent stderr WARNING.
	//   4. The audit log records Verified=false for all TOFU-based checks.
	//
	// 守卫测试 TestPlaceholderTOFU_TransitionalPolicy 以生产调用方的派生方式构造
	// Options，把上述过渡语义钉死：策略翻转时该测试必须同步更新。
	gotHash := hex.EncodeToString(hasher.Sum(nil))

	// Determine which SHA to use for verification:
	// 1. If URLShaMap contains the current URL, use that URL-specific SHA
	// 2. Otherwise fall back to the global ExpectedSHA
	expectedSHA := opts.ExpectedSHA
	if opts.URLShaMap != nil {
		if urlSHA, ok := opts.URLShaMap[opts.URL]; ok {
			expectedSHA = urlSHA
		}
	}

	if expectedSHA != "" {
		if util.IsAllZeroHash(expectedSHA) {
			// Security (L-10): When signature verification is enabled, refuse
			// to accept all-zero placeholder hashes on *pinned* built-in
			// archives. Sniff-discovered versions (Unverified / Discovered)
			// intentionally ship a zero hash so the first download can
			// establish a TOFU pin — that is not a signed-hash downgrade.
			// Ticket 13: the derivation is centralised in UnsignedVersion
			// (Discovered || Unverified, no hash condition) so the same
			// verdict applies before AND after the TOFU write-back.
			if signer.HasRootKey() && !opts.IsThirdParty && !opts.AllowPlaceholderTOFU && !opts.UnsignedVersion {
				log.Printf("[download] ERROR: sha256 is all-zero placeholder and signature verification is enabled — refusing download")
				_ = os.Remove(partPath)
				return fmt.Errorf("manifest has all-zero SHA-256 placeholder for %s and signature verification is enabled — refusing to download without a verified hash", opts.ProgressDesc)
			}
			// Security (F-04 / D15): 已有 sidecar pin 时校验本次下载内容，
			// 绝不静默覆写既有 pin；不匹配则删除 .part 并报错。
			sidecarPath := opts.Dest + ".sha256"
			if sidecarData, err := os.ReadFile(sidecarPath); err == nil && len(sidecarData) > 0 {
				parts := strings.Fields(strings.TrimSpace(string(sidecarData)))
				if len(parts) >= 1 && !util.IsAllZeroHash(parts[0]) {
					if !strings.EqualFold(gotHash, parts[0]) {
						log.Printf("[download] ERROR: sha256 mismatch with cached sidecar pin! cached=%s got=%s", parts[0], gotHash)
						_ = os.Remove(partPath)
						return fmt.Errorf("sha256 mismatch with cached sidecar pin: expected %s, got %s", parts[0], gotHash)
					}
					log.Printf("[download] SHA-256 verification passed (cached sidecar pin): %s", gotHash[:16]+"...")
				} else {
					// pin 内容无效：允许覆写。
					log.Printf("[download] WARNING: sidecar %s is malformed — rewriting pin", sidecarPath)
					writeSidecarHash(sidecarPath, gotHash, opts.Dest)
				}
			} else {
				log.Printf("[download] WARNING: sha256 is all zeros for %s - saving computed hash for future verification (TOFU)", opts.ProgressDesc)
				fmt.Fprintf(os.Stderr, "⚠ WARNING: No SHA-256 hash in manifest for %s. Downloaded content will be trusted on first use (TOFU).\n  For production security, embed a root public key and sign manifests.\n", opts.ProgressDesc)
				// Save the computed hash to a sidecar file for future verification
				writeSidecarHash(sidecarPath, gotHash, opts.Dest)
			}
		} else {
			if !strings.EqualFold(gotHash, expectedSHA) {
				log.Printf("[download] ERROR: sha256 mismatch! expected=%s got=%s", expectedSHA, gotHash)
				_ = os.Remove(partPath)
				return fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA, gotHash)
			}
			log.Printf("[download] SHA-256 verification passed: %s", gotHash[:16]+"...")
		}
	} else {
		// No expected hash from manifest.
		// Security (L-10): When signature verification is enabled, refuse
		// downloads that have no hash at all on built-in manifests —
		// sidecar TOFU is disabled for them. Third-party manifests
		// fall back to TOFU. Ticket 13: legitimately unsigned versions
		// (Discovered / Unverified / custom / Lua) keep the TOFU path.
		if signer.HasRootKey() && !opts.IsThirdParty && !opts.AllowPlaceholderTOFU && !opts.UnsignedVersion {
			log.Printf("[download] ERROR: no sha256 hash provided and signature verification is enabled — refusing download")
			_ = os.Remove(partPath)
			return fmt.Errorf("manifest has no SHA-256 hash for %s and signature verification is enabled — refusing to download without a verified hash", opts.ProgressDesc)
		}
		// No root key embedded: fall back to sidecar TOFU model (with warnings)
		sidecarHash, err := os.ReadFile(opts.Dest + ".sha256")
		if err == nil && len(sidecarHash) > 0 {
			// Parse sidecar: "hash  filename\n"
			parts := strings.Fields(strings.TrimSpace(string(sidecarHash)))
			if len(parts) >= 1 && !util.IsAllZeroHash(parts[0]) {
				if !strings.EqualFold(gotHash, parts[0]) {
					log.Printf("[download] ERROR: sha256 mismatch with cached hash! cached=%s got=%s", parts[0], gotHash)
					_ = os.Remove(partPath)
					return fmt.Errorf("sha256 mismatch with cached hash: expected %s, got %s", parts[0], gotHash)
				}
				log.Printf("[download] SHA-256 verification passed (cached sidecar): %s", gotHash[:16]+"...")
			}
		} else {
			// First download: save computed hash for future verification (TOFU)
			log.Printf("[download] WARNING: no sha256 hash available — first download will be trusted on first use (TOFU)")
			fmt.Fprintf(os.Stderr, "⚠ WARNING: No SHA-256 hash in manifest for %s. Downloaded content will be trusted on first use (TOFU).\n  For production security, embed a root public key and sign manifests.\n", opts.ProgressDesc)
			sidecarPath := opts.Dest + ".sha256"
			writeSidecarHash(sidecarPath, gotHash, opts.Dest)
			log.Printf("[download] SHA-256 computed and cached (TOFU): %s", gotHash[:16]+"...")
		}
	}

	// Atomic rename
	if err := os.Rename(partPath, opts.Dest); err != nil {
		log.Printf("[download] ERROR: failed to rename %s -> %s: %v", partPath, opts.Dest, err)
		return err
	}
	log.Printf("[download] File renamed to final destination: %s", opts.Dest)

	// Audit log
	LogAudit(AuditEntry{
		URL:      opts.URL,
		SHA256:   gotHash,
		Source:   sourceType(fromBytes),
		Verified: expectedSHA != "" && !util.IsAllZeroHash(expectedSHA),
		Size:     written + fromBytes,
	})

	return nil
}

// writeSidecarHash writes the SHA-256 sidecar file for TOFU (Trust On First
// Use) verification.
//
// Ticket 23 (audit-fixes / D-08): the write is atomic so a concurrent reader
// (another xdev process checking the pin, or the same process after a crash)
// can never observe a half-written sidecar — a torn pin would either reject
// a good archive or, worse, look like a valid user-edited pin. Steps:
//
//  1. write <sidecar>.tmp
//  2. fsync it
//  3. clear the read-only attribute of an existing target (0o444 from a
//     previous run; Windows would otherwise fail the rename)
//  4. rename over the target
//  5. mark the result read-only (0o444)
//
// Security (L-10): the read-only attribute is not a strong security
// boundary — an attacker who can write to the directory can still clear it —
// but it raises the bar for opportunistic modification.
func writeSidecarHash(sidecarPath, gotHash, dest string) {
	content := gotHash + "  " + filepath.Base(dest) + "\n"

	tmpPath := sidecarPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		log.Printf("[download] WARNING: failed to create sidecar temp %s: %v", tmpPath, err)
		return
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		log.Printf("[download] WARNING: failed to write sidecar temp %s: %v", tmpPath, err)
		return
	}
	// fsync so the rename cannot expose an empty/partial file after a crash.
	if err := f.Sync(); err != nil {
		log.Printf("[download] WARNING: failed to sync sidecar temp %s: %v", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		log.Printf("[download] WARNING: failed to close sidecar temp %s: %v", tmpPath, err)
	}
	// A previous pin is read-only — clear the attribute or the rename fails.
	if _, statErr := os.Stat(sidecarPath); statErr == nil {
		_ = os.Chmod(sidecarPath, 0o644)
	}
	if err := os.Rename(tmpPath, sidecarPath); err != nil {
		_ = os.Remove(tmpPath)
		log.Printf("[download] WARNING: failed to replace sidecar %s: %v", sidecarPath, err)
		return
	}
	// Set sidecar read-only to hinder casual tampering
	_ = os.Chmod(sidecarPath, 0o444)
}

// verifyManifestSignature verifies the manifest signature against the
// embedded root public key. Returns nil on success or if no root key
// is embedded (graceful degradation for unsigned builds).
func verifyManifestSignature(opts Options) error {
	// Import signer package types indirectly to avoid circular imports
	// We parse the JSON signature manually here
	type manifestSig struct {
		Tool      string `json:"tool"`
		SHA256Map string `json:"sha256_map"`
		Timestamp string `json:"timestamp"`
		Signature string `json:"signature"`
	}

	var ms manifestSig
	if err := json.Unmarshal(opts.ManifestSignature, &ms); err != nil {
		return fmt.Errorf("parse manifest signature: %w", err)
	}

	// Use the signer package for actual verification
	// (imported at the top of this file)
	sig := &signer.ManifestSignature{
		Tool:      ms.Tool,
		SHA256Map: ms.SHA256Map,
		Signature: ms.Signature,
	}
	// Parse timestamp
	if ms.Timestamp != "" {
		ts, err := time.Parse(time.RFC3339, ms.Timestamp)
		if err == nil {
			sig.Timestamp = ts
		}
	}

	rootPub := signer.RootPublicKey()
	if rootPub == nil {
		// No root key embedded — skip verification but warn loudly
		log.Printf("[download] ⚠ WARNING: manifest signature verification is DISABLED (no root public key embedded) — download integrity cannot be verified")
		fmt.Fprintf(os.Stderr, "⚠ WARNING: manifest signature verification is DISABLED — downloads are not integrity-verified\n")
		return nil
	}

	if !signer.VerifySignature(rootPub, sig) {
		return fmt.Errorf("manifest signature is invalid for tool %q — possible tampering detected", ms.Tool)
	}

	// Ticket 13 (N-02): the signed payload must belong to the tool being
	// installed. Without this check a valid signature of tool A could
	// authorise a download for tool B (whose hash happens to appear in A's
	// map) — cross-tool confusion.
	if opts.ToolName != "" && ms.Tool != "" && !strings.EqualFold(ms.Tool, opts.ToolName) {
		return fmt.Errorf("manifest signature tool mismatch: signature is for %q but the download is for %q — refusing (possible cross-tool confusion)", ms.Tool, opts.ToolName)
	}

	// Verify the ExpectedSHA matches the signed SHA-256 map.
	// Sniff-discovered versions carry an all-zero placeholder hash that is
	// intentionally absent from the signed map; those fall through to the
	// existing TOFU sidecar path instead of being treated as tampering.
	//
	// Ticket 13: legitimately unsigned versions (Discovered / Unverified /
	// custom / Lua plugins) skip the membership check entirely — after a
	// TOFU write-back the hash is real but still not in the signed map, so
	// enforcing membership would report tampering on re-install.
	if opts.ExpectedSHA != "" && ms.SHA256Map != "" && !util.IsAllZeroHash(opts.ExpectedSHA) && !opts.UnsignedVersion {
		if !verifySHAInMapForVersion(ms.SHA256Map, opts.Version, opts.OSArch, opts.ExpectedSHA) {
			shaPreview := opts.ExpectedSHA
			if len(shaPreview) > 16 {
				shaPreview = shaPreview[:16] + "..."
			}
			return fmt.Errorf("expected SHA-256 %s not found in signed manifest for tool %q version %q (%s) — possible hash tampering", shaPreview, ms.Tool, opts.Version, opts.OSArch)
		}
	}

	return nil
}

// signedSHA256Map extracts the sha256_map JSON payload from a manifest
// signature, or "" when the signature carries none.
func signedSHA256Map(opts Options) string {
	if len(opts.ManifestSignature) == 0 {
		return ""
	}
	var ms struct {
		SHA256Map string `json:"sha256_map"`
	}
	if err := json.Unmarshal(opts.ManifestSignature, &ms); err != nil {
		return ""
	}
	return ms.SHA256Map
}

// filterSignatureCoveredURLs drops candidate URLs whose effective hash is
// not covered by the signed map for this version. URLs without a usable
// hash are kept — the L-10 gate inside downloadOnce decides their fate.
func filterSignatureCoveredURLs(opts Options, urls []string) []string {
	signedMap := signedSHA256Map(opts)
	if signedMap == "" {
		// Signature without a hash map: nothing can be proven covered.
		// Keep the list intact and let signature verification (which runs
		// earlier) decide — it rejects a missing/invalid map upstream.
		return urls
	}
	kept := make([]string, 0, len(urls))
	for _, u := range urls {
		effSHA := opts.ExpectedSHA
		if opts.URLShaMap != nil {
			if urlSHA, ok := opts.URLShaMap[u]; ok {
				effSHA = urlSHA
			}
		}
		if effSHA == "" || util.IsAllZeroHash(effSHA) {
			kept = append(kept, u)
			continue
		}
		if verifySHAInMapForVersion(signedMap, opts.Version, opts.OSArch, effSHA) {
			kept = append(kept, u)
			continue
		}
		log.Printf("[download] WARNING: candidate %s is not covered by the signed hash map for %s@%s — skipping", sanitizedURL(u), opts.ToolName, opts.Version)
		notifyStatus(opts, "mirror hash not covered by signature — skipping %s", shortHost(u))
	}
	return kept
}

// verifySHAInMapForVersion checks the expected hash against the signed map
// with exact-key semantics plus a SAME-VERSION fallback:
//
//  1. exact key: m[version][os/arch] exists and equals the expected hash;
//  2. same-version fallback: any os/arch key under m[version] matches —
//     this keeps old signatures (whose os/arch key naming may differ) and
//     same-version cross-platform re-use working.
//
// Ticket 13 (P0-02 / G2): the previous implementation searched EVERY
// version's keys, so a newly added version could reuse another version's —
// or, since tool maps share the same shape, another tool's — signed hash.
// Cross-version matches are now rejected, and a missing version key is a
// hard fail (fail closed).
func verifySHAInMapForVersion(sha256MapJSON, version, osArch, expectedSHA string) bool {
	if sha256MapJSON == "" || version == "" || expectedSHA == "" {
		return false
	}
	var m map[string]map[string]string
	if err := json.Unmarshal([]byte(sha256MapJSON), &m); err != nil {
		return false
	}
	verMap, ok := m[version]
	if !ok {
		return false
	}
	if osArch != "" {
		if h, ok := verMap[osArch]; ok && strings.EqualFold(h, expectedSHA) {
			return true
		}
	}
	for _, h := range verMap {
		if strings.EqualFold(h, expectedSHA) {
			return true
		}
	}
	return false
}

// sourceType returns the download source type for audit logging.
func sourceType(fromBytes int64) string {
	if fromBytes > 0 {
		return "resume"
	}
	return "download"
}

// proxyFromOptions returns a proxy function based on the ProxyURL in options.
// Returns nil if no proxy is configured (uses system default).
func proxyFromOptions(opts Options) func(*http.Request) (*url.URL, error) {
	if opts.ProxyURL == "" {
		return http.ProxyFromEnvironment // fallback to HTTP_PROXY/HTTPS_PROXY env vars
	}
	proxyURL, err := url.Parse(opts.ProxyURL)
	if err != nil {
		// F-17：url.Parse 的错误文本会原样带上含凭据的 URL，先脱敏再进日志。
		log.Printf("[download] WARNING: invalid proxy URL %q: %v", redactProxyCredentials(opts.ProxyURL), redactProxyCredentials(err.Error()))
		return http.ProxyFromEnvironment
	}
	log.Printf("[download] Using proxy: %s", proxyURL.Host)
	return func(req *http.Request) (*url.URL, error) {
		return proxyURL, nil
	}
}

// progressWriter prints the download progress to stderr on a
// fixed cadence so users can see what's happening without flooding
// the terminal. It supports:
//   - Progress bar with speed/ETA (TTY mode)
//   - Plain text percentage (pipe/non-TTY mode)
//   - OnProgress and OnProgressEx callbacks
//   - Moving average speed calculation (last 5 samples @ 200ms)
//   - ETA estimation based on remaining bytes / speed
type progressWriter struct {
	desc         string
	total        int64
	current      int64
	quiet        bool
	inner        io.Writer
	last         time.Time
	startTime    time.Time
	onProgress   func(downloaded, total int64, elapsed time.Duration)
	onProgressEx func(downloaded, total int64, speed int64, eta time.Duration)
	speed        int64         // bytes/sec moving average
	eta          time.Duration // estimated time remaining
	samples      []int64       // recent byte counts for moving average (last 5)
	lastBytes    int64         // bytes at last sample
	isTTY        bool          // whether stderr is a terminal
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.inner.Write(b)
	p.current += int64(n)
	now := time.Now()

	// Throttle progress updates to ~5 per second.
	if now.Sub(p.last) >= 200*time.Millisecond {
		p.last = now
		elapsed := now.Sub(p.startTime)

		// Update speed/ETA with moving average (only when total is known)
		if p.total > 0 {
			// Record sample: delta bytes since last sample
			delta := p.current - p.lastBytes
			p.samples = append(p.samples, delta)
			if len(p.samples) > 5 {
				p.samples = p.samples[1:]
			}
			p.lastBytes = p.current

			// Moving average speed (bytes/sec).
			// Each sample covers ~200ms, so multiply by 5 for per-second rate.
			if len(p.samples) > 0 {
				var sum int64
				for _, s := range p.samples {
					sum += s
				}
				avg := sum / int64(len(p.samples))
				p.speed = avg * 5
			}

			// ETA = remaining bytes / speed
			if p.speed > 0 {
				remaining := p.total - p.current
				etaSeconds := remaining / p.speed
				p.eta = time.Duration(etaSeconds) * time.Second
			}
		}

		// Call OnProgress callback if set
		if p.onProgress != nil && p.total > 0 {
			p.onProgress(p.current, p.total, elapsed)
		}

		// Call OnProgressEx callback if set (provides speed/eta)
		if p.onProgressEx != nil && p.total > 0 {
			p.onProgressEx(p.current, p.total, p.speed, p.eta)
		}

		// Print to stderr if not quiet
		if !p.quiet {
			if p.total > 0 {
				pct := p.current * 100 / p.total
				if pct > 100 {
					pct = 100
				}
				if p.isTTY {
					// Progress bar format: [====>    ] 45% 12.3 MB/s ETA 8s
					const barWidth = 20
					filled := int(pct * barWidth / 100)
					if filled > barWidth {
						filled = barWidth
					}
					var bar string
					if filled >= barWidth {
						bar = strings.Repeat("=", barWidth)
					} else {
						bar = strings.Repeat("=", filled) + ">" + strings.Repeat(" ", barWidth-filled-1)
					}

					speedStr := ""
					if p.speed > 0 {
						speedStr = util.HumanBytesStr(p.speed) + "/s "
					}
					etaStr := ""
					if p.eta > 0 {
						etaStr = "ETA " + util.FormatDurationStr(p.eta)
					}

					fmt.Fprintf(os.Stderr, "\r  [%s] %3d%% %s%s", bar, pct, speedStr, etaStr)
				} else {
					fmt.Fprintf(os.Stderr, "\r  %s  %s / %s  (%d%%)",
						p.desc,
						util.HumanBytesStr(p.current),
						util.HumanBytesStr(p.total),
						pct,
					)
				}
			} else {
				fmt.Fprintf(os.Stderr, "\r  %s  %s", p.desc, util.HumanBytesStr(p.current))
			}
		}
	}

	return n, err
}
