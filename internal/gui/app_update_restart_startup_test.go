package gui

import (
	"context"
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/update"
)

func TestStartupPreparedDownloadRequestsRestartWithoutClaimingApplied(t *testing.T) {
	app, desktop, policy := startupUpdateFixture(t, "auto")
	pending := &restartSessionFixture{}
	app.runStartupSelfCheckWith(policy, func() string { return "owned target" }, func(ctx context.Context, r *update.CheckResult, target string, report func(update.Progress)) error {
		report(update.Progress{Phase: "downloading", DownloadedBytes: 100, TotalBytes: 100, Percent: 99})
		report(update.Progress{Phase: "ready", DownloadedBytes: 100, TotalBytes: 100, Percent: 99})
		app.preparedUpdate = pending
		app.preparedView = guiUpdateResult(r)
		app.preparedView.Ready = true
		return nil
	})
	events := desktop.snapshot()
	if len(events) != 3 {
		t.Fatalf("unexpected events: %+v", events)
	}
	for _, e := range events {
		data := e.data.(map[string]interface{})
		if data["applied"] == true || data["action"] == "applied" {
			t.Fatalf("startup preparation reported installation: %+v", data)
		}
	}
	last := events[len(events)-1].data.(map[string]interface{})
	if last["action"] != "ready" || last["ready"] != true || last["restarting"] != false || pending.restarted != 0 {
		t.Fatalf("startup download did not wait for confirmation: %+v", last)
	}
	app.discardPreparedUpdate()
}

func TestRestartRollbackErrorIsReportedOnceWithoutOfferingInstalledState(t *testing.T) {
	t.Setenv("XDEV_SELF_UPDATE_RESTART_ERROR", "replacement failed; previous executable restored")
	app, desktop, _ := restartAppFixture(t)
	app.reportRestartFailure()
	if os.Getenv("XDEV_SELF_UPDATE_RESTART_ERROR") != "" {
		t.Fatal("rollback error would be inherited by every future restart")
	}
	events := desktop.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected rollback event: %+v", events)
	}
	data := events[0].data.(map[string]interface{})
	if data["action"] != "applied-error" || data["applied"] != false || data["ready"] != false || data["hasUpdate"] != false || data["error"] == "" {
		t.Fatalf("rollback was misreported: %+v", data)
	}
	app.reportRestartFailure()
	if len(desktop.snapshot()) != 1 {
		t.Fatal("rollback error was emitted twice")
	}
}
