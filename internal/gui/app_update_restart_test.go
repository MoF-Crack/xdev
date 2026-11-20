package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/update"
)

type restartSessionFixture struct {
	discarded  int
	restarted  int
	restartErr error
	options    update.RestartOptions
}

func (s *restartSessionFixture) Discard() error { s.discarded++; return nil }
func (s *restartSessionFixture) Restart(ctx context.Context, opts update.RestartOptions) error {
	s.restarted++
	s.options = opts
	return s.restartErr
}

type restartDesktopFixture struct {
	*guiUpdateDesktop
	quits int
}

func (d *restartDesktopFixture) Quit() { d.quits++ }

func restartAppFixture(t *testing.T) (*App, *restartDesktopFixture, *restartSessionFixture) {
	t.Helper()
	desktop := &restartDesktopFixture{guiUpdateDesktop: &guiUpdateDesktop{}}
	app := &App{rootCtx: context.Background(), desktop: desktop, cfg: &config.Config{Core: config.CoreConfig{XMDevDir: t.TempDir()}}}
	return app, desktop, &restartSessionFixture{}
}

func TestPrepareUpdateStagesWithoutInstallingAndReusesReadyDownload(t *testing.T) {
	app, desktop, pending := restartAppFixture(t)
	target := filepath.Join(t.TempDir(), "xdev.exe")
	if err := os.WriteFile(target, []byte("old executable"), 0600); err != nil {
		t.Fatal(err)
	}
	checks, prepares := 0, 0
	check := func(context.Context, string) (*update.CheckResult, error) { checks++; return guiUpdateFixture(), nil }
	prepare := func(ctx context.Context, r *update.CheckResult, exe string, report func(update.Progress)) (preparedUpdateSession, error) {
		prepares++
		if exe != target {
			t.Fatalf("wrong preparation target: %s", exe)
		}
		report(update.Progress{Phase: "downloading", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99})
		report(update.Progress{Phase: "ready", DownloadedBytes: 1000, TotalBytes: 1000, Percent: 99})
		return pending, nil
	}
	view := app.prepareUpdateWith(check, func() string { return target }, prepare)
	if view.Error != "" || !view.Ready || view.Applied || view.Restarting || !view.HasUpdate {
		t.Fatalf("download was misreported as installation: %+v", view)
	}
	bytes, err := os.ReadFile(target)
	if err != nil || string(bytes) != "old executable" {
		t.Fatalf("preparation modified current executable: %q %v", bytes, err)
	}
	if desktop.quits != 0 || pending.restarted != 0 {
		t.Fatal("download exited or armed restart without confirmation")
	}
	for _, event := range desktop.snapshot() {
		if event.data.(map[string]interface{})["applied"] == true {
			t.Fatal("download progress claimed installation")
		}
	}
	second := app.prepareUpdateWith(check, func() string { t.Fatal("ready download resolved a new target"); return "" }, prepare)
	if !second.Ready || checks != 1 || prepares != 1 {
		t.Fatalf("prepared download was repeated: checks=%d prepares=%d view=%+v", checks, prepares, second)
	}
	// The public check must reopen the verified candidate rather than replacing
	// it with an unrelated new network snapshot.
	if view = app.CheckForUpdate(); !view.Ready || view.AssetName != second.AssetName {
		t.Fatalf("check lost prepared selection: %+v", view)
	}
	app.discardPreparedUpdate()
	if pending.discarded != 1 {
		t.Fatal("unused staged payload was not discarded")
	}
}

func TestRestartUpdateArmsHelperBeforeQuitAndRetriesWithoutDownload(t *testing.T) {
	app, desktop, pending := restartAppFixture(t)
	app.noEnvWrite = true
	app.preparedUpdate = pending
	app.preparedView = guiUpdateResult(guiUpdateFixture())
	app.preparedView.Ready = true
	pending.restartErr = errors.New("helper could not start")
	var scheduled []func()
	schedule := func(fn func()) { scheduled = append(scheduled, fn) }
	failed := app.restartPreparedUpdateWith(schedule)
	if failed.Error == "" || !failed.Ready || failed.Applied || failed.Restarting || len(scheduled) != 0 || desktop.quits != 0 {
		t.Fatalf("failed handoff quit or lost ready payload: %+v", failed)
	}
	pending.restartErr = nil
	accepted := app.restartPreparedUpdateWith(schedule)
	if accepted.Error != "" || !accepted.Ready || !accepted.Restarting || accepted.Applied || pending.restarted != 2 || len(scheduled) != 1 || desktop.quits != 0 {
		t.Fatalf("restart handoff/order: %+v calls=%d scheduled=%d", accepted, pending.restarted, len(scheduled))
	}
	want := []string{"--xmdevenv-dir", app.cfg.Core.XMDevDir, "--no-path-fix", "gui"}
	if !reflect.DeepEqual(pending.options.Args, want) || pending.options.WorkingDir == "" {
		t.Fatalf("restart lost home/environment policy: %+v", pending.options)
	}
	app.restartPreparedUpdateWith(schedule)
	if pending.restarted != 2 || len(scheduled) != 1 {
		t.Fatal("repeated click launched another helper")
	}
	app.discardPreparedUpdate()
	if pending.discarded != 0 {
		t.Fatal("shutdown deleted an armed helper's files")
	}
	scheduled[0]()
	if desktop.quits != 1 {
		t.Fatal("confirmed restart did not gracefully quit")
	}
}

func TestRestartWithoutPreparedPayloadOrDesktopDoesNotQuit(t *testing.T) {
	app, desktop, pending := restartAppFixture(t)
	schedule := func(func()) { t.Fatal("invalid restart scheduled quit") }
	if got := app.restartPreparedUpdateWith(schedule); got.Error == "" || got.Ready {
		t.Fatalf("missing payload accepted: %+v", got)
	}
	app.preparedUpdate = pending
	app.preparedView = guiUpdateResult(guiUpdateFixture())
	app.preparedView.Ready = true
	app.desktop = &guiUpdateDesktop{}
	if got := app.restartPreparedUpdateWith(schedule); got.Error == "" || !got.Ready || got.Restarting {
		t.Fatalf("missing quit support accepted: %+v", got)
	}
	if pending.restarted != 0 || desktop.quits != 0 {
		t.Fatal("helper started without a valid desktop quit path")
	}
	app.discardPreparedUpdate()
}

func TestPrepareFailureAndCancellationNeverBecomeReady(t *testing.T) {
	app, _, pending := restartAppFixture(t)
	got := app.prepareUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }, func() string { return "owned target" }, func(context.Context, *update.CheckResult, string, func(update.Progress)) (preparedUpdateSession, error) {
		return pending, errors.New("checksum mismatch")
	})
	if got.Error != "checksum mismatch" || got.Ready || got.Applied || app.preparedUpdate != nil || pending.discarded != 1 {
		t.Fatalf("preparation failure accepted: %+v pending=%+v", got, pending)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.rootCtx = ctx
	got = app.prepareUpdateWith(func(context.Context, string) (*update.CheckResult, error) {
		t.Fatal("cancelled app checked updates")
		return nil, nil
	}, func() string { return "" }, nil)
	if got.Error == "" || got.Ready || got.Applied {
		t.Fatalf("cancelled prepare accepted: %+v", got)
	}
}
