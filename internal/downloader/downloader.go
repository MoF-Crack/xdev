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
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

	ok, failures, proxyFailed := tryURLList(ctx, opts, urls, true)
	if ok {
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
				return fmt.Errorf("download failed: proxy %s unreachable (%s); direct also failed (%s); start the proxy service or disable proxy.enabled",
					opts.ProxyURL, strings.Join(failures, "; "), strings.Join(directFailures, "; "))
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

func sanitizedReason(err error) string {
	reason := err.Error()
	for _, field := range strings.Fields(reason) {
		if strings.HasPrefix(field, "https://") {
			reason = strings.ReplaceAll(reason, field, sanitizedURL(strings.TrimRight(field, ",;:)")))
		}
	}
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
	partPath := opts.Dest + ".part"

	// Security (M-6): Open the .part file first, then stat the open handle
	// to avoid TOCTOU race between os.Stat and os.OpenFile.
	// Use O_EXCL for fresh downloads to prevent symlink attacks.
	var fromBytes int64
	flags := os.O_CREATE | os.O_WRONLY | os.O_EXCL
	f, openErr := os.OpenFile(partPath, flags, 0o644)
	if openErr != nil {
		// File may already exist — retry without O_EXCL for resume
		flags = os.O_CREATE | os.O_WRONLY
		f, openErr = os.OpenFile(partPath, flags, 0o644)
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
	f.Close()
	if err != nil {
		log.Printf("[download] ERROR: write failed after %d bytes: %v", written, err)
		return err
	}
	log.Printf("[download] Write complete: %d bytes written", written+fromBytes)

	// SHA-256 verification.
	//
	// Security (C-4 + L-10): The sidecar .sha256 file uses a TOFU (Trust On
	// First Use) model — the first download's hash is recorded and used to
	// verify subsequent downloads. This is inherently weaker than a manifest-
	// provided hash because an attacker who can replace the archive can also
	// replace the sidecar. The following hardening measures are applied:
	//
	//   1. When a root public key is embedded (signer.HasRootKey()), downloads
	//      without a real SHA-256 hash are REJECTED — the sidecar TOFU path is
	//      disabled entirely, forcing manifest-signed hashes.
	//   2. Sidecar files are written read-only (0o444) to raise the bar for
	//      casual tampering.
	//   3. First-use downloads print a prominent stderr WARNING.
	//   4. The audit log records Verified=false for all TOFU-based checks.
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
			if signer.HasRootKey() && !opts.IsThirdParty && !opts.AllowPlaceholderTOFU {
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
		// fall back to TOFU.
		if signer.HasRootKey() && !opts.IsThirdParty && !opts.AllowPlaceholderTOFU {
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
// Use) verification. If the file already exists (from a previous download)
// and is read-only (0o444 from a prior run), the read-only attribute is
// cleared before writing so the update succeeds.
//
// Security (L-10): After writing, the file is set to read-only (0o444) to
// hinder casual tampering. This is not a strong security boundary — an
// attacker who can write to the directory can still clear the attribute —
// but it raises the bar for opportunistic modification.
func writeSidecarHash(sidecarPath, gotHash, dest string) {
	content := gotHash + "  " + filepath.Base(dest) + "\n"
	// Clear read-only attribute if the file exists from a previous run
	if _, err := os.Stat(sidecarPath); err == nil {
		_ = os.Chmod(sidecarPath, 0o644)
	}
	if err := os.WriteFile(sidecarPath, []byte(content), 0o644); err != nil {
		log.Printf("[download] WARNING: failed to write sidecar hash file %s: %v", sidecarPath, err)
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

	// Verify the ExpectedSHA matches the signed SHA-256 map.
	// Sniff-discovered versions carry an all-zero placeholder hash that is
	// intentionally absent from the signed map; those fall through to the
	// existing TOFU sidecar path instead of being treated as tampering.
	if opts.ExpectedSHA != "" && ms.SHA256Map != "" && !util.IsAllZeroHash(opts.ExpectedSHA) {
		if !verifySHAInMap(ms.SHA256Map, opts.ExpectedSHA) {
			shaPreview := opts.ExpectedSHA
			if len(shaPreview) > 16 {
				shaPreview = shaPreview[:16] + "..."
			}
			return fmt.Errorf("expected SHA-256 %s not found in signed manifest for tool %q — possible hash tampering", shaPreview, ms.Tool)
		}
	}

	return nil
}

// verifySHAInMap checks if the expected SHA-256 hash exists in the signed map.
func verifySHAInMap(sha256MapJSON, expectedSHA string) bool {
	var m map[string]map[string]string
	if err := json.Unmarshal([]byte(sha256MapJSON), &m); err != nil {
		return false
	}
	lower := strings.ToLower(expectedSHA)
	for _, verMap := range m {
		for _, hash := range verMap {
			if strings.ToLower(hash) == lower {
				return true
			}
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
		log.Printf("[download] WARNING: invalid proxy URL %q: %v", opts.ProxyURL, err)
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
