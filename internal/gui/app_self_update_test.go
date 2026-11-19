package gui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/update"
	"github.com/xmdemt/xdev/internal/version"
)

func guiUpdateFixture() *update.CheckResult {
	return &update.CheckResult{
		CurrentVersion: "1.0.0-alpha.3", LatestVersion: "1.0.0-beta.2", HasUpdate: true,
		Release: &update.ReleaseInfo{TagName: "v1.0.0-beta.2", Assets: []update.Asset{
			{Name: "xdev-windows-amd64-v1.0.0-beta.2.zip", BrowserDownloadURL: "https://example.test/releases/beta.2.zip", Size: 1000},
			{Name: "xdev-linux-amd64-v1.0.0-beta.2.tar.gz", BrowserDownloadURL: "https://example.test/releases/beta.2.tar.gz"},
			{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.test/releases/SHA256SUMS"},
			{Name: "xdev-unsafe.zip", BrowserDownloadURL: "javascript:alert(1)"},
		}},
	}
}

type guiUpdateEvent struct {
	name string
	data any
}
type guiUpdateDesktop struct {
	desktopRuntime
	mu     sync.Mutex
	events []guiUpdateEvent
}

func (d *guiUpdateDesktop) Emit(name string, data any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, guiUpdateEvent{name, data})
}
func (d *guiUpdateDesktop) snapshot() []guiUpdateEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]guiUpdateEvent(nil), d.events...)
}

func TestGUIUpdateCheckProjectsReleaseAndRootContext(t *testing.T) {
	type contextKey struct{}
	app := &App{rootCtx: context.WithValue(context.Background(), contextKey{}, "root")}
	result := app.checkForUpdateWith(func(ctx context.Context, current string) (*update.CheckResult, error) {
		if current != version.Version || ctx.Value(contextKey{}) != "root" {
			t.Fatalf("check inputs current=%q root=%v", current, ctx.Value(contextKey{}))
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 15*time.Second || time.Until(deadline) < 14*time.Second {
			t.Fatalf("check deadline must derive from the 15-second application timeout: %v %v", deadline, ok)
		}
		return guiUpdateFixture(), nil
	})
	if !result.HasUpdate || result.Applied || result.Error != "" || result.Current != "1.0.0-alpha.3" || result.Latest != "1.0.0-beta.2" {
		t.Fatalf("unexpected check result: %+v", result)
	}
	if len(result.DownloadURLs) != 2 || result.DownloadURLs["xdev-windows-amd64-v1.0.0-beta.2.zip"] != "https://example.test/releases/beta.2.zip" {
		t.Fatalf("archive links must come from actual selected release assets: %+v", result.DownloadURLs)
	}
}

func TestGUIUpdateCheckErrorIsNotUpToDate(t *testing.T) {
	app := &App{}
	result := app.checkForUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return nil, errors.New("network offline") })
	if result.Error != "network offline" || result.HasUpdate || result.Applied || result.Current != version.Version {
		t.Fatalf("check failure must stay explicit: %+v", result)
	}
	result = app.checkForUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return nil, nil })
	if result.Error == "" || result.Applied {
		t.Fatalf("an invalid result must not become up-to-date: %+v", result)
	}
}

func TestGUIUpdateApplyRefetchNoUpdateDoesNotInstall(t *testing.T) {
	app := &App{}
	checks, targets, applies := 0, 0, 0
	result := app.applyUpdateWith(
		func(context.Context, string) (*update.CheckResult, error) {
			checks++
			return &update.CheckResult{CurrentVersion: "1.0.0-beta.2", LatestVersion: "1.0.0-beta.2", HasUpdate: false}, nil
		},
		func() string { targets++; return "" },
		func(context.Context, *update.CheckResult, string, func(update.Progress)) error { applies++; return nil },
	)
	if checks != 1 || targets != 0 || applies != 0 || result.HasUpdate || result.Applied || result.Error != "" || result.Latest != "1.0.0-beta.2" {
		t.Fatalf("no-update refetch must not resolve/replace a binary: checks=%d targets=%d applies=%d result=%+v", checks, targets, applies, result)
	}
}

func TestGUIUpdateApplyUsesRefetchedResultAndForwardsExactProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desktop := &guiUpdateDesktop{}
	app := &App{rootCtx: ctx, desktop: desktop}
	refetched := guiUpdateFixture()
	refetched.LatestVersion = "1.0.0-beta.3" // Never claim the previously displayed release was applied.
	progress := []update.Progress{
		{Phase: "downloading", DownloadedBytes: 320, TotalBytes: 1000, Percent: 32},
		{Phase: "verifying", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99},
		{Phase: "extracting", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99},
		{Phase: "installing", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99},
		{Phase: "completed", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 100},
	}
	applies := 0
	result := app.applyUpdateWith(
		func(context.Context, string) (*update.CheckResult, error) { return refetched, nil },
		func() string { return "fixture/xdev.exe" },
		func(ctx context.Context, res *update.CheckResult, target string, onProgress func(update.Progress)) error {
			applies++
			if res != refetched || target != "fixture/xdev.exe" || onProgress == nil {
				t.Fatalf("apply must receive the actual second-check result and target: %+v %q", res, target)
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 10*time.Minute || time.Until(deadline) < 9*time.Minute {
				t.Fatalf("apply must use the application-derived 10-minute context: %v", deadline)
			}
			for _, p := range progress {
				onProgress(p)
			}
			return nil
		},
	)
	if applies != 1 || !result.HasUpdate || !result.Applied || result.Error != "" || result.Latest != "1.0.0-beta.3" {
		t.Fatalf("only actual application may report applied: %+v calls=%d", result, applies)
	}
	events := desktop.snapshot()
	if len(events) != len(progress) {
		t.Fatalf("wanted %d progress snapshots, got %d", len(progress), len(events))
	}
	for i, event := range events {
		if event.name != "self-update" {
			t.Fatalf("event binding changed: %q", event.name)
		}
		want := map[string]interface{}{"action": "progress", "source": "manual", "current": refetched.CurrentVersion, "latest": refetched.LatestVersion, "applied": false,
			"phase": progress[i].Phase, "downloadedBytes": progress[i].DownloadedBytes, "totalBytes": progress[i].TotalBytes, "percent": progress[i].Percent}
		if !reflect.DeepEqual(event.data, want) {
			t.Fatalf("progress %d was estimated or changed: got=%+v want=%+v", i, event.data, want)
		}
		data, err := json.Marshal(event.data)
		if err != nil || !strings.Contains(string(data), `"downloadedBytes"`) || !strings.Contains(string(data), `"totalBytes"`) {
			t.Fatalf("progress JSON contract: %s %v", data, err)
		}
	}
}

func TestGUIUpdateStartupProgressHasDistinctSource(t *testing.T) {
	desktop := &guiUpdateDesktop{}
	app := &App{desktop: desktop}
	app.emitSelfUpdateProgress(guiUpdateFixture(), update.Progress{Phase: "downloading", DownloadedBytes: 320, TotalBytes: 1000, Percent: 32}, "startup")
	events := desktop.snapshot()
	if len(events) != 1 || events[0].data.(map[string]interface{})["source"] != "startup" {
		t.Fatalf("startup progress must be distinguishable from delayed manual progress: %+v", events)
	}
}

func TestGUIUpdateApplyFailureCanRetryAndNeverClaimsApplied(t *testing.T) {
	app := &App{desktop: &guiUpdateDesktop{}}
	check := func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }
	target := func() string { return "fixture/xdev.exe" }
	result := app.applyUpdateWith(check, target, func(_ context.Context, _ *update.CheckResult, _ string, progress func(update.Progress)) error {
		progress(update.Progress{Phase: "verifying", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99})
		return errors.New("checksum mismatch")
	})
	if result.Error != "checksum mismatch" || result.Applied || !result.HasUpdate {
		t.Fatalf("partial download is not application success: %+v", result)
	}
	for _, event := range app.desktop.(*guiUpdateDesktop).snapshot() {
		if event.data.(map[string]interface{})["phase"] == "completed" {
			t.Fatal("GUI must not manufacture completed progress on errors")
		}
	}
	result = app.applyUpdateWith(check, target, func(context.Context, *update.CheckResult, string, func(update.Progress)) error { return nil })
	if result.Error != "" || !result.Applied {
		t.Fatalf("failed update did not release its guard for retry: %+v", result)
	}
}

func TestGUIUpdateApplyInvalidResultAndMissingTargetAreErrors(t *testing.T) {
	app := &App{}
	apply := func(context.Context, *update.CheckResult, string, func(update.Progress)) error {
		t.Error("invalid check/target must not apply")
		return nil
	}
	for _, invalid := range []*update.CheckResult{nil, {HasUpdate: true, CurrentVersion: "old"}} {
		result := app.applyUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return invalid, nil }, func() string { return "fixture" }, apply)
		if result.Error == "" || result.Applied {
			t.Fatalf("invalid check result became success: %+v", result)
		}
	}
	result := app.applyUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }, func() string { return "" }, apply)
	if result.Error == "" || result.Applied || !result.HasUpdate {
		t.Fatalf("missing target must retain the update and report an error: %+v", result)
	}
}

func TestGUIUpdateGuardRejectsConcurrentManualAndStartupWork(t *testing.T) {
	app := &App{}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	done := make(chan UpdateCheckResult, 1)
	go func() {
		done <- app.applyUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }, func() string { return "fixture" },
			func(context.Context, *update.CheckResult, string, func(update.Progress)) error {
				close(started)
				<-release
				return nil
			})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first apply never started")
	}
	// Public bindings must reject before network access or binary replacement.
	for _, result := range []UpdateCheckResult{app.CheckForUpdate(), app.ApplyUpdate()} {
		if result.Error == "" || result.Applied || !strings.Contains(result.Error, "in progress") {
			t.Fatalf("concurrent public update must fail busy, not queue a second apply: %+v", result)
		}
	}
	if guiSelfUpdateMu.TryLock() {
		guiSelfUpdateMu.Unlock()
		t.Fatal("startup guard did not share the active manual update lock")
	}
	unblock()
	select {
	case result := <-done:
		if !result.Applied {
			t.Fatalf("first update failed: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("first update failed to finish")
	}
	result := app.checkForUpdateWith(func(context.Context, string) (*update.CheckResult, error) {
		return &update.CheckResult{HasUpdate: false}, nil
	})
	if result.Error != "" {
		t.Fatalf("guard was not released: %+v", result)
	}
}

func TestGUIUpdateRootCancellationStopsCheckAndApply(t *testing.T) {
	for _, stage := range []string{"check", "apply"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			app := &App{rootCtx: ctx}
			started := make(chan struct{})
			done := make(chan UpdateCheckResult, 1)
			check := func(ctx context.Context, _ string) (*update.CheckResult, error) {
				if stage == "check" {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return guiUpdateFixture(), nil
			}
			go func() {
				if stage == "check" {
					done <- app.checkForUpdateWith(check)
					return
				}
				done <- app.applyUpdateWith(check, func() string { return "fixture" }, func(ctx context.Context, _ *update.CheckResult, _ string, _ func(update.Progress)) error {
					close(started)
					<-ctx.Done()
					return ctx.Err()
				})
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("update did not start")
			}
			cancel()
			select {
			case result := <-done:
				if result.Applied || !strings.Contains(result.Error, context.Canceled.Error()) {
					t.Fatalf("root cancellation was lost: %+v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("update ignored the application root cancellation")
			}
		})
	}
}

func TestUpdateCheckResultAppliedJSONIsAlwaysExplicit(t *testing.T) {
	for _, applied := range []bool{false, true} {
		data, err := json.Marshal(UpdateCheckResult{HasUpdate: true, Current: "old", Latest: "new", Applied: applied})
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]interface{}
		if err := json.Unmarshal(data, &view); err != nil {
			t.Fatal(err)
		}
		if value, ok := view["applied"].(bool); !ok || value != applied {
			t.Fatalf("explicit applied JSON missing: %s", data)
		}
		if _, ok := view["error"]; ok {
			t.Fatalf("empty error should remain omitted: %s", data)
		}
	}
}
