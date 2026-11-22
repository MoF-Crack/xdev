package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestD14RecoveryIncludesNewAndExpiredTools(t *testing.T) {
	for _, recoverNow := range []bool{false, true} {
		t.Run(map[bool]string{false: "backoff", true: "due"}[recoverNow], func(t *testing.T) {
			s, f := outcomeFixture(t, "old", "retry", "new", "fresh")
			past := time.Now().Add(-48 * time.Hour)
			deadline := time.Now().Add(time.Hour)
			if recoverNow {
				deadline = past
			}
			outcomeSeed(t, s, &SniffCache{SchemaVersion: 2, LastSuccess: past, LastAttempt: past, Partial: true, Stale: true, PendingKnown: true, Pending: []string{"retry"}, Tools: map[string]ToolSniffResult{"old": successfulSnapshot(past), "fresh": successfulSnapshot(time.Now()), "retry": {Status: SniffFailed, RetryAfter: deadline, LastAttempt: past}}})
			if !s.StartupSniffPending() {
				t.Fatal("due catalog work starved behind pending failures")
			}
			if err := s.sniffAllWithForce(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			c := outcomeCache(t, s)
			wantRetry := 0
			if recoverNow {
				wantRetry = 1
			}
			if f.count("old") != 1 || f.count("new") != 1 || f.count("fresh") != 0 || f.count("retry") != wantRetry {
				t.Fatalf("calls=%v", f.calls)
			}
			if recoverNow {
				if c.Partial || len(c.Pending) != 0 || !c.LastSuccess.After(past) {
					t.Fatalf("complete=%+v", c)
				}
			} else {
				if !c.Partial || len(c.Pending) != 1 || c.Pending[0] != "retry" || !c.LastSuccess.Equal(past) {
					t.Fatalf("partial=%+v", c)
				}
			}
			if s.StartupSniffPending() {
				t.Fatal("fresh successes or backed-off failure repeatedly spawn")
			}
		})
	}
}

func TestD13RealArchiveNoOpDoesNotSpendToken(t *testing.T) {
	s, _ := outcomeFixture(t, "fixed")
	s.registry = newSnifferRegistry()
	obj := &manifest.Tool{Name: "fixed", VersionList: []manifest.Version{{Version: "1.2.3", Archives: []manifest.Archive{{URL: "https://github.com/sample/tool/releases/download/stable/tool.zip", MirrorURLs: []string{"https://mirror.test/1.2.3.zip"}}}}}}
	before := s.limiter.available()
	out := s.sniffTool(context.Background(), obj, false, time.Time{})
	r := outcomeCache(t, s).Tools["fixed"]
	if out.err != nil || out.action != noSniffer || out.attempts != 0 || r.Status != SniffUnsupported || !r.LastSuccess.IsZero() || !r.LastAttempt.IsZero() || s.limiter.available() != before {
		t.Fatalf("outcome=%+v cache=%+v budget=%d", out, r, s.limiter.available())
	}
	if obj.VersionList[0].Archives[0].MirrorURLs[0] != "https://mirror.test/1.2.3.zip" {
		t.Fatal("support preflight mutated source")
	}
	// A later usable entry is discoverable even if the first has a fixed URL.
	obj.VersionList = append(obj.VersionList, manifest.Version{Version: "1.2.2", Archives: []manifest.Archive{{URL: "https://github.com/sample/tool/releases/download/1.2.2/tool.zip"}}})
	if !s.registry.sniffers[4].Supports(obj) {
		t.Fatal("valid later template was skipped")
	}
	// Successful empty query remains success and spends precisely one attempt.
	fake, fixture := outcomeFixture(t, "empty")
	if err := fake.sniffNowInternal(context.Background(), "empty", false); err != nil {
		t.Fatal(err)
	}
	if fixture.count("empty") != 1 || outcomeCache(t, fake).Tools["empty"].LastSuccess.IsZero() || fake.limiter.available() != 59 {
		t.Fatal("empty actual query confused with unsupported")
	}
}

func TestD08BarePathCommandIsUserOwned(t *testing.T) {
	const user = "0 12 * * * xdev sniff --all --headless\n"
	cron, path := cronFixture(t, user)
	cron.executable = func() (string, error) { return "/usr/local/bin/xdev", nil }
	if err := cron.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if readCronFixture(t, path) != user {
		t.Fatal("bare user command removed")
	}
	if err := cron.Install(""); err != nil {
		t.Fatal(err)
	}
	if err := cron.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if readCronFixture(t, path) != user {
		t.Fatal("roundtrip changed user PATH command")
	}
}

func TestD13MergeUpdatesEffectiveThirdPartySource(t *testing.T) {
	s, f := outcomeFixture(t, "addon")
	root := filepath.Join(s.manifestDir, "third-party")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: addon\nlatest: 0.1.0\n")
	if err := os.WriteFile(filepath.Join(s.manifestDir, "addon.yaml"), original, 0600); err != nil {
		t.Fatal(err)
	}
	writeToolYAML(t, root, "addon", "name: addon\nlatest: 1.0.0\nversions:\n  - version: 1.0.0\n")
	f.fn = func(context.Context, *manifest.Tool) ([]DiscoveredVersion, error) {
		return []DiscoveredVersion{{Version: "2.0.0"}}, nil
	}
	if err := s.SniffNow(context.Background(), "addon"); err != nil {
		t.Fatal(err)
	}
	cat, err := s.catalog()
	if err != nil {
		t.Fatal(err)
	}
	obj, _ := cat.Tool("addon")
	if obj.Latest != "2.0.0" || outcomeCache(t, s).Tools["addon"].Status != SniffSuccess {
		t.Fatalf("effective source not updated %+v", obj)
	}
	got, err := os.ReadFile(filepath.Join(s.manifestDir, "addon.yaml"))
	if err != nil || string(got) != string(original) {
		t.Fatal("root shadow changed")
	}
}
