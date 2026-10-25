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
	"errors"
	"fmt"
	"os"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
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
// confirmed skips the unverified-version confirmation dialog. force records a
// repair/force intent: the existing install directory (complete or incomplete)
// is removed **only after the archive has been downloaded and its SHA-256
// verified** — never before. That ordering mirrors internal/installer's A-07
// fix (ticket 26) and is guarded here by F-05 (Round 7): the old
// "remove first, then fetch" behaviour destroyed a working install whenever
// the subsequent download failed (offline / 404 / mirror outage / user cancel).
// formFilter, when non-empty, selects a form-specific archive and does not
// silently fall back to a different form.
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
		// Ticket 24: read the manifest through the plugin accessor
		// (g.Manifest() takes the Generic RWMutex) instead of touching the
		// M field directly.
		if g, ok := p.(*plugin.Generic); ok && g.Manifest() != nil {
			if v, found := g.Manifest().FindVersion(ver); found {
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

	// 先校验安装路径合法（SafeName 失败时插件会 fail-open 换成
	// unsafe-version-blocked 目录）；真正的写入由下面的 InstallWithProgress 负责。
	if _, err := p.InstallDir(cfg.Core.InstallDir, ver); err != nil {
		return err
	}

	// 2026-10-25（架构收口）：委托 installer.InstallWithProgress。
	//
	// 修复前这里是约 330 行的自建编排（BuildDownloadOptions → StartDownload
	// → 轮询 → TOFU → force 删除 → ExtractAndActivate），与 installer 里
	// 已有的实现重复。后果不只是代码冗余 —— 「先下载校验成功、再删旧安装」
	// 这条 A-07 安全语义曾在 GUI 侧漏过一次（F-05：旧代码在下载之前就
	// os.RemoveAll，下载失败会摧毁完好安装）。
	//
	// 现在只保留 GUI 独有的两件事：
	//   1. 进度/错误事件上报（DownloadFunc 内的 OnProgress/OnStatus）
	//   2. 基于 downloader.Manager 的下载（支持 pause/resume/cancel）
	// 其余全部由 installer 单一拥有，两条路径的语义不可能再分叉。
	go func() {
		res, err := ins.InstallWithProgress(
			a.rootContext(), p, ver,
			func(phase installer.ProgressPhase, pct float64, speed, eta string) {
				a.emitInstallProgress(taskID, toolName, version, phase.String(), pct, speed, eta)
			},
			installer.InstallOptions{
				Force:        force,
				DownloadFunc: a.guiDownloadFunc(taskID, toolName, version),
			},
		)
		if err != nil {
			// 不完整安装：交给前端弹 Naive UI 对话框（原生 Wails 对话框
			// 可能被窗口遮挡），因此不作为 installError 上报。
			if errors.Is(err, installer.ErrIncompleteInstall) {
				a.emitInstallProgress(taskID, toolName, version, "Incomplete", 0, "", "")
				a.dlMgr.RemoveTask(taskID)
				return
			}
			a.emitInstallError(taskID, toolName, version, err.Error())
			a.dlMgr.RemoveTask(taskID)
			return
		}

		// 成功，或「版本已装好但激活失败」（激活失败降级为软警告）。
		a.emitInstallProgress(taskID, toolName, version, "Completed", 100, "", "")
		if res != nil && res.ActivationError != "" {
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "xdev:activation-warning", map[string]string{
					"tool":    toolName,
					"version": version,
					"message": res.ActivationError,
				})
			}
		}

		// 缓存失效与归档保留：安装已改变磁盘状态，必须让 UI 立刻反映。
		a.refreshAndSaveLocalCache()
		a.applyArchiveRetention(toolName)
		a.InvalidateSizeBreakdown()
		a.InvalidateToolsCache()
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
	// Ticket 24: read the catalog through getCat() so the snapshot is taken
	// under catMu (reloadCatalog may swap the pointer concurrently).
	if cat := a.getCat(); cat != nil {
		if t, ok := cat.Tool(toolName); ok {
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
	installPath, err := p.InstallDir(cfg.Core.InstallDir, version)
	if err != nil {
		return err
	}
	// B-06（票 29 / P1-19）：存在性门 —— 未安装的版本必须在动任何东西之前
	// 就明确报「未安装」。旧代码把不存在的 installPath 直接交给
	// Activate：Activate 先建 current/<tool> 链接（失败路径还会尝试 symlink/
	// 拷贝兜底），于是错误信息来自链接层（"symlink … failed"）甚至留下悬挂
	// 链接，把「切换到一个没装的版本」误报成文件系统故障。
	// 措辞与 SwitchToXdevVersion 的存在性门保持一致。
	if fi, statErr := os.Stat(installPath); statErr != nil || !fi.IsDir() {
		return fmt.Errorf("version %s of %s is not installed", version, toolName)
	}
	ins := a.newInstaller(cfg)
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
