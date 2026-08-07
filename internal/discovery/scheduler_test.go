package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// rateLimiter tests
// ---------------------------------------------------------------------------

func TestRateLimiter_Acquire(t *testing.T) {
	rl := newRateLimiter(2) // 2 per hour

	if !rl.acquire() {
		t.Error("expected first acquire to succeed")
	}
	if !rl.acquire() {
		t.Error("expected second acquire to succeed")
	}
	if rl.acquire() {
		t.Error("expected third acquire to fail (bucket empty)")
	}
}

func TestRateLimiter_Available(t *testing.T) {
	rl := newRateLimiter(5)
	if rl.available() != 5 {
		t.Errorf("expected 5 available tokens, got %d", rl.available())
	}
	rl.acquire()
	if rl.available() != 4 {
		t.Errorf("expected 4 available tokens after acquire, got %d", rl.available())
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := newRateLimiter(3600) // 3600 per hour = 1 per second

	// Consume all tokens.
	for i := 0; i < 3600; i++ {
		rl.acquire()
	}
	if rl.available() != 0 {
		t.Errorf("expected 0 tokens after consuming all, got %d", rl.available())
	}

	// Manually advance lastRefill to simulate time passing.
	rl.mu.Lock()
	rl.lastRefill = time.Now().Add(-2 * time.Second)
	rl.mu.Unlock()

	// Should have refilled ~2 tokens.
	avail := rl.available()
	if avail < 1 || avail > 3 {
		t.Errorf("expected ~2 refilled tokens, got %d", avail)
	}
}

func TestRateLimiter_DefaultRate(t *testing.T) {
	rl := newRateLimiter(0) // should default to 60
	if rl.maxTokens != 60 {
		t.Errorf("expected maxTokens=60 for rate=0, got %d", rl.maxTokens)
	}
}

// ---------------------------------------------------------------------------
// Cache tests
// ---------------------------------------------------------------------------

func TestCacheSaveLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "manual",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	// Save a cache entry.
	cache := &SniffCache{
		Tools: map[string]ToolSniffResult{
			"python": {
				LastSniff: time.Now().UTC().Truncate(time.Second),
				Versions:  []string{"3.13.0", "3.12.0"},
			},
		},
		LastSniffAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveCache(cache); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	// Load it back.
	loaded, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache failed: %v", err)
	}

	// Check tool entry.
	py, ok := loaded.Tools["python"]
	if !ok {
		t.Fatal("expected python entry in cache")
	}
	if len(py.Versions) != 2 || py.Versions[0] != "3.13.0" {
		t.Errorf("python versions mismatch: %v", py.Versions)
	}
	if !py.LastSniff.Equal(cache.Tools["python"].LastSniff) {
		t.Errorf("LastSniff mismatch: %v vs %v", py.LastSniff, cache.Tools["python"].LastSniff)
	}
}

func TestCacheLoadMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{}
	s := NewScheduler(cfg, dir, dir)

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache on missing file: %v", err)
	}
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
	if len(cache.Tools) != 0 {
		t.Errorf("expected empty tools map, got %d entries", len(cache.Tools))
	}
}

func TestCacheLoadCorrupted(t *testing.T) {
	dir := t.TempDir()
	// Write corrupted JSON.
	os.WriteFile(filepath.Join(dir, "sniff-cache.json"), []byte("{invalid json}"), 0o644)

	cfg := &config.SniffConfig{}
	s := NewScheduler(cfg, dir, dir)

	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache on corrupted file should not return error: %v", err)
	}
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
}

// ---------------------------------------------------------------------------
// mergeIntoManifest tests
// ---------------------------------------------------------------------------

func TestMergeIntoManifest(t *testing.T) {
	dir := t.TempDir()

	// Create a simple manifest file.
	tool := &manifest.Tool{
		Name: "testtool",
		VersionList: []manifest.Version{
			{Version: "1.0.0", Stable: true},
		},
	}

	// Write initial manifest.
	initialData, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, initialData, 0o644)

	// Discovered versions to merge.
	discovered := []DiscoveredVersion{
		{
			Version:     "1.1.0",
			ReleaseDate: "2026-01-15",
			Source:      "github",
			Unverified:  true, // FR-009: all discovered versions unverified
			Archives:    []manifest.Archive{{URL: "https://example.com/testtool-1.1.0.zip", SHA256: zeroSHA256, Size: 0}},
		},
		{
			Version:    "1.2.0",
			Source:     "archive_pattern",
			Unverified: true,
			Archives:   []manifest.Archive{{URL: "https://example.com/testtool-1.2.0.zip", SHA256: zeroSHA256, Size: 0}},
		},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	// Read back and verify.
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var updated manifest.Tool
	if err := yaml.Unmarshal(data, &updated); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if len(updated.VersionList) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(updated.VersionList))
	}

	// Check that original version is preserved.
	if updated.VersionList[0].Version != "1.0.0" {
		t.Errorf("expected first version 1.0.0, got %s", updated.VersionList[0].Version)
	}

	// Check discovered version 1.
	v1 := updated.VersionList[1]
	if v1.Version != "1.1.0" {
		t.Errorf("expected version 1.1.0, got %s", v1.Version)
	}
	if !v1.Discovered {
		t.Error("expected Discovered=true for merged version")
	}
	if !v1.Unverified {
		t.Error("expected Unverified=true for all discovered versions (FR-009)")
	}
	if v1.Release != "2026-01-15" {
		t.Errorf("expected Release=2026-01-15, got %s", v1.Release)
	}
	if len(v1.Archives) != 1 {
		t.Errorf("expected 1 archive, got %d", len(v1.Archives))
	}

	// Check discovered version 2 (unverified).
	v2 := updated.VersionList[2]
	if v2.Version != "1.2.0" {
		t.Errorf("expected version 1.2.0, got %s", v2.Version)
	}
	if !v2.Discovered {
		t.Error("expected Discovered=true for merged version")
	}
	if !v2.Unverified {
		t.Error("expected Unverified=true for archive_pattern-sourced version")
	}

	// Check that .bak backup exists.
	bakPath := manifestPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Error("expected .bak backup file to exist")
	}
}

func TestMergeIntoManifest_Duplicate(t *testing.T) {
	dir := t.TempDir()

	tool := &manifest.Tool{
		Name: "test",
		VersionList: []manifest.Version{
			{Version: "1.0.0"},
			{Version: "1.1.0"},
		},
	}
	data, _ := manifest.MarshalTool(tool)
	os.WriteFile(filepath.Join(dir, "test.yaml"), data, 0o644)

	// Try to merge already-existing version.
	discovered := []DiscoveredVersion{
		{Version: "1.0.0", Source: "github"},
		{Version: "1.2.0", Source: "github"},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	// Read back.
	readData, _ := os.ReadFile(filepath.Join(dir, "test.yaml"))
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if len(updated.VersionList) != 3 {
		t.Fatalf("expected 3 versions (1.0.0 already existed, should not duplicate), got %d", len(updated.VersionList))
	}
}

func TestMergeIntoManifest_EmptyDiscovered(t *testing.T) {
	err := mergeIntoManifest(&manifest.Tool{Name: "test"}, nil, t.TempDir())
	if err != nil {
		t.Errorf("expected nil for empty discovered, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// mergeIntoManifest latest update tests (Phase 2B)
// ---------------------------------------------------------------------------

func TestMergeIntoManifest_UpdateLatest(t *testing.T) {
	dir := t.TempDir()

	// Existing manifest with latest=2.47.0.
	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "2.47.0",
		VersionList: []manifest.Version{
			{Version: "1.0.0", Stable: true},
			{Version: "2.47.0", Stable: true},
		},
	}

	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	// T2B.1: discovered 2.55.0 (newer) → latest updated to 2.55.0.
	discovered := []DiscoveredVersion{
		{Version: "2.55.0", Source: "github", Unverified: true},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "2.55.0" {
		t.Errorf("T2B.1: expected latest=2.55.0, got %q", updated.Latest)
	}
}

func TestMergeIntoManifest_NoDowngrade(t *testing.T) {
	dir := t.TempDir()

	// T2B.2: existing latest=2.55.0, discovered 2.47.0 (older) → latest unchanged.
	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "2.55.0",
		VersionList: []manifest.Version{
			{Version: "2.55.0", Stable: true},
			{Version: "2.47.0", Stable: true},
		},
	}

	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "2.47.0", Source: "github", Unverified: true},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "2.55.0" {
		t.Errorf("T2B.2: expected latest=2.55.0 (unchanged), got %q", updated.Latest)
	}
}

func TestMergeIntoManifest_UpdateLatestMultiple(t *testing.T) {
	dir := t.TempDir()

	// T2B.3: discovered [1.26.5, 1.25.3], existing latest=1.23.2 → latest=1.26.5.
	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "1.23.2",
		VersionList: []manifest.Version{
			{Version: "1.23.2", Stable: true},
		},
	}

	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "1.26.5", Source: "github", Unverified: true},
		{Version: "1.25.3", Source: "github", Unverified: true},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "1.26.5" {
		t.Errorf("T2B.3: expected latest=1.26.5, got %q", updated.Latest)
	}
	if len(updated.VersionList) != 3 {
		t.Errorf("T2B.3: expected 3 versions, got %d", len(updated.VersionList))
	}
}

func TestMergeIntoManifest_SkipNonSemver(t *testing.T) {
	dir := t.TempDir()

	// T2B.4: discovered [r29], existing latest=27.2.12479018 → latest unchanged.
	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "27.2.12479018",
		VersionList: []manifest.Version{
			{Version: "27.2.12479018", Stable: true},
		},
	}

	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "r29", Source: "github", Unverified: true},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "27.2.12479018" {
		t.Errorf("T2B.4: expected latest=27.2.12479018 (unchanged), got %q", updated.Latest)
	}
}

func TestMergeIntoManifest_UpdateLatestPureNumeric(t *testing.T) {
	dir := t.TempDir()

	// T2B.5: discovered [26], existing latest=25 → latest=26.
	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "25",
		VersionList: []manifest.Version{
			{Version: "25", Stable: true},
		},
	}

	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "26", Source: "github", Unverified: true},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "26" {
		t.Errorf("T2B.5: expected latest=26, got %q", updated.Latest)
	}
}

// ---------------------------------------------------------------------------
// Scheduler OnStartup strategies
// ---------------------------------------------------------------------------

func TestSchedulerOnStartup_Manual(t *testing.T) {
	// Manual strategy should do nothing (no sniff triggered).
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "manual",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)
	ctx := context.Background()
	s.OnStartup(ctx)

	// Cache should not exist (no sniff was triggered).
	cache, err := s.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !cache.LastSniffAt.IsZero() {
		t.Error("expected no sniff for manual strategy")
	}
}

func TestSchedulerOnStartup_Service(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "service",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)
	ctx := context.Background()
	s.OnStartup(ctx)

	cache, _ := s.LoadCache()
	if !cache.LastSniffAt.IsZero() {
		t.Error("expected no sniff for service strategy")
	}
}

func TestSchedulerOnStartup_IntervalSkip(t *testing.T) {
	// Interval strategy should skip when last sniff is recent.
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "interval",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	// Write a cache with recent LastSniffAt.
	recent := time.Now().Add(-1 * time.Hour)
	cache := &SniffCache{
		Tools:       make(map[string]ToolSniffResult),
		LastSniffAt: recent,
	}
	s.SaveCache(cache)

	ctx := context.Background()
	s.OnStartup(ctx)

	// LastSniffAt should be unchanged (no sniff triggered).
	loaded, _ := s.LoadCache()
	if !loaded.LastSniffAt.Equal(recent) {
		t.Errorf("expected LastSniffAt unchanged (%v), got %v", recent, loaded.LastSniffAt)
	}
}

func TestSchedulerOnStartup_Startup(t *testing.T) {
	// Startup strategy should launch async goroutine.
	// We verify by checking that OnStartup returns immediately (non-blocking).
	// The background goroutine will complete via context cancellation.

	// Channel to track when OnStartup returns.
	startupReturned := make(chan struct{})

	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	// Create a context that will be cancelled after OnStartup returns.
	ctx, cancel := context.WithCancel(context.Background())

	// Run OnStartup in a goroutine and signal when done.
	go func() {
		s.OnStartup(ctx)
		close(startupReturned)
	}()

	// Wait briefly for OnStartup to return (it should be immediate).
	select {
	case <-startupReturned:
		// Good — OnStartup returned immediately (non-blocking).
	case <-time.After(500 * time.Millisecond):
		t.Fatal("OnStartup blocked for startup strategy")
	}

	// Cancel the context to stop the background goroutine.
	cancel()
	// Give a short grace period for the goroutine to clean up.
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Scheduler NewScheduler
// ---------------------------------------------------------------------------

func TestNewScheduler(t *testing.T) {
	cfg := &config.SniffConfig{Strategy: "manual"}
	s := NewScheduler(cfg, "/tmp/cache", "/tmp/manifests")
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.cfg != cfg {
		t.Error("cfg not preserved")
	}
	if s.cachePath != filepath.Join("/tmp/cache", "sniff-cache.json") {
		t.Errorf("unexpected cachePath: %s", s.cachePath)
	}
	if s.limiter == nil {
		t.Error("rate limiter not initialised")
	}
	if s.registry == nil {
		t.Error("sniffer registry not initialised")
	}
}

// ---------------------------------------------------------------------------
// Regression: cache remaining=0 + zero ResetAt
// ---------------------------------------------------------------------------

func TestNewScheduler_CacheZeroRemainingZeroResetAt(t *testing.T) {
	// When sniff-cache.json has remaining=0 and ResetAt is zero (or expired),
	// NewScheduler must NOT leave the bucket stuck at 0 — acquire() should
	// succeed because the bucket should be full by default.
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "manual",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}

	// Write a cache with remaining=0 and zero ResetAt.
	cache := &SniffCache{
		Tools: make(map[string]ToolSniffResult),
		RateLimit: RateLimitState{
			Remaining: 0,
			ResetAt:   time.Time{}, // zero value, e.g. "0001-01-01T00:00:00Z"
		},
	}
	s := NewScheduler(cfg, dir, dir)
	if err := s.SaveCache(cache); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	// Create a new scheduler that loads the cache above.
	s2 := NewScheduler(cfg, dir, dir)

	// The bucket should NOT be stuck at 0.
	if !s2.limiter.acquire() {
		t.Error("acquire() returned false: bucket stuck at 0 despite zero/expired ResetAt")
	}
	if s2.limiter.available() < 1 {
		t.Error("no tokens available: bucket stuck at 0")
	}
}

// ---------------------------------------------------------------------------
// Regression: setTokens updates lastRefill
// ---------------------------------------------------------------------------

func TestRateLimiter_SetTokensUpdatesLastRefill(t *testing.T) {
	rl := newRateLimiter(60)

	// Consume all tokens.
	for i := 0; i < 60; i++ {
		rl.acquire()
	}
	if rl.available() != 0 {
		t.Fatal("expected 0 tokens after consuming all")
	}

	// Force lastRefill far in the past so refill would normally add tokens.
	rl.mu.Lock()
	rl.lastRefill = time.Now().Add(-1 * time.Hour)
	rl.mu.Unlock()

	// setTokens should reset lastRefill to time.Now().
	rl.setTokens(10)

	// available() calls refill(). With old code, lastRefill is still 1h ago
	// so refill would add ~60 tokens (capped at maxTokens). With the fix,
	// lastRefill is now, so no refill happens and available() == 10.
	avail := rl.available()
	if avail != 10 {
		t.Errorf("expected exactly 10 tokens (no refill after setTokens), got %d", avail)
	}
}
