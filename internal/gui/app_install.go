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
	"context"
	"fmt"
	"os"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
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
	SessionID string `json:"sessionId"`
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Form      string `json:"form"`
	Confirmed bool   `json:"confirmed"`
	Force     bool   `json:"force"`
	Kind      string `json:"kind"`   // "", "tool", "package-manager", "sub-tool"
	Method    string `json:"method"` // sub-tool install method (pip / uv / corepack / npm)
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

// Legacy bindings wait for the real outcome. New UI callers use StartInstall
// and its unique identity so acceptance cannot be confused with completion.
func (a *App) install(spec InstallSpec) error {
	id, err := a.StartInstall(spec)
	if err != nil {
		return err
	}
	a.installSessions.mu.Lock()
	session := a.installSessions.sessions[id]
	a.installSessions.mu.Unlock()
	<-session.done
	return session.err
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
func (a *App) runToolInstall(ctx context.Context, taskID string, spec InstallSpec) error {
	toolName, version := spec.Tool, spec.Version
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	cfg := a.getCfg()

	// Check if this version is sniff-discovered (Unverified: true)
	// and requires user confirmation (FR-018). Use the in-memory
	// catalog / plugin manifest — do not re-parse every YAML here.
	if !spec.Confirmed && a.lookupVersionUnverified(toolName, version) {
		if a.ctx == nil {
			return fmt.Errorf("unverified version requires confirmation")
		}
		ok, err := a.confirmInstall(ctx, toolName, version)
		if err != nil {
			return err
		}
		if !ok {
			return context.Canceled
		}
	}

	ins := a.newInstaller(cfg)
	// TOFU 完成后通过 Wails 事件通知前端展示警告对话框（Spec 021 D7：
	// 复用 installer 的 TOFUArchiveIfNeeded 与回调机制，避免 GUI 内联重复实现）。
	ins.SetTOFUWarningCallback(func(tool, version, osName, arch, sha256 string) {
		// 2026-10-26：补 a.ctx nil 保护。Wails 的 EventsEmit 对 nil context
		// 会 log.Fatalf 直接杀进程 —— 测试环境（无 Wails runtime）跑到这里
		// 会静默退出、连 FAIL 行都不打印。
		if a.ctx == nil {
			return
		}
		a.emitEvent("xdev:tofu-warning", map[string]string{
			"tool":    tool,
			"version": version,
			"sha256":  sha256,
		})
	})

	// Resolve one form for the entire operation and arbitrate cancellation
	// before any install tree or activation is changed.
	res, err := ins.InstallWithProgress(ctx, p, version,
		func(phase installer.ProgressPhase, pct float64, speed, eta string) {
			a.emitInstallProgress(taskID, toolName, version, phase.String(), pct, speed, eta)
		}, installer.InstallOptions{
			Force:        spec.Force,
			Form:         spec.Form,
			BeforeCommit: func() error { return a.commitInstallSession(taskID) },
			DownloadFunc: a.guiDownloadFunc(taskID, toolName, version),
		})
	if err != nil {
		return err
	}
	if res != nil && res.ActivationError != "" {
		a.emitActivationWarning(toolName, version, res.ActivationError)
	}
	a.refreshAndSaveLocalCache()
	a.applyArchiveRetention(toolName)
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
		a.invalidateDashboardCache()
	}
	return err
}

// PauseDownload 暂停下载任务
func (a *App) PauseDownload(taskID string) error {
	return a.controlInstallDownload(taskID, false)
}

// ResumeDownload 恢复暂停的下载任务
func (a *App) ResumeDownload(taskID string) error {
	return a.controlInstallDownload(taskID, true)
}

// CancelDownload 取消下载任务并清理临时文件
func (a *App) CancelDownload(taskID string) error {
	if _, err := a.GetInstallSession(taskID); err == nil {
		return a.CancelInstall(taskID)
	}
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
