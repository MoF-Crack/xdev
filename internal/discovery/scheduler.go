package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	startupRound  atomic.Pointer[time.Time]
	startupSince  time.Time
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
		if s.StartupSniffPending() {
			s.onStartupStartup(ctx)
		}
	case StrategyService, StrategyManual:
		// Nothing — only load cache is needed, handled by caller.
	}
}

func (s *Scheduler) onStartupInterval(ctx context.Context) {
	if !s.intervalElapsed() {
		return
	}

	s.startupSince = time.Now()
	s.startupCtx, s.startupCancel = context.WithTimeout(ctx, 120*time.Second)
	s.startupDone = make(chan struct{})
	go func() {
		defer close(s.startupDone)
		_ = s.runBatch(s.startupCtx, false, true) // auto: respect per-tool TTL
	}()
}

// intervalElapsed shares the real spawn predicate with StartupSniffPending.
func (s *Scheduler) intervalElapsed() bool { return s.startupDue(false) }

// StartupSniffPending reports whether OnStartup would launch a background
// sniff for the configured strategy (票 34 / P1-30 / E-11).
//
// interval 策略的 TTL 到期条件必须计入（与 onStartupInterval 同一判定
// intervalElapsed），否则 cli.HasStartupSniff / main.go 的等待条件不完整：
// 缓存新鲜时不 spawn 就不该等待。strategy=manual/service 不自动触发。
func (s *Scheduler) StartupSniffPending() bool {
	switch Strategy(s.cfg.Strategy) {
	case StrategyStartup:
		return s.startupDue(true)
	case StrategyInterval:
		return s.intervalElapsed()
	default:
		return false
	}
}

func (s *Scheduler) onStartupStartup(ctx context.Context) {
	s.startupSince = time.Now()
	s.startupCtx, s.startupCancel = context.WithCancel(ctx)
	s.startupDone = make(chan struct{})
	go func() {
		defer close(s.startupDone)
		_ = s.runBatch(s.startupCtx, false, true) // auto: respect per-tool TTL
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

// errShutdownFallbackSkipped 是 Shutdown 兜底事务的内部控制流信号：嗅探在我们
// 拿到独占锁之前已经自行收尾并落盘，兜底**不得**回写（Round 8 A-04 契约）。
// 它绝不会逃出 Shutdown。
var errShutdownFallbackSkipped = errors.New("shutdown fallback skipped: sniff already completed")

// Shutdown cancels the startup background sniff and waits up to 2 seconds
// for partial results to be saved.  Call from the process shutdown hook
// (SC-002).
//
// 第十轮审计 P2-1：返回值报告**兜底落盘是否成功**。旧实现把错误吞掉
// （`_ = s.SaveCache(cache)`），缓存损坏时既写坏了文件又对外报成功。现在读失败
// /写失败一律返回错误（缓存文件保持原样，F-56），调用方可以据此判断。
// 无兜底需求（嗅探已完成、或从未 spawn）时返回 nil。
func (s *Scheduler) Shutdown() error {
	if s.startupCancel == nil {
		return nil
	}
	// 第七轮审计 F-55 + 第八轮审计 A-04：后台嗅探**自己**就是 Partial 的权威判定者
	// —— sniffAllWithForce 在结尾按 `ctx.Err() != nil` 算出 Partial 并原子落盘
	//（见本文件的 `Partial: ctx.Err() != nil` 与随后的 SaveCache）。
	//
	// F-55 的初版修复只堵住了「进入 Shutdown 前就已完成」这一种时序；若嗅探在
	// 第一个 select 与第二个 select 之间完成（第八轮用确定性交错复现：
	// `Partial=true … Tools=1`，而那次嗅探其实已把活干完），第二个 select 仍会走
	// `<-s.startupDone` 分支，然后**无条件**把 Partial 覆写成 true —— 同一个
	// 「完整嗅探被错标 partial」缺陷只是窗口变窄了。
	//
	// 正确语义：只要 startupDone 关闭（无论何时），嗅探就已自行保存过正确结果，
	// Shutdown **绝不**回写；只有等待超时（嗅探仍在飞行、没机会保存）时，
	// Shutdown 才作为兜底写一次 Partial=true。
	select {
	case <-s.startupDone:
		s.startupCancel()
		return nil
	default:
	}

	s.startupCancel()
	select {
	case <-s.startupDone:
		// 嗅探已收尾并自行保存（Partial 由它按 ctx.Err() 决定）——不要回写。
		return nil
	case <-time.After(2 * time.Second):
		log.Printf("[discovery] shutdown: timeout waiting for background sniff")
	}
	// 兜底：嗅探仍在飞行，没能保存——把它已产出的部分结果标为 partial 落盘。
	//
	// 第十轮审计 P2-1：兜底必须是**锁内 RMW**。旧实现是
	// LoadCache()（共享锁 50ms 拿不到就无锁读）→ 改内存 → SaveCache()（此时才取
	// 独占锁、且先 marshal 后取锁）——「读 → marshal → 写」跨了两把锁，在两者
	// 之间由并发写者落地的条目会被这份过时快照整份覆盖（lost update，实测
	// `concurrent_entry_survived=false`）。同一路径还把损坏缓存替换成空缓存
	//（LoadCache 对损坏文件返回「空缓存 + nil」），违背 F-56「读失败绝不覆写」。
	//
	// 现在整个兜底交给 withCacheLock：锁内重新读取 → 只改未完成状态与退避
	// → 写回。**不读全量快照、不整份写回**，因此并发条目不会被覆盖；读失败
	//（损坏/不可读）时 withCacheLock 直接返回错误、原文件保持原样。
	//
	// 提交窗口（Round 8 A-04 契约的第三种时序）：若嗅探在 2s 超时**之后**、我们
	// 拿到独占锁之前完成并自行落盘（它会先保存、再 close(startupDone)），锁内
	// 复查 startupDone 就能确定性识别出「刚好完成」——此时兜底必须放弃回写，
	// 不能把一个完整嗅探错标为 partial。用 startupDone 而不是时间戳启发式：
	// 关闭信号是「后台嗅探已收尾」的**精确**判据，而时间戳会把并发写者刚落地的
	// 条目误判成嗅探完成（那正是 P2-1 要修的 lost update）。
	err := s.withCacheLock(func(cache *SniffCache) error {
		select {
		case <-s.startupDone:
			return errShutdownFallbackSkipped
		default:
		}
		startupRound := s.startupRound.Load()
		if startupRound != nil && (!cache.LastAttempt.Equal(*startupRound) || cache.Status != SniffRunning) {
			return errShutdownFallbackSkipped
		}
		if startupRound == nil && !s.startupSince.IsZero() && cache.LastAttempt.After(s.startupSince) {
			return errShutdownFallbackSkipped
		}
		cache.Partial = true
		cache.Stale = true
		cache.Status = SniffCanceled
		if startupRound == nil {
			cache.PendingKnown = false
		}
		cache.RetryAfter = time.Now().Add(canceledRetryDelay)
		return nil
	})
	if errors.Is(err, errShutdownFallbackSkipped) {
		return nil
	}
	if err != nil {
		log.Printf("[discovery] shutdown: fallback cache update failed (cache left untouched): %v", err)
		return err
	}
	return nil
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
	if !force && util.LoadIgnoredUpdateNames(filepath.Dir(s.cachePath))[tool] {
		return nil
	}
	return s.sniffTool(ctx, t, force, time.Time{}).err
}

func (s *Scheduler) catalog() (*manifest.Catalog, error) {
	load := s.loadCatalog
	if load == nil {
		load = manifest.LoadCatalog
	}
	return load(s.manifestDir)
}

// SetStartupStateForTest 手工搭建「后台嗅探仍在飞行」的状态，供跨包守卫使用。
//
// 为什么需要它：`Shutdown` 的**第一个** select 在 `startupDone` 已关闭时直接
// `return nil`（「嗅探已自行落盘，无需回写」）。因此想确定性地驱动**兜底写盘**分支，
// 必须构造出「startupDone 尚未关闭」的飞行中状态 —— 而 `startupDone`/`startupCancel`
// 是包内私有字段，`internal/cli` 的守卫无法直接赋值。
//
// 与 Scheduler 包内既有守卫 `shutdown_f55_a04_test.go` 的做法一致（那边直接在包内
// 赋值），这里只是把同一构造开放给跨包测试。**仅测试使用**：生产代码不得调用。
func (s *Scheduler) SetStartupStateForTest(ctx context.Context, cancel context.CancelFunc, done chan struct{}) {
	s.startupCtx = ctx
	s.startupCancel = cancel
	s.startupDone = done
}

// mergeVersions prepares archive placeholders and publishes the manifest under its own lock.
func (s *Scheduler) mergeVersions(t *manifest.Tool, versions []DiscoveredVersion) error {
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

	return mergeIntoManifestOpts(t, versions, s.manifestDir, s.cfg.AutoArchiveFill)
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
				if idx+len(prefix) >= len(msg) {
					continue
				}
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
// Workers publish each outcome while holding the cache lock. The final
// transaction closes only the matching round. Manual calls force a refresh.
func (s *Scheduler) SniffAll(ctx context.Context) error {
	return s.sniffAllWithForce(ctx, true)
}

// sniffAllWithForce is the shared implementation. When force is false
// (automatic OnStartup triggers), per-tool TTL skip applies so recently
// sniffed tools are not re-queried (SC-007). When force is true
// (manual `xdev sniff`), the TTL skip is bypassed.
func (s *Scheduler) sniffAllWithForce(ctx context.Context, force bool) error {
	return s.runBatch(ctx, force, false)
}

func (s *Scheduler) runBatch(ctx context.Context, force, startup bool) error {
	cat, err := s.catalog()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	tools, round, err := s.beginRound(cat.Tools(), force, startup)
	if err != nil {
		return err
	}
	jobs := make(chan *manifest.Tool)
	outcomes := make(chan toolOutcome, len(tools))
	var wg sync.WaitGroup
	for i := 0; i < s.cfg.Concurrency && i < len(tools); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tool := range jobs {
				outcomes <- s.sniffTool(ctx, tool, force, round)
			}
		}()
	}
dispatch:
	for _, tool := range tools {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- tool:
		}
	}
	close(jobs)
	wg.Wait()
	close(outcomes)
	var errs []error
	for out := range outcomes {
		if out.err != nil {
			errs = append(errs, fmt.Errorf("%s (%s, %d attempts): %w", out.name, out.action, out.attempts, out.err))
		}
	}
	if err := s.finishRound(round, ctx.Err() != nil); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(append(errs, ctx.Err())...)
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

// rateLimitWindowActive 报告 c 里是否记录着一个**尚未过期**的限流窗口
// （第八轮审计 D1/D1b）。窗口活跃时，缓存中的 Remaining 是权威值，
// 不得被本地令牌桶覆盖。
func (s *Scheduler) rateLimitWindowActive(c *SniffCache) bool {
	if c == nil || c.RateLimit.ResetAt.IsZero() {
		return false
	}
	return time.Now().Before(c.RateLimit.ResetAt)
}

// withCacheLock 在**同一把独占文件锁**内执行一次完整的「读 → 改 → 写」缓存事务
// （第七轮审计 F-56）。mutate 收到的 cache 保证非 nil。
//
// 第十轮审计 P2-1：拿不到独占锁时**不得**降级为无锁 RMW。旧实现是
// `if lockErr == nil { defer lock.Release() }` —— 锁不可得就静默继续，于是
// 「读 → 改 → 写」整段在无锁状态下执行，并发写者的条目会被覆盖（正是 F-56
// 要消灭的 lost update）。明确失败优于静默丢写：这里直接返回错误，**不写盘**。
func (s *Scheduler) withCacheLock(mutate func(*SniffCache) error) error {
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	lockPath := s.cachePath + ".lock"
	lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
	if lockErr != nil {
		return fmt.Errorf("acquire cache lock (refusing unlocked read-modify-write): %w", lockErr)
	}
	defer lock.Release()

	// 锁内读取（绕过 LoadCache 的共享锁，避免同进程内两把锁互斥）。
	//
	// F-56 加固：必须区分「文件不存在」（首次运行，从空缓存开始是正确的）与
	// 「读取/解析失败」（并发写者占用、瞬时共享冲突、JSON 损坏）。旧写法把两者
	// 都当成 nil 并从空缓存开始，然后**照样写回** —— 一次瞬时读失败就会把
	// 整份缓存清空。这类「读失败 → 写空」的覆盖正是 F-56 要消灭的丢更新形态，
	// 因此失败时直接返回错误，绝不写盘。
	cache, err := s.readCacheUnlocked()
	if err != nil {
		return fmt.Errorf("read sniff cache (refusing to overwrite): %w", err)
	}
	if cache == nil {
		cache = &SniffCache{Tools: make(map[string]ToolSniffResult)}
	}
	if cache.Tools == nil {
		cache.Tools = make(map[string]ToolSniffResult)
	}
	if err := mutate(cache); err != nil {
		if errors.Is(err, errCacheUnchanged) {
			return nil
		}
		return err
	}
	// 第八轮审计 D1b：统一用 rateLimitWindowActive 判定，而不是「Remaining != 0」
	// 这种启发式（Remaining 恰为 0 且窗口已过期时，旧写法会拒绝刷新，留下一个
	// 永远为 0 的桶）。窗口活跃 → 保留缓存计数；否则用本地令牌桶刷新。
	if !s.rateLimitWindowActive(cache) && cache.RateLimit.LastRefill.IsZero() {
		cache.RateLimit.Remaining = s.limiter.available()
	}
	cache.SchemaVersion = 2
	return s.writeCacheUnlocked(cache)
}

// readCacheUnlocked 读缓存但不取任何文件锁（调用方须已持有独占锁）。
//
// 返回 (nil, nil) 表示「文件确实不存在」（首次运行）；返回 (nil, err) 表示
// 读到了内容却无法解析/无法读取 —— 调用方**不得**据此写空缓存（F-56 加固）。
func (s *Scheduler) readCacheUnlocked() (*SniffCache, error) {
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c SniffCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("corrupt sniff cache: %w", err)
	}
	normalizeSniffCache(&c)
	return &c, nil
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
	normalizeSniffCache(&cache)
	return &cache, nil
}

// SaveCache writes the sniff cache to disk using an atomic write
// (FR-008, NFR-004).  It acquires an exclusive file lock to coordinate
// with concurrent processes.
//
// 审查 S-01（2026-10-24）：旧实现在抢锁失败时 `if lockErr == nil { defer Release() }`
// —— 锁不可得就静默继续写盘，与同文件 withCacheLock（第十轮 P2-1）的
// 「抢不到锁绝不写盘」策略矛盾。SaveCache 是整份快照覆盖语义，两个进程
// 并发时后写者会整份覆盖前写者（实测：预占锁后调用仍返回 nil 且文件被改写）。
//
// 现改为与 withCacheLock 对齐：锁不可得直接返回错误，**不写盘**。
//
// 注意：生产代码已全部改用 withCacheLock（见 :264/:638/:713/:813），
// 本函数当前仅供测试与兼容保留；新代码请勿使用。
func (s *Scheduler) SaveCache(c *SniffCache) error {
	if c.Tools == nil {
		c.Tools = make(map[string]ToolSniffResult)
	}
	// 第八轮审计 D1b（F-53 的自引入回归）：**不得**在限流窗口活跃期间用本地
	// 令牌桶刷新 Remaining。旧实现无条件 `c.RateLimit.Remaining = s.limiter.available()`
	// ——本地桶在一次 SniffAll 后是满的，于是刚写进去的「已限流（Remaining=0）」
	// 被立刻改回 60，NewScheduler 的「窗口仍活跃 → 保留缓存计数」分支永不生效，
	// 跨进程限流再次失效。只在窗口未活跃时才刷新。
	if !s.rateLimitWindowActive(c) {
		c.RateLimit.Remaining = s.limiter.available()
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	// 审查 S-01：抢不到独占锁即拒绝写盘（与 withCacheLock 同一策略）。
	lockPath := s.cachePath + ".lock"
	lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
	if lockErr != nil {
		return fmt.Errorf("acquire cache lock (refusing unlocked write): %w", lockErr)
	}
	defer lock.Release()

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

// requireArchivesForPromotion 是「晋升 latest 时是否要求候选带 archive」的开关，
// 由 AutoArchiveFill 配置推导。
//
// 为什么用包级变量而不是给 mergeIntoManifest 加参数：
// mergeIntoManifest 有 20 处调用（含大量测试），加参数会制造一次
// 高噪声的机械改动。本仓库的既有 seam 风格就是包级 var（参见
// archiveFillerFn / sniffFn 等），保持一致。
//
// 语义：
//   - 开启（默认）：发现的版本会被补上可用占位 archive，因此「无 archives」
//     是真实缺陷，必须拦。
//   - 关闭：用户明确选择不生成占位，此时所有发现版本都没 archives，
//     仍然强制要求会让 latest 永远推不动，整个嗅探功能失效。
var requireArchivesForPromotion = true

// WithRequireArchivesForPromotion 临时修改开关，返回恢复函数（测试用）。
func WithRequireArchivesForPromotion(v bool) func() {
	prev := requireArchivesForPromotion
	requireArchivesForPromotion = v
	return func() { requireArchivesForPromotion = prev }
}

func mergeIntoManifest(t *manifest.Tool, discovered []DiscoveredVersion, manifestDir string) error {
	return mergeIntoManifestOpts(t, discovered, manifestDir, requireArchivesForPromotion)
}

// mergeIntoManifestOpts 是带显式开关的形式。
//
// requireArchives 语义见 requireArchivesForPromotion 的注释。
// 生产路径（SniffAll）传 s.cfg.AutoArchiveFill；测试与旧调用点
// 走 mergeIntoManifest（默认 true）。
func mergeIntoManifestOpts(t *manifest.Tool, discovered []DiscoveredVersion, manifestDir string, requireArchives bool) error {
	if len(discovered) == 0 {
		return nil
	}

	path, err := manifest.ResolveWritableToolPath(manifestDir, t.Name)
	if err != nil {
		return fmt.Errorf("resolve sniff manifest: %w", err)
	}

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

	selected, err := manifest.ResolveWritableToolPath(manifestDir, t.Name)
	if err != nil {
		return err
	}
	if selected != path {
		return fmt.Errorf("effective manifest changed while waiting for lock: %s -> %s", path, selected)
	}
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
		manifest.SortToolVersionsByRelease(t.Name, existing.VersionList)
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
	//   - registered tool-specific schemes (e.g. NDK r29) use their own
	//     precedence. Unknown versions are excluded from promotion.
	latestUpdated := false
	latestIsPre := util.IsPrereleaseVersion(existing.Latest)
	latestChannel := util.PrereleaseChannel(existing.Latest)
	for _, dv := range discovered {
		// 2026-10-26（架构评估修复）：晋升判据统一到 manifest.CanPromoteLatest。
		//
		// 修复前这里是本文件自己的一套（只比 CompareSemver），
		// 而 cli/refresh.go 是另一套（VersionReleaseNewer 全序），
		// 且两边**都不检查候选是否可安装**。
		//
		// 实测后果：android-ndk 的 latest 从 r29（有 archives）
		// 被嗅探推到 "30"（无 archives 的占位条目），于是
		// `xdev install android-ndk`（默认装 latest）直接失败。
		//
		// allowPrerelease 传入的语义：
		//   - latest 为空 → 允许（首次确定 latest）
		//   - latest 是预发布且候选同一通道 → 允许（rc→rc、alpha→alpha）
		//   - latest 是稳定版 → 不允许（稳定 latest 不被 alpha/rc 劫持）
		allowPre := existing.Latest == "" ||
			(latestIsPre && util.PrereleaseChannel(dv.Version) == latestChannel)

		if !manifest.CanPromoteToolLatestWithOptions(
			t.Name, existing.VersionList, dv.Version, existing.Latest,
			manifest.PromoteOptions{
				AllowPrerelease: allowPre,
				RequireArchives: requireArchives,
			},
		) {
			continue
		}
		existing.Latest = dv.Version
		latestUpdated = true
		latestIsPre = util.IsPrereleaseVersion(existing.Latest)
		latestChannel = util.PrereleaseChannel(existing.Latest)
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
	// A newly introduced higher-precedence source must not turn this write into
	// a successful-looking shadow. Keep the failure visible to the caller.
	selected, err = manifest.ResolveWritableToolPath(manifestDir, t.Name)
	if err != nil {
		return err
	}
	if selected != path {
		return fmt.Errorf("effective manifest changed during merge: %s -> %s", path, selected)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("verify sniff manifest: %w", err)
	}
	if string(persisted) != string(newData) {
		return fmt.Errorf("sniff manifest changed before verification: %s", path)
	}
	return nil
}

var _ = (Strategy)(StrategyInterval)
var _ = (Strategy)(StrategyStartup)
var _ = (Strategy)(StrategyService)
var _ = (Strategy)(StrategyManual)
