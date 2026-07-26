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
	"path/filepath"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/util"
)

// InstallTool installs a tool at the specified version.
// Progress events are emitted as "installProgress" events.
func (a *App) InstallTool(toolName, version string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()
	ins := a.newInstaller(cfg)

	// 生成下载任务 ID
	taskID := fmt.Sprintf("%s@%s", toolName, version)

	// 解析版本和获取 archive 信息
	ver, err := p.ResolveVersion(version)
	if err != nil {
		return err
	}
	arch, err := p.ArchiveFor(ver)
	if err != nil {
		return err
	}

	// 构造下载选项 — use shared installer.BuildDownloadOptions
	url, fallbackURLs, dest, urlShaMap := ins.BuildDownloadOptions(toolName, ver, arch)

	// Check if this is a third-party manifest for layered trust model
	isThirdParty := false
	if _, err := os.Stat(filepath.Join(cfg.Core.ManifestDir, "third-party", toolName+".yaml")); err == nil {
		isThirdParty = true
	}

	dlOpts := downloader.Options{
		URL:          url,
		Dest:         dest,
		ExpectedSHA:  arch.SHA256,
		URLShaMap:    urlShaMap,
		ProgressDesc: fmt.Sprintf("%s@%s", toolName, ver),
		Cookie:       util.CookieFromInstallArgs(arch.InstallArgs),
		IsThirdParty: isThirdParty,
		FallbackURLs: fallbackURLs,
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
	}

	// 启动下载任务
	if err := a.dlMgr.StartDownload(taskID, toolName, ver, dlOpts); err != nil {
		return err
	}

	// 在后台等待下载完成，然后执行安装
	go func() {
		const maxPolls = 7200 // 500ms * 7200 = 1 hour max wait
		for i := 0; i < maxPolls; i++ {
			time.Sleep(500 * time.Millisecond)
			task, err := a.dlMgr.GetTask(taskID)
			if err != nil {
				return
			}
			status := task.GetStatus()

			if status == downloader.StatusCompleted {
				// 下载完成，执行解压和激活
				wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
					TaskID:  taskID,
					Tool:    toolName,
					Version: version,
					Phase:   "Extracting...",
					Percent: 0,
				})

				installPath := p.InstallDir(cfg.Core.InstallDir, ver)
				if err := ins.ExtractAndActivate(p, arch, ver, installPath); err != nil {
					wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
						"taskId":  taskID,
						"tool":    toolName,
						"version": version,
						"error":   err.Error(),
					})
				} else {
					wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
						TaskID:  taskID,
						Tool:    toolName,
						Version: version,
						Phase:   "Completed",
						Percent: 100,
					})
					a.refreshAndSaveLocalCache()

					// 安装成功后清理缓存包
					cfg := a.getCfg()
					cachePattern := filepath.Join(cfg.Core.CacheDir, toolName+"-*")
					matches, _ := filepath.Glob(cachePattern)
					for _, f := range matches {
						base := filepath.Base(f)
						// 确保文件名以 toolName- 开头后跟版本号，避免误删其他工具
						prefix := toolName + "-"
						if !strings.HasPrefix(base, prefix) {
							continue
						}
						if strings.HasSuffix(f, ".zip") || strings.HasSuffix(f, ".tar.gz") || strings.HasSuffix(f, ".tgz") || strings.HasSuffix(f, ".sha256") {
							os.Remove(f)
						}
					}
				}
				a.dlMgr.RemoveTask(taskID)
				return
			}

			if status == downloader.StatusFailed || status == downloader.StatusCancelled {
				wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
					"taskId":  taskID,
					"tool":    toolName,
					"version": version,
					"error":   "下载已取消或失败",
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

// UseVersion switches the active version of a tool.
func (a *App) UseVersion(toolName, version string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()
	ins := a.newInstaller(cfg)
	installPath := p.InstallDir(cfg.Core.InstallDir, version)
	return ins.Activate(p, installPath, version)
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
