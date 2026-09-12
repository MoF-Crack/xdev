// app_install.go is the install-session module behind the Wails App
// adapter (Spec 029 B2/B3). One InstallSpec drives catalog tools,
// package-manager scripts, and sub-tool enable/install.
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

// InstallSpec is the single install-session input (Spec 029 B3).
// Wails wrappers (InstallTool / WithForm / Confirmed / Repair /
// InstallPackageManager / InstallSubTool) all forward here so the
// frontend keeps its existing bindings while the implementation has
// one interface.
//
// Kind selects the pipeline:
//   - "" / "tool": catalog download → extract → activate
//   - "package-manager": official script (scoop/brew/choco) or catalog tool
//   - "sub-tool": corepack enable / pip|uv|npm method
type InstallSpec struct {
	Tool      string
	Version   string
	Form      string
	Confirmed bool
	Force     bool
	Kind      string // "", "tool", "package-manager", "sub-tool"
	Method    string // sub-tool install method (pip / uv / corepack / npm)
}

// InstallTool installs a tool at the specified version.
func (a *App) InstallTool(toolName, version string) error {
	return a.install(InstallSpec{Tool: toolName, Version: version})
}

// InstallToolWithForm installs a tool, filtering archives by form factor.
func (a *App) InstallToolWithForm(toolName, version, formFilter string, confirmed bool) error {
	return a.install(InstallSpec{Tool: toolName, Version: version, Form: formFilter, Confirmed: confirmed})
}

// InstallToolConfirmed is like InstallTool with an explicit confirmed flag.
func (a *App) InstallToolConfirmed(toolName, version string, confirmed bool) error {
	return a.install(InstallSpec{Tool: toolName, Version: version, Confirmed: confirmed})
}

// RepairInstall force-reinstalls an existing installation.
func (a *App) RepairInstall(toolName, version string) error {
	return a.install(InstallSpec{Tool: toolName, Version: version, Confirmed: true, Force: true})
}

// InstallToolWithOptions is kept for older callers; prefer Install.
func (a *App) InstallToolWithOptions(toolName, version string, confirmed, force bool, formFilter string) error {
	return a.install(InstallSpec{Tool: toolName, Version: version, Form: formFilter, Confirmed: confirmed, Force: force})
}

// install runs one install-session from spec (not exported: keep Wails
// bindings on the existing string wrappers).
func (a *App) install(spec InstallSpec) error {
	switch spec.Kind {
	case "", "tool":
		if spec.Tool == "" {
			return fmt.Errorf("install spec: tool is required")
		}
		return a.installWithOptions(spec.Tool, spec.Version, spec.Confirmed, spec.Force, spec.Form)
	case "package-manager":
		return a.installPackageManager(spec)
	case "sub-tool":
		return a.installSubTool(spec)
	default:
		return fmt.Errorf("install spec: unknown kind %q", spec.Kind)
	}
}

// installWithOptions is the shared install implementation.
//
// confirmed skips the unverified-version confirmation dialog; force
// removes any existing install directory (complete or incomplete) before
// downloading. formFilter, when non-empty, selects a form-specific
// archive and does not silently fall back to a different form.
func (a *App) installWithOptions(toolName, version string, confirmed, force bool, formFilter string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()

	// Check if this version is sniff-discovered (Unverified: true)
	// and requires user confirmation (FR-018). Use the in-memory
	// catalog / plugin manifest — do not re-parse every YAML here.
	if !confirmed && a.lookupVersionUnverified(toolName, version) {
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
	var arch *manifest.Archive
	if formFilter != "" {
		// 形态筛选路径：必须命中 Form==filter 或 Form=="" 的 archive。
		// 找不到时直接报错，禁止静默回退到另一种形态的默认包。
		if g, ok := p.(*plugin.Generic); ok && g.M != nil {
			if v, found := g.M.FindVersion(ver); found {
				if a2, ok2 := v.ArchiveForCurrentWithForm(plugin.HostOS(), plugin.HostArch(), formFilter); ok2 {
					arch = a2
				}
			}
		}
		if arch == nil {
			return fmt.Errorf("no archive for %s@%s with form %q on %s/%s", toolName, ver, formFilter, plugin.HostOS(), plugin.HostArch())
		}
	} else {
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

	// Spec 039: npm-kind installs never go through the download manager —
	// npm resolves and fetches the dependency tree itself (registry integrity
	// verified by npm). Event contract matches the download pipeline.
	if installer.ResolveKind(arch) == manifest.InstallKindNPM {
		return a.installNPMPackage(taskID, p, arch, ver, installPath)
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
		URL:                  url,
		Dest:                 dest,
		ExpectedSHA:          arch.SHA256,
		URLShaMap:            urlShaMap,
		ProgressDesc:         fmt.Sprintf("%s@%s", toolName, ver),
		Cookie:               util.CookieFromInstallArgs(arch.InstallArgs),
		ManifestSignature:    manifestSig,
		IsThirdParty:         isThirdParty,
		AllowPlaceholderTOFU: arch.SHA256 == "" || util.IsAllZeroHash(arch.SHA256),
		// Ticket 13 (P0-02): same signed-map slot + unsigned-version
		// derivation as the CLI pipeline — derived through the SHARED
		// installer.DeriveUnsignedVersion so the two pipelines can never
		// disagree (the older GUI-only versionUnverified helper missed
		// Discovered and carried an npm special case).
		ToolName:        toolName,
		Version:         ver,
		OSArch:          arch.OS + "/" + arch.Arch,
		UnsignedVersion: installer.DeriveUnsignedVersion(p.Manifest(), ver),
		ProxyURL:        cfg.GetProxyURL("https"),
		FallbackURLs:    fallbackURLs,
		OnProgress: func(downloaded, total int64, elapsed time.Duration) {
			// Content-Length 缺失时 total<=0，避免进度条变成 NaN。
			pct := -1.0
			if total > 0 {
				pct = float64(downloaded) / float64(total) * 100
			}
			wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
				TaskID:  taskID,
				Tool:    toolName,
				Version: version,
				Phase:   "Downloading...",
				Percent: pct,
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
				// 下载完成，执行解压/运行安装器并激活。
				// Keep the percent at 100 (download finished) — the phase
				// string is enough; resetting to 0 makes the progress bar
				// restart for no reason.
				// 阶段语义：archive → "Extracting..."；executable/script
				// （rustup 等滚动安装器）→ "Installing..."（运行安装器可能
				// 耗时数分钟下载 toolchain，进度条保持 100 但文案明确在安装）。
				phaseStr := "Extracting..."
				// Spec 035 F10: use the installer's ResolveKind (includes the
				// rustup URL heuristic) so preview/phase logic and the real
				// install path can never diverge.
				if installer.ResolveKind(arch) != manifest.InstallKindArchive {
					phaseStr = "Installing..."
				}
				wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
					TaskID:  taskID,
					Tool:    toolName,
					Version: version,
					Phase:   phaseStr,
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
		// Spec 035 F11: emit installError BEFORE cleanup — without it the
		// frontend progress dialog waits for a Completed event that never
		// arrives (single install) and the batch waiter hangs forever.
		wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
			"taskId":  taskID,
			"tool":    toolName,
			"version": version,
			"error":   "安装等待超时（1 小时）",
		})
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
	return a.lookupVersionUnverified(toolName, version)
}

// PreviewInstall returns the cheap preflight the install button needs
// (unverified flag, form choices, existing-install integrity) in one
// round-trip. It must not scan categories or re-parse the catalog.
func (a *App) PreviewInstall(toolName, version string) InstallPreview {
	preview := InstallPreview{
		Tool:      toolName,
		Version:   version,
		Integrity: a.CheckInstallIntegrity(toolName, version),
	}
	if t := a.lookupToolManifest(toolName); t != nil {
		preview.Unverified = versionUnverified(t, version)
		preview.HasFormSpecificArchives = t.HasFormSpecificArchives()
		preview.AvailableForms = t.AvailableArchiveForms()
	}
	return preview
}

func (a *App) lookupVersionUnverified(toolName, version string) bool {
	return versionUnverified(a.lookupToolManifest(toolName), version)
}

func versionUnverified(t *manifest.Tool, version string) bool {
	if t == nil {
		return false
	}
	v, found := t.FindVersion(version)
	if !found {
		return false
	}
	// Spec 039: npm-kind 安装不下载归档条目（依赖树与完整性由 npm 负责），
	// 「归档 URL 未验证」确认弹窗不适用。按该版本的归档判定；版本缺
	// archives（auto_archive_fill 关闭）时回退按工具任一归档判定。
	if versionArchivesNPMManaged(t, v) {
		return false
	}
	return v.Unverified
}

// versionArchivesNPMManaged reports whether the given version's archives
// declare an npm-managed install (install_kind: npm); versions without
// archives fall back to the tool's other versions.
func versionArchivesNPMManaged(t *manifest.Tool, v *manifest.Version) bool {
	for _, a := range v.Archives {
		if a.EffectiveKind() == manifest.InstallKindNPM {
			return true
		}
	}
	if len(v.Archives) > 0 {
		return false
	}
	for _, other := range t.VersionList {
		for _, a := range other.Archives {
			if a.EffectiveKind() == manifest.InstallKindNPM {
				return true
			}
		}
	}
	return false
}

func (a *App) lookupToolManifest(toolName string) *manifest.Tool {
	if a.cat != nil {
		if t, ok := a.cat.Tool(toolName); ok {
			return t
		}
	}
	if p, err := a.mustGetPlugin(toolName); err == nil {
		return p.Manifest()
	}
	return nil
}

// reloadCatalog reloads on-disk manifests (including sniff merges) into
// a.cat and swaps Generic plugin pointers so subsequent installs see
// newly discovered versions without restarting.
func (a *App) reloadCatalog() error {
	cfg := a.getCfg()
	cat, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
	if err != nil {
		return err
	}
	// Concurrency: reloadCatalog may run on a Wails goroutine (SniffAll/
	// SniffNow) while other bindings read a.cat — guard the swap.
	a.catMu.Lock()
	a.cat = cat
	a.catMu.Unlock()
	if a.registry == nil {
		return nil
	}
	for _, t := range cat.Tools() {
		p, ok := a.registry.Get(t.Name)
		if !ok {
			continue
		}
		if setter, ok := p.(interface{ SetManifest(*manifest.Tool) }); ok {
			setter.SetManifest(t)
		}
	}
	return nil
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
