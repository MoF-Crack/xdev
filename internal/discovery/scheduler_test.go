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

	byVer := map[string]manifest.Version{}
	for _, v := range updated.VersionList {
		byVer[v.Version] = v
	}
	// Newest-first: 1.2.0, 1.1.0, 1.0.0 (semver; 1.1.0 also has a release date).
	if updated.VersionList[0].Version != "1.2.0" || updated.VersionList[1].Version != "1.1.0" || updated.VersionList[2].Version != "1.0.0" {
		t.Errorf("version order = %v, want newest-first [1.2.0 1.1.0 1.0.0]", []string{updated.VersionList[0].Version, updated.VersionList[1].Version, updated.VersionList[2].Version})
	}

	v1, ok := byVer["1.1.0"]
	if !ok {
		t.Fatal("missing merged version 1.1.0")
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

	v2, ok := byVer["1.2.0"]
	if !ok {
		t.Fatal("missing merged version 1.2.0")
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

// TestMergeIntoManifest_FillsArchivesForExisting — 已存在但缺 archives 的
// 版本（历史嗅探时模板不可用），本次嗅探带 archives → 补填（upsert）。
func TestMergeIntoManifest_FillsArchivesForExisting(t *testing.T) {
	dir := t.TempDir()
	tool := &manifest.Tool{
		Name: "rusty",
		VersionList: []manifest.Version{
			{Version: "1.98.0", Discovered: true, Unverified: true}, // 历史嗅探：无 archives
		},
	}
	initialData, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "rusty.yaml")
	os.WriteFile(manifestPath, initialData, 0o644)

	discovered := []DiscoveredVersion{
		{
			Version:    "1.98.0",
			Unverified: true,
			Archives: []manifest.Archive{{
				URL:         "https://static.example.com/rustup-init.exe",
				InstallArgs: []string{"--default-toolchain", "{{version}}"},
				SHA256:      zeroSHA256,
			}},
		},
	}

	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	data, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	if err := yaml.Unmarshal(data, &updated); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(updated.VersionList) != 1 {
		t.Fatalf("expected 1 version, got %d", len(updated.VersionList))
	}
	v := updated.VersionList[0]
	if v.Version != "1.98.0" {
		t.Fatalf("version = %q, want 1.98.0", v.Version)
	}
	if len(v.Archives) != 1 {
		t.Fatalf("archives not patched: got %d, want 1", len(v.Archives))
	}
	if v.Archives[0].URL != "https://static.example.com/rustup-init.exe" {
		t.Errorf("patched URL = %q", v.Archives[0].URL)
	}
	if len(v.Archives[0].InstallArgs) != 2 || v.Archives[0].InstallArgs[1] != "{{version}}" {
		t.Errorf("install_args not preserved: %v", v.Archives[0].InstallArgs)
	}
	if v.Stable {
		t.Error("patched entry must not mutate stable flag")
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
	// 2026-10-26：本用例测的是 **latest 语义**（哪个版本该成为 latest），
	// 与「候选是否可安装」无关。构造的 DiscoveredVersion 不带 archives，
	// 因此在测试期间关闭 archives 门，把两者隔离。
	defer WithRequireArchivesForPromotion(false)()
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
	// 2026-10-26：本用例测的是 **latest 语义**（哪个版本该成为 latest），
	// 与「候选是否可安装」无关。构造的 DiscoveredVersion 不带 archives，
	// 因此在测试期间关闭 archives 门，把两者隔离。
	defer WithRequireArchivesForPromotion(false)()
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
	// 2026-10-26：本用例测的是 **latest 语义**（哪个版本该成为 latest），
	// 与「候选是否可安装」无关。构造的 DiscoveredVersion 不带 archives，
	// 因此在测试期间关闭 archives 门，把两者隔离。
	defer WithRequireArchivesForPromotion(false)()
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

// Spec 039（dsh 预发布版本流）：latest 本身是预发布（rc）时，同通道的更新
// 预发布按 SemVer 2.0.0 正常推进 latest——修复 IsVersionNewer 对任何含
// "-" 的版本串恒 false 导致 rc-only 流 latest 永不前进的问题。异通道
// （alpha）候选不参与提升。
func TestMergeIntoManifest_PrereleaseAdvancesPrereleaseLatest(t *testing.T) {
	// 2026-10-26：本用例测的是 **latest 语义**（哪个版本该成为 latest），
	// 与「候选是否可安装」无关。构造的 DiscoveredVersion 不带 archives，
	// 因此在测试期间关闭 archives 门，把两者隔离。
	defer WithRequireArchivesForPromotion(false)()
	dir := t.TempDir()

	tool := &manifest.Tool{
		Name:   "dsh",
		Latest: "0.1.2-rc.1",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Stable: false},
		},
	}
	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "dsh.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	// 发现批次（npm 列表降序形态）：0.1.3-rc.1 与 rc 同通道、semver 更高
	// → 提升；0.1.3-alpha.1 虽 semver 更高但为 alpha 通道 → 不提升。
	discovered := []DiscoveredVersion{
		{Version: "0.1.3-rc.1", Source: "npm", Unverified: true},
		{Version: "0.1.3-alpha.1", Source: "npm", Unverified: true},
		{Version: "0.1.2-alpha.5", Source: "npm", Unverified: true}, // 更旧
	}
	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "0.1.3-rc.1" {
		t.Errorf("expected latest=0.1.3-rc.1, got %q", updated.Latest)
	}
}

// Spec 039（真实 dsh 数据形态）：npm dist-tags latest=0.1.2-rc.1 而
// alpha=0.1.5-alpha.1（semver 更高的 alpha 通道）。alpha-only 发现批次
// 不得劫持 rc-latest——用户 `xdev install dsh` 默认装 latest，不能装到
// registry 不认作 latest 的 alpha。
func TestMergeIntoManifest_AlphaChannelNeverHijacksRCLatest(t *testing.T) {
	dir := t.TempDir()

	tool := &manifest.Tool{
		Name:   "dsh",
		Latest: "0.1.2-rc.1",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Stable: false},
		},
	}
	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "dsh.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "0.1.5-alpha.1", Source: "npm", Unverified: true},
		{Version: "0.1.3-alpha.2", Source: "npm", Unverified: true},
	}
	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "0.1.2-rc.1" {
		t.Errorf("alpha channel must not hijack the rc-latest, got %q", updated.Latest)
	}
	// alpha 版本仍进入版本列表（用户可显式选择安装 alpha 通道）。
	found := false
	for _, v := range updated.VersionList {
		if v.Version == "0.1.5-alpha.1" {
			found = true
		}
	}
	if !found {
		t.Error("expected 0.1.5-alpha.1 appended to the version list")
	}
}

// Spec 039：稳定 latest 不被预发布版本抢占（npm dist-tags 语义：alpha/rc
// 走独立通道）。dsh 未来发布 1.0.0 稳定版后，1.1.0-alpha.1 不得劫持 latest。
func TestMergeIntoManifest_PrereleaseNeverHijacksStableLatest(t *testing.T) {
	dir := t.TempDir()

	tool := &manifest.Tool{
		Name:   "testtool",
		Latest: "1.0.0",
		VersionList: []manifest.Version{
			{Version: "1.0.0", Stable: true},
		},
	}
	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "testtool.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "1.1.0-alpha.1", Source: "npm", Unverified: true},
		{Version: "1.0.1-rc.1", Source: "npm", Unverified: true},
		{Version: "1.0.0-beta.9", Source: "npm", Unverified: true},
	}
	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	if updated.Latest != "1.0.0" {
		t.Errorf("stable latest must not be hijacked, got %q", updated.Latest)
	}
	// 预发布版本仍然进入版本列表（用户可显式选择安装）。
	found := map[string]bool{}
	for _, v := range updated.VersionList {
		found[v.Version] = true
	}
	for _, want := range []string{"1.1.0-alpha.1", "1.0.1-rc.1"} {
		if !found[want] {
			t.Errorf("expected %s appended to the version list", want)
		}
	}
}

// Spec 039：追加版本的 stable 标志按版本串推导——稳定 → true，
// 预发布（rc/alpha）→ false。
func TestMergeIntoManifest_StableFlagFromVersionString(t *testing.T) {
	dir := t.TempDir()

	tool := &manifest.Tool{
		Name:   "dsh",
		Latest: "0.1.2-rc.1",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Stable: false},
		},
	}
	data, _ := manifest.MarshalTool(tool)
	manifestPath := filepath.Join(dir, "dsh.yaml")
	os.WriteFile(manifestPath, data, 0o644)

	discovered := []DiscoveredVersion{
		{Version: "0.1.3-rc.1", Source: "npm", Unverified: true},
		{Version: "1.0.0", Source: "npm", Unverified: true},
	}
	if err := mergeIntoManifest(tool, discovered, dir); err != nil {
		t.Fatalf("mergeIntoManifest failed: %v", err)
	}

	readData, _ := os.ReadFile(manifestPath)
	var updated manifest.Tool
	yaml.Unmarshal(readData, &updated)

	byVer := map[string]manifest.Version{}
	for _, v := range updated.VersionList {
		byVer[v.Version] = v
	}
	if v := byVer["0.1.3-rc.1"]; v.Stable {
		t.Error("appended rc version must have Stable=false")
	}
	if v := byVer["1.0.0"]; !v.Stable {
		t.Error("appended stable version must have Stable=true")
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
	// 2026-10-26：本用例测的是 **latest 语义**（哪个版本该成为 latest），
	// 与「候选是否可安装」无关。构造的 DiscoveredVersion 不带 archives，
	// 因此在测试期间关闭 archives 门，把两者隔离。
	defer WithRequireArchivesForPromotion(false)()
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
	// Startup strategy should launch an async goroutine.
	//
	// 判据是确定性的：把后台嗅探按在 loadCatalog 里，直到测试确认 OnStartup
	// 已返回。于是「OnStartup 在嗅探仍在飞行时就已经返回」成为可证事实——
	// 同步实现不可能满足。原先的「500ms 内返回」是与机器负载赛跑的墙钟断言：
	// 全量 go test ./... 并发跑 30 个包时会被调度饥饿打成假红。
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "startup",
		Interval:       "24h",
		Concurrency:    3,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)

	started := make(chan struct{})
	release := make(chan struct{})
	s.loadCatalog = func(string) (*manifest.Catalog, error) {
		close(started)
		<-release // 一直按住，直到测试确认 OnStartup 已返回
		return &manifest.Catalog{}, nil
	}

	// Create a context that will be cancelled after OnStartup returns.
	ctx, cancel := context.WithCancel(context.Background())

	// Run OnStartup in a goroutine and signal when done.
	startupReturned := make(chan struct{})
	go func() {
		s.OnStartup(ctx)
		close(startupReturned)
	}()

	// 先等后台嗅探真的开始（证明它确实被 spawn 了）。
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("background sniff never started for startup strategy")
	}

	// 嗅探仍被按住：此时 OnStartup 若同步执行就绝不可能已经返回。
	select {
	case <-startupReturned:
		// Good — OnStartup returned while the sniff was still in flight.
	case <-time.After(10 * time.Second):
		t.Fatal("OnStartup blocked for startup strategy")
	}
	if !s.StartupSniffSpawned() {
		t.Error("StartupSniffSpawned() = false after a spawned startup sniff (票 34 / P1-30)")
	}

	// 放行后台嗅探并取消上下文，让 goroutine 干净收尾。
	close(release)
	cancel()
	s.Shutdown()
}

func TestSchedulerOnStartup_IntervalExpiredIsAsync(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{
		Strategy:       "interval",
		Interval:       "1h",
		Concurrency:    1,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, dir, dir)
	s.SaveCache(&SniffCache{
		Tools:       make(map[string]ToolSniffResult),
		LastSniffAt: time.Now().Add(-2 * time.Hour),
	})

	// 确定性判据（同上）：嗅探被按住不放，OnStartup 必须已经返回。
	started := make(chan struct{})
	release := make(chan struct{})
	s.loadCatalog = func(string) (*manifest.Catalog, error) {
		close(started)
		<-release
		return &manifest.Catalog{}, nil
	}

	returned := make(chan struct{})
	go func() {
		s.OnStartup(context.Background())
		close(returned)
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("background sniff never started after cache expiry (Spec 030)")
	}
	select {
	case <-returned:
		// Good — 返回时嗅探仍在飞行，确证非阻塞。
	case <-time.After(10 * time.Second):
		t.Fatal("interval OnStartup blocked after cache expiry (Spec 030)")
	}

	close(release)
	s.Shutdown()
}

func TestSniffAll_LoadsCatalogOnceWithTools(t *testing.T) {
	dir := t.TempDir()
	writeTool := func(name string) {
		t.Helper()
		body := "name: " + name + "\nauto_sniff: false\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTool("alpha")
	writeTool("beta")
	writeTool("gamma")

	cfg := &config.SniffConfig{Strategy: "manual", Concurrency: 3, RateLimitPerHr: 60}
	s := NewScheduler(cfg, dir, dir)
	loads := 0
	s.loadCatalog = func(d string) (*manifest.Catalog, error) {
		loads++
		return manifest.LoadCatalog(d)
	}
	if err := s.SniffAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Errorf("SniffAll catalog loads = %d, want 1 (Spec 030)", loads)
	}
}

// fakeIgnoreSniffer 记录对 gradle 的 Sniff 调用次数（不触发网络）。
type fakeIgnoreSniffer struct {
	called *int
}

func (f fakeIgnoreSniffer) Name() string { return "fake" }
func (f fakeIgnoreSniffer) Supports(t *manifest.Tool) bool {
	return t.Name == "gradle"
}
func (f fakeIgnoreSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	*f.called++
	return nil, nil
}

// TestSniffAll_AutoSkipsIgnoredUpdates — 后台自动触发（force=false）跳过
// 忽略清单（kind=update）中的工具；手动触发不受影响。
func TestSniffAll_AutoSkipsIgnoredUpdates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "gradle.yaml"), []byte("name: gradle\nauto_sniff: true\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignored_tools.json"), []byte(`{"items":[{"name":"gradle","kind":"update","ignoredAt":"2026-08-23T00:00:00Z"}]}`), 0o644)

	cfg := &config.SniffConfig{Strategy: "interval", Concurrency: 3, RateLimitPerHr: 60}
	s := NewScheduler(cfg, dir, dir)
	called := 0
	s.registry = &SnifferRegistry{sniffers: []Sniffer{fakeIgnoreSniffer{called: &called}}}
	s.loadCatalog = func(d string) (*manifest.Catalog, error) { return manifest.LoadCatalog(d) }

	if err := s.sniffAllWithForce(context.Background(), false); err != nil {
		t.Fatalf("sniffAllWithForce(auto): %v", err)
	}
	if called != 0 {
		t.Errorf("auto sniff called ignored tool %d times, want 0", called)
	}

	// 手动触发（force=true）保持显式语义：不跳过。
	if err := s.sniffAllWithForce(context.Background(), true); err != nil {
		t.Fatalf("sniffAllWithForce(manual): %v", err)
	}
	if called != 1 {
		t.Errorf("manual sniff called ignored tool %d times, want 1", called)
	}
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
