package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache expiry duration: entries older than 24h are re-probed.
const mirrorHealthCacheExpiry = 24 * time.Hour

// Default probe timeout for HEAD requests to candidate mirrors.
const defaultProbeTimeout = 3 * time.Second

// MirrorHealth records the result of a single mirror probe.
type MirrorHealth struct {
	URL         string    `json:"url"`
	Available   bool      `json:"available"`
	LastChecked time.Time `json:"last_checked"`
	LatencyMs   int64     `json:"latency_ms"`
}

// MirrorHealthCache persists mirror health results to a JSON file so that
// known-unavailable mirrors are skipped on subsequent installs.
type MirrorHealthCache struct {
	entries map[string]MirrorHealth
	path    string
	mu      sync.Mutex
}

// LoadMirrorHealthCache reads the mirror health cache from
// <cacheDir>/mirror-health.json. If the file does not exist or is corrupt,
// an empty cache is returned (no error).
func LoadMirrorHealthCache(cacheDir string) *MirrorHealthCache {
	c := &MirrorHealthCache{
		entries: make(map[string]MirrorHealth),
		path:    filepath.Join(cacheDir, "mirror-health.json"),
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		// File not found or unreadable — start fresh.
		return c
	}
	var stored []MirrorHealth
	if err := json.Unmarshal(data, &stored); err != nil {
		log.Printf("[mirror] WARNING: failed to parse mirror health cache %s: %v — starting fresh", c.path, err)
		return c
	}
	for _, e := range stored {
		c.entries[e.URL] = e
	}
	log.Printf("[mirror] Loaded %d mirror health entries from %s", len(stored), c.path)
	return c
}

// IsAvailable checks whether a mirror URL is known to be available.
// Returns false if:
//   - the entry exists AND is marked unavailable AND is not yet expired
//   - the entry exists AND is expired (caller should re-probe)
//
// Returns true if:
//   - no entry exists (unknown = treat as potentially available)
//   - the entry exists AND is marked available
func (c *MirrorHealthCache) IsAvailable(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[url]
	if !ok {
		return true // unknown — treat as available
	}
	if c.isExpiredLocked(e) {
		return true // expired — allow re-probe
	}
	return e.Available
}

// RecordResult updates the cache with a probe result for the given URL.
// The entry is created or updated and the cache is marked dirty (caller
// should call Save() to persist).
func (c *MirrorHealthCache) RecordResult(url string, available bool, latencyMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[url] = MirrorHealth{
		URL:         url,
		Available:   available,
		LastChecked: time.Now(),
		LatencyMs:   latencyMs,
	}
}

// Save persists the cache to the JSON file. Errors are logged but not
// returned to the caller (install should not fail due to cache write failure).
func (c *MirrorHealthCache) Save() {
	c.mu.Lock()
	defer c.mu.Unlock()

	stored := make([]MirrorHealth, 0, len(c.entries))
	for _, e := range c.entries {
		stored = append(stored, e)
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].URL < stored[j].URL })

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		log.Printf("[mirror] WARNING: failed to marshal mirror health cache: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		log.Printf("[mirror] WARNING: failed to create cache dir: %v", err)
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("[mirror] WARNING: failed to write mirror health cache: %v", err)
		return
	}
	if err := os.Rename(tmp, c.path); err != nil {
		log.Printf("[mirror] WARNING: failed to rename mirror health cache: %v", err)
	}
}

// IsExpired returns true if the cached entry for the given URL is older
// than 24 hours, meaning it should be re-probed.
func (c *MirrorHealthCache) IsExpired(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[url]
	if !ok {
		return true // no entry = treat as expired
	}
	return c.isExpiredLocked(e)
}

// isExpiredLocked checks expiry without locking (caller must hold mu).
func (c *MirrorHealthCache) isExpiredLocked(e MirrorHealth) bool {
	return time.Since(e.LastChecked) > mirrorHealthCacheExpiry
}

// probeURL sends a HEAD request to the given URL and returns the latency
// in milliseconds. Returns -1 and an error if the request fails.
// Only HTTPS URLs are accepted (security: prevent plaintext probe leaks).
func probeURL(ctx context.Context, url string) (int64, error) {
	if !strings.HasPrefix(url, "https://") {
		return -1, fmt.Errorf("probe URL must be HTTPS, got %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return -1, fmt.Errorf("create request: %w", err)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1, fmt.Errorf("HEAD %s: %w", url, err)
	}
	resp.Body.Close()
	latency := time.Since(start).Milliseconds()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return latency, nil
	}
	return -1, fmt.Errorf("HEAD %s returned status %d", url, resp.StatusCode)
}

// probeResult is the outcome of probing a single mirror URL.
type probeResult struct {
	URL       string
	LatencyMs int64
	Err       error
}

// SelectBestMirror concurrently probes all candidate mirror URLs and returns
// the best available one. Selection policy:
//   - First URL that responds with 2xx/3xx and lowest latency wins.
//   - If none respond successfully, the candidate with the lowest latency
//     (even on error) is returned as a fallback, along with an error.
//   - If all probes fail entirely, returns ("", error).
//
// The timeout parameter controls the per-request deadline. The overall
// function blocks until the fastest successful probe completes (or all
// probes have finished).
func SelectBestMirror(candidates []string, cache *MirrorHealthCache, timeout time.Duration) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no mirror candidates to probe")
	}
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type result struct {
		URL       string
		LatencyMs int64
		Err       error
	}

	ch := make(chan result, len(candidates))
	var wg sync.WaitGroup

	for _, url := range candidates {
		wg.Add(1)
		url := url // capture
		go func() {
			defer wg.Done()
			latency, err := probeURL(ctx, url)
			ch <- result{URL: url, LatencyMs: latency, Err: err}
		}()
	}

	// Close the channel once all probes complete.
	go func() {
		wg.Wait()
		close(ch)
	}()

	var bestURL string
	var bestLatency int64 = -1
	var successCount int
	var allErrors []string

	for r := range ch {
		if r.Err == nil {
			successCount++
			if bestURL == "" || r.LatencyMs < bestLatency {
				bestURL = r.URL
				bestLatency = r.LatencyMs
			}
		} else {
			allErrors = append(allErrors, fmt.Sprintf("%s: %v", r.URL, r.Err))
		}
		// Record result in cache (best-effort, even on error).
		if cache != nil {
			cache.RecordResult(r.URL, r.Err == nil, r.LatencyMs)
		}
	}

	if bestURL != "" {
		return bestURL, nil
	}

	// All probes failed — build a descriptive error.
	errMsg := "all mirrors unavailable"
	if len(allErrors) > 0 {
		errMsg = errMsg + ": " + allErrors[0]
		if len(allErrors) > 1 {
			errMsg += fmt.Sprintf(" (+%d more)", len(allErrors)-1)
		}
	}
	return "", fmt.Errorf("%s", errMsg)
}
