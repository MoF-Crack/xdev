// app_install.go implements the install/use/uninstall/download operations
// for the GUI, with real-time progress reporting via Wails events.
//
// # Install Flow
//
//  1. Frontend calls InstallTool(name, version)
//  2. App validates the tool exists in the registry
//  3. A goroutine runs Installer.InstallWithProgress with a progress callback
//  4. The callback emits Wails events ("install-progress") with InstallProgress JSON
//  5. On completion, an "install-result" event is emitted
//  6. Frontend updates the UI and calls GetInstalledVersions to refresh
//
// # Download Only
//
// DownloadTool downloads the archive without extracting or activating,
// useful for pre-caching. Progress is reported via the same event mechanism.
//
// # Cancel/Rename
//
// CancelDownload removes the .downloading temp file for a tool.
// RenameVersion moves an installed version directory to a new name.
package gui

import (
	"fmt"
	"os"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// InstallTool installs a tool at the specified version.
// Progress events are emitted as "installProgress" events.
// If the version is sniff-discovered (Unverified: true), it
// automatically prompts the user with a confirmation dialog
// (FR-018).  This avoids requiring the frontend to handle the
// unverified case.
func (a *App) InstallTool(toolName, version string) error {
	return a.InstallToolWithOptions(toolName, version, false, false)
}

// InstallToolWithForm installs a tool at the specified version, filtering
// archives by form factor (Spec 025). When formFilter is non-empty (e.g.
// "gui" or "cli"), the archive selection prefers archives whose Form
// field matches formFilter, falling back to Form=="" (form-agnostic).
//
// This is used by the frontend form selector for tools that have
// form-specific archives (HasFormSpecificArchives=true, e.g. OpenCode's
// Desktop App vs CLI). Tools without form-specific archives ignore
// formFilter and behave identically to InstallTool.
func (a *App) InstallToolWithForm(toolName, version, formFilter string) error {
	a.pendingFormFilterMu.Lock()
	a.pendingFormFilter = formFilter
	a.pendingFormFilterMu.Unlock()
	// Ensure the filter is cleared after install (success or failure).
	defer func() {
		a.pendingFormFilterMu.Lock()
		a.pendingFormFilter = ""
		a.pendingFormFilterMu.Unlock()
	}()
	return a.InstallToolWithOptions(toolName, version, false, false)
}

// InstallToolConfirmed is like InstallTool but with an explicit
// confirmed flag for sniff-discovered (unverified) versions.
// If confirmed is false and the version is unverified, it shows a
// Wails message dialog to ask the user for confirmation before
// proceeding.
func (a *App) InstallToolConfirmed(toolName, version string, confirmed bool) error {
	return a.InstallToolWithOptions(toolName, version, confirmed, false)
}

// RepairInstall force-reinstalls an existing installation (complete or
// incomplete) at the same version. The frontend is responsible for
// showing a confirmation dialog (Naive UI) before calling this method
// with force=true — this avoids the native Wails MessageDialog which can
// be hidden behind the window.
func (a *App) RepairInstall(toolName, version string) error {
	return a.InstallToolWithOptions(toolName, version, true, true)
}

// InstallToolWithOptions is the shared install implementation.
//
// confirmed skips the unverified-version confirmation dialog; force
// removes any existing install directory (complete or incomplete) before
// downloading. When the version is already installed and intact (and no
// force is requested), the tool is only re-activated — no download
// happens. When it is installed but incomplete, a native dialog asks the
// user whether to repair by reinstalling the same version.
func (a *App) InstallToolWithOptions(toolName, version string, confirmed, force bool) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()

	// Check if this version is sniff-discovered (Unverified: true)
	// and requires user confirmation (FR-018).
	cat, catErr := manifest.LoadCatalog(cfg.Core.ManifestDir)
	if catErr == nil {
		if t, ok := cat.Tool(toolName); ok {
			if v, found := t.FindVersion(version); found && v.Unverified {
				if !confirmed {
					// Show Wails dialog for user confirmation.
					dialogOpts := wailsRuntime.MessageDialogOptions{
						Type:          "warning",
						Title:         "Unverified Version",
						Message:       fmt.Sprintf("Version %s@%s was discovered by sniffing.\n\nThe archive URL has not been verified — downloads may fail.\n\nContinue with installation?", toolName, version),
						Buttons:       []string{"Yes", "No"},
						DefaultButton: "No",
					}
					choice, err := wailsRuntime.MessageDialog(a.ctx, dialogOpts)
					if err != nil || choice != "Yes" {
						return fmt.Errorf("installation cancelled by user")
					}
				}
			}
		}
	}

	ins := a.newInstaller(cfg)
	// TOFU 完成后通过 Wails 事件通知前端展示警告对话框（Spec 021 D7：
	// 复用 installer 的 TOFUArchiveIfNeeded 与回调机制，避免 GUI 内联重复实现）。
	ins.SetTOFUWarningCallback(func(tool, version, osName, arch, sha256 string) {
		wailsRuntime.EventsEmit(a.ctx, "xdev:tofu-warning", map[string]string{
			"tool":    tool,
			"version": version,
			"sha256":  sha256,
		})
	})

	// 生成下载任务 ID
	taskID := fmt.Sprintf("%s@%s", toolName, version)

	// 解析版本和获取 archive 信息
	ver, err := p.ResolveVersion(version)
	if err != nil {
		return err
	}
	// Spec 025: 形态筛选。InstallToolWithForm 设置 pendingFormFilter，
	// 此处取出并在 archive 选择时应用。非空时优先匹配 Form==filter
	// 的 archive，降级到 Form==""（form-agnostic，向后兼容）。
	a.pendingFormFilterMu.Lock()
	formFilter := a.pendingFormFilter
	a.pendingFormFilterMu.Unlock()

	var arch *manifest.Archive
	if formFilter != "" {
		// 形态筛选路径：通过 manifest.Version.ArchiveForCurrentWithForm
		if g, ok := p.(*plugin.Generic); ok && g.M != nil {
			if v, found := g.M.FindVersion(ver); found {
				if a2, ok2 := v.ArchiveForCurrentWithForm(plugin.HostOS(), plugin.HostArch(), formFilter); ok2 {
					arch = a2
				}
			}
		}
	}
	if arch == nil {
		// 无 formFilter 或 Generic 类型不匹配或筛选未命中 → 回退到默认路径
		arch, err = p.ArchiveFor(ver)
		if err != nil {
			return err
		}
	}

	installPath := p.InstallDir(cfg.Core.InstallDir, ver)

	// Already-installed fast path + incomplete-install handling.
	if fi, err := os.Stat(installPath); err == nil && fi.IsDir() {
		integrity := installer.VerifyInstallIntegrity(p, installPath)
		if integrity.OK && !force {
			// Intact install — activate only, no download.
			logInfo("[install] %s@%s already installed and intact at %s — activating only", toolName, ver, installPath)
			if err := ins.Activate(p, installPath, ver); err != nil {
				logWarn("[install] activation of existing install failed: %v", err)
				return fmt.Errorf("activation failed for existing install: %w", err)
			}
			wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
				TaskID:  taskID,
				Tool:    toolName,
				Version: version,
				Phase:   "Completed",
				Percent: 100,
			})
			a.invalidateDashboardCache()
			a.refreshAndSaveLocalCache()
			return nil
		}
		if !integrity.OK && !force {
			// Incomplete install — return an error so the frontend can
			// show a Naive UI dialog (not a native Wails dialog that
			// may be hidden behind the window). The frontend pre-checks
			// with CheckInstallIntegrity and handles this case.
			return &installer.IncompleteInstallError{
				Tool:    toolName,
				Version: ver,
				Path:    installPath,
				Reason:  integrity.Reason,
				Missing: integrity.Missing,
			}
		}
		// User confirmed repair (or force was requested): remove the
		// existing directory and reinstall from scratch.
		logInfo("[install] removing existing %s install dir %s for reinstall", toolName, installPath)
		if err := os.RemoveAll(installPath); err != nil {
			return fmt.Errorf("failed to remove existing install dir: %w", err)
		}
	}

	// Proxy warning: if the archive explicitly requires a proxy but none
	// is configured (either via env vars or xdev config), warn the user
	// (shared helper with the CLI path, Spec 021 D7).
	if arch.RequiresProxy && !installer.CheckProxyConfigured(cfg) {
		logWarn("[install] WARNING: %s@%s requires a proxy but none is configured", toolName, ver)
	}

	// 构造下载选项 — use shared installer.BuildDownloadOptions
	url, fallbackURLs, dest, urlShaMap := ins.BuildDownloadOptions(toolName, ver, arch)

	// 加载 manifest 签名（内置/第三方/嵌入式，与 CLI 一致，Spec 021 D6）。
	// 真实根密钥嵌入时，缺签名的 GUI 下载会被 downloader 拒绝。
	manifestSig, isThirdParty := ins.LoadManifestSignature(toolName)

	dlOpts := downloader.Options{
		URL:               url,
		Dest:              dest,
		ExpectedSHA:       arch.SHA256,
		URLShaMap:         urlShaMap,
		ProgressDesc:      fmt.Sprintf("%s@%s", toolName, ver),
		Cookie:            util.CookieFromInstallArgs(arch.InstallArgs),
		ManifestSignature: manifestSig,
		IsThirdParty:      isThirdParty,
		ProxyURL:          cfg.GetProxyURL("https"),
		FallbackURLs:      fallbackURLs,
		OnProgress: func(downloaded, total int64, elapsed time.Duration) {
			// 下载进度通过事件发送到前端
			wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
				TaskID:  taskID,
				Tool:    toolName,
				Version: version,
				Phase:   "Downloading...",
				Percent: float64(downloaded) / float64(total) * 100,
				Speed:   util.HumanSpeedStr(downloaded, elapsed),
				ETA:     util.HumanETAStr(downloaded, total, elapsed),
			})
		},
		// OnStatus surfaces non-byte status updates (retry attempts,
		// fallback URL switches, range restarts, proxy fallback) as
		// progress phases. A brand-new transfer resets the bar to 0:
		// first attempt of a URL, fallback URL switch, and proxy→direct
		// switch all start from 0 bytes. In-URL retries (Attempt 2/3…)
		// keep the current bar position via the -1 sentinel — the
		// transfer resumes from the partial file, so the byte progress
		// remains valid.
		OnStatus: func(message string) {
			pct := -1.0
			if strings.HasPrefix(message, "Attempt 1/") ||
				strings.HasPrefix(message, "Trying fallback") ||
				strings.HasPrefix(message, "Proxy unreachable") {
				pct = 0
			}
			wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
				TaskID:  taskID,
				Tool:    toolName,
				Version: version,
				Phase:   message,
				Percent: pct,
			})
		},
	}

	// 启动下载任务
	if err := a.dlMgr.StartDownload(taskID, toolName, ver, dlOpts); err != nil {
		return err
	}

	// 在后台等待下载完成，然后执行安装
	// F-20 / D18：轮询绑定应用根上下文 —— 应用退出（shutdown → rootCancel）
	// 时立即停止，而不是继续空转最多 1 小时；maxPolls 仅作兜底。
	go func() {
		const maxPolls = 7200 // 500ms * 7200 = 1 hour max wait
		rootDone := a.rootContext().Done()
		for i := 0; i < maxPolls; i++ {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-rootDone:
				logInfo("[InstallTool] cancelled by app shutdown for %s@%s", toolName, version)
				a.dlMgr.RemoveTask(taskID)
				return
			}
			task, err := a.dlMgr.GetTask(taskID)
			if err != nil {
				return
			}
			status := task.GetStatus()

			if status == downloader.StatusCompleted {
				// 下载完成，执行解压和激活
				// Keep the percent at 100 (download finished) — the phase
				// string "Extracting..." is enough; resetting to 0 makes
				// the progress bar restart for no reason.
				wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
					TaskID:  taskID,
					Tool:    toolName,
					Version: version,
					Phase:   "Extracting...",
					Percent: 100,
				})

				// TOFU: If the archive had an empty or placeholder SHA-256,
				// compute the actual hash and write it to the on-disk
				// manifest (shared installer helper; the SetTOFUWarningCallback
				// above emits the xdev:tofu-warning Wails event).
				if arch.SHA256 == "" || util.IsAllZeroHash(arch.SHA256) {
					if err := ins.TOFUArchiveIfNeeded(toolName, ver, arch, dest); err != nil {
						logWarn("[install] TOFU warning: failed to update manifest for %s@%s: %v", toolName, ver, err)
					}
				}

				installPath := p.InstallDir(cfg.Core.InstallDir, ver)
				err := ins.ExtractAndActivate(p, arch, ver, installPath)
				if err != nil && !installer.IsActivationError(err) {
					// Real failure (extraction/download) — report as error.
					wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
						"taskId":  taskID,
						"tool":    toolName,
						"version": version,
						"error":   err.Error(),
					})
				} else {
					// Success (or activation-only failure: version is
					// installed, just not switched). Emit Completed and
					// a soft warning if activation failed.
					wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
						TaskID:  taskID,
						Tool:    toolName,
						Version: version,
						Phase:   "Completed",
						Percent: 100,
					})
					if err != nil {
						// Activation failed — version installed but not active.
						wailsRuntime.EventsEmit(a.ctx, "xdev:activation-warning", map[string]string{
							"tool":    toolName,
							"version": version,
							"message": err.Error(),
						})
					}
					a.refreshAndSaveLocalCache()

					// 安装成功后按保留策略处理缓存归档（immediate 删除，其余保留）。
					a.applyArchiveRetention(toolName)
				}
				// 新版本已安装且缓存归档已按策略处理，明细缓存必须立即失效。
				a.InvalidateSizeBreakdown()
				// 工具列表（编程语言/包管理器）缓存也需失效，反映新安装状态。
				a.InvalidateToolsCache()
				a.dlMgr.RemoveTask(taskID)
				return
			}

			if status == downloader.StatusFailed || status == downloader.StatusCancelled {
				errMsg := task.ErrorMessage
				if status == downloader.StatusFailed {
					if errMsg == "" {
						errMsg = "下载失败"
					}
				} else {
					errMsg = "下载已取消"
				}
				wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
					"taskId":  taskID,
					"tool":    toolName,
					"version": version,
					"error":   errMsg,
				})
				a.dlMgr.RemoveTask(taskID)
				return
			}
		}
		// 超时：下载任务耗时过长，清理
		logWarn("[InstallTool] download poll timeout for %s@%s", toolName, version)
		a.dlMgr.Cancel(taskID)
		a.dlMgr.RemoveTask(taskID)
	}()

	return nil
}

// IsVersionUnverified reports whether the given version was discovered
// by sniffing (Unverified: true in the manifest). The frontend uses this
// to show a Naive UI confirmation dialog before calling
// InstallToolConfirmed with confirmed=true, avoiding the native Wails
// MessageDialog which can be hidden behind the window.
func (a *App) IsVersionUnverified(toolName, version string) bool {
	cfg := a.getCfg()
	cat, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
	if err != nil {
		return false
	}
	t, ok := cat.Tool(toolName)
	if !ok {
		return false
	}
	v, found := t.FindVersion(version)
	return found && v.Unverified
}
func (a *App) UseVersion(toolName, version string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()
	ins := a.newInstaller(cfg)
	installPath := p.InstallDir(cfg.Core.InstallDir, version)
	if err := ins.Activate(p, installPath, version); err != nil {
		return err
	}
	// Invalidate dashboard cache so the version number refreshes
	// immediately after switching.
	a.invalidateDashboardCache()
	a.refreshAndSaveLocalCache()
	// 切换不改变占用，但保持明细缓存与磁盘一致（TTL 前也强制刷新一次）。
	a.InvalidateSizeBreakdown()
	// 工具列表缓存反映激活版本变化。
	a.InvalidateToolsCache()
	return nil
}

// UninstallVersion removes a specific version of a tool.
func (a *App) UninstallVersion(toolName, version string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()
	ins := a.newInstaller(cfg)
	err = ins.Uninstall(p, version)
	if err == nil {
		a.refreshAndSaveLocalCache()
		// 安装目录已删除，明细缓存必须立即失效。
		a.InvalidateSizeBreakdown()
		// 工具列表缓存反映卸载状态。
		a.InvalidateToolsCache()
	}
	return err
}

// PauseDownload 暂停下载任务
func (a *App) PauseDownload(taskID string) error {
	return a.dlMgr.Pause(taskID)
}

// ResumeDownload 恢复暂停的下载任务
func (a *App) ResumeDownload(taskID string) error {
	return a.dlMgr.Resume(taskID)
}

// CancelDownload 取消下载任务并清理临时文件
func (a *App) CancelDownload(taskID string) error {
	return a.dlMgr.Cancel(taskID)
}

// GetDownloadTasks 获取所有下载任务状态
func (a *App) GetDownloadTasks() []DownloadTaskInfo {
	tasks := a.dlMgr.GetAllTasks()
	result := make([]DownloadTaskInfo, 0, len(tasks))
	for _, t := range tasks {
		snap := t.GetSnapshot()
		result = append(result, DownloadTaskInfo{
			ID:           snap.ID,
			Tool:         snap.Tool,
			Version:      snap.Version,
			Status:       string(snap.Status),
			Progress:     snap.Progress,
			Speed:        snap.Speed,
			ETA:          snap.ETA,
			Downloaded:   snap.Downloaded,
			Total:        snap.Total,
			ErrorMessage: snap.ErrorMessage,
		})
	}
	return result
}

// CleanupDownloadTempFiles 清理所有下载临时文件
func (a *App) CleanupDownloadTempFiles() error {
	cfg := a.getCfg()
	return downloader.CleanupAllPartialsInDir(cfg.Core.CacheDir)
}
