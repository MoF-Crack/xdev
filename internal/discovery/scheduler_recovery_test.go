package discovery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestD18DiscoveryNDKOrderingAndPromotion(t *testing.T) {
	s, _ := outcomeFixture(t, "android-ndk")
	writeToolYAML(t, s.manifestDir, "android-ndk", "name: android-ndk\nlatest: r26\nversions:\n  - version: r26\n")
	tool := &manifest.Tool{Name: "android-ndk"}
	if err := mergeIntoManifestOpts(tool, []DiscoveredVersion{{Version: "r29"}, {Version: "r27c"}, {Version: "unknown"}}, s.manifestDir, false); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.catalog()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := catalog.Tool(tool.Name)
	if !ok || got.Latest != "r29" || got.VersionList[0].Version != "r29" {
		t.Fatalf("unexpected merged manifest %+v", got)
	}
}

// Two schedulers share the same state file, just as two CLI processes do.
func TestD13SharedBudgetCannotBeSpentTwice(t *testing.T) {
	s, f := outcomeFixture(t, "alpha", "beta")
	n := NewScheduler(s.cfg, filepath.Dir(s.cachePath), s.manifestDir)
	n.registry = s.registry
	if err := s.withCacheLock(func(c *SniffCache) error {
		c.RateLimit = RateLimitState{Remaining: 1, LastRefill: time.Now()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan error, 2)
	for tool, scheduler := range map[string]*Scheduler{"alpha": s, "beta": n} {
		go func() { <-start; done <- scheduler.sniffNowInternal(context.Background(), tool, false) }()
	}
	close(start)
	a, b := <-done, <-done
	if (a == nil) == (b == nil) || f.count("alpha")+f.count("beta") != 1 {
		t.Fatalf("requests=%d+%d errors=%v, %v", f.count("alpha"), f.count("beta"), a, b)
	}
}

func TestD14OlderRoundPreservesNewerAttemptAndPending(t *testing.T) {
	s, f := outcomeFixture(t, "alpha")
	n := NewScheduler(s.cfg, filepath.Dir(s.cachePath), s.manifestDir)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	f.fn = func(context.Context, *manifest.Tool) ([]DiscoveredVersion, error) {
		close(entered)
		<-release
		return []DiscoveredVersion{{Version: "1.0.0"}}, nil
	}
	n.registry = &SnifferRegistry{sniffers: []Sniffer{&outcomeSniffer{calls: map[string]int{}, fn: func(context.Context, *manifest.Tool) ([]DiscoveredVersion, error) {
		return nil, errors.New("fixture unavailable")
	}}}}
	go func() { done <- s.SniffAll(context.Background()) }()
	<-entered
	newerErr := n.SniffAll(context.Background())
	before := outcomeCache(t, n)
	close(release)
	olderErr := <-done
	after := outcomeCache(t, n)
	if olderErr != nil || newerErr == nil || !after.LastAttempt.Equal(before.LastAttempt) || !after.Partial || !after.LastSuccess.IsZero() || len(after.Pending) != 1 || after.Pending[0] != "alpha" {
		t.Fatalf("new=%v old=%v state=%+v", newerErr, olderErr, after)
	}
	r := after.Tools["alpha"]
	if r.Status != SniffFailed || r.Error != "fixture unavailable" || len(r.Versions) != 1 || r.Versions[0] != "1.0.0" || !r.LastAttempt.Equal(before.Tools["alpha"].LastAttempt) {
		t.Fatalf("newer failure with successful snapshot: %+v", r)
	}
}

func TestD14CanceledRoundResumesUnreachedExpiredSuccess(t *testing.T) {
	s, f := outcomeFixture(t, "alpha", "beta", "gamma")
	old := time.Now().Add(-48 * time.Hour)
	seed := map[string]ToolSniffResult{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		seed[n] = successfulSnapshot(old)
	}
	outcomeSeed(t, s, &SniffCache{LastSuccess: old, Tools: seed})
	var completed, canceled string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.fn = func(_ context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
		if completed == "" {
			completed = t.Name
			return nil, nil
		}
		canceled = t.Name
		cancel()
		return nil, ctx.Err()
	}
	if err := s.sniffAllWithForce(ctx, false); err == nil {
		t.Fatal("canceled batch returned success")
	}
	c := outcomeCache(t, s)
	if len(c.Pending) != 2 || nameSet(c.Pending)[completed] || !c.LastSuccess.Equal(old) {
		t.Fatalf("unexpected pending %v", c.Pending)
	}
	if err := s.withCacheLock(func(c *SniffCache) error { c.RetryAfter = time.Time{}; return nil }); err != nil {
		t.Fatal(err)
	}
	f.fn = nil
	if !s.StartupSniffPending() {
		t.Fatal("missing recovery for unattempted tool")
	}
	if err := s.sniffAllWithForce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	c = outcomeCache(t, s)
	if f.count(completed) != 1 || len(c.Pending) != 1 || c.Pending[0] != canceled {
		t.Fatalf("completed=%q canceled=%q cache=%+v", completed, canceled, c)
	}
}

func TestD13KnownResetAndLegacyMigration(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	reset := now.Add(2 * time.Minute)
	if got := serverReset(fmt.Errorf("github remote: API rate limit exhausted (reset at %d)", reset.Unix()), now); !got.Equal(reset) {
		t.Fatalf("reset=%v want=%v", got, reset)
	}
	c := SniffCache{LastSniffAt: now, Tools: map[string]ToolSniffResult{"ok": {LastSniff: now}, "bad": {LastSniff: now, Error: "failed"}, "unsupported": {LastSniff: now, Unsupported: true}}}
	normalizeSniffCache(&c)
	if !c.LastSuccess.IsZero() || !c.LastSniffAt.IsZero() || !c.Partial || !c.Tools["ok"].LastSuccess.Equal(now) || !c.Tools["bad"].LastAttempt.Equal(now) || !c.Tools["unsupported"].LastSniff.IsZero() {
		t.Fatalf("unexpected migration %+v", c)
	}
}
