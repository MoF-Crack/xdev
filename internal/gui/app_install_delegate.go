package gui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/util"
)

// guiDownloadFunc delegates only the transfer. The installer retains ownership
// of verification, extraction, force removal, activation and rollback.
func (a *App) guiDownloadFunc(taskID, toolName, displayVersion string) installer.DownloadFunc {
	return func(ctx context.Context, opts downloader.Options) error {
		opts.OnProgress = func(downloaded, total int64, elapsed time.Duration) {
			pct := -1.0
			if total > 0 {
				pct = float64(downloaded) / float64(total) * 100
			}
			a.emitInstallProgress(taskID, toolName, displayVersion, "Downloading...", pct,
				util.HumanSpeedStr(downloaded, elapsed), util.HumanETAStr(downloaded, total, elapsed))
		}
		opts.OnStatus = func(message string) {
			pct := -1.0
			if isTransferRestart(message) {
				pct = 0
			}
			a.emitInstallProgress(taskID, toolName, displayVersion, message, pct, "", "")
		}
		root := a.rootContext()
		if err := root.Err(); err != nil {
			return fmt.Errorf("installation stopped by app shutdown: %w", err)
		}
		request, stop := context.WithTimeout(ctx, time.Hour)
		defer stop()
		if err := a.dlMgr.StartDownloadContext(request, taskID, toolName, displayVersion, opts); err != nil {
			return err
		}
		finishCancelled := func(cause error) error {
			cleanupErr := a.dlMgr.Cancel(taskID)
			return errors.Join(cause, cleanupErr, a.dlMgr.RemoveTask(taskID))
		}
		if view, ok := a.installSessions.update(taskID, func(v *InstallSession) {
			if v.Status != "cancelling" {
				v.Status = "downloading"
				v.CanPause = true
			}
		}); ok {
			a.emitInstallSession(view)
		}
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-request.Done():
				return finishCancelled(request.Err())
			case <-root.Done():
				return finishCancelled(fmt.Errorf("installation stopped by app shutdown: %w", root.Err()))
			case <-ticker.C:
			}
			task, err := a.dlMgr.GetTask(taskID)
			if err != nil {
				return err
			}
			snap := task.GetSnapshot()
			if cause := request.Err(); cause != nil {
				return finishCancelled(cause)
			}
			switch snap.Status {
			case downloader.StatusCompleted:
				if view, ok := a.installSessions.update(taskID, func(v *InstallSession) { v.CanPause = false; v.Paused = false }); ok {
					a.emitInstallSession(view)
				}
				return errors.Join(ctx.Err(), a.dlMgr.RemoveTask(taskID))
			case downloader.StatusFailed:
				msg := snap.ErrorMessage
				if msg == "" {
					msg = "下载失败"
				}
				return errors.Join(errors.New(msg), a.dlMgr.RemoveTask(taskID))
			case downloader.StatusCancelled:
				return finishCancelled(fmt.Errorf("下载已取消: %w", context.Canceled))
			}
		}
	}
}

func isTransferRestart(message string) bool {
	return hasAnyPrefix(message, "Attempt 1/", "Trying fallback", "Proxy unreachable")
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

// Legacy events are preserved for older observers; the new UI consumes the
// session snapshot stream, which also includes controls and terminal status.
func (a *App) emitInstallProgress(taskID, tool, version, phase string, percent float64, speed, eta string) {
	if view, ok := a.installSessions.update(taskID, func(v *InstallSession) {
		if v.Status == "cancelling" {
			return
		}
		v.Phase = phase
		if percent >= 0 {
			v.Percent, v.Speed, v.ETA = percent, speed, eta
		}
	}); ok {
		a.emitInstallSession(view)
	}
	a.emitEvent("installProgress", InstallProgress{
		TaskID: taskID,
		Tool:   tool, Version: version, Phase: phase, Percent: percent, Speed: speed, ETA: eta,
	})
}

func (a *App) emitInstallError(taskID, tool, version, msg string) {
	a.emitEvent("installError", map[string]string{
		"taskId": taskID,
		"tool":   tool, "version": version, "error": msg,
	})
}

func (a *App) emitActivationWarning(tool, version, msg string) {
	a.emitEvent("xdev:activation-warning", map[string]string{"tool": tool, "version": version, "message": msg})
}

func (a *App) emitTOFUWarning(tool, version, msg string) {
	a.emitEvent("xdev:tofu-warning", map[string]string{"tool": tool, "version": version, "message": msg})
}

var _ installer.DownloadFunc = (&App{}).guiDownloadFunc("", "", "")
