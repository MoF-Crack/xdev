package gui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/update"
)

func startupUpdateFixture(t *testing.T, mode string) (*App, *guiUpdateDesktop, *update.Policy) {
	t.Helper()
	desktop := &guiUpdateDesktop{}
	app := &App{cfg: &config.Config{Core: config.CoreConfig{SelfUpdate: true, SelfUpdateMode: mode, CacheDir: t.TempDir()}}, desktop: desktop, rootCtx: context.Background()}
	policy := &update.Policy{Now: time.Now, Check: func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }}
	return app, desktop, policy
}

func TestStartupUpdateSharesGuardAndPublishesExactProgressAndResult(t *testing.T) {
	app, desktop, policy := startupUpdateFixture(t, "auto")
	app.runStartupSelfCheckWith(policy, func() string { return "owned-xdev" }, func(ctx context.Context, result *update.CheckResult, exe string, report func(update.Progress)) error {
		if exe != "owned-xdev" || result.LatestVersion != "1.0.0-beta.2" {
			t.Fatalf("wrong target/result: %s %+v", exe, result)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 9*time.Minute {
			t.Fatal("automatic download still uses the old 30-second bound")
		}
		manual := app.checkForUpdateWith(func(context.Context, string) (*update.CheckResult, error) {
			t.Fatal("concurrent manual operation ran")
			return nil, nil
		})
		if manual.Error == "" {
			t.Fatal("manual operation did not share startup guard")
		}
		report(update.Progress{Phase: "downloading", DownloadedBytes: 400, TotalBytes: 1000, Percent: 40})
		report(update.Progress{Phase: "completed", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 100})
		return nil
	})
	events := desktop.snapshot()
	if len(events) != 3 {
		t.Fatalf("expected progress+progress+terminal, got %+v", events)
	}
	first := events[0].data.(map[string]interface{})
	if first["source"] != "startup" || first["phase"] != "downloading" || first["percent"] != 40 || first["downloadedBytes"] != int64(400) {
		t.Fatalf("progress changed: %+v", first)
	}
	last := events[2].data.(map[string]interface{})
	if last["action"] != "applied" || last["applied"] != true || last["source"] != "startup" {
		t.Fatalf("no authoritative terminal result: %+v", last)
	}
	if !guiSelfUpdateMu.TryLock() {
		t.Fatal("startup leaked guard")
	}
	guiSelfUpdateMu.Unlock()
}

func TestStartupUpdateFailuresRemainRealAndNeverApplied(t *testing.T) {
	for _, missingTarget := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply-failure", true: "missing-target"}[missingTarget], func(t *testing.T) {
			app, desktop, policy := startupUpdateFixture(t, "auto")
			app.runStartupSelfCheckWith(policy, func() string {
				if missingTarget {
					return ""
				}
				return "owned-xdev"
			}, func(context.Context, *update.CheckResult, string, func(update.Progress)) error {
				return errors.New("checksum verification failed")
			})
			events := desktop.snapshot()
			if len(events) != 1 {
				t.Fatalf("unexpected events: %+v", events)
			}
			payload := events[0].data.(map[string]interface{})
			if payload["action"] != "applied-error" || payload["applied"] != false || payload["error"] == "" {
				t.Fatalf("failure was masked: %+v", payload)
			}
			if !missingTarget && payload["error"] != "checksum verification failed" {
				t.Fatal("real failure discarded")
			}
		})
	}
}

func TestStartupPromptIncludesDownloadsWithoutInstalling(t *testing.T) {
	for _, mode := range []string{"prompt", "notify"} {
		t.Run(mode, func(t *testing.T) {
			app, desktop, policy := startupUpdateFixture(t, mode)
			app.runStartupSelfCheckWith(policy, func() string { t.Fatal("prompt resolved target"); return "" }, func(context.Context, *update.CheckResult, string, func(update.Progress)) error {
				t.Fatal("prompt installed")
				return nil
			})
			payload := desktop.snapshot()[0].data.(map[string]interface{})
			if payload["applied"] != false || payload["source"] != "startup" || !strings.Contains(payload["latest"].(string), "beta.2") {
				t.Fatalf("invalid prompt: %+v", payload)
			}
			selected, err := update.SelectedAsset(guiUpdateFixture())
			if err != nil {
				t.Fatal(err)
			}
			urls := payload["downloadUrls"].(map[string]string)
			if len(urls) != 1 || urls[selected.Name] != selected.BrowserDownloadURL || payload["assetName"] != selected.Name || payload["platform"] != update.TargetPlatform() {
				t.Fatalf("startup prompt must identify only its own installable platform: %+v", payload)
			}
		})
	}
}

func TestStartupUpdateSkipsWhenManualGuardHeldOrRootCancelled(t *testing.T) {
	app, desktop, policy := startupUpdateFixture(t, "auto")
	policy.Check = func(context.Context, string) (*update.CheckResult, error) {
		t.Fatal("startup checked while unavailable")
		return nil, nil
	}
	if !guiSelfUpdateMu.TryLock() {
		t.Fatal("unexpected busy fixture")
	}
	app.runStartupSelfCheckWith(policy, func() string { t.Fatal("target called"); return "" }, nil)
	guiSelfUpdateMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.rootCtx = ctx
	app.runStartupSelfCheckWith(policy, func() string { t.Fatal("cancelled target called"); return "" }, nil)
	if len(desktop.snapshot()) != 0 {
		t.Fatal("cancelled startup emitted false success")
	}
}
