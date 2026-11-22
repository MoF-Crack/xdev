package discovery

import (
	"errors"
	"hash/fnv"
	"path/filepath"
	"sort"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

const attemptLease = 3 * time.Minute
const canceledRetryDelay = time.Minute

var errCacheUnchanged = errors.New("sniff cache unchanged")

func (s *Scheduler) sniffInterval() time.Duration {
	d, err := time.ParseDuration(s.cfg.Interval)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// Legacy failures wrote LastSniff on unsuccessful attempts. Only a legacy
// entry without an error is evidence of a successful check. Migration never
// assigns a current timestamp to an old observation.
func normalizeSniffCache(c *SniffCache) {
	if c.Tools == nil {
		c.Tools = make(map[string]ToolSniffResult)
	}
	legacyFailure := false
	for name, r := range c.Tools {
		if r.Status == "" {
			if r.Error != "" {
				r.Status = SniffFailed
				legacyFailure = true
				if r.LastAttempt.IsZero() {
					r.LastAttempt = r.LastSniff
				}
				if r.RetryAfter.IsZero() && !r.LastAttempt.IsZero() {
					r.RetryAfter = r.LastAttempt.Add(5 * time.Minute)
				}
				r.LastSniff = r.LastSuccess
			} else if r.Unsupported {
				r.Status = SniffUnsupported
				if r.RetryAfter.IsZero() {
					r.RetryAfter = r.LastSniff.Add(24 * time.Hour)
				}
				r.LastSniff = r.LastSuccess
			} else if !r.LastSniff.IsZero() {
				r.Status = SniffSuccess
				if r.LastSuccess.IsZero() {
					r.LastSuccess = r.LastSniff
				}
				if r.LastAttempt.IsZero() {
					r.LastAttempt = r.LastSuccess
				}
			}
		}
		if !r.LastSuccess.IsZero() {
			r.LastSniff = r.LastSuccess
			if r.SuccessAttempt.IsZero() {
				r.SuccessAttempt = r.LastSuccess
			}
		}
		c.Tools[name] = r
	}
	if c.SchemaVersion < 2 {
		if c.LastAttempt.IsZero() {
			c.LastAttempt = c.LastSniffAt
		}
		if c.LastSuccess.IsZero() && !c.Partial && !c.Stale && !legacyFailure {
			c.LastSuccess = c.LastSniffAt
		}
		if legacyFailure {
			c.Partial = true
			c.Stale = true
		}
		if c.LastSuccess.IsZero() && (c.Partial || c.Stale) {
			c.LastSniffAt = time.Time{}
		}
	}
	if !c.LastSuccess.IsZero() {
		c.LastSniffAt = c.LastSuccess
	}
}

func incompleteSniff(c *SniffCache) bool { return c.Partial || c.Stale || len(c.Pending) > 0 }
func completedTool(r ToolSniffResult) bool {
	return r.Status == SniffSuccess || r.Status == SniffUnsupported
}

// shouldQuery is shared by the startup gate and the atomic attempt reservation.
// A failure has its own retry deadline even when it retains a fresh success.
func (s *Scheduler) shouldQuery(r ToolSniffResult, now time.Time) bool {
	if now.Before(r.RetryAfter) {
		return false
	}
	if r.Status == SniffSuccess && !r.LastSuccess.IsZero() && now.Sub(r.LastSuccess) < s.sniffInterval() {
		return false
	}
	return true
}

func (s *Scheduler) startupDue(everyStartup bool) bool {
	c, err := s.LoadCache()
	if err != nil || c == nil {
		return true
	}
	now := time.Now()
	if now.Before(c.RateLimit.ResetAt) || now.Before(c.RetryAfter) {
		return false
	}
	if !incompleteSniff(c) {
		return everyStartup || c.LastSuccess.IsZero() || now.Sub(c.LastSuccess) >= s.sniffInterval()
	}
	cat, err := s.catalog()
	if err != nil {
		return true
	}
	ignored := util.LoadIgnoredUpdateNames(filepath.Dir(s.cachePath))
	pending := nameSet(c.Pending)
	unfinished := false
	for _, t := range cat.Tools() {
		if !t.ShouldAutoSniff() || ignored[t.Name] {
			continue
		}
		r := c.Tools[t.Name]
		if !pendingCandidate(pending, t.Name, r, now, s.sniffInterval()) {
			continue
		}
		if completedTool(r) && !s.shouldQuery(r, now) {
			continue
		}
		unfinished = true
		if s.shouldQuery(r, now) {
			return true
		}
	}
	// If only deleted/disabled/completed items remain, run the cheap reconciliation
	// once so a stale Partial flag does not become permanent.
	return !unfinished
}

func nameSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, name := range names {
		m[name] = true
	}
	return m
}

// Recovery includes the old pending set plus newly due work. A pending set
// records unfinished work; it must not freeze catalog coverage indefinitely.
// Fresh successes remain excluded, including those completed during recovery.
func pendingCandidate(pending map[string]bool, name string, r ToolSniffResult, now time.Time, ttl time.Duration) bool {
	if pending[name] {
		return true
	}
	if r.Status == SniffSuccess {
		return r.LastSuccess.IsZero() || now.Sub(r.LastSuccess) >= ttl
	}
	return r.Status != SniffUnsupported || !now.Before(r.RetryAfter)
}

func (c *SniffCache) removePending(name string) {
	for i, n := range c.Pending {
		if n == name {
			c.Pending = append(c.Pending[:i], c.Pending[i+1:]...)
			return
		}
	}
}

// A single failed refresh following a completed batch must wake the interval
// scheduler at its own retry deadline. Without prior batch coverage the next
// round still needs to examine the rest of the catalog.
func (c *SniffCache) trackSingleFailure(name string) {
	c.Partial, c.Stale = true, true
	if c.PendingKnown || !c.LastSuccess.IsZero() {
		c.PendingKnown = true
		if !nameSet(c.Pending)[name] {
			c.Pending = append(c.Pending, name)
			sort.Strings(c.Pending)
		}
	}
}

// Each remote attempt gets a unique timestamp under the exclusive lock.
// Superseded workers cannot publish over the newer attempt or its snapshot.
func (s *Scheduler) publishTool(name string, r ToolSniffResult, limitedUntil, round time.Time) error {
	return s.withCacheLock(func(c *SniffCache) error {
		if limitedUntil.After(c.RateLimit.ResetAt) {
			c.RateLimit.ResetAt = limitedUntil
			c.RateLimit.Remaining = 0
		}
		current := c.Tools[name]
		if !current.LastAttempt.Equal(r.LastAttempt) || current.Status != SniffRunning {
			// A superseded success still supplies a valid snapshot if the newer
			// attempt failed. Never replace a newer successful observation.
			if r.Status == SniffSuccess && r.LastAttempt.After(current.SuccessAttempt) {
				copySuccess(&current, r)
				c.Tools[name] = current
			}
			return nil
		}
		if r.Status != SniffSuccess {
			copySuccess(&r, current)
		}
		c.Tools[name] = r
		if round.IsZero() && !completedTool(r) {
			c.trackSingleFailure(name)
		}
		if completedTool(r) && (round.IsZero() || round.Equal(c.LastAttempt)) {
			c.removePending(name)
		}
		return nil
	})
}

func copySuccess(dst *ToolSniffResult, src ToolSniffResult) {
	dst.LastSuccess = src.LastSuccess
	dst.SuccessAttempt = src.SuccessAttempt
	dst.LastSniff = src.LastSuccess
	dst.Versions = src.Versions
	dst.Etag = src.Etag
}

// Persist a refill baseline with each automatic reservation. The cache lock
// serializes tokens across schedulers/processes sharing one home.
func (s *Scheduler) takeAttemptToken(c *SniffCache, now time.Time) (bool, time.Time) {
	local := s.limiter.available()
	if c.RateLimit.LastRefill.IsZero() {
		c.RateLimit.LastRefill = now
		c.RateLimit.Remaining = local
	}
	rl := s.limiter
	earned := int(now.Sub(c.RateLimit.LastRefill) / rl.refillDur)
	if earned > 0 {
		c.RateLimit.Remaining += earned
		if c.RateLimit.Remaining > rl.maxTokens {
			c.RateLimit.Remaining = rl.maxTokens
		}
		c.RateLimit.LastRefill = c.RateLimit.LastRefill.Add(time.Duration(earned) * rl.refillDur)
	}
	// Once a refill timestamp exists, the locked on-disk budget is authoritative.
	if c.RateLimit.Remaining <= 0 {
		c.RateLimit.Remaining = 0
		rl.setState(0, c.RateLimit.LastRefill)
		return false, c.RateLimit.LastRefill.Add(rl.refillDur)
	}
	c.RateLimit.Remaining--
	rl.setState(c.RateLimit.Remaining, c.RateLimit.LastRefill)
	return true, time.Time{}
}

func (rl *rateLimiter) setState(n int, at time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n > rl.maxTokens {
		n = rl.maxTokens
	}
	rl.tokens = n
	rl.lastRefill = at
}

func failureRetry(name string, failures int, base time.Duration) time.Duration {
	for i := 1; i < failures && base < time.Hour; i++ {
		base *= 2
	}
	if base > time.Hour {
		base = time.Hour
	}
	// Stable per-tool jitter spreads simultaneous next-start retries without
	// adding RNG/global state or making the persisted deadline nondeterministic.
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return base + time.Duration(h.Sum32()%21)*base/100
}

// beginRound saves the exact candidate set before work starts. Recovery selects
// this set, rather than forcing all successful tools to refresh again.
func (s *Scheduler) beginRound(tools []*manifest.Tool, force, startup bool) ([]*manifest.Tool, time.Time, error) {
	ignored := map[string]bool{}
	if !force {
		ignored = util.LoadIgnoredUpdateNames(filepath.Dir(s.cachePath))
	}
	var selected []*manifest.Tool
	var round time.Time
	err := s.withCacheLock(func(c *SniffCache) error {
		now := time.Now().UTC()
		if !force && (now.Before(c.RetryAfter) || now.Before(c.RateLimit.ResetAt)) {
			return errCacheUnchanged
		}
		if startup && c.LastAttempt.After(s.startupSince) {
			return errCacheUnchanged
		}
		resume := !force && incompleteSniff(c)
		pending := nameSet(c.Pending)
		var names []string
		for _, t := range tools {
			if !t.ShouldAutoSniff() || ignored[t.Name] {
				continue
			}
			r := c.Tools[t.Name]
			if resume && !pendingCandidate(pending, t.Name, r, now, s.sniffInterval()) {
				continue
			}
			if !force && !s.shouldQuery(r, now) {
				if !completedTool(r) {
					names = append(names, t.Name)
				}
				continue
			}
			selected = append(selected, t)
			names = append(names, t.Name)
		}
		if !now.After(c.LastAttempt) {
			now = c.LastAttempt.Add(time.Nanosecond)
		}
		round = now
		if startup {
			id := round
			s.startupRound.Store(&id)
		}
		c.LastAttempt = round
		c.Status = SniffRunning
		c.Partial = true
		c.RetryAfter = now.Add(attemptLease)
		c.PendingKnown = true
		c.Pending = names
		sort.Strings(c.Pending)
		return nil
	})
	return selected, round, err
}

func (s *Scheduler) finishRound(round time.Time, canceled bool) error {
	if round.IsZero() {
		return nil
	}
	return s.withCacheLock(func(c *SniffCache) error {
		if !c.LastAttempt.Equal(round) {
			return errCacheUnchanged
		}
		c.Partial = canceled || len(c.Pending) > 0
		c.Stale = c.Partial
		c.RetryAfter = time.Time{}
		if canceled {
			c.Status = SniffCanceled
			c.RetryAfter = time.Now().Add(canceledRetryDelay)
		} else if c.Partial {
			c.Status = SniffFailed
		} else {
			c.Status = SniffSuccess
			c.LastSuccess = time.Now().UTC()
			c.LastSniffAt = c.LastSuccess
		}
		return nil
	})
}
