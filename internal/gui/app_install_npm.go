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
		// 2026-10-26：改用统一的 emitInstallProgress/emitInstallError。
		//
		// 修复前这里自己构造 EventsEmit，**没有 a.ctx nil 保护** —— 而 Wails
		// 的 EventsEmit 对 nil context 会 log.Fatalf 直接杀进程（历史上让
		// 测试进程静默退出、连 FAIL 行都不打印，见 app_profile.go 的注释）。
		//
		// 统一到辅助函数还带来两个好处：事件载荷只在一处构造（taskID 不会
		// 漏），且与下载管线的事件契约完全一致。
		emitProgress := func(phase string, pct float64) {
			a.emitInstallProgress(taskID, toolName, ver, phase, pct, "", "")
		}
		emitProgress("Installing...", -1)

		// 绑定应用根上下文：应用退出（shutdown → rootCancel）时 npm 子进程
		// 立即被取消（runner 走 context），不空转到 15 分钟超时。
		var ctx context.Context = a.rootContext()
		ins := a.newInstaller(a.getCfg())
		res, err := ins.InstallNPMPackage(ctx, p, arch, ver, installPath)
		if err != nil {
			logWarn("[install] npm install failed for %s@%s: %v", toolName, ver, err)
			a.emitInstallError(taskID, toolName, ver, err.Error())
			a.InvalidateToolsCache()
			return
		}

		emitProgress("Completed", 100)
		if res != nil && !res.Activated && res.ActivationError != "" {
			// Version installed but not switched (locked binary etc.) —
			// same soft-warning contract as the download pipeline.
			a.emitActivationWarning(toolName, ver, res.ActivationError)
		}

		a.invalidateDashboardCache()
		a.refreshAndSaveLocalCache()
		a.InvalidateSizeBreakdown()
		a.InvalidateToolsCache()
	}()
	return nil
}
