package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Strategy
// ---------------------------------------------------------------------------

// Strategy represents a trigger strategy for automatic sniffing.
type Strategy string

const (
	StrategyInterval Strategy = "interval" // Periodic, checked at startup
	StrategyStartup  Strategy = "startup"  // Every startup, async
	StrategyService  Strategy = "service"  // System scheduler, no auto-trigger
	StrategyManual   Strategy = "manual"   // User-initiated only
)

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

// Scheduler orchestrates tool-version sniffing according to the configured
// trigger strategy.  It owns a SnifferRegistry, reads/writes the sniff
// cache, applies token-bucket rate limiting, and merges discovered
// versions back into the on-disk manifests.
type Scheduler struct {
	registry    *SnifferRegistry
	cfg         *config.SniffConfig
	cachePath   string
	manifestDir string
	limiter     *rateLimiter
	mu          sync.Mutex // protects cache read/write (NFR-004)

	// shutdown management for startup / interval background sniff (SC-002, Spec 030).
	startupCtx    context.Context
	startupCancel context.CancelFunc
	startupDone   chan struct{}

	// loadCatalog is the catalog loader used by SniffNow / SniffAll.
	// Tests replace it to assert a single load per SniffAll.
	loadCatalog func(dir string) (*manifest.Catalog, error)
}

// NewScheduler creates a ready-to-use Scheduler.
//
//   - cfg:       The SniffConfig from xdev's config.yaml (strategy, interval,
//     concurrency, rate_limit_per_hour, auto_archive_fill).
//   - cacheDir:  Directory for the sniff-cache.json (typically
//     $XMDEV_HOME/cache).
//   - manifestDir: Directory containing tool manifest YAML files.
func NewScheduler(cfg *config.SniffConfig, cacheDir, manifestDir string) *Scheduler {
	// Normalise config fields (R2).
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 3
	}
	if cfg.RateLimitPerHr <= 0 {
		cfg.RateLimitPerHr = 300
	}

	s := &Scheduler{
		registry:    newSnifferRegistry(),
		cfg:         cfg,
		cachePath:   filepath.Join(cacheDir, "sniff-cache.json"),
		manifestDir: manifestDir,
		limiter:     newRateLimiter(cfg.RateLimitPerHr),
		loadCatalog: manifest.LoadCatalog,
	}

	// Load rate limit state from cache so the token bucket is
	// initialised with the cross-process remaining count (FR-007).
	if cache, err := s.LoadCache(); err == nil && cache != nil {
		resetAt := cache.RateLimit.ResetAt
		remaining := cache.RateLimit.Remaining

		if !resetAt.IsZero() && time.Now().Before(resetAt) {
			// Rate limit window still active — keep the count from cache
			// (may be 0).  acquire() returns false until reset passes.
			s.limiter.setTokens(remaining)
		} else if remaining > 0 {
			// No active window (reset zero or expired), restore positive
			// remaining count.  setTokens also resets lastRefill so the
			// refill baseline is correct.
			s.limiter.setTokens(remaining)
		}
		// remaining == 0 with no active window: keep the full bucket
		// (default from newRateLimiter).  This prevents a stale cache
		// from permanently starving the bucket.
	}

	return s
}

// OnStartup implements the four trigger strategies (FR-006):
//
//   - interval:  Loads the cache; if LastSniffAt is older than cfg.Interval,
//     launches an async SniffAll with a 60-second timeout (Spec 030: never
//     block CLI OnInitialize).
//   - startup:   Launches an async goroutine that runs SniffAll; the caller
//     should cancel ctx on process exit so partial results are saved.
//   - service:   No automatic trigger — the system scheduler calls
//     `xdev sniff --all --headless` externally.
//   - manual:    No automatic trigger — only `xdev sniff` triggers sniffing.
func (s *Scheduler) OnStartup(ctx context.Context) {
	switch Strategy(s.cfg.Strategy) {
	case StrategyInterval:
		s.onStartupInterval(ctx)
	case StrategyStartup:
		s.onStartupStartup(ctx)
	case StrategyService, StrategyManual:
		// Nothing — only load cache is needed, handled by caller.
	}
}

func (s *Scheduler) onStartupInterval(ctx context.Context) {
	if !s.intervalElapsed() {
		return
	}

	s.startupCtx, s.startupCancel = context.WithTimeout(ctx, 120*time.Second)
	s.startupDone = make(chan struct{})
	go func() {
		defer close(s.startupDone)
		_ = s.sniffAllWithForce(s.startupCtx, false) // auto: respect per-tool TTL
	}()
}

// intervalElapsed reports whether the interval strategy's TTL has expired
// (no cache / unreadable cache / missing LastSniffAt / LastSniffAt older than
// cfg.Interval → true).  onStartupInterval and StartupSniffPending share this
// single judgement so the predicate always equals the real spawn condition.
func (s *Scheduler) intervalElapsed() bool {
	cache, err := s.LoadCache()
	if err != nil || cache == nil {
		// Failed to load cache — proceed with sniffing (no log).
		return true
	}
	if cache.LastSniffAt.IsZero() {
		return true
	}
	interval, err := time.ParseDuration(s.cfg.Interval)
	if err != nil || interval <= 0 {
		interval = 24 * time.Hour
	}
	return time.Since(cache.LastSniffAt) >= interval
}

// StartupSniffPending reports whether OnStartup would launch a background
// sniff for the configured strategy (票 34 / P1-30 / E-11).
//
// interval 策略的 TTL 到期条件必须计入（与 onStartupInterval 同一判定
// intervalElapsed），否则 cli.HasStartupSniff / main.go 的等待条件不完整：
// 缓存新鲜时不 spawn 就不该等待。strategy=manual/service 不自动触发。
func (s *Scheduler) StartupSniffPending() bool {
	switch Strategy(s.cfg.Strategy) {
	case StrategyStartup:
		return true
	case StrategyInterval:
		return s.intervalElapsed()
	default:
		return false
	}
}

func (s *Scheduler) onStartupStartup(ctx context.Context) {
	s.startupCtx, s.startupCancel = context.WithCancel(ctx)
	s.startupDone = make(chan struct{})
	go func() {
		defer close(s.startupDone)
		_ = s.sniffAllWithForce(s.startupCtx, false) // auto: respect per-tool TTL
	}()
}

// StartupSniffSpawned reports whether OnStartup actually launched a background
// sniff in this process (票 34 / P1-30 / E-11).  Kept for diagnostics/tests:
// the interval strategy's TTL condition is inherent here because
// onStartupInterval only sets startupCancel when the TTL has expired.
// cli.HasStartupSniff uses the pure predicate StartupSniffPending instead
// (it must be evaluated before Shutdown rewrites the cache).
func (s *Scheduler) StartupSniffSpawned() bool {
	return s.startupCancel != nil
}

// Shutdown cancels the startup background sniff and waits up to 2 seconds
// for partial results to be saved.  Call from the process shutdown hook
// (SC-002).
func (s *Scheduler) Shutdown() {
	if s.startupCancel == nil {
		return
	}
	// 第七轮审计 F-55：后台嗅探可能在进程收到退出信号**之前就已经正常跑完**——
	// 那种情况下 sniffAllWithForce 已按 ctx.Err() 判定过 Partial 并保存了缓存
	//（见本文件 sniffAllWithForce 的 `Partial: ctx.Err() != nil` 与随后的
	// SaveCache）。旧代码在这里无条件把 Partial 覆写为 true，把一次**完整**的
	// 嗅探错标成 partial，使 `xdev sniff` 摘要无条件多打一个 partial，误导排障。
	// 因此先非阻塞探测完成信号：已完成则只释放 context，不再回写缓存。
	select {
	case <-s.startupDone:
		// 已完成：Partial 已由 sniffAllWithForce 按 ctx.Err() 决定并落盘。
		s.startupCancel()
		return
	default:
	}

	s.startupCancel()
	select {
	case <-s.startupDone:
		// Normal completion after cancellation.
	case <-time.After(2 * time.Second):
		log.Printf("[discovery] shutdown: timeout waiting for background sniff")
	}
	// Flush partial results to cache. 只有确实被中断的嗅探才走到这里。
	if cache, err := s.LoadCache(); err == nil && cache != nil {
		cache.Partial = true
		cache.LastSniffAt = time.Now()
		_ = s.SaveCache(cache)
	}
}

// SniffNow discovers new versions for a single tool, merges them into
// the manifest, and updates the sniff cache (FR-007).
//
// Transient network errors are retried with exponential backoff
// (1s/2s/4s — NFR-004).  On 403 rate-limit errors, the reset_at is
// persisted to cache so the tool is skipped until reset.
//
// force bypasses the per-tool TTL skip so manual `xdev sniff` always
// refreshes (SC-007): the cached result is ignored and the remote is
// contacted regardless of how recently the tool was last sniffed.
func (s *Scheduler) SniffNow(ctx context.Context, tool string) error {
	return s.sniffNowInternal(ctx, tool, true)
}

// sniffNowInternal is the shared implementation.
//
//   - force=false (automatic OnStartup triggers): per-tool TTL skip applies
//     AND the internal token-bucket rate limiter is checked.  This prevents
//     background sniffing from exhausting the token bucket and starving
//     subsequent manual or automatic operations.
//   - force=true (manual `xdev sniff --all` or `xdev sniff <tool>`): the TTL
//     skip is bypassed AND the internal rate limiter is NOT checked.  This
//     ensures the user can always force a refresh even if the internal bucket
//     is temporarily empty (e.g. after a full SniffAll consumed all 300
//     tokens).  The remote GitHub API rate limit is still enforced server-side.
func (s *Scheduler) sniffNowInternal(ctx context.Context, tool string, force bool) error {
	cat, err := s.catalog()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	t, ok := cat.Tool(tool)
	if !ok {
		return fmt.Errorf("tool %q not found in catalog", tool)
	}
	return s.sniffNowTool(ctx, t, force)
}

func (s *Scheduler) catalog() (*manifest.Catalog, error) {
	load := s.loadCatalog
	if load == nil {
		load = manifest.LoadCatalog
	}
	return load(s.manifestDir)
}

// sniffNowTool sniffs one already-loaded tool. SniffAll must pass the
// snapshot *Tool so the catalog is not re-parsed per worker (Spec 030).
func (s *Scheduler) sniffNowTool(ctx context.Context, t *manifest.Tool, force bool) error {
	if t == nil {
		return fmt.Errorf("nil tool")
	}
	if !force && !s.limiter.acquire() {
		return fmt.Errorf("rate limit exceeded for %q — try again later", t.Name)
	}

	name := t.Name
	if !t.ShouldAutoSniff() {
		log.Printf("[discovery] sniff %q: auto_sniff disabled, skipping", name)
		return nil
	}

	// TTL check: skip if the tool was sniffed recently.  This is a
	// TTL-based alternative to full ETag persistence (SC-007) and only
	// applies to automatic (non-forced) triggers so that manual
	// `xdev sniff` always refreshes.
	if !force {
		existingCache, _ := s.LoadCache()
		if existingCache != nil {
			if r, ok := existingCache.Tools[name]; ok && !r.LastSniff.IsZero() {
				interval, err := time.ParseDuration(s.cfg.Interval)
				if err != nil || interval <= 0 {
					interval = 24 * time.Hour
				}
				if time.Since(r.LastSniff) < interval {
					log.Printf("[discovery] sniff %q: skipped (last sniff %s, interval %s)",
						name, r.LastSniff.Format(time.RFC3339), s.cfg.Interval)
					return nil
				}
			}
		}
	}

	log.Printf("[discovery] sniff %q: starting...", name)
	start := time.Now()

	// Exponential backoff loop.
	var versions []DiscoveredVersion
	var sniffErr error
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(backoff); attempt++ {
		if attempt > 0 {
			log.Printf("[discovery] sniff %q: retry attempt %d/%d after %v",
				name, attempt, len(backoff), backoff[attempt-1])
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff[attempt-1]):
			}
		}

		versions, sniffErr = s.registry.Sniff(ctx, t)
		if sniffErr == nil {
			break
		}

		errStr := sniffErr.Error()
		if strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "403") {
			log.Printf("[discovery] sniff %q: rate limited, persisting to cache", name)
			s.mu.Lock()
			_ = s.updateCacheEntryLocked(name, ToolSniffResult{
				LastSniff: time.Now(),
				Error:     sniffErr.Error(),
			})
			// 第七轮审计 F-53：把限流**窗口**写进缓存。旧实现只写 Remaining，
			// RateLimitState.ResetAt 全仓从未被赋值（只有读取点），于是
			// specs/011 FR-007 承诺的「记录 reset_at、到 reset 前不再重试」从未生效：
			// 下一个进程读到 resetAt.IsZero() → 走「无活跃窗口」救援分支保留满桶，
			// 令牌桶限额无法跨进程累计（重启即重置），仅靠服务端 60/h 兜底。
			// 这里按 GitHub 的 X-RateLimit-Reset 语义保守地写一个窗口：
			// 无法拿到精确 reset 时用 1 小时（未认证配额的重置周期）。
			_ = s.markRateLimitedLocked()
			s.mu.Unlock()
			return sniffErr
		}

		if !isTransientError(sniffErr) {
			break
		}
		if attempt >= len(backoff) {
			break
		}
	}

	elapsed := time.Since(start)

	// Build the result entry for the cache.
	result := ToolSniffResult{
		LastSniff: time.Now(),
	}

	if sniffErr != nil {
		log.Printf("[discovery] sniff %q: error after %v: %v", name, elapsed, sniffErr)
		result.Error = sniffErr.Error()
		s.mu.Lock()
		_ = s.updateCacheEntryLocked(name, result)
		s.mu.Unlock()
		return sniffErr
	}

	if versions == nil {
		log.Printf("[discovery] sniff %q: unsupported (no matching sniffer)", name)
		result.Unsupported = true
		s.mu.Lock()
		_ = s.updateCacheEntryLocked(name, result)
		s.mu.Unlock()
		return nil
	}

	// Apply auto_archive_fill: if enabled and versions have no archives,
	// generate placeholder archives from the tool's existing version
	// template (FR-010).  If disabled, strip any archives the sniffer
	// may have generated.
	if s.cfg.AutoArchiveFill {
		for i, dv := range versions {
			if len(dv.Archives) == 0 {
				// Try to build archive template from existing versions.
				template, ok := buildArchiveTemplateFromTool(t)
				if ok {
					versions[i].Archives = FillArchivePlaceholders(t, dv.Version, template)
				}
			}
		}
	} else {
		for i := range versions {
			versions[i].Archives = nil
		}
	}

	for _, dv := range versions {
		result.Versions = append(result.Versions, dv.Version)
	}

	log.Printf("[discovery] sniff %q: found %d new version(s) in %v", name, len(versions), elapsed)

	if err := mergeIntoManifest(t, versions, s.manifestDir); err != nil {
		log.Printf("[discovery] sniff %q: merge into manifest failed: %v", name, err)
		result.Error = err.Error()
		s.mu.Lock()
		_ = s.updateCacheEntryLocked(name, result)
		s.mu.Unlock()
		return err
	}

	// Update cache with successful result (ETag from sniff response).
	s.mu.Lock()
	_ = s.updateCacheEntryLocked(name, result)
	s.mu.Unlock()
	return nil
}

// isTransientError returns true for errors that are safe to retry.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	// Network timeouts and connection errors are transient.
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "connection") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "reset by peer") ||
		strings.Contains(msg, "no such host") {
		return true
	}

	// Match remote.go error format: "API returned 500: ..." / "returned 503: ..."
	// Also "status: 502" from various HTTP responses.
	if strings.Contains(msg, "returned 5") || strings.Contains(msg, "status: 5") {
		// Verify it's actually a 5xx status code (500-599).
		for _, prefix := range []string{"returned 5", "status: 5"} {
			if idx := strings.Index(msg, prefix); idx >= 0 {
				candidate := msg[idx+len(prefix)]
				if candidate >= '0' && candidate <= '9' {
					return true
				}
			}
		}
	}
	return false
}

// SniffAll discovers new versions for every tool in the catalog that
// has AutoSniff enabled (FR-007).  Concurrency is capped by the config's
// Concurrency setting.
//
// Results are collected via channels to avoid concurrent cache writes:
// each goroutine sends its result back, and the main goroutine does a
// single SaveCache at the end (NFR-004).
//
// If the context is cancelled (e.g. process exit with startup strategy),
// partial results are saved to the cache with Partial: true.
// SniffAll discovers new versions for every tool in the catalog that
// has AutoSniff enabled (FR-007). This is the manual entry point: it
// forces a refresh, bypassing the per-tool TTL skip so that
// `xdev sniff --all` always contacts the remote server.
func (s *Scheduler) SniffAll(ctx context.Context) error {
	return s.sniffAllWithForce(ctx, true)
}

// sniffAllWithForce is the shared implementation. When force is false
// (automatic OnStartup triggers), per-tool TTL skip applies so recently
// sniffed tools are not re-queried (SC-007). When force is true
// (manual `xdev sniff`), the TTL skip is bypassed.
func (s *Scheduler) sniffAllWithForce(ctx context.Context, force bool) error {
	cat, err := s.catalog()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}

	tools := cat.Tools()
	log.Printf("[discovery] SniffAll: checking %d tools (concurrency=%d, force=%v)", len(tools), s.cfg.Concurrency, force)

	type toolResult struct {
		name   string
		result ToolSniffResult
		err    error
	}

	sem := make(chan struct{}, s.cfg.Concurrency)
	results := make(chan toolResult, len(tools))
	var wg sync.WaitGroup

	// 后台自动触发（force=false）时尊重 GUI 忽略清单（kind=update）：
	// 用户已忽略更新的工具不再被自动嗅探打扰（不 merge、不写缓存）。
	// 手动触发（force=true，xdev sniff / GUI 全部嗅探）保持显式语义。
	ignored := map[string]bool{}
	if !force {
		ignored = util.LoadIgnoredUpdateNames(filepath.Dir(s.cachePath))
	}

	for _, t := range tools {
		if ctx.Err() != nil {
			break
		}
		if !t.ShouldAutoSniff() {
			continue
		}
		if ignored[t.Name] {
			log.Printf("[discovery] sniff %q: skipped (user ignored updates for this tool)", t.Name)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(tool *manifest.Tool) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			sniffErr := s.sniffNowTool(ctx, tool, force)
			// Re-read cache to get the result we just wrote.
			cache, _ := s.LoadCache()
			tr := ToolSniffResult{LastSniff: time.Now()}
			if cache != nil {
				if r, ok := cache.Tools[tool.Name]; ok {
					tr = r
				}
			}
			if sniffErr != nil {
				tr.Error = sniffErr.Error()
			}
			results <- toolResult{name: tool.Name, result: tr, err: sniffErr}
		}(t)
	}
	wg.Wait()
	close(results)

	// Build merged cache: load existing, overlay this round's results.
	// This preserves entries for tools that were not sniffed this round
	// and avoids losing results from concurrent processes (NFR-004).
	existingCache, _ := s.LoadCache()
	newCache := &SniffCache{
		Tools:       make(map[string]ToolSniffResult),
		RateLimit:   RateLimitState{Remaining: s.limiter.available()},
		LastSniffAt: time.Now(),
		Partial:     ctx.Err() != nil,
	}
	if existingCache != nil {
		for k, v := range existingCache.Tools {
			newCache.Tools[k] = v
		}
		if !existingCache.LastSniffAt.IsZero() {
			// Preserve stale flag from previous run.
			newCache.Stale = existingCache.Stale
		}
	}

	// Mark tools that timed out as stale.
	if ctx.Err() != nil {
		newCache.Stale = true
	}

	// Overlay this round's results.
	var errs []string
	for r := range results {
		newCache.Tools[r.name] = r.result
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.name, r.err))
		}
	}

	// Single atomic save.
	s.mu.Lock()
	if saveErr := s.SaveCache(newCache); saveErr != nil {
		log.Printf("[discovery] SniffAll: save cache failed: %v", saveErr)
	}
	s.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("sniff errors: %s", strings.Join(errs, "; "))
	}
	return ctx.Err()
}

// buildArchiveTemplateFromTool scans a tool's existing versions for one
// with archives that can serve as a URL template, and returns the template.
func buildArchiveTemplateFromTool(t *manifest.Tool) ([]manifest.Archive, bool) {
	for _, v := range t.VersionList {
		if len(v.Archives) > 0 {
			return buildArchiveTemplate(v.Version, v.Archives)
		}
	}
	return nil, false
}

// rateLimitWindow 是拿不到服务端 X-RateLimit-Reset 时的保守限流窗口
// （GitHub 未认证配额按小时重置）。第七轮审计 F-53。
const rateLimitWindow = time.Hour

// updateCacheEntryLocked upserts a tool entry in the cache.
// Caller must hold s.mu.
//
// 第七轮审计 F-56：读-改-写必须整体落在**同一把独占锁**内。旧实现是
// `LoadCache`（只取**共享**锁，50ms 非阻塞）→ 改内存 → `SaveCache`（此时才取
// 独占锁），两把锁之间没有覆盖整个 RMW 周期。于是两个 xdev 进程在同一 home 上
// 并发嗅探（常见：GUI 启动自检嗅探与手动 `xdev sniff --all` 重叠）会各自基于同一
// 份快照写回，**后写者覆盖前者的工具条目**（文件本身由 AtomicWriteFile 保证不撕裂，
// 丢的是更新）。这里改为在独占锁内完成「读→合并→写」。
func (s *Scheduler) updateCacheEntryLocked(tool string, result ToolSniffResult) error {
	return s.withCacheLock(func(cache *SniffCache) error {
		if cache.Tools == nil {
			cache.Tools = make(map[string]ToolSniffResult)
		}
		cache.Tools[tool] = result
		return nil
	})
}

// withCacheLock 在**同一把独占文件锁**内执行一次完整的「读 → 改 → 写」缓存事务
// （第七轮审计 F-56）。mutate 收到的 cache 保证非 nil；锁不可得时降级为「读现有
// 缓存 + 改 + 写」的无锁路径（与既有 SaveCache 的降级语义一致：拿不到锁不应让
// 嗅探整体失败）。
func (s *Scheduler) withCacheLock(mutate func(*SniffCache) error) error {
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	lockPath := s.cachePath + ".lock"
	lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
	if lockErr == nil {
		defer lock.Release()
	}

	// 锁内读取（绕过 LoadCache 的共享锁，避免同进程内两把锁互斥）。
	cache := s.readCacheUnlocked()
	if cache == nil {
		cache = &SniffCache{Tools: make(map[string]ToolSniffResult)}
	}
	if cache.Tools == nil {
		cache.Tools = make(map[string]ToolSniffResult)
	}
	if err := mutate(cache); err != nil {
		return err
	}
	// 注意：mutate 可能已显式设定 Remaining（限流时置 0）。只有当 mutate 没有把
	// Remaining 归零时才用本地令牌桶刷新它，否则会把「已限流」状态覆盖回满桶
	//（F-53 回归：窗口写进去了但剩余数被重置，跨进程限流仍失效）。
	if cache.RateLimit.Remaining != 0 {
		cache.RateLimit.Remaining = s.limiter.available()
	}
	return s.writeCacheUnlocked(cache)
}

// readCacheUnlocked 读缓存但不取任何文件锁（调用方须已持有独占锁）。
func (s *Scheduler) readCacheUnlocked() *SniffCache {
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		return nil
	}
	var c SniffCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

// writeCacheUnlocked 原子写缓存但不取任何文件锁（调用方须已持有独占锁）。
func (s *Scheduler) writeCacheUnlocked(c *SniffCache) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := util.AtomicWriteFile(s.cachePath, data, 0o644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

// markRateLimitedLocked 记录一次限流：把 Remaining 归零并写入 ResetAt，
// 使限流窗口能**跨进程**生效（第七轮审计 F-53）。Caller must hold s.mu.
func (s *Scheduler) markRateLimitedLocked() error {
	return s.withCacheLock(func(cache *SniffCache) error {
		cache.RateLimit.Remaining = 0
		cache.RateLimit.ResetAt = time.Now().Add(rateLimitWindow)
		return nil
	})
}

// LoadCache reads and parses the sniff cache from disk (FR-008).
// It acquires a shared file lock to coordinate with concurrent processes
// (NFR-004).
// If the file does not exist or is corrupt, an empty cache is returned
// (the caller can re-sniff to populate it).
func (s *Scheduler) LoadCache() (*SniffCache, error) {
	lockPath := s.cachePath + ".lock"
	// Acquire shared (read) lock with 0 timeout = non-blocking.
	lock, lockErr := util.AcquireSharedFileLock(lockPath, 50*time.Millisecond)
	if lockErr == nil {
		defer lock.Release()
	}

	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &SniffCache{
				Tools: make(map[string]ToolSniffResult),
			}, nil
		}
		log.Printf("[discovery] load cache: %v — returning empty cache", err)
		return &SniffCache{
			Tools: make(map[string]ToolSniffResult),
		}, nil
	}

	var cache SniffCache
	if err := json.Unmarshal(data, &cache); err != nil {
		log.Printf("[discovery] load cache: parse error %v — returning empty cache", err)
		return &SniffCache{
			Tools: make(map[string]ToolSniffResult),
		}, nil
	}
	if cache.Tools == nil {
		cache.Tools = make(map[string]ToolSniffResult)
	}
	return &cache, nil
}

// SaveCache writes the sniff cache to disk using an atomic write
// (FR-008, NFR-004).  It acquires an exclusive file lock to coordinate
// with concurrent processes.
func (s *Scheduler) SaveCache(c *SniffCache) error {
	if c.Tools == nil {
		c.Tools = make(map[string]ToolSniffResult)
	}
	c.RateLimit.Remaining = s.limiter.available()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	// Acquire exclusive (write) lock — blocks up to 5 seconds.
	lockPath := s.cachePath + ".lock"
	lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
	if lockErr == nil {
		defer lock.Release()
	}

	if err := util.AtomicWriteFile(s.cachePath, data, 0o644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// rateLimiter
// ---------------------------------------------------------------------------

type rateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	refillDur  time.Duration
	lastRefill time.Time
}

func newRateLimiter(ratePerHour int) *rateLimiter {
	if ratePerHour <= 0 {
		ratePerHour = 60
	}
	return &rateLimiter{
		tokens:     ratePerHour,
		maxTokens:  ratePerHour,
		refillDur:  time.Hour / time.Duration(ratePerHour),
		lastRefill: time.Now(),
	}
}

func (rl *rateLimiter) acquire() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	if rl.tokens <= 0 {
		return false
	}
	rl.tokens--
	return true
}

func (rl *rateLimiter) available() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	return rl.tokens
}

// setTokens overrides the current token count (used when restoring
// from cache on Scheduler creation).  It resets lastRefill to now
// so the next refill() computes from a correct baseline.
func (rl *rateLimiter) setTokens(n int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if n > rl.maxTokens {
		n = rl.maxTokens
	}
	rl.tokens = n
	rl.lastRefill = time.Now()
}

func (rl *rateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	if elapsed < rl.refillDur {
		return
	}
	earned := int(elapsed / rl.refillDur)
	if earned > 0 {
		rl.tokens += earned
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = rl.lastRefill.Add(time.Duration(earned) * rl.refillDur)
	}
}

// ---------------------------------------------------------------------------
// mergeIntoManifest
// ---------------------------------------------------------------------------

func mergeIntoManifest(t *manifest.Tool, discovered []DiscoveredVersion, manifestDir string) error {
	if len(discovered) == 0 {
		return nil
	}

	path := filepath.Join(manifestDir, t.Name+".yaml")

	// Ticket 23 (audit-fixes / D-03): the whole read-modify-write cycle runs
	// under a cross-process file lock. The sniff scheduler (this process)
	// and a TOFU write-back from another xdev process (installer →
	// manifest.UpdateArchiveSHA256) used to race the same YAML: one side's
	// newly appended versions or hashes were silently lost.
	//
	// The lock is stale-safe by construction: AcquireFileLock uses OS
	// byte-range locks (LockFileEx on Windows, flock on Unix), which the
	// kernel releases when the holder dies — a leftover .lock file never
	// blocks a later process (inventory conclusion reused by ticket 21's
	// .part.lock). Lock order is a single edge here (no nesting with the
	// sniff-cache lock: merge runs first, updateCacheEntry afterwards).
	lockPath := path + ".lock"
	lock, lockErr := util.AcquireFileLock(lockPath, 30*time.Second)
	if lockErr != nil {
		return fmt.Errorf("manifest lock %s: %w", lockPath, lockErr)
	}
	defer lock.Release()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Manifest doesn't exist on disk yet.  Try to export
			// from the in-memory catalog (which may have been loaded
			// from embedded manifests) so we have a base to merge
			// into (R3).
			if data2, marshalErr := manifest.MarshalTool(t); marshalErr == nil {
				data = data2
				// Write the base manifest so it exists on disk.
				// Ticket 23: atomic write + logged errors (the old
				// `_ =` discards hid a failed baseline write).
				if err := os.MkdirAll(manifestDir, 0o755); err != nil {
					log.Printf("[discovery] warning: failed to create manifest dir %s: %v", manifestDir, err)
				}
				if err := util.AtomicWriteFile(path, data, 0o644); err != nil {
					log.Printf("[discovery] warning: failed to write baseline manifest %s: %v", path, err)
				}
			} else {
				return fmt.Errorf("marshal base manifest %s: %w", path, marshalErr)
			}
		} else {
			return fmt.Errorf("read manifest %s: %w", path, err)
		}
	}

	var existing manifest.Tool
	if err := yaml.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("parse manifest %s: %w", path, err)
	}

	existingVersions := make(map[string]bool, len(existing.VersionList))
	for _, v := range existing.VersionList {
		existingVersions[v.Version] = true
	}

	appended := 0
	patched := 0
	for _, dv := range discovered {
		if existingVersions[dv.Version] {
			// 已存在版本：若旧条目因历史嗅探（模板不可用等）缺少
			// archives 而本次带 archives，则补填（upsert），不再强制重嗅。
			if len(dv.Archives) > 0 {
				for i := range existing.VersionList {
					if existing.VersionList[i].Version == dv.Version && len(existing.VersionList[i].Archives) == 0 {
						existing.VersionList[i].Archives = dv.Archives
						patched++
						break
					}
				}
			}
			continue
		}

		// Spec 039: 稳定版本串 → stable: true；预发布（-rc.N/-alpha.N 等）→
		// stable: false（GUI 徽标区分预发行通道）。
		existing.VersionList = append(existing.VersionList, manifest.Version{
			Version:    dv.Version,
			Stable:     !util.IsPrereleaseVersion(dv.Version),
			Release:    dv.ReleaseDate,
			Archives:   dv.Archives,
			Discovered: true,
			Unverified: dv.Unverified,
		})
		existingVersions[dv.Version] = true
		appended++
	}

	if appended > 0 {
		manifest.SortVersionsByRelease(existing.VersionList)
	}

	// Phase 2B: auto-update latest from discovered versions.
	//
	// Spec 039 prerelease rules（dsh 频繁更新 + alpha 通道）:
	//   - a pre-release candidate never hijacks a STABLE latest (npm
	//     dist-tags semantics: alphas live on their own channel — a tool
	//     that released 1.0.0 stable keeps latest=1.0.0 even after
	//     1.1.0-alpha.1 is discovered);
	//   - a pre-release candidate only advances a pre-release latest of
	//     the SAME channel: rc → rc promotions work (dsh's npm
	//     dist-tags.latest is itself an rc, and IsVersionNewer
	//     false-negatives every "-" string, which froze such streams),
	//     but the alpha channel (dsh: alpha=0.1.5-alpha.1 > latest
	//     0.1.2-rc.1) never hijacks an rc-latest;
	//   - comparisons are prerelease-aware (SemVer 2.0.0);
	//   - non-semver keys ("r29") parse to 0.0.0 and never promote —
	//     same conservative skip as before.
	latestUpdated := false
	latestIsPre := util.IsPrereleaseVersion(existing.Latest)
	latestChannel := util.PrereleaseChannel(existing.Latest)
	for _, dv := range discovered {
		if existing.Latest != "" && util.IsPrereleaseVersion(dv.Version) {
			if !latestIsPre {
				continue // stable latest is never hijacked by a pre-release
			}
			if util.PrereleaseChannel(dv.Version) != latestChannel {
				continue // different pre-release channel (alpha vs rc) never promotes
			}
		}
		if util.CompareSemver(dv.Version, existing.Latest) > 0 {
			existing.Latest = dv.Version
			latestUpdated = true
			latestIsPre = util.IsPrereleaseVersion(existing.Latest)
			latestChannel = util.PrereleaseChannel(existing.Latest)
		}
	}

	if appended == 0 && patched == 0 && !latestUpdated {
		return nil
	}

	if appended > 0 {
		log.Printf("[discovery] merge: added %d new version(s) to %s", appended, t.Name)
	}
	if patched > 0 {
		log.Printf("[discovery] merge: filled archives for %d existing version(s) in %s", patched, t.Name)
	}
	if latestUpdated {
		log.Printf("[discovery] merge: updated latest to %s for %s", existing.Latest, t.Name)
	}

	bakPath := path + ".bak"
	// Ticket 23: the backup write is atomic too — a torn .bak is worse than
	// no backup (it would look like a valid restore source).
	if err := util.AtomicWriteFile(bakPath, data, 0o644); err != nil {
		log.Printf("[discovery] warning: failed to write backup %s: %v", bakPath, err)
	}

	newData, err := yaml.Marshal(&existing)
	if err != nil {
		return fmt.Errorf("marshal manifest %s: %w", path, err)
	}

	if err := util.AtomicWriteFile(path, newData, 0o644); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}

	return nil
}

var _ = (Strategy)(StrategyInterval)
var _ = (Strategy)(StrategyStartup)
var _ = (Strategy)(StrategyService)
var _ = (Strategy)(StrategyManual)
