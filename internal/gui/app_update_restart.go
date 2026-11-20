package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/xmdemt/xdev/internal/update"
	"github.com/xmdemt/xdev/internal/version"
)

type preparedUpdateSession interface {
	Discard() error
	Restart(context.Context, update.RestartOptions) error
}

type prepareUpdateFunc func(context.Context, *update.CheckResult, string, func(update.Progress)) (preparedUpdateSession, error)

// PrepareUpdate downloads and verifies an update without replacing the running
// executable. Windows installation is committed only after RestartToUpdate.
func (a *App) PrepareUpdate() UpdateCheckResult {
	if runtime.GOOS != "windows" {
		return a.ApplyUpdate()
	}
	return a.prepareUpdateWith(update.Check, cliBinaryNextToSelf, func(ctx context.Context, result *update.CheckResult, target string, report func(update.Progress)) (preparedUpdateSession, error) {
		return update.PrepareForRestart(ctx, result, target, report)
	})
}

func (a *App) prepareUpdateWith(check func(context.Context, string) (*update.CheckResult, error), target func() string, prepare prepareUpdateFunc) UpdateCheckResult {
	if !guiSelfUpdateMu.TryLock() {
		return UpdateCheckResult{Current: version.Version, Error: "an XDev self-update operation is already in progress"}
	}
	defer guiSelfUpdateMu.Unlock()
	if a.preparedUpdate != nil {
		return a.preparedView
	}
	ctx, closeNetwork := a.selfUpdateContext(a.rootContext())
	defer closeNetwork()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return UpdateCheckResult{Current: version.Version, Error: err.Error()}
	}
	result, err := check(ctx, version.Version)
	if err != nil {
		return UpdateCheckResult{Current: version.Version, Error: err.Error()}
	}
	view := guiUpdateResult(result)
	if view.Error != "" || !view.HasUpdate {
		return view
	}
	exe := target()
	if exe == "" {
		view.Error = "cannot locate the XDev executable to update"
		return view
	}
	pending, err := prepare(ctx, result, exe, func(p update.Progress) { a.emitSelfUpdateProgress(result, p) })
	if err != nil {
		if pending != nil {
			_ = pending.Discard()
		}
		view.Error = err.Error()
		return view
	}
	if pending == nil {
		view.Error = "update preparation returned no verified payload"
		return view
	}
	if err := ctx.Err(); err != nil {
		_ = pending.Discard()
		view.Error = err.Error()
		return view
	}
	view.Ready = true
	view.Applied = false
	a.preparedUpdate = pending
	a.preparedView = view
	return view
}

// RestartToUpdate arms an independent helper before requesting a graceful quit.
// No frontend WindowClose call is allowed to race ahead of helper readiness.
func (a *App) RestartToUpdate() UpdateCheckResult {
	return a.restartPreparedUpdateWith(func(quit func()) { time.AfterFunc(250*time.Millisecond, quit) })
}

func (a *App) restartPreparedUpdateWith(scheduleQuit func(func())) UpdateCheckResult {
	if !guiSelfUpdateMu.TryLock() {
		return UpdateCheckResult{Current: version.Version, Error: "an XDev self-update operation is already in progress"}
	}
	defer guiSelfUpdateMu.Unlock()
	if a.preparedUpdate == nil {
		return UpdateCheckResult{Current: version.Version, Error: "no verified update is ready; download the update first"}
	}
	view := a.preparedView
	if a.restartScheduled {
		return view
	}
	quit, ok := a.desktop.(interface{ Quit() })
	if !ok {
		view.Error = "restarting requires a desktop session"
		return view
	}
	cfg := a.getCfg()
	if cfg == nil || cfg.Core.XMDevDir == "" {
		view.Error = "cannot restart without the current XDev home"
		return view
	}
	cwd, err := os.Getwd()
	if err != nil {
		view.Error = fmt.Sprintf("resolve restart directory: %v", err)
		return view
	}
	args := []string{"--xmdevenv-dir", cfg.Core.XMDevDir}
	if a.noEnvWrite {
		args = append(args, "--no-path-fix")
	}
	args = append(args, "gui")
	ctx, cancel := context.WithTimeout(a.rootContext(), 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		view.Error = err.Error()
		return view
	}
	if err := a.preparedUpdate.Restart(ctx, update.RestartOptions{Args: args, WorkingDir: cwd}); err != nil {
		view.Error = err.Error()
		if errors.Is(err, update.ErrPreparedUpdateInvalid) {
			_ = a.preparedUpdate.Discard()
			a.preparedUpdate = nil
			a.preparedView = UpdateCheckResult{}
			view.Ready = false
		}
		return view
	}
	view.Error = ""
	view.Restarting = true
	view.Ready = true
	a.preparedView = view
	a.restartScheduled = true
	scheduleQuit(quit.Quit)
	return view
}

// Closing the app normally discards an unused download; an armed helper owns
// its staging directory and must outlive the old GUI's shutdown cleanup.
func (a *App) discardPreparedUpdate() {
	guiSelfUpdateMu.Lock()
	defer guiSelfUpdateMu.Unlock()
	if a.preparedUpdate != nil && !a.restartScheduled {
		if err := a.preparedUpdate.Discard(); err != nil {
			logWarn("[self-update] discard pending update: %v", err)
		}
		a.preparedUpdate = nil
		a.preparedView = UpdateCheckResult{}
	}
}

func (a *App) emitPreparedUpdate() {
	view := a.preparedView
	a.emitEvent(selfUpdateEvent, map[string]interface{}{"action": "ready", "source": "startup", "current": view.Current, "latest": view.Latest, "platform": view.Platform, "assetName": view.AssetName, "downloadUrls": view.DownloadURLs, "hasUpdate": true, "applied": false, "ready": true, "restarting": false})
}

func (a *App) reportRestartFailure() {
	if message := os.Getenv("XDEV_SELF_UPDATE_RESTART_ERROR"); message != "" {
		_ = os.Unsetenv("XDEV_SELF_UPDATE_RESTART_ERROR")
		logWarn("[self-update] restart/install failed: %s", message)
		a.emitEvent(selfUpdateEvent, map[string]interface{}{"action": "applied-error", "source": "startup", "current": version.Version, "latest": version.Version, "hasUpdate": false, "applied": false, "ready": false, "restarting": false, "error": message})
	}
}
