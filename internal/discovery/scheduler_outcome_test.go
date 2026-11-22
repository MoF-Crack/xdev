package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

type outcomeSniffer struct {
	mu    sync.Mutex
	calls map[string]int
	fn    func(context.Context, *manifest.Tool) ([]DiscoveredVersion, error)
}

func (f *outcomeSniffer) Name() string                 { return "local-outcome-fixture" }
func (f *outcomeSniffer) Supports(*manifest.Tool) bool { return true }
func (f *outcomeSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	f.mu.Lock()
	f.calls[t.Name]++
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, t)
	}
	return []DiscoveredVersion{}, nil
}
func (f *outcomeSniffer) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}
func outcomeFixture(t *testing.T, names ...string) (*Scheduler, *outcomeSniffer) {
	t.Helper()
	root := t.TempDir()
	md := filepath.Join(root, "manifests")
	if err := os.MkdirAll(md, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		writeToolYAML(t, md, name, "name: "+name+"\nauto_sniff: true\n")
	}
	s := NewScheduler(&config.SniffConfig{Strategy: "interval", Interval: "24h", Concurrency: 1, RateLimitPerHr: 60}, filepath.Join(root, "cache"), md)
	f := &outcomeSniffer{calls: map[string]int{}}
	s.registry = &SnifferRegistry{sniffers: []Sniffer{f}}
	return s, f
}
func outcomeCache(t *testing.T, s *Scheduler) *SniffCache {
	t.Helper()
	c, err := s.LoadCache()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func outcomeSeed(t *testing.T, s *Scheduler, c *SniffCache) {
	t.Helper()
	if err := s.SaveCache(c); err != nil {
		t.Fatal(err)
	}
}
func successfulSnapshot(at time.Time) ToolSniffResult {
	return ToolSniffResult{LastAttempt: at, LastSuccess: at, LastSniff: at, Status: SniffSuccess, Versions: []string{"1.0.0"}}
}
func runOutcome(t *testing.T, s *Scheduler, batch bool, ctx context.Context) error {
	t.Helper()
	if batch {
		return s.sniffAllWithForce(ctx, false)
	}
	return s.sniffNowInternal(ctx, "alpha", false)
}

func TestD13SkipsNeverSpendTokens(t *testing.T) {
	for _, kind := range []string{"fresh", "disabled", "ignored", "unsupported", "precanceled"} {
		t.Run(kind, func(t *testing.T) {
			s, f := outcomeFixture(t, "alpha")
			ctx := context.Background()
			switch kind {
			case "fresh":
				outcomeSeed(t, s, &SniffCache{Tools: map[string]ToolSniffResult{"alpha": successfulSnapshot(time.Now().Add(-time.Hour))}})
			case "disabled":
				writeToolYAML(t, s.manifestDir, "alpha", "name: alpha\nauto_sniff: false\n")
			case "ignored":
				if err := os.MkdirAll(filepath.Dir(s.cachePath), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(s.cachePath), "ignored_tools.json"), []byte(`{"items":[{"name":"alpha","kind":"update","ignoredAt":"2026-01-01T00:00:00Z"}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "unsupported":
				s.registry = &SnifferRegistry{}
			case "precanceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			before := s.limiter.available()
			_ = s.sniffNowInternal(ctx, "alpha", false)
			if f.count("alpha") != 0 || s.limiter.available() != before {
				t.Errorf("skip %s: calls=%d tokens=%d want=%d", kind, f.count("alpha"), s.limiter.available(), before)
			}
			if kind == "precanceled" && len(outcomeCache(t, s).Tools) != 0 {
				t.Error("pre-cancel created a synthetic tool result")
			}
		})
	}
}

func TestD13FailuresRetainSuccessfulSnapshotAndBackOff(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, history := range []bool{false, true} {
			for _, kind := range []string{"404", "429", "403", "transient", "canceled", "backoff-cancel", "local-limit"} {
				t.Run(fmt.Sprintf("batch=%v/history=%v/%s", batch, history, kind), func(t *testing.T) {
					s, f := outcomeFixture(t, "alpha")
					old := time.Now().Add(-48 * time.Hour).UTC()
					entries := map[string]ToolSniffResult{}
					if history {
						entries["alpha"] = successfulSnapshot(old)
					}
					outcomeSeed(t, s, &SniffCache{Tools: entries, LastSniffAt: old, LastSuccess: old})
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					f.fn = func(context.Context, *manifest.Tool) ([]DiscoveredVersion, error) {
						switch kind {
						case "404":
							return nil, errors.New("API returned 404: absent")
						case "429":
							return nil, errors.New("API returned 429: too many requests")
						case "403":
							return nil, errors.New("API returned 403: rate limit")
						case "transient":
							return nil, errors.New("temporary connection failure")
						case "canceled":
							cancel()
							return nil, ctx.Err()
						case "backoff-cancel":
							cancel()
							return nil, errors.New("temporary connection failure")
						}
						panic(kind)
					}
					if kind == "local-limit" {
						s.limiter.setTokens(0)
					}
					before := s.limiter.available()
					if err := runOutcome(t, s, batch, ctx); err == nil {
						t.Error("failure must return error")
					}
					cache := outcomeCache(t, s)
					r := cache.Tools["alpha"]
					want := 1
					if kind == "transient" {
						want = 4
					}
					if kind == "local-limit" {
						want = 0
					}
					if f.count("alpha") != want {
						t.Errorf("calls=%d want=%d", f.count("alpha"), want)
					}
					if got := before - s.limiter.available(); got != want && kind != "403" && kind != "429" {
						t.Errorf("spent %d tokens for %d attempts", got, want)
					}
					if history {
						if !r.LastSuccess.Equal(old) || !r.LastSniff.Equal(old) || !reflect.DeepEqual(r.Versions, []string{"1.0.0"}) {
							t.Errorf("successful snapshot lost: %+v", r)
						}
					} else if !r.LastSuccess.IsZero() || !r.LastSniff.IsZero() {
						t.Errorf("failure claimed success: %+v", r)
					}
					wantPriorAttempt := time.Time{}
					if history {
						wantPriorAttempt = old
					}
					if kind == "local-limit" && !r.LastAttempt.Equal(wantPriorAttempt) {
						t.Error("local limiter fabricated remote attempt")
					}
					if kind != "local-limit" && r.LastAttempt.IsZero() {
						t.Error("missing actual attempt time")
					}
					if !r.RetryAfter.After(time.Now()) || r.Error == "" {
						t.Errorf("missing persisted backoff: %+v", r)
					}
					if batch && !cache.LastSuccess.Equal(old) {
						t.Errorf("failed batch advanced success: %+v", cache)
					}
					// Recreate the process while restoring ample local tokens: persisted
					// retry time must still suppress repeated CLI startups.
					n := NewScheduler(s.cfg, filepath.Dir(s.cachePath), s.manifestDir)
					n.registry = s.registry
					n.limiter.setTokens(60)
					f.fn = nil
					if err := n.sniffNowInternal(context.Background(), "alpha", false); err != nil {
						t.Errorf("backoff skip: %v", err)
					}
					if f.count("alpha") != want {
						t.Error("restart retried before backoff expired")
					}
					if err := n.withCacheLock(func(c *SniffCache) error {
						x := c.Tools["alpha"]
						x.RetryAfter = time.Now().Add(-time.Second)
						c.Tools["alpha"] = x
						c.RateLimit.ResetAt = time.Time{}
						c.RateLimit.LastRefill = time.Now().Add(-time.Hour) // elapsed time refills the shared budget
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					n.limiter.setTokens(60)
					if err := n.sniffNowInternal(context.Background(), "alpha", false); err != nil {
						t.Fatal(err)
					}
					if f.count("alpha") != want+1 {
						t.Error("due failure suppressed by success TTL")
					}
					if r := outcomeCache(t, n).Tools["alpha"]; r.Status != SniffSuccess || r.LastSuccess.IsZero() || r.Error != "" {
						t.Errorf("empty success not recorded: %+v", r)
					}
				})
			}
		}
	}
}

func TestD14PartialRecoversOnlyDueUnfinishedTools(t *testing.T) {
	s, f := outcomeFixture(t, "alpha", "beta", "gamma")
	old := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(time.Hour)
	outcomeSeed(t, s, &SniffCache{LastSuccess: old, LastSniffAt: time.Now(), Partial: true, Stale: true, Pending: []string{"beta", "gamma"}, Tools: map[string]ToolSniffResult{
		"alpha": successfulSnapshot(time.Now().Add(-time.Hour)), "beta": {Status: SniffFailed, RetryAfter: time.Now().Add(-time.Second), Error: "offline"}, "gamma": {Status: SniffRateLimited, RetryAfter: future, Error: "429"},
	}})
	if !s.StartupSniffPending() {
		t.Fatal("due incomplete tool must bypass global success TTL")
	}
	s.OnStartup(context.Background())
	select {
	case <-s.startupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not finish")
	}
	if f.count("alpha") != 0 || f.count("beta") != 1 || f.count("gamma") != 0 {
		t.Fatalf("calls: alpha=%d beta=%d gamma=%d", f.count("alpha"), f.count("beta"), f.count("gamma"))
	}
	c := outcomeCache(t, s)
	if !c.Partial || !reflect.DeepEqual(c.Pending, []string{"gamma"}) || !c.LastSuccess.Equal(old) {
		t.Errorf("incomplete state=%+v", c)
	}
	n := NewScheduler(s.cfg, filepath.Dir(s.cachePath), s.manifestDir)
	n.registry = s.registry
	if n.StartupSniffPending() {
		t.Error("future retry must not spawn on every startup")
	}
	if err := n.withCacheLock(func(c *SniffCache) error {
		r := c.Tools["gamma"]
		r.RetryAfter = time.Now().Add(-time.Second)
		c.Tools["gamma"] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !n.StartupSniffPending() {
		t.Fatal("retry deadline did not reopen gate")
	}
	n.OnStartup(context.Background())
	select {
	case <-n.startupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("second resume did not finish")
	}
	c = outcomeCache(t, n)
	if c.Partial || c.Stale || len(c.Pending) != 0 || !c.LastSuccess.After(old) {
		t.Errorf("complete=%+v", c)
	}
	if f.count("alpha") != 0 || f.count("beta") != 1 || f.count("gamma") != 1 {
		t.Error("completed items repeated during recovery")
	}
}

func TestD14PreCanceledBatchAndShutdownDoNotInventSuccess(t *testing.T) {
	t.Run("pre-cancel", func(t *testing.T) {
		s, f := outcomeFixture(t, "alpha")
		old := time.Now().Add(-48 * time.Hour)
		outcomeSeed(t, s, &SniffCache{LastSuccess: old, LastSniffAt: old, Tools: map[string]ToolSniffResult{}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.sniffAllWithForce(ctx, false); err == nil {
			t.Fatal("cancel should return error")
		}
		c := outcomeCache(t, s)
		if !c.LastSuccess.Equal(old) || !c.LastSniffAt.Equal(old) || len(c.Tools) != 0 || f.count("alpha") != 0 || !c.Partial {
			t.Errorf("pre-cancel=%+v", c)
		}
		if s.StartupSniffPending() {
			t.Error("immediate restart must honor cancellation cooldown")
		}
		if err := s.withCacheLock(func(c *SniffCache) error { c.RetryAfter = time.Now().Add(-time.Second); return nil }); err != nil {
			t.Fatal(err)
		}
		if !s.StartupSniffPending() {
			t.Error("unattempted tool must resume once cooldown expires")
		}
	})
	t.Run("shutdown", func(t *testing.T) {
		s := newShutdownFallbackScheduler(t)
		old := time.Now().Add(-48 * time.Hour)
		outcomeSeed(t, s, &SniffCache{LastSuccess: old, LastSniffAt: old, Tools: map[string]ToolSniffResult{"alpha": successfulSnapshot(old)}})
		if err := s.Shutdown(); err != nil {
			t.Fatal(err)
		}
		c := outcomeCache(t, s)
		if !c.LastSuccess.Equal(old) || !c.LastSniffAt.Equal(old) || !c.Tools["alpha"].LastSuccess.Equal(old) || !c.Partial {
			t.Errorf("fallback=%+v", c)
		}
	})
}

func TestD13ForcePreservesManualContract(t *testing.T) {
	s, f := outcomeFixture(t, "alpha")
	future := time.Now().Add(time.Hour)
	r := successfulSnapshot(time.Now())
	r.Status = SniffRateLimited
	r.RetryAfter = future
	outcomeSeed(t, s, &SniffCache{Tools: map[string]ToolSniffResult{"alpha": r}, RateLimit: RateLimitState{ResetAt: future}})
	s.limiter.setTokens(0)
	if err := s.SniffNow(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if f.count("alpha") != 1 || s.limiter.available() != 0 {
		t.Errorf("force calls=%d tokens=%d", f.count("alpha"), s.limiter.available())
	}
	for _, strategy := range []string{"manual", "service"} {
		s.cfg.Strategy = strategy
		if s.StartupSniffPending() {
			t.Errorf("%s should not spawn", strategy)
		}
		s.OnStartup(context.Background())
		if s.StartupSniffSpawned() {
			t.Errorf("%s spawned", strategy)
		}
	}
}
