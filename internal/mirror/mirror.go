// Package mirror implements URL rewriting for known download mirrors,
// with latency-based selection for optimal download speed.
//
// # URL Rewriting
//
// Given an upstream URL (e.g. https://nodejs.org/dist/v20.10.0/...) and
// a mirror entry (e.g. https://npmmirror.com/mirrors/node), the rewrite
// function performs a best-effort prefix swap:
//  1. Parse both the original and mirror URLs
//  2. Extract the "leaf" path (everything below the first path component)
//  3. Append the leaf to the mirror's path
//  4. Handle duplication where the leaf starts with the mirror's basename
//
// # Speed Testing
//
// When multiple mirrors are configured for a tool, SpeedTest performs
// concurrent HEAD requests to rank them by latency. Results are cached
// in-memory for the process lifetime. Apply picks the fastest mirror
// after SpeedTest, or the first configured mirror without testing.
//
// # Configuration
//
// Mirror URLs are read from the [mirror] section of config.yaml.
// Default mirrors are China-optimized (aliyun, npmmirror, goproxy.cn, etc.).
// Example:
//
//	[mirror]
//	python = "https://mirrors.aliyun.com/python"
//	node = "https://npmmirror.com/mirrors/node"
//	go = "https://goproxy.cn/dl"
//
// # Known Limitations
//
//   - Speed-test results are in-memory only (not persisted to disk);
//     every new process invocation starts fresh
//   - The rewrite logic is a simple prefix swap and may not work for
//     mirrors with completely different directory structures
//   - headCheck considers any response with status < 500 as OK, which
//     means 3xx redirects and 4xx client errors are treated as "mirror is up"
package mirror

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// defaultClient is a shared HTTP client with connection pooling for mirror checks.
var defaultClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	},
}

// mirrorResult holds the latency measurement for one mirror.
type mirrorResult struct {
	URL         string
	Latency     time.Duration
	OK          bool
	Priority    int
	LastSuccess time.Time
}

// Table is a list of mirrors keyed by tool name.
type Table struct {
	m      map[string][]string
	tested map[string][]mirrorResult // speed-test results per tool
	testMu sync.RWMutex
}

// FromConfig builds a Table from the [mirror] section of the config.
func FromConfig(cfg *config.Config) *Table {
	t := &Table{
		m:      map[string][]string{},
		tested: map[string][]mirrorResult{},
	}
	for tool, base := range cfg.Mirror {
		t.m[tool] = append(t.m[tool], base)
	}
	return t
}

// Candidates returns the configured mirror URLs for a tool.
func (t *Table) Candidates(tool string) []string {
	return t.m[tool]
}

// SpeedTest performs concurrent HEAD requests against all mirrors
// registered for the given tool, ranks them by latency, and caches
// the results.  The fastest mirror will be used by subsequent Apply
// calls for that tool.
//
// If a mirror does not respond within timeout, it is marked as
// failed and ranked last.  The test is best-effort: if all mirrors
// fail, Apply falls back to the first configured mirror.
func (t *Table) SpeedTest(ctx context.Context, tool string, timeout time.Duration) error {
	tool = family(tool)
	candidates := t.m[tool]
	if len(candidates) == 0 {
		return nil
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	log.Printf("[mirror] SpeedTest: testing %d mirrors for tool %s", len(candidates), tool)

	results := make([]mirrorResult, len(candidates))
	var wg sync.WaitGroup

	for i, mirrorURL := range candidates {
		wg.Add(1)
		go func(idx int, mURL string) {
			defer wg.Done()
			if !concreteArchiveURL(mURL) {
				results[idx] = mirrorResult{URL: mURL, Priority: idx}
				return
			}
			start := time.Now()
			ok := headCheck(ctx, mURL, timeout)
			latency := time.Since(start)
			result := mirrorResult{URL: mURL, Latency: latency, OK: ok, Priority: idx}
			if ok {
				result.LastSuccess = time.Now()
			}
			results[idx] = result
			if ok {
				log.Printf("[mirror]   %s: %s (OK)", mURL, latency.Truncate(time.Millisecond))
			} else {
				log.Printf("[mirror]   %s: FAILED", mURL)
			}
		}(i, mirrorURL)
	}
	wg.Wait()

	// Sort: OK mirrors first (by latency ascending), then failed ones
	sort.Slice(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK // OK=true before OK=false
		}
		if results[i].Latency != results[j].Latency {
			return results[i].Latency < results[j].Latency
		}
		if results[i].Priority != results[j].Priority {
			return results[i].Priority < results[j].Priority
		}
		return results[i].LastSuccess.After(results[j].LastSuccess)
	})

	t.testMu.Lock()
	t.tested[tool] = results
	t.testMu.Unlock()

	log.Printf("[mirror] SpeedTest complete for %s: fastest=%s", tool, results[0].URL)
	return nil
}

// SpeedTestAll runs SpeedTest for every tool that has mirrors configured.
func (t *Table) SpeedTestAll(ctx context.Context, timeout time.Duration) {
	for tool := range t.m {
		_ = t.SpeedTest(ctx, tool, timeout)
	}
}

// Apply rewrites a URL to use the mirror registered for the given
// tool, if any.  If no mirror is known the original URL is returned
// unchanged.
//
// After a SpeedTest, the fastest mirror is used.  If no test has been
// run, the first configured mirror is used (preserving M1 behavior).
func (t *Table) Apply(tool, original string) string {
	if original == "" {
		return original
	}

	tool = family(tool)
	// Check speed-test results first
	t.testMu.RLock()
	tested, hasTested := t.tested[tool]
	t.testMu.RUnlock()

	if hasTested && len(tested) > 0 {
		// Use the fastest mirror that passed the test
		for _, r := range tested {
			if r.OK {
				if rewritten, ok := rewrite(tool, original, r.URL); ok {
					return rewritten
				}
			}
		}
		// All failed; fall through to first configured
	}

	candidates := t.m[tool]
	if len(candidates) == 0 {
		return original
	}
	if rewritten, ok := rewrite(tool, original, candidates[0]); ok {
		return rewritten
	}
	return original
}

// headCheck performs a HEAD request to verify a mirror is reachable.
func headCheck(ctx context.Context, rawURL string, timeout time.Duration) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	client := defaultClient
	req, err := http.NewRequestWithContext(ctx, "HEAD", rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "xdev-mirror-test")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.Request.URL.Scheme != "https" {
		return false
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotImplemented {
		return false
	}

	rangeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err = http.NewRequestWithContext(rangeCtx, "GET", rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "xdev-mirror-test")
	req.Header.Set("Range", "bytes=0-7")
	resp, err = client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.Request.URL.Scheme != "https" || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent) {
		return false
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8))
	return err == nil && len(payload) > 0
}

// rewrite performs a best-effort host/path prefix swap.
//
// We try to keep the trailing path of the upstream intact because
// most mirrors expose the same directory layout.  When the
// mirror's path is a prefix of the upstream's leaf, we strip the
// duplicate; otherwise we simply concatenate the two halves.
func rewrite(tool, original, mirror string) (string, bool) {
	u, err := url.Parse(original)
	if err != nil {
		return original, false
	}
	m, err := url.Parse(mirror)
	if err != nil || m.Scheme != "https" || m.Host == "" {
		return original, false
	}
	base := strings.TrimRight(m.Path, "/")
	var suffix string
	switch tool {
	case "android":
		if u.Host != "dl.google.com" || !strings.HasPrefix(u.Path, "/android/repository/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/android/repository/")
	case "rust":
		if u.Host != "static.rust-lang.org" || !strings.HasPrefix(u.Path, "/rustup/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/")
	case "node":
		if u.Host != "nodejs.org" || !strings.HasPrefix(u.Path, "/dist/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/dist/")
	case "python":
		if !strings.HasPrefix(u.Path, "/ftp/python/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/ftp/python/")
	case "flutter":
		if u.Host != "storage.googleapis.com" || !strings.HasPrefix(u.Path, "/flutter_infra_release/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/")
	case "maven":
		suffix = strings.TrimPrefix(u.Path, "/maven2/")
	case "gradle":
		if !strings.HasPrefix(u.Path, "/distributions/") {
			return original, false
		}
		suffix = strings.TrimPrefix(u.Path, "/distributions/")
	default:
		return original, false
	}
	if suffix == "" {
		return original, false
	}
	return fmt.Sprintf("https://%s%s/%s", m.Host, base, suffix), true
}

func family(tool string) string {
	if tool == "android" || strings.HasPrefix(tool, "android-") {
		return "android"
	}
	return tool
}

func concreteArchiveURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	return name != "" && strings.Contains(name, ".")
}
