// app_selfcheck.go — startup and manual updates share the same installation
// lock, release selection, progress envelope and authoritative terminal result.
package gui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/xmdemt/xdev/internal/update"
)

const selfUpdateEvent = "self-update"

// runStartupSelfCheck is called asynchronously after the desktop is ready.
func (a *App) runStartupSelfCheck() {
	if runtime.GOOS != "windows" {
		a.runStartupSelfCheckWith(update.NewPolicy(), cliBinaryNextToSelf, update.ApplyToWithProgress)
		return
	}
	a.runStartupSelfCheckWith(update.NewPolicy(), cliBinaryNextToSelf, func(ctx context.Context, result *update.CheckResult, target string, report func(update.Progress)) error {
		view := guiUpdateResult(result)
		if view.Error != "" {
			return fmt.Errorf("%s", view.Error)
		}
		pending, err := update.PrepareForRestart(ctx, result, target, report)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			_ = pending.Discard()
			return err
		}
		view.Ready = true
		view.Applied = false
		a.preparedUpdate = pending
		a.preparedView = view
		return nil
	})
}

// Dependencies stay private: tests exercise the real adapter without replacing
// the test executable or performing a network request.
func (a *App) runStartupSelfCheckWith(
	policy *update.Policy,
	target func() string,
	apply func(context.Context, *update.CheckResult, string, func(update.Progress)) error,
) {
	cfg := a.getCfg()
	if cfg == nil || !guiSelfUpdateMu.TryLock() {
		return
	}
	defer guiSelfUpdateMu.Unlock()
	if a.preparedUpdate != nil {
		return
	}
	// Discovery has its own 15-second bound; an automatic download has the same
	// 10-minute bound as a manual apply, rather than a 30-second startup timer.
	ctx, closeNetwork := update.WithProxy(a.rootContext(), cfg.Proxy)
	defer closeNetwork()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if ctx.Err() != nil {
		return
	}

	act, res := policy.CheckPolicy(ctx, &cfg.Core, cfg.Core.CacheDir)
	if ctx.Err() != nil || res == nil {
		return
	}
	switch act {
	case update.ActionAsk:
		a.emitSelfUpdate("ask", res)
	case update.ActionNotify:
		a.emitSelfUpdate("notify", res)
	case update.ActionUpdate:
		exe := target()
		if exe == "" {
			a.emitSelfUpdate("applied-error", res, fmt.Errorf("cannot locate the XDev executable to update"))
			return
		}
		logInfo("[self-update] auto-updating XDev %s -> %s", exe, res.LatestVersion)
		if err := apply(ctx, res, exe, func(progress update.Progress) {
			a.emitSelfUpdateProgress(res, progress, "startup")
		}); err != nil {
			logWarn("[self-update] auto update failed: %v", err)
			a.emitSelfUpdate("applied-error", res, err)
			return
		}
		if a.preparedUpdate != nil && a.preparedView.Ready {
			a.emitPreparedUpdate()
		} else {
			a.emitSelfUpdate("applied", res)
		}
	}
}

// CLI, GUI and shim dispatch use the same distributed executable.
func cliBinaryNextToSelf() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func (a *App) emitSelfUpdate(action string, res *update.CheckResult, failures ...error) {
	if res == nil {
		return
	}
	view := guiUpdateResult(res)
	payload := map[string]interface{}{
		"action":       action,
		"source":       "startup",
		"current":      view.Current,
		"latest":       view.Latest,
		"platform":     view.Platform,
		"assetName":    view.AssetName,
		"hasUpdate":    view.HasUpdate,
		"applied":      action == "applied",
		"downloadUrls": view.DownloadURLs,
	}
	if view.Error != "" {
		payload["action"] = "applied-error"
		payload["applied"] = false
		payload["error"] = view.Error
	}
	if len(failures) > 0 && failures[0] != nil {
		payload["error"] = failures[0].Error()
	}
	a.emitEvent(selfUpdateEvent, payload)
}

func (a *App) IgnoreSelfUpdate(latest string) {
	cfg := a.getCfg()
	if cfg == nil {
		return
	}
	if err := update.IgnoreVersion(cfg.Core.CacheDir, latest); err != nil {
		logWarn("[self-update] record ignore %s failed: %v", latest, err)
	}
}
