package gui

import (
	"context"
	"fmt"
	"testing"

	"github.com/xmdemt/xdev/internal/update"
)

func TestRestartInvalidPreparedPayloadAllowsFreshDownload(t *testing.T) {
	app, desktop, pending := restartAppFixture(t)
	app.preparedUpdate = pending
	app.preparedView = guiUpdateResult(guiUpdateFixture())
	app.preparedView.Ready = true
	pending.restartErr = fmt.Errorf("%w: staged executable changed", update.ErrPreparedUpdateInvalid)
	got := app.restartPreparedUpdateWith(func(func()) { t.Fatal("invalid payload scheduled GUI exit") })
	if got.Error == "" || got.Ready || got.Restarting || got.Applied || app.preparedUpdate != nil || pending.discarded != 1 || desktop.quits != 0 {
		t.Fatalf("invalid payload remained ready: view=%+v session=%+v", got, pending)
	}
	fresh := &restartSessionFixture{}
	got = app.prepareUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return guiUpdateFixture(), nil }, func() string { return "owned target" }, func(context.Context, *update.CheckResult, string, func(update.Progress)) (preparedUpdateSession, error) {
		return fresh, nil
	})
	if got.Error != "" || !got.Ready || app.preparedUpdate != fresh {
		t.Fatalf("could not prepare again after invalidation: %+v", got)
	}
	app.discardPreparedUpdate()
}

func TestRestartCancelledAppNeverArmsHelper(t *testing.T) {
	app, desktop, pending := restartAppFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.rootCtx = ctx
	app.preparedUpdate = pending
	app.preparedView = guiUpdateResult(guiUpdateFixture())
	app.preparedView.Ready = true
	got := app.restartPreparedUpdateWith(func(func()) { t.Fatal("cancelled app scheduled quit") })
	if got.Error == "" || got.Restarting || pending.restarted != 0 || desktop.quits != 0 {
		t.Fatalf("cancelled app started a helper: %+v", got)
	}
	app.discardPreparedUpdate()
}
