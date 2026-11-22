package discovery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// Per-dispatch disposition is distinct from the status of retained data.
// For example, a TTL skip can retain a successful snapshot.
type disposition string

const (
	skipDisabled   disposition = "disabled"
	skipFresh      disposition = "fresh"
	skipBackoff    disposition = "backoff"
	skipSuperseded disposition = "superseded"
	noSniffer      disposition = "unsupported"
	noBudget       disposition = "limited"
	queryAttempt   disposition = "attempt"
	interrupted    disposition = "interrupted"
)

type toolOutcome struct {
	name     string
	action   disposition
	status   SniffStatus
	attempts int
	err      error
}

func serverReset(err error, now time.Time) time.Time {
	var reset interface{ RetryAfter() time.Time }
	if errors.As(err, &reset) && reset.RetryAfter().After(now) {
		return reset.RetryAfter()
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "rate limit") && !strings.Contains(msg, "returned 403") && !strings.Contains(msg, "returned 429") {
		return time.Time{}
	}
	if _, tail, ok := strings.Cut(msg, "(reset at "); ok {
		secs, _, found := strings.Cut(tail, ")")
		if n, e := strconv.ParseInt(secs, 10, 64); found && e == nil && time.Unix(n, 0).After(now) {
			return time.Unix(n, 0)
		}
	}
	return now.Add(rateLimitWindow)
}

// reserve rechecks eligibility, the worker's ownership and the shared budget
// together. Each successful reservation dispatches exactly one Sniff call.
func (s *Scheduler) reserve(ctx context.Context, name string, matched, force bool, round, previous time.Time) (r ToolSniffResult, action disposition, err error) {
	var denied error
	err = s.withCacheLock(func(c *SniffCache) error {
		if err := ctx.Err(); err != nil {
			action = interrupted
			return err
		}
		r = c.Tools[name]
		now := time.Now().UTC()
		if !round.IsZero() && !round.Equal(c.LastAttempt) {
			action = skipSuperseded
			return errCacheUnchanged
		}
		if !previous.IsZero() && (!previous.Equal(r.LastAttempt) || r.Status != SniffRunning) {
			action = skipSuperseded
			return errCacheUnchanged
		}
		if !force && previous.IsZero() && !s.shouldQuery(r, now) {
			action = skipBackoff
			if completedTool(r) {
				action = skipFresh
			}
			if completedTool(r) && round.Equal(c.LastAttempt) && !round.IsZero() {
				c.removePending(name)
				return nil
			}
			return errCacheUnchanged
		}
		if !matched {
			action = noSniffer
			r.Status = SniffUnsupported
			r.Unsupported = true
			r.Error = ""
			r.Failures = 0
			r.RetryAfter = now.Add(s.sniffInterval())
			c.Tools[name] = r
			c.removePending(name)
			return nil
		}
		if !force {
			until := c.RateLimit.ResetAt
			if !now.Before(until) {
				ok, retry := s.takeAttemptToken(c, now)
				if !ok {
					until = retry
				}
			}
			if now.Before(until) {
				action = noBudget
				r.Status = SniffDeferred
				r.RetryAfter = until
				r.Error = "rate limit exceeded for " + name
				r.Unsupported = false
				c.Tools[name] = r
				if round.IsZero() {
					c.trackSingleFailure(name)
				}
				denied = errors.New(r.Error)
				return nil
			}
		}
		if !now.After(r.LastAttempt) {
			now = r.LastAttempt.Add(time.Nanosecond)
		}
		r.LastAttempt = now
		r.Status = SniffRunning
		r.RetryAfter = now.Add(attemptLease)
		r.Unsupported = false
		r.Error = ""
		c.Tools[name] = r
		action = queryAttempt
		return nil
	})
	if err == nil {
		err = denied
	}
	return
}

func (s *Scheduler) sniffTool(ctx context.Context, t *manifest.Tool, force bool, round time.Time) toolOutcome {
	out := toolOutcome{name: t.Name, action: skipDisabled}
	if !t.ShouldAutoSniff() {
		return out
	}
	sniffer := s.registry.match(t)
	var r ToolSniffResult
	var previous time.Time
	var until time.Time
	var versions []DiscoveredVersion
	for attempt := 0; ; attempt++ {
		next, action, err := s.reserve(ctx, t.Name, sniffer != nil, force, round, previous)
		out.action = action
		if err != nil {
			out.err = err
			out.status = next.Status
			if previous.IsZero() || ctx.Err() == nil {
				return out
			}
			break
		}
		if action != queryAttempt {
			out.err = nil
			out.status = next.Status
			return out
		}
		r = next
		previous = r.LastAttempt
		out.attempts++
		versions, out.err = sniffer.Sniff(ctx, t)
		if ctx.Err() != nil {
			out.err = ctx.Err()
			break
		}
		if out.err == nil {
			out.err = s.mergeVersions(t, versions)
			break
		}
		until = serverReset(out.err, time.Now())
		if !until.IsZero() || !isTransientError(out.err) || attempt == 3 {
			break
		}
		timer := time.NewTimer(time.Second << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			out.err = ctx.Err()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			break
		}
	}
	now := time.Now().UTC()
	if out.err == nil {
		r.Status = SniffSuccess
		r.SuccessAttempt = r.LastAttempt
		r.LastSuccess = now
		r.LastSniff = now
		r.RetryAfter = time.Time{}
		r.Failures = 0
		r.Versions = nil
		for _, v := range versions {
			r.Versions = append(r.Versions, v.Version)
		}
	} else {
		r.Status = SniffFailed
		r.Error = out.err.Error()
		r.Failures++
		delay := 5 * time.Minute
		switch {
		case !until.IsZero():
			r.Status = SniffRateLimited
			r.RetryAfter = until
		case errors.Is(out.err, context.Canceled) || errors.Is(out.err, context.DeadlineExceeded):
			r.Status = SniffCanceled
			r.RetryAfter = now.Add(failureRetry(t.Name, r.Failures, canceledRetryDelay))
		default:
			if strings.Contains(r.Error, "404") {
				delay = time.Hour
			}
			r.RetryAfter = now.Add(failureRetry(t.Name, r.Failures, delay))
		}
	}
	out.status = r.Status
	if err := s.publishTool(t.Name, r, until, round); err != nil {
		out.err = errors.Join(out.err, fmt.Errorf("persist sniff result: %w", err))
	}
	return out
}
