// Package remote discovers available tool versions from remote sources
// (GitHub Releases, JSON APIs, HTML pages).
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// httpClient is the default HTTP client with a reasonable timeout.
// Security (N-6): CheckRedirect blocks HTTPS→HTTP downgrade to
// prevent MITM on remote version discovery requests.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("insecure redirect: %s → %s (HTTPS downgrade blocked)", via[0].URL, req.URL)
		}
		return nil
	},
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
		return fmt.Errorf("github remote: API rate limit exhausted (reset at %s); please wait and retry", reset)
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
	case "none", "":
		return nil, fmt.Errorf("remote discovery not configured (type=%q)", r.Type)
	default:
		return nil, fmt.Errorf("unsupported remote type %q", r.Type)
	}
}

// discoverGitHub uses the GitHub Releases API to list tag names and
// extracts versions using the Pattern regex.
// It supports:
//   - TTL cache (5 min) with ETag / If-None-Match
//   - Pagination via the Link header
//   - Rate-limit awareness (X-RateLimit-Remaining)
func discoverGitHub(ctx context.Context, r manifest.RemoteSource) ([]string, error) {
	if r.URL == "" {
		return nil, fmt.Errorf("github remote: url (owner/repo) is required")
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100", r.URL)

	// Compile pattern once.
	pat := r.Pattern
	if pat == "" {
		pat = `v?(\d+\.\d+\.\d+)$`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("github remote: bad pattern %q: %w", pat, err)
	}

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
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}

		resp, err := httpClient.Do(req)
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
				return nil, fmt.Errorf("github remote: decode cached JSON: %w", err)
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

		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MiB
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("github remote: read body: %w", err)
		}

		// Store in cache with ETag.
		etag := resp.Header.Get("ETag")
		cachePut(currentURL, bodyBytes, etag)

		var pageReleases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(bodyBytes, &pageReleases); err != nil {
			return nil, fmt.Errorf("github remote: decode JSON: %w", err)
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

	resp, err := httpClient.Do(req)
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

	resp, err := httpClient.Do(req)
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
