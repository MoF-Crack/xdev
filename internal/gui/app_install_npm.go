// app_install_npm.go is the GUI-side branch for npm-kind installs
// (Spec 039): tools whose archive entries declare install_kind: npm
// (e.g. dsh — @deepseek-ai/dsh) are installed by delegating to the npm
// CLI instead of the download-manager pipeline.
//
// The Wails event contract is identical to the download pipeline
// ("installProgress" with taskId/phase/percent, "installError",
// "xdev:activation-warning"), so the existing InstallProgressDialog,
// useInstall composable, and batch flows work unchanged:
//
//	Installing... (percent -1, indeterminate — npm reports no byte
//	percentage) → Completed | installError
//
// Known limitation (documented in .scratch/dsh-npm-manage/spec.md):
// pause/resume/cancel are download-manager operations; they have no
// npm equivalent mid-run, so those buttons do nothing for npm-kind
// installs until a cancel-func registry is added.
package gui

import (
	"context"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// installNPMPackage runs the npm install asynchronously and reports
// progress through Wails events. It returns immediately (mirroring the
// download-manager flow, where StartDownload also returns before the
// work completes) so the Wails binding call does not block the frontend
// promise for the whole npm run.
func (a *App) installNPMPackage(taskID string, p plugin.Plugin, arch *manifest.Archive, ver, installPath string) error {
	go func() {
		toolName := p.Name()
		emitProgress := func(phase string, pct float64) {
			wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
				TaskID:  taskID,
				Tool:    toolName,
				Version: ver,
				Phase:   phase,
				Percent: pct,
			})
		}
		emitProgress("Installing...", -1)

		// 绑定应用根上下文：应用退出（shutdown → rootCancel）时 npm 子进程
		// 立即被取消（runner 走 context），不空转到 15 分钟超时。
		var ctx context.Context = a.rootContext()
		ins := a.newInstaller(a.getCfg())
		res, err := ins.InstallNPMPackage(ctx, p, arch, ver, installPath)
		if err != nil {
			logWarn("[install] npm install failed for %s@%s: %v", toolName, ver, err)
			wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
				"taskId":  taskID,
				"tool":    toolName,
				"version": ver,
				"error":   err.Error(),
			})
			a.InvalidateToolsCache()
			return
		}

		emitProgress("Completed", 100)
		if res != nil && !res.Activated && res.ActivationError != "" {
			// Version installed but not switched (locked binary etc.) —
			// same soft-warning contract as the download pipeline.
			wailsRuntime.EventsEmit(a.ctx, "xdev:activation-warning", map[string]string{
				"tool":    toolName,
				"version": ver,
				"message": res.ActivationError,
			})
		}

		a.invalidateDashboardCache()
		a.refreshAndSaveLocalCache()
		a.InvalidateSizeBreakdown()
		a.InvalidateToolsCache()
	}()
	return nil
}
