// app_local_detect.go is the GUI adapter for the host-scan module.
// PATH lookup and "is this inside xdev?" live in internal/hostscan;
// this file maps Hits onto PackageManagerInfo / ToolInfo.
//
// ScanLocalEnvironments scans the system for development tools that are
// already installed but not managed by xdev. It checks:
//   - Tools on the system PATH (via hostscan, skipping shimDir)
//   - Common install locations (Program Files, /usr/local, etc.)
//   - Version detection by running the tool with --version
//
// Detected tools are returned with their version, path, and source ("local").
// The GUI uses this to offer migration: importing existing tool installations
// into xdev management via MigrateLocalTool / MigrateAllLocalTools.
package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/hostscan"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/toolmeta"
	"github.com/xmdemt/xdev/internal/update"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// ScanLocalEnvironments re-scans the system for locally installed development tools
// and returns the results. Also refreshes the cache.
// This is triggered by the user explicitly clicking "Scan Local Environments",
// so it is safe to call getVersion here (the user expects a brief delay).
func (a *App) ScanLocalEnvironments() []ToolInfo {
	logInfo("[ScanLocalEnvironments] user-initiated scan starting")
	a.refreshLocalCache()

	// 快照 localMap（持锁时间极短）
	a.localMu.RLock()
	snapshot := make(map[string]localInfo, len(a.localMap))
	for k, v := range a.localMap {
		snapshot[k] = v
	}
	a.localMu.RUnlock()

	// 不持锁执行耗时操作
	for name, info := range snapshot {
		if info.Version == "" && info.Path != "" {
			ver := getVersion(name, info.Path)
			// 更新 localMap
			a.localMu.Lock()
			if existing, ok := a.localMap[name]; ok {
				existing.Version = ver
				a.localMap[name] = existing
			}
			a.localMu.Unlock()
			logDebug("[ScanLocalEnvironments] %s version=%s", name, ver)
		}
	}

	tools := a.registry.All()
	// 只返回真正的系统本地安装（source == "local"），排除 xdev 已管理；
	// 且路径在 xdev 管理目录内的也应排除（双保险）。
	cfg := a.getCfg()
	xdevDirs := []string{cfg.Core.XMDevDir, cfg.Core.InstallDir, cfg.Core.ShimDir}
	var result []ToolInfo
	for _, t := range tools {
		_, localPath, localSource := a.getLocalCached(t.Name())
		if !isLocalInstallForScan(localInfo{Path: localPath, Source: localSource}, xdevDirs) {
			continue
		}
		result = append(result, a.pluginToToolInfo(t))
	}

	// Filter out ignored tools (kind=local-scan)
	before := len(result)
	filtered := result[:0]
	for _, ti := range result {
		if !a.isIgnored(ti.Name, "local-scan", ti.LocalPath) {
			filtered = append(filtered, ti)
		}
	}
	if before != len(filtered) {
		logDebug("[ignore] filtered scan: before=%d after=%d", before, len(filtered))
	}
	return filtered
}

// isLocalInstallForScan determines whether a cached local entry should be
// included in ScanLocalEnvironments results. Only genuine system-local
// installs are returned: source must not be "xdev", the path must be
// non-empty, and it must not reside inside any xdev-managed directory
// (XMDevDir / InstallDir / ShimDir) as a double safety net.
func isLocalInstallForScan(info localInfo, xdevDirs []string) bool {
	if info.Source == "xdev" || info.Path == "" {
		return false
	}
	for _, dir := range xdevDirs {
		if dir != "" && isInsideDir(info.Path, dir) {
			return false
		}
	}
	return true
}

// GetDashboardStats returns statistics for the dashboard.
// Results are cached for 30 seconds. If the cache is expired, the old
// cached value (or a zero value) is returned immediately and a background
// goroutine refreshes the stats, emitting "dashboardStatsUpdated" when done.
// Uses atomic flag to prevent concurrent refresh storms.
func (a *App) GetDashboardStats() DashboardStats {
	a.dashMu.RLock()
	if a.dashCache != nil && time.Now().Before(a.dashExpire) {
		cache := *a.dashCache
		a.dashMu.RUnlock()
		return cache
	}
	hasCache := a.dashCache != nil
	oldCache := a.dashCache
	a.dashMu.RUnlock()

	// 如果有旧缓存，返回旧值并触发后台刷新（仅当没有刷新在进行中时）
	if hasCache && oldCache != nil {
		if atomic.CompareAndSwapInt32(&a.dashRefreshing, 0, 1) {
			go a.refreshDashboardStats()
		}
		return *oldCache
	}

	// 首次访问，触发后台刷新
	if atomic.CompareAndSwapInt32(&a.dashRefreshing, 0, 1) {
		go a.refreshDashboardStats()
	}

	// 首次不要返回全零 — 至少填入路径字段和 TotalTools
	cfg := a.getCfg()
	tools := a.registry.All()
	return DashboardStats{
		TotalTools:  len(tools),
		XMDevDir:    cfg.Core.XMDevDir,
		InstallDir:  cfg.Core.InstallDir,
		ShimDir:     cfg.Core.ShimDir,
		CacheDir:    cfg.Core.CacheDir,
		DownloadDir: cfg.Core.DownloadDir,
	}
}

// refreshDashboardStats computes dashboard stats in the background
// and updates the cache, then notifies the frontend.
func (a *App) refreshDashboardStats() {
	cfg := a.getCfg()
	tools := a.registry.All()
	totalTools := len(tools)
	installedCount := 0
	for _, t := range tools {
		if _, ok := a.currentVersion(t.Name()); ok {
			installedCount++
		}
	}

	// 并行计算两个目录大小
	var installSize, cacheSize int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		dirSizeFast(cfg.Core.InstallDir, &installSize)
	}()
	go func() {
		defer wg.Done()
		dirSizeFast(cfg.Core.CacheDir, &cacheSize)
	}()
	wg.Wait()

	shimCount := 0
	if entries, err := os.ReadDir(cfg.Core.ShimDir); err == nil {
		shimCount = len(entries)
	}

	// 填充已安装工具列表
	var installedTools []InstalledToolInfo
	for _, name := range a.registry.Names() {
		p, _ := a.registry.Get(name)
		if p == nil {
			continue
		}
		activeVer, hasActive := a.currentVersion(name)
		if !hasActive {
			continue
		}
		installedTools = append(installedTools, InstalledToolInfo{
			Name:        name,
			DisplayName: p.DisplayName(),
			Version:     activeVer,
			Category:    p.Category(),
		})
	}

	result := DashboardStats{
		TotalTools:     totalTools,
		InstalledCount: installedCount,
		InstallSize:    installSize,
		CacheSize:      cacheSize,
		ShimCount:      shimCount,
		XMDevDir:       cfg.Core.XMDevDir,
		InstallDir:     cfg.Core.InstallDir,
		ShimDir:        cfg.Core.ShimDir,
		CacheDir:       cfg.Core.CacheDir,
		DownloadDir:    cfg.Core.DownloadDir,
		InstalledTools: installedTools,
	}

	a.dashMu.Lock()
	a.dashCache = &result
	a.dashExpire = time.Now().Add(30 * time.Second)
	a.dashMu.Unlock()

	// 通知前端数据已更新
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "dashboardStatsUpdated")
	}

	// 重置 in-flight 标志，允许下次刷新
	atomic.StoreInt32(&a.dashRefreshing, 0)
}

// invalidateDashboardCache forces the next GetDashboardStats call to
// recompute from scratch instead of returning stale cached data. Called
// after version switches, installs, and uninstalls so the dashboard
// reflects the change immediately.
func (a *App) invalidateDashboardCache() {
	a.dashMu.Lock()
	a.dashExpire = time.Time{} // zero → cache always expired
	a.dashMu.Unlock()
}

// CheckForUpdate checks if a new version is available.
func (a *App) CheckForUpdate() UpdateCheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := update.Check(ctx, version.Version)
	if err != nil {
		return UpdateCheckResult{
			Error:     err.Error(),
			HasUpdate: false,
			Current:   version.Version,
			Latest:    "",
		}
	}
	return UpdateCheckResult{
		HasUpdate: result.HasUpdate,
		Current:   result.CurrentVersion,
		Latest:    result.LatestVersion,
	}
}

// ApplyUpdate downloads and applies the latest update.
// Spec 034: GUI 场景更新目标是同目录 CLI 二进制（xdev.exe）——
// 运行中的 GUI 壳（xdev-gui.exe）不能被替换；CLI 归档只写 CLI 二进制。
func (a *App) ApplyUpdate() UpdateCheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	target := cliBinaryNextToSelf()
	if target == "" {
		return UpdateCheckResult{Error: "未找到与 GUI 同目录的 xdev CLI 二进制", Current: version.Version}
	}

	result, err := update.Check(ctx, version.Version)
	if err != nil {
		return UpdateCheckResult{Error: err.Error(), Current: version.Version}
	}
	if !result.HasUpdate {
		return UpdateCheckResult{HasUpdate: false, Current: version.Version, Latest: result.LatestVersion}
	}
	if err := update.ApplyTo(ctx, result, target); err != nil {
		return UpdateCheckResult{Error: err.Error(), HasUpdate: true, Current: version.Version, Latest: result.LatestVersion}
	}
	return UpdateCheckResult{HasUpdate: true, Current: version.Version, Latest: result.LatestVersion}
}

// ScanLocalTools scans the system for all locally installed development tools
// and returns them grouped by category. This is a comprehensive scan that
// checks environment variables, PATH, and common install directories.
func (a *App) ScanLocalTools() []LocalToolInfo {
	logInfo("[ScanLocalTools] comprehensive local scan starting")
	a.refreshLocalCache()

	// Fill in version info for detected tools (snapshot pattern:
	// don't hold the write lock while spawning subprocesses).
	a.localMu.RLock()
	snapshot := make(map[string]localInfo, len(a.localMap))
	for k, v := range a.localMap {
		snapshot[k] = v
	}
	a.localMu.RUnlock()

	for name, info := range snapshot {
		if info.Version == "" && info.Path != "" {
			ver := getVersion(name, info.Path)
			a.localMu.Lock()
			if existing, ok := a.localMap[name]; ok {
				existing.Version = ver
				a.localMap[name] = existing
			}
			a.localMu.Unlock()
		}
	}

	tools := a.registry.All()
	var result []LocalToolInfo
	for _, t := range tools {
		localVer, localPath, _ := a.getLocalCached(t.Name())
		if localPath == "" {
			continue
		}
		activeVer, installed := a.currentVersion(t.Name())
		result = append(result, LocalToolInfo{
			Name:        t.Name(),
			DisplayName: t.DisplayName(),
			Category:    t.Category(),
			Version:     localVer,
			Path:        localPath,
			Managed:     installed,
			ActiveVer:   activeVer,
		})
	}
	logInfo("[ScanLocalTools] found %d local tools", len(result))
	return result
}

// MigrateLocalTool migrates a locally installed tool into xdev management.
// It copies the tool's installation directory into xdev's install/ directory,
// creates the current/ symlink, and sets up shims.
// If the tool is currently in use (locked), it rolls back and returns an error.
func (a *App) MigrateLocalTool(toolName, localPath string) error {
	logInfo("[MigrateLocalTool] migrating %s from %s", toolName, localPath)
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	cfg := a.getCfg()

	// Determine version from local installation
	ver := ""
	a.localMu.RLock()
	if info, ok := a.localMap[toolName]; ok && info.Version != "" {
		ver = info.Version
	}
	a.localMu.RUnlock()
	if ver == "" {
		ver = a.detectToolVersion(toolName, localPath)
	}

	installPath := p.InstallDir(cfg.Core.InstallDir, ver)

	// Check if already managed
	if _, err := os.Stat(installPath); err == nil {
		return fmt.Errorf("version %s of %s is already managed by xdev", ver, toolName)
	}

	// Copy the local installation to xdev's install directory
	// First check if the source is accessible (not locked)
	srcInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access local installation at %s: %v", localPath, err)
	}

	// If localPath points to a binary, find its parent directory
	srcDir := localPath
	if !srcInfo.IsDir() {
		// Walk up to find the tool root directory
		// e.g. C:\Program Files\Java\jdk-17\bin\java.exe -> C:\Program Files\Java\jdk-17
		dir := filepath.Dir(localPath)
		if filepath.Base(dir) == "bin" {
			dir = filepath.Dir(dir)
		}
		srcDir = dir
	}

	// Try to copy the directory. If any file is locked, roll back.
	logInfo("[MigrateLocalTool] copying %s -> %s", srcDir, installPath)
	if err := util.CopyDir(srcDir, installPath); err != nil {
		// Roll back: remove partially copied directory
		os.RemoveAll(installPath)
		return fmt.Errorf("migration failed (files may be in use): %v", err)
	}

	// Activate the migrated version
	ins := a.newInstaller(cfg)
	if err := ins.Activate(p, installPath, ver); err != nil {
		// Roll back
		os.RemoveAll(installPath)
		return fmt.Errorf("activation failed: %v", err)
	}

	// Refresh local cache
	a.refreshAndSaveLocalCache()
	logInfo("[MigrateLocalTool] %s migrated successfully as version %s", toolName, ver)
	return nil
}

// pmDef defines a package manager for detection.
type pmDef struct {
	name, displayName, description string
	detectCmd                      string
	detectArgs                     []string
	toolName                       string // xdev tool name (empty = not installable via xdev)
	installHint                    string // hint when not installable
	// Spec 025: 包管理器自动安装支持。
	// installCommand 非空时，InstallPackageManager 走 script install 路径
	// （后台执行官方安装命令，而非 xdev download→extract→activate 管线）。
	installCommand string // 官方安装命令（空 = 不可自动安装，保持现有行为）
	requiresAdmin  bool   // 是否需要管理员权限（Windows 下检测，非管理员时提示）
	executionEnv   string // "powershell" | "bash" — 执行环境
	platform       string // "windows" | "darwin" | "linux" | "any" — 限定平台
	// Spec 026: 附属工具动态安装支持。
	// enableCommand 非空时，该工具是"可启用附属工具"（如 yarn/pnpm 通过
	// corepack enable 启用）。CanInstall 在父工具（parentTool）已安装时为 true。
	enableCommand string // 启用命令（如 "corepack enable yarn"），空 = 非附属工具
	parentTool    string // 父工具名（如 "node"），用于检测父工具是否已安装
	// installMethods 列出可选安装方式（如 pipx 可通过 pip 或 uv 安装）。
	// 非空时，安装流程根据可用方法动态选择/弹选项。
	installMethods []installMethod // 空 = 单一安装方式或不可安装
}

// installMethod 描述附属工具的一种安装方式（Spec 026）。
// 例如 pipx 可通过 pip install pipx 或 uv tool install pipx 安装。
type installMethod struct {
	name      string // 方法标识（如 "pip" / "uv"），前端用于选项对话框
	command   string // 执行命令（如 "pip install pipx"）
	detectCmd string // 检测该方法前置工具是否可用的命令名（如 "pip" / "uv"）
}

// GetPackageManagers returns available package managers with their status.
// Spec 026: 两阶段检测——第一阶段并发检测所有 pmDef 的基础状态，
// 第二阶段串行计算附属工具（enableCommand/installMethods 非空）的动态
// CanInstall + InstallHint（复用同批次检测结果，避免新增扫描）。
func (a *App) GetPackageManagers() []PackageManagerInfo {
	pms := packageManagerDefs()

	// Phase 1: 并发检测所有 pmDef 的基础状态（现有逻辑）。
	results := make([]PackageManagerInfo, len(pms))
	var wg sync.WaitGroup
	for i, pm := range pms {
		wg.Add(1)
		go func(idx int, pm pmDef) {
			defer wg.Done()
			results[idx] = a.detectPackageManager(pm)
		}(i, pm)
	}
	wg.Wait()

	// Phase 2: 串行计算附属工具的动态 CanInstall + InstallHint。
	// 构建已检测工具名 → Installed 的索引，供附属工具查父工具状态。
	detectedByName := make(map[string]bool, len(results))
	for _, r := range results {
		if r.Installed {
			detectedByName[r.Name] = true
		}
	}
	for i, pm := range pms {
		if pm.enableCommand == "" && len(pm.installMethods) == 0 {
			continue // 非附属工具，跳过
		}
		results[i] = a.applySubToolDynamicState(pm, results[i], results, detectedByName)
	}
	return results
}

// applySubToolDynamicState 根据同批次检测结果，计算附属工具的动态
// CanInstall + InstallHint + EnableCommand + InstallMethods（Spec 026）。
func (a *App) applySubToolDynamicState(pm pmDef, info PackageManagerInfo, allResults []PackageManagerInfo, detectedByName map[string]bool) PackageManagerInfo {
	parentInstalled := subToolParentInstalled(pm, allResults)

	// 场景1+2 合并：yarn/pnpm 同时有 enableCommand 与 installMethods
	// （corepack / npm）。优先列出 xdev 管理的 Node 或当前 PATH 上可用
	// 的安装方式；父工具已装但方法检测器都不在 PATH 时，仍回退到 enableCommand。
	if len(pm.installMethods) > 0 {
		var availableMethods []InstallMethodInfo
		for _, m := range pm.installMethods {
			if a.subToolMethodAvailable(m.detectCmd) {
				availableMethods = append(availableMethods, InstallMethodInfo{
					Name:    m.name,
					Command: m.command,
				})
			}
		}
		if len(availableMethods) > 0 {
			info.CanInstall = true
			info.InstallMethods = availableMethods
			info.InstallHint = ""
			// 多种安装方式时前端走选择器，不再单独显示「启用」。
			if len(availableMethods) == 1 && pm.enableCommand != "" && availableMethods[0].Command == pm.enableCommand {
				info.EnableCommand = pm.enableCommand
			} else {
				info.EnableCommand = ""
			}
			return info
		}
		if pm.enableCommand != "" && parentInstalled {
			info.CanInstall = true
			info.InstallHint = ""
			info.EnableCommand = pm.enableCommand
			return info
		}
		info.CanInstall = false
		if pm.installHint != "" {
			info.InstallHint = pm.installHint
		} else {
			info.InstallHint = "需先安装 pip 或 uv"
		}
		return info
	}

	// 场景1: 仅 enableCommand（旧附属工具路径）。
	if pm.enableCommand != "" && pm.parentTool != "" {
		if parentInstalled {
			info.CanInstall = true
			info.InstallHint = ""
			info.EnableCommand = pm.enableCommand
		} else {
			info.CanInstall = false
			info.InstallHint = pm.installHint
		}
		return info
	}

	return info
}

func subToolParentInstalled(pm pmDef, allResults []PackageManagerInfo) bool {
	if pm.parentTool == "" {
		return false
	}
	for _, r := range allResults {
		if (r.Name == pm.parentTool || r.ToolName == pm.parentTool) && r.Installed {
			return true
		}
	}
	if _, err := exec.LookPath(pm.parentTool); err == nil {
		return true
	}
	return false
}

// subToolMethodAvailable reports whether an install-method detector
// (corepack / npm / pip / uv) is usable. xdev-managed Node/Python is
// checked first so a D: PATH leftover does not hide E: XDev Node.
func (a *App) subToolMethodAvailable(detectCmd string) bool {
	switch detectCmd {
	case "corepack":
		if _, ok := a.findXdevManagedCorepack(); ok {
			return true
		}
	case "npm":
		if _, ok := a.findXdevManagedNpm(); ok {
			return true
		}
	case "pip":
		if _, ok := a.findXdevManagedPip(); ok {
			return true
		}
	}
	_, err := exec.LookPath(detectCmd)
	return err == nil
}

// detectPackageManager detects a single package manager's status.
func (a *App) detectPackageManager(pm pmDef) PackageManagerInfo {
	// 对于附属工具（detectCmd != toolName，如 yarn/pnpm 是 node 的附属工具），
	// xdev 不能直接安装它们，需要先安装父工具再启用。
	// CanInstall 仅对 xdev 可以直接安装的工具为 true。
	// Spec 025：installCommand 非空时也可自动安装（script install 路径）。
	// Spec 025 L3 审查阻塞项2：CanInstall 结合平台——brew 在 Windows 上
	// CanInstall=false（pmCanInstall 内部用 pmPlatformMatches 检查）。
	canInstall := pmCanInstall(pm)
	info := PackageManagerInfo{
		Name:           pm.name,
		DisplayName:    pm.displayName,
		Description:    pm.description,
		Category:       "package-manager",
		CanInstall:     canInstall,
		ToolName:       pm.toolName,
		InstallHint:    pm.installHint,
		RequiresAdmin:  pm.requiresAdmin,
		InstallCommand: pm.installCommand,
	}

	// Check if available via PATH. Prefer xdev-managed Node over a stale
	// PATH hit (e.g. D: leftover pnpm when Node lives in E:\XDev).
	shimDir := a.getCfg().Core.ShimDir
	installDir := a.getCfg().Core.InstallDir
	if path, ok := hostscan.LookPath(pm.detectCmd, shimDir); ok {
		isXdevManaged := false
		if shimDir != "" && hostscan.InsideDir(path, shimDir) {
			isXdevManaged = true
		}
		if installDir != "" && hostscan.InsideDir(path, installDir) {
			isXdevManaged = true
		}

		if isXdevManaged {
			// 路径在 xdev 管理的目录内，检查 xdev 管理的安装
			if pm.toolName != "" {
				if activeVer, installed := a.currentVersion(pm.toolName); installed {
					// 对于附属工具（yarn/pnpm/npm/npx 等），先验证附属工具可执行文件是否真的存在
					if pm.detectCmd != pm.toolName {
						parentDir := a.currentLink(pm.toolName)
						if target, ok := shim.FindInDir(parentDir, pm.detectCmd); ok {
							info.Installed = true
							info.LocalPath = target
							info.Version = "随 " + pm.toolName + " " + activeVer + " 附带"
						} else {
							// 附属工具可执行文件不存在，不标记为已安装
							// （例如 yarn/pnpm 未通过 corepack enable 启用）
						}
					} else {
						info.Installed = true
						info.LocalPath = a.currentLink(pm.toolName)
						info.Version = activeVer
					}
				}
			}
		} else {
			// 非xdev管理的本地安装
			// 对 yarn/pnpm，检查是否在某个 node 安装目录内
			if (pm.name == "yarn" || pm.name == "pnpm") && isInsideNodeDir(path) {
				// 先验证是否真的可用（corepack shim 可能返回错误）
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				cmd := exec.CommandContext(ctx, path, pm.detectArgs...)
				util.HideWindow(cmd)
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					// 执行失败，可能是 corepack shim 但未启用，不标记为已安装
				} else {
					outputStr := strings.TrimSpace(string(output))
					if idx := strings.Index(outputStr, "\n"); idx > 0 {
						outputStr = outputStr[:idx]
					}
					lower := strings.ToLower(outputStr)
					if strings.Contains(lower, "not available") ||
						(strings.Contains(lower, "corepack") && strings.Contains(lower, "enable")) {
						// corepack shim 但未启用，不标记为已安装
					} else {
						info.Installed = true
						info.LocalPath = path
						if v := toolmeta.ExtractVersion(outputStr); v != "" {
							info.Version = v
						} else {
							info.Version = "随 Node.js 附带"
						}
					}
				}
			} else if pm.name == "yarn" || pm.name == "pnpm" {
				// 非xdev管理且不在 node 目录内的 yarn/pnpm，同样需要验证
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				cmd := exec.CommandContext(ctx, path, pm.detectArgs...)
				util.HideWindow(cmd)
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					// 执行失败，可能是 corepack shim 但未启用，不标记为已安装
				} else {
					outputStr := strings.TrimSpace(string(output))
					if idx := strings.Index(outputStr, "\n"); idx > 0 {
						outputStr = outputStr[:idx]
					}
					lower := strings.ToLower(outputStr)
					if strings.Contains(lower, "not available") ||
						(strings.Contains(lower, "corepack") && strings.Contains(lower, "enable")) {
						// corepack shim 但未启用，不标记为已安装
					} else {
						info.Installed = true
						info.LocalPath = path
						if v := toolmeta.ExtractVersion(outputStr); v != "" {
							info.Version = v
						} else {
							info.Version = outputStr
						}
					}
				}
			} else {
				// Spec 031: list path only; do not exec --version on the catalog row.
				info.Installed = true
				info.LocalPath = path
			}
		}
	}

	// Also check if installed via xdev.
	// Spec 026 增量：对附属工具（detectCmd != toolName，如 pip 是 python 的
	// 附属），即使 PATH 已找到系统版本，也优先检查 xdev 管理的版本并覆盖——
	// 确保 xdev 管理的 pip 等附属工具被正确识别（而非只识别 PATH 中的系统 pip）。
	// 非附属工具（detectCmd == toolName，如 npm）保持原条件：仅 PATH 未找到时才查 xdev。
	// Spec 027 增量：pipx 的 toolName="" 但实际是 python 的附属工具——
	// 当 xdev 安装了 python 时，也检查 python 目录中的 pipx 副本。
	effectiveParent := pm.toolName
	if effectiveParent == "" && pm.name == "pipx" {
		effectiveParent = "python"
	}
	// Independent PMs (uv: detectCmd == toolName) used to skip the xdev
	// lookup once PATH found a D: leftover. Always overlay the xdev
	// current link when it exists so E:\XDev wins the primary row.
	checkXdevManaged := effectiveParent != ""
	if checkXdevManaged {
		if activeVer, installed := a.currentVersion(effectiveParent); installed {
			if pm.detectCmd != effectiveParent {
				// 附属工具：验证可执行文件是否存在
				parentDir := a.currentLink(effectiveParent)
				if target, ok := shim.FindInDir(parentDir, pm.detectCmd); ok {
					info.Installed = true
					info.LocalPath = target
					info.Version = "随 " + effectiveParent + " " + activeVer + " 附带"
				}
			} else {
				info.Installed = true
				info.Version = activeVer
				link := a.currentLink(effectiveParent)
				if target, err := os.Readlink(link); err == nil {
					info.LocalPath = target
				} else {
					info.LocalPath = link
				}
			}
		}
	}

	// HasUpdate: only for independently installable, xdev-managed PMs
	// (detectCmd == toolName, e.g. uv). yarn/pnpm 的 Version 是附属工具
	// 版本，不能和父工具 node 的 latest 比，否则会画出假「更新」按钮。
	if info.Installed && info.CanInstall && pm.toolName != "" && pm.detectCmd == pm.toolName && isComparableVersion(info.Version) {
		if latest := a.GetLatestVersionForTool(pm.toolName); latest != "" && isComparableVersion(latest) {
			info.HasUpdate = util.CompareSemver(latest, info.Version) > 0
		}
	}

	// Spec 027: 收集所有检测到的实例（xdev 管理的 + 系统安装的）。
	info.Instances = a.collectPMInstances(pm, info)

	// Fallback: 如果主检测逻辑未设置 Installed/Version/LocalPath，
	// 但 collectPMInstances 找到了实例，从第一个实例填充——
	// 确保 yarn/pnpm 等附属工具即使 corepack shim 在非标准位置
	// 也能在列表中显示版本和路径（Spec 027 Bug 2 修复）。
	if len(info.Instances) > 0 && (!info.Installed || info.Version == "" || info.LocalPath == "") {
		inst := info.Instances[0]
		if !info.Installed {
			info.Installed = true
		}
		if info.Version == "" {
			info.Version = inst.Version
		}
		if info.LocalPath == "" {
			info.LocalPath = inst.Path
		}
	}

	if (pm.name == "yarn" || pm.name == "pnpm") && a.shouldOfferSubToolInstall(pm, info) {
		info.CanInstall = true
		info.OfferSubToolInstall = true
	}

	return info
}

// shouldOfferSubToolInstall reports whether yarn/pnpm still needs a
// corepack/npm enable action for the xdev-managed Node. True only when
// a local copy exists but no xdev-managed instance does — pipx and
// already-enabled xdev copies must not keep an Install button.
func (a *App) shouldOfferSubToolInstall(pm pmDef, info PackageManagerInfo) bool {
	if pm.name != "yarn" && pm.name != "pnpm" {
		return false
	}
	if !info.Installed {
		return false
	}
	for _, inst := range info.Instances {
		if inst.Managed {
			return false
		}
	}
	if info.LocalPath == "" {
		return true
	}
	cfg := a.getCfg()
	if cfg.Core.ShimDir != "" && isInsideDir(info.LocalPath, cfg.Core.ShimDir) {
		return false
	}
	if cfg.Core.InstallDir != "" && isInsideDir(info.LocalPath, cfg.Core.InstallDir) {
		return false
	}
	return true
}

// collectPMInstances 收集包管理器的所有检测实例（Spec 027）。
// 返回去重后的实例列表：xdev 管理的 + 系统安装的。
// info.LocalPath 是 detectPackageManager 最终选定的路径（xdev 优先）；
// 此函数额外查 PATH 收集系统实例，与 info.LocalPath 去重。
func (a *App) collectPMInstances(pm pmDef, info PackageManagerInfo) []PMInstanceInfo {
	var instances []PMInstanceInfo
	seenPaths := make(map[string]bool)
	shimDir := a.getCfg().Core.ShimDir
	installDir := a.getCfg().Core.InstallDir

	// 1. 已选定的实例（info.LocalPath）——可能是 xdev 管理或系统
	if info.LocalPath != "" {
		normalized := filepath.Clean(strings.ToLower(info.LocalPath))
		managed := false
		if (shimDir != "" && isInsideDir(info.LocalPath, shimDir)) ||
			(installDir != "" && isInsideDir(info.LocalPath, installDir)) {
			managed = true
		}
		source := "local"
		if managed {
			source = "xdev"
		}
		instances = append(instances, PMInstanceInfo{
			Version: info.Version,
			Path:    info.LocalPath,
			Managed: managed,
			Source:  source,
		})
		seenPaths[normalized] = true
	}

	// 2. xdev 管理的附属工具实例（即使 info.LocalPath 已是系统版本，
	// 附属工具可能在 xdev 管理的父工具目录中也有副本）。
	// pipx 的 toolName="" 但实际是 python 的附属工具——当 xdev 安装了
	// python 时，也查 python 目录中的 pipx 副本。
	parentToolName := pm.toolName
	if parentToolName == "" && pm.detectCmd != "python" && pm.name == "pipx" {
		parentToolName = "python"
	}
	if parentToolName != "" {
		if activeVer, installed := a.currentVersion(parentToolName); installed {
			parentDir := a.currentLink(parentToolName)
			var target string
			if pm.detectCmd != parentToolName {
				if found, ok := shim.FindInDir(parentDir, pm.detectCmd); ok {
					target = found
				}
			} else if parentDir != "" {
				if resolved, err := os.Readlink(parentDir); err == nil {
					target = resolved
				} else {
					target = parentDir
				}
			}
			if target != "" {
				normalized := filepath.Clean(strings.ToLower(target))
				if !seenPaths[normalized] {
					ver := activeVer
					if pm.detectCmd != parentToolName && info.Version != "" {
						ver = info.Version
					}
					instances = append(instances, PMInstanceInfo{
						Version: ver,
						Path:    target,
						Managed: true,
						Source:  "xdev",
					})
					seenPaths[normalized] = true
				}
			}
		}
	}

	// 3. PATH 上的全部实例（不只 LookPath 第一个命中）。
	// D: leftover pnpm 和 E: xdev Node 必须同时出现，迁移/启用才看得见。
	// 仍可能落在 xdev shim/install 目录内，必须按路径判定 Managed，
	// 否则已托管副本会被标成系统实例并画出假迁移按钮。
	for _, hit := range hostscan.ScanPATH([]string{pm.detectCmd}, shimDir) {
		path := hit.Path
		normalized := filepath.Clean(strings.ToLower(path))
		if seenPaths[normalized] {
			continue
		}
		ver := info.Version
		if ver == "" {
			ver = "unknown"
		}
		managed := (shimDir != "" && hostscan.InsideDir(path, shimDir)) ||
			(installDir != "" && hostscan.InsideDir(path, installDir))
		source := "local"
		if managed {
			source = "xdev"
		}
		instances = append(instances, PMInstanceInfo{
			Version: ver,
			Path:    path,
			Managed: managed,
			Source:  source,
		})
		seenPaths[normalized] = true
	}

	return instances
}

// InstallPackageManager installs a package manager via xdev.
// toolName is the xdev tool name (e.g. "uv", "node", "python") or the
// pmDef name for script-installable managers (e.g. "scoop", "brew", "choco").
//
// Spec 025: 当 toolName 匹配一个有 installCommand 的 pmDef 时，走 script
// install 路径（后台执行官方安装命令）；否则走现有 xdev 安装管线。
func (a *App) InstallPackageManager(toolName string) error {
	return a.install(InstallSpec{Tool: toolName, Kind: "package-manager"})
}

func (a *App) installPackageManager(spec InstallSpec) error {
	toolName := spec.Tool
	logInfo("[InstallPackageManager] installing %s", toolName)

	// Spec 025: 先查 pmDef 是否有 script install 命令。
	if pm, ok := findPMDefByName(toolName); ok && pm.installCommand != "" {
		return a.installPackageManagerViaScript(pm)
	}

	p, ok := a.registry.Get(toolName)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", toolName)
	}

	ver := spec.Version
	if ver == "" {
		ver = p.DefaultVersion()
	}
	if ver == "" {
		ver = "latest"
	}

	return a.install(InstallSpec{Tool: toolName, Version: ver, Kind: "tool", Confirmed: spec.Confirmed, Force: spec.Force, Form: spec.Form})
}

// InstallSubTool 安装附属工具（Spec 026）。
// toolName 是 pmDef.name（如 "yarn" / "pnpm" / "pipx"）。
// method 指定安装方式（如 "pip" / "uv"）；空表示自动选择：
//   - enableCommand 非空（yarn/pnpm）→ 执行 corepack enable
//   - installMethods 长度==1 → 用唯一 method
//   - installMethods 长度>1 且 method="" → 返回错误"请指定安装方式"
func (a *App) InstallSubTool(toolName, method string) error {
	return a.install(InstallSpec{Tool: toolName, Method: method, Kind: "sub-tool"})
}

func (a *App) installSubTool(spec InstallSpec) error {
	toolName := spec.Tool
	method := spec.Method
	logInfo("[InstallSubTool] %s method=%q", toolName, method)
	pm, ok := findPMDefByName(toolName)
	if !ok {
		return fmt.Errorf("package manager %q not found in pmDef", toolName)
	}

	// 同时有 enableCommand 与 installMethods（yarn/pnpm）时：
	// 指定 method 走对应安装命令；未指定则仍默认 corepack enable。
	if len(pm.installMethods) > 0 && (method != "" || pm.enableCommand == "") {
		var selected *installMethod
		if method == "" {
			if len(pm.installMethods) == 1 {
				selected = &pm.installMethods[0]
			} else {
				return fmt.Errorf("%s 有多种安装方式，请指定 method（%s）",
					pm.displayName, formatMethodNames(pm.installMethods))
			}
		} else {
			for i := range pm.installMethods {
				if pm.installMethods[i].name == method {
					selected = &pm.installMethods[i]
					break
				}
			}
			if selected == nil {
				return fmt.Errorf("unknown install method %q for %s (available: %s)",
					method, pm.displayName, formatMethodNames(pm.installMethods))
			}
		}
		return a.installSubToolViaMethod(pm, *selected)
	}

	if pm.enableCommand != "" {
		return a.installSubToolViaEnable(pm)
	}

	return fmt.Errorf("%q is not a sub-tool (no enableCommand or installMethods)", toolName)
}

// installSubToolViaEnable 执行 corepack enable 命令（yarn/pnpm）。
// 在 PATH 含 node 的环境中执行。
func (a *App) installSubToolViaEnable(pm pmDef) error {
	logInfo("[InstallSubTool] enable: %s", pm.enableCommand)
	// corepack enable yarn → args = ["enable", "yarn"]
	parts := strings.Fields(pm.enableCommand)
	if len(parts) < 2 {
		return fmt.Errorf("invalid enableCommand %q for %s", pm.enableCommand, pm.name)
	}
	// parts[0] = "corepack", parts[1:] = ["enable", "yarn"]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	bin := parts[0]
	if xdevCorepack, ok := a.findXdevManagedCorepack(); ok {
		logInfo("[InstallSubTool] using xdev-managed corepack: %s (instead of PATH corepack)", xdevCorepack)
		bin = xdevCorepack
	}
	cmd := exec.CommandContext(ctx, bin, parts[1:]...)
	// 仅装了 xdev Node 时系统 PATH 可能没有 corepack；前置 shim 目录。
	cmd.Env = a.envWithXdevShims(os.Environ())
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	logInfo("[InstallSubTool] %s enable output:\n%s", pm.name, string(output))
	if err != nil {
		return fmt.Errorf("%s 启用失败: %w\n输出:\n%s", pm.displayName, err, string(output))
	}
	logInfo("[InstallSubTool] %s 启用完成", pm.name)
	a.InvalidateToolsCache()
	return nil
}

// installSubToolViaMethod 执行指定安装方式的命令（pipx via pip/uv）。
// Spec 026 增量：当 method 用 pip 安装时，优先用 xdev 管理的 Python 的 pip，
// 而非 PATH 中的系统 pip——确保 pipx 安装到 xdev 管理的目录。
func (a *App) installSubToolViaMethod(pm pmDef, m installMethod) error {
	logInfo("[InstallSubTool] method %s: %s", m.name, m.command)
	parts := strings.Fields(m.command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command for method %s of %s", m.name, pm.name)
	}

	// Spec 026 增量：若 method 是 pip，优先用 xdev 管理的 Python 的 pip。
	// parts[0] 是 "pip"——替换为 xdev python 的 pip 完整路径。
	if m.detectCmd == "pip" {
		if xdevPip, ok := a.findXdevManagedPip(); ok {
			logInfo("[InstallSubTool] using xdev-managed pip: %s (instead of PATH pip)", xdevPip)
			parts[0] = xdevPip
		}
	}
	if m.detectCmd == "npm" {
		if xdevNpm, ok := a.findXdevManagedNpm(); ok {
			logInfo("[InstallSubTool] using xdev-managed npm: %s (instead of PATH npm)", xdevNpm)
			parts[0] = xdevNpm
		}
	}
	if m.detectCmd == "corepack" {
		if xdevCorepack, ok := a.findXdevManagedCorepack(); ok {
			logInfo("[InstallSubTool] using xdev-managed corepack: %s (instead of PATH corepack)", xdevCorepack)
			parts[0] = xdevCorepack
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	// 确保 PATH 含 xdev 的 shim 目录（这样 pip 能找到 xdev 管理的 python）。
	cmd.Env = a.envWithXdevShims(os.Environ())
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	logInfo("[InstallSubTool] %s via %s output:\n%s", pm.name, m.name, string(output))
	if err != nil {
		return fmt.Errorf("%s 安装失败（%s）: %w\n输出:\n%s", pm.displayName, m.name, err, string(output))
	}
	logInfo("[InstallSubTool] %s via %s 安装完成", pm.name, m.name)
	a.InvalidateToolsCache()
	return nil
}

// findXdevManagedPip 查找 xdev 管理的 Python 的 pip 可执行文件路径。
// 检查 xdev 是否已安装 python → 若已装，在 python 的安装目录中找 pip。
// 返回 pip 完整路径（如 E:\XDev\install\python\3.14.4\Scripts\pip.exe）。
func (a *App) findXdevManagedPip() (string, bool) {
	if activeVer, installed := a.currentVersion("python"); installed {
		pythonDir := a.currentLink("python")
		if pythonDir == "" {
			return "", false
		}
		// pip 通常在 <python_dir>/Scripts/pip (Windows) 或 <python_dir>/bin/pip (Unix)
		candidates := []string{
			filepath.Join(pythonDir, "Scripts", "pip.exe"), // Windows
			filepath.Join(pythonDir, "Scripts", "pip"),     // Windows (no .exe)
			filepath.Join(pythonDir, "bin", "pip"),         // Unix
		}
		for _, c := range candidates {
			if path, err := exec.LookPath(c); err == nil {
				return path, true
			}
			// 也检查文件直接存在（LookPath 可能因权限/PATH 问题失败）
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c, true
			}
		}
		logInfo("[InstallSubTool] xdev python %s installed but pip not found in %s", activeVer, pythonDir)
	}
	return "", false
}

// findXdevManagedNpm 查找 xdev 管理的 Node 的 npm 可执行文件路径。
func (a *App) findXdevManagedNpm() (string, bool) {
	return a.findXdevManagedNodeBin("npm")
}

// findXdevManagedCorepack 查找 xdev 管理的 Node 的 corepack 可执行文件路径。
func (a *App) findXdevManagedCorepack() (string, bool) {
	return a.findXdevManagedNodeBin("corepack")
}

// findXdevManagedNodeBin 在 xdev 管理的 Node 安装目录中查找 npm/corepack。
func (a *App) findXdevManagedNodeBin(bin string) (string, bool) {
	if _, installed := a.currentVersion("node"); !installed {
		return "", false
	}
	nodeDir := a.currentLink("node")
	if nodeDir == "" {
		return "", false
	}
	candidates := []string{
		filepath.Join(nodeDir, bin+".cmd"),
		filepath.Join(nodeDir, bin+".exe"),
		filepath.Join(nodeDir, bin),
		filepath.Join(nodeDir, "bin", bin),
		filepath.Join(nodeDir, "bin", bin+".cmd"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}
	return "", false
}

// envWithXdevShims 返回追加 xdev shim 目录到 PATH 的环境变量列表，
// 确保 exec.Command 能找到 xdev 管理的工具（如 python）。
func (a *App) envWithXdevShims(base []string) []string {
	cfg := a.getCfg()
	var prefixes []string
	if cfg.Core.ShimDir != "" {
		prefixes = append(prefixes, cfg.Core.ShimDir)
	}
	// corepack/npm.cmd 需要同目录的 node.exe；仅有 shim 时 PATH 上的
	// D: 旧 Node 仍可能抢先。把 xdev Node 安装目录一并前置。
	if nodeDir := a.currentLink("node"); nodeDir != "" {
		if fi, err := os.Stat(nodeDir); err == nil && fi.IsDir() {
			prefixes = append(prefixes, nodeDir)
			binDir := filepath.Join(nodeDir, "bin")
			if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
				prefixes = append(prefixes, binDir)
			}
		}
	}
	if len(prefixes) == 0 {
		return base
	}
	prefix := strings.Join(prefixes, string(os.PathListSeparator))
	for i, e := range base {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			pathVal := e[5:] // strip "PATH="
			base[i] = "PATH=" + prefix + string(os.PathListSeparator) + pathVal
			return base
		}
	}
	return append(base, "PATH="+prefix)
}

// formatMethodNames 返回 installMethod 列表的 name 逗号分隔串（错误信息用）。
func formatMethodNames(methods []installMethod) string {
	names := make([]string, 0, len(methods))
	for _, m := range methods {
		names = append(names, m.name)
	}
	return strings.Join(names, ", ")
}

// MigratePackageManager 把系统安装的包管理器迁移到 xdev 管理目录（Spec 027）。
//
// 对于附属工具（pip/pipx 等 toolName 非空且 detectCmd != toolName）：
// 在 xdev 管理的父工具目录中创建附属工具的链接/复制——
// 例如 pip 迁移 = 在 E:\XDev\install\python\3.14.4\Scripts\ 创建 pip 链接。
//
// 对于独立工具（scoop/brew/choco）：
// 复制目录到 xdev install dir + 创建 current 链接（类似 MigrateLocalTool）。
func (a *App) MigratePackageManager(toolName string) error {
	logInfo("[MigratePackageManager] migrating %s", toolName)
	pm, ok := findPMDefByName(toolName)
	if !ok {
		// 前端曾误传父工具名（pip.toolName=python）；按 toolName 字段反查一次。
		if alt, found := findPMDefByToolName(toolName); found {
			pm = alt
			ok = true
			logInfo("[MigratePackageManager] resolved %q via toolName to pm %q", toolName, pm.name)
		}
	}
	if !ok {
		return fmt.Errorf("package manager %q not found in pmDef", toolName)
	}

	// 查找系统安装的实例（非 xdev 管理的）。
	var systemPath string
	instances := a.collectPMInstances(pm, PackageManagerInfo{LocalPath: ""})
	for _, inst := range instances {
		if !inst.Managed && inst.Path != "" {
			systemPath = inst.Path
			break
		}
	}
	if systemPath == "" {
		return fmt.Errorf("%s 没有可迁移的系统实例", pm.displayName)
	}

	// 附属工具迁移：在 xdev 管理的父工具目录创建链接。
	// pipx 的 toolName="" 但实际是 python 的附属工具——当 xdev 安装了
	// python 时，也走附属工具路径（用 python 作为 parentTool）。
	if pm.toolName != "" && pm.detectCmd != pm.toolName {
		return a.migrateSubToolToXdev(pm, systemPath, pm.toolName)
	}
	if pm.toolName == "" && pm.detectCmd != "python" {
		// pipx 等 toolName="" 的 Python 附属工具：检查 xdev 是否安装了 python。
		if _, installed := a.currentVersion("python"); installed {
			return a.migrateSubToolToXdev(pm, systemPath, "python")
		}
	}

	// 独立工具迁移：复制到 xdev install dir。
	return a.migrateIndependentPMToXdev(pm, systemPath)
}

// migrateSubToolToXdev 把附属工具（pip/pipx）从系统路径迁移到 xdev 管理
// 的父工具目录（Spec 027）。通过复制可执行文件实现。
// parentToolName 指定父工具名（pm.toolName 或 "python" 用于 pipx 等 toolName="" 的附属工具）。
func (a *App) migrateSubToolToXdev(pm pmDef, systemPath, parentToolName string) error {
	if parentToolName == "" {
		return fmt.Errorf("%s is not a sub-tool (parentToolName empty)", pm.name)
	}
	if _, installed := a.currentVersion(parentToolName); !installed {
		return fmt.Errorf("父工具 %s 未通过 xdev 安装，无法迁移附属工具 %s", parentToolName, pm.displayName)
	}
	parentDir := a.currentLink(parentToolName)
	if parentDir == "" {
		return fmt.Errorf("无法确定 %s 的安装目录", parentToolName)
	}

	// 确定目标目录：Windows 在 Scripts/，Unix 在 bin/
	var targetDir string
	if runtime.GOOS == "windows" {
		targetDir = filepath.Join(parentDir, "Scripts")
	} else {
		targetDir = filepath.Join(parentDir, "bin")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建目标目录 %s 失败: %w", targetDir, err)
	}

	// 构造目标路径：同名可执行文件（保留 .exe 后缀如果系统路径有）
	targetName := filepath.Base(systemPath)
	targetPath := filepath.Join(targetDir, targetName)

	// 如果目标已存在，跳过（xdev 管理版本已存在）。
	if _, err := os.Stat(targetPath); err == nil {
		logInfo("[MigratePackageManager] %s 已存在于 xdev 目录 %s，跳过", pm.name, targetPath)
		return nil
	}

	// 复制可执行文件（附属工具是单个文件，复制最安全可靠）。
	if err := util.CopyFile(systemPath, targetPath); err != nil {
		return fmt.Errorf("迁移 %s 到 %s 失败: %w", pm.displayName, targetPath, err)
	}

	logInfo("[MigratePackageManager] %s 迁移完成: %s -> %s", pm.name, systemPath, targetPath)
	a.InvalidateToolsCache()
	return nil
}

// migrateIndependentPMToXdev 把独立包管理器（scoop/brew/choco）从系统路径
// 迁移到 xdev install dir（Spec 027）。复制目录 + 创建 current 链接。
func (a *App) migrateIndependentPMToXdev(pm pmDef, systemPath string) error {
	cfg := a.getCfg()
	installDir := cfg.Core.InstallDir
	if installDir == "" {
		return fmt.Errorf("xdev install dir 未配置")
	}

	// 推断版本号（从系统实例）
	ver := ""
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	cmd := exec.CommandContext(ctx, systemPath, pm.detectArgs...)
	util.HideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err == nil {
		line := strings.TrimSpace(string(output))
		if idx := strings.Index(line, "\n"); idx > 0 {
			line = line[:idx]
		}
		ver = toolmeta.ExtractVersion(line)
	}
	cancel()
	if ver == "" {
		ver = "unknown"
	}

	// 目标路径：<installDir>/<pm.name>/<ver>
	targetDir := filepath.Join(installDir, pm.name, ver)
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("%s %s 已存在于 xdev 目录，无需迁移", pm.displayName, ver)
	}

	srcDir, err := inferIndependentPMRoot(pm, systemPath)
	if err != nil {
		return err
	}
	if err := util.CopyDir(srcDir, targetDir); err != nil {
		os.RemoveAll(targetDir)
		return fmt.Errorf("复制 %s 到 %s 失败: %w", srcDir, targetDir, err)
	}

	// 创建 current 链接
	link := filepath.Join(cfg.Core.XMDevDir, "current", pm.name)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return fmt.Errorf("创建 current 目录失败: %w", err)
	}
	// 移除旧链接（如果存在）
	os.Remove(link)
	// 创建 current 链接（Windows 用 junction，Unix 用 symlink）。
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(link, targetDir); err != nil {
			return fmt.Errorf("创建 current junction 失败: %w", err)
		}
	} else {
		if err := os.Symlink(targetDir, link); err != nil {
			return fmt.Errorf("创建 current symlink 失败: %w", err)
		}
	}

	logInfo("[MigratePackageManager] %s 迁移完成: %s -> %s", pm.name, srcDir, targetDir)
	a.InvalidateToolsCache()
	return nil
}

// findPMDefByName 在 GetPackageManagers 使用的 pmDef 列表中按 name 查找。
// 返回匹配的 pmDef（scoop/brew/choco 等用于 script install）。
func findPMDefByName(name string) (pmDef, bool) {
	for _, pm := range packageManagerDefs() {
		if pm.name == name {
			return pm, true
		}
	}
	return pmDef{}, false
}

// findPMDefByToolName 按父工具/xdev 工具名反查附属 PM（pip→python）。
// 多个附属工具共享同一 toolName 时返回第一个，仅作兼容兜底。
func findPMDefByToolName(toolName string) (pmDef, bool) {
	if toolName == "" {
		return pmDef{}, false
	}
	for _, pm := range packageManagerDefs() {
		if pm.toolName == toolName && pm.detectCmd != pm.toolName {
			return pm, true
		}
	}
	return pmDef{}, false
}

// inferIndependentPMRoot 从系统可执行文件路径解析独立包管理器的安装根。
// LookPath 通常命中 scoop/shims、chocolatey/bin、homebrew/bin，不能直接用 Dir(exe)。
func inferIndependentPMRoot(pm pmDef, exePath string) (string, error) {
	if exePath == "" {
		return "", fmt.Errorf("%s 可执行路径为空，无法推断安装根", pm.displayName)
	}
	exePath = filepath.Clean(exePath)
	switch pm.name {
	case "scoop":
		if root := strings.TrimSpace(os.Getenv("SCOOP")); root != "" {
			return filepath.Clean(root), nil
		}
		dir := filepath.Dir(exePath)
		base := strings.ToLower(filepath.Base(dir))
		if base == "shims" || base == "bin" {
			return filepath.Dir(dir), nil
		}
		return dir, nil
	case "choco":
		if root := strings.TrimSpace(os.Getenv("ChocolateyInstall")); root != "" {
			return filepath.Clean(root), nil
		}
		dir := filepath.Dir(exePath)
		if strings.EqualFold(filepath.Base(dir), "bin") {
			return filepath.Dir(dir), nil
		}
		return dir, nil
	case "brew":
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, exePath, "--prefix")
		util.HideWindow(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			if prefix := strings.TrimSpace(string(out)); prefix != "" {
				return filepath.Clean(prefix), nil
			}
		}
		dir := filepath.Dir(exePath)
		if strings.EqualFold(filepath.Base(dir), "bin") {
			return filepath.Dir(dir), nil
		}
		return dir, nil
	default:
		return filepath.Dir(exePath), nil
	}
}

// packageManagerDefs 返回包管理器定义列表（GetPackageManagers 共享）。
// 抽出为独立函数以便 findPMDefByName 在 InstallPackageManager 中复用。
func packageManagerDefs() []pmDef {
	return []pmDef{
		{"npx", "NPX", "Node 包执行器 - 运行 npm 包", "npx", []string{"--version"}, "node", "随 Node.js 自动安装", "", false, "", "", "", "", nil},
		{"npm", "NPM", "Node 包管理器", "npm", []string{"--version"}, "node", "随 Node.js 自动安装", "", false, "", "", "", "", nil},
		// Spec 026: yarn/pnpm 是附属工具，通过 corepack enable 启用。
		// enableCommand 非空时，detectPackageManager 两阶段检测会查 parentTool
		// （node）是否已安装，动态设置 CanInstall + InstallHint。
		{name: "yarn", displayName: "Yarn", description: "快速、可靠的依赖管理工具", detectCmd: "yarn", detectArgs: []string{"--version"}, toolName: "node", installHint: "需先安装 Node.js，然后通过 corepack enable 或 npm install -g yarn 安装",
			enableCommand: "corepack enable yarn", parentTool: "node",
			installMethods: []installMethod{
				{name: "corepack", command: "corepack enable yarn", detectCmd: "corepack"},
				{name: "npm", command: "npm install -g yarn", detectCmd: "npm"},
			}},
		{name: "pnpm", displayName: "PNPM", description: "快速、节省磁盘的包管理器", detectCmd: "pnpm", detectArgs: []string{"--version"}, toolName: "node", installHint: "需先安装 Node.js，然后通过 corepack enable 或 npm install -g pnpm 安装",
			enableCommand: "corepack enable pnpm", parentTool: "node",
			installMethods: []installMethod{
				{name: "corepack", command: "corepack enable pnpm", detectCmd: "corepack"},
				{name: "npm", command: "npm install -g pnpm", detectCmd: "npm"},
			}},
		{"uvx", "UVX", "通过 UV 运行 Python 工具", "uvx", []string{"--version"}, "uv", "", "", false, "", "", "", "", nil},
		{"uv", "UV", "极速 Python 包安装器", "uv", []string{"--version"}, "uv", "", "", false, "", "", "", "", nil},
		{"pip", "PIP", "Python 包安装器", "pip", []string{"--version"}, "python", "随 Python 自动安装", "", false, "", "", "", "", nil},
		// Spec 026: pipx 可通过 pip 或 uv 安装。installMethods 非空时，
		// detectPackageManager 两阶段检测会查每个 method 的 detectCmd 是否
		// 可用，动态设置 CanInstall + InstallHint + InstallMethods。
		{name: "pipx", displayName: "PIPX", description: "在隔离环境中安装运行 Python 应用", detectCmd: "pipx", detectArgs: []string{"--version"}, toolName: "", installHint: "通过 pip install pipx 或 uv tool install pipx 安装",
			installMethods: []installMethod{
				{name: "pip", command: "pip install pipx", detectCmd: "pip"},
				{name: "uv", command: "uv tool install pipx", detectCmd: "uv"},
			}},
		{"cargo", "Cargo", "Rust 包管理器", "cargo", []string{"--version"}, "rust", "随 Rust 自动安装", "", false, "", "", "", "", nil},
		{"maven", "Maven", "Java 构建自动化工具", "mvn", []string{"--version"}, "maven", "随 Maven 自动安装", "", false, "", "", "", "", nil},
		{name: "brew", displayName: "Homebrew", description: "macOS/Linux 包管理器", detectCmd: "brew", detectArgs: []string{"--version"}, installHint: "访问 https://brew.sh 安装",
			installCommand: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`,
			requiresAdmin:  false, executionEnv: "bash", platform: "unix"}, // darwin/linux/WSL，非 windows
		{name: "scoop", displayName: "Scoop", description: "Windows 命令行安装器", detectCmd: "scoop", detectArgs: []string{"--version"}, installHint: "访问 https://scoop.sh 安装",
			installCommand: "irm get.scoop.sh | iex",
			requiresAdmin:  false, executionEnv: "powershell", platform: "windows"},
		{name: "choco", displayName: "Chocolatey", description: "Windows 包管理器", detectCmd: "choco", detectArgs: []string{"--version"}, installHint: "访问 https://chocolatey.org 安装",
			installCommand: `Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))`,
			requiresAdmin:  true, executionEnv: "powershell", platform: "windows"},
	}
}

// installPackageManagerViaScript 执行包管理器的官方安装命令（Spec 025）。
// 后台执行 + 捕获输出到 GUI log + cmd.Wait() 防僵尸。
func (a *App) installPackageManagerViaScript(pm pmDef) error {
	logInfo("[InstallPackageManager] script install: %s (env=%s, admin=%v)", pm.name, pm.executionEnv, pm.requiresAdmin)

	// 平台检查
	if !pmPlatformMatches(pm.platform) {
		return fmt.Errorf("%s 仅支持 %s 平台（当前: %s）", pm.displayName, pm.platform, runtime.GOOS)
	}

	// 管理员权限检查（Windows）
	if pm.requiresAdmin && runtime.GOOS == "windows" {
		if !isWindowsAdmin() {
			return fmt.Errorf("%s 需要管理员权限。请以管理员身份重启 xdev，或在管理员 PowerShell 中手动执行以下命令：\n\n%s", pm.displayName, pm.installCommand)
		}
	}

	// 构造 exec.Command
	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	switch pm.executionEnv {
	case "powershell":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", pm.installCommand)
	case "bash":
		cmd = exec.CommandContext(ctx, "bash", "-c", pm.installCommand)
		// Homebrew 安装脚本默认交互式确认，设 NONINTERACTIVE=1 跳过。
		if pm.name == "brew" {
			cmd.Env = append(os.Environ(), "NONINTERACTIVE=1")
		}
	default:
		return fmt.Errorf("unknown executionEnv %q for %s", pm.executionEnv, pm.name)
	}

	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	logInfo("[InstallPackageManager] %s output:\n%s", pm.name, string(output))
	if err != nil {
		return fmt.Errorf("%s 安装失败: %w\n输出:\n%s", pm.displayName, err, string(output))
	}
	logInfo("[InstallPackageManager] %s 安装完成", pm.name)
	// Spec 025 L3 审查阻塞项4：脚本安装成功后失效工具列表缓存，
	// 使下次 GetToolsByCategory("package-manager") 重新检测安装状态，
	// 而非在 60s TTL 内继续显示"未安装"。
	a.InvalidateToolsCache()
	return nil
}

// isWindowsAdmin 检测当前进程是否以管理员权限运行。
// Windows 实现在 is_windows_admin_windows.go（用 token elevation API）；
// 非 Windows 实现在 is_windows_admin_other.go（返回 false）。
// 调用方应在 runtime.GOOS=="windows" 时才依赖其返回值。

// pmPlatformMatches 报告 pmDef.platform 是否匹配当前 runtime.GOOS。
// 支持的平台值：windows / darwin / linux / unix（=darwin+linux）/ any。
// Spec 025 L3 审查阻塞项2：brew platform 从 "any" 改为 "unix"，避免
// 在 Windows 上尝试运行 Homebrew。
func pmPlatformMatches(platform string) bool {
	switch platform {
	case "any":
		return true
	case "windows":
		return runtime.GOOS == "windows"
	case "darwin":
		return runtime.GOOS == "darwin"
	case "linux":
		return runtime.GOOS == "linux"
	case "unix":
		return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	default:
		return false
	}
}

// pmCanInstall 报告包管理器在当前平台是否可安装。
// Spec 025 L3 审查阻塞项2：CanInstall 结合平台——brew 在 Windows 上
// CanInstall=false（避免前端展示可安装但实际无法执行）。
func pmCanInstall(pm pmDef) bool {
	if pm.installCommand != "" {
		return pmPlatformMatches(pm.platform)
	}
	// 现有 xdev 管线安装：toolName 非空且 detectCmd==toolName
	return pm.toolName != "" && pm.detectCmd == pm.toolName
}

// UpdatePackageManager updates a package manager to the latest version.
// toolName 是 pmDef.name。独立工具（uv）走 xdev InstallTool；
// 附属工具（yarn/pnpm，detectCmd ≠ toolName）没有独立 catalog 条目，
// 不能拿 "pnpm" 去 GetAvailableVersions/InstallTool。
func (a *App) UpdatePackageManager(toolName string) error {
	logInfo("[UpdatePackageManager] updating %s", toolName)
	installName := toolName
	if pm, ok := findPMDefByName(toolName); ok {
		if pm.toolName != "" && pm.detectCmd != pm.toolName {
			return fmt.Errorf("%s 是 %s 的附属工具，请在详情抽屉启用/安装，不能按独立工具更新", pm.displayName, pm.toolName)
		}
		if pm.toolName != "" {
			installName = pm.toolName
		}
	}

	versions := a.GetAvailableVersions(installName)
	if len(versions) == 0 {
		return fmt.Errorf("no versions available for %s", installName)
	}

	latestVer := versions[0].Version // first is latest after SortVersionsByRelease
	// Frontend already confirmed sniff-discovered versions (Naive dialog).
	// Passing Confirmed skips the native Wails MessageDialog, which is
	// hidden behind the frameless window and looks like "cancelled by user".
	return a.install(InstallSpec{Tool: installName, Version: latestVer, Confirmed: true})
}

// ---- Local environment detection helpers ----

// detectToolVersion detects the version of a tool from its name and path.
// It tries getVersion first, then extractVersionFromPath, defaulting to "local".
// The result is sanitized for use as a directory name.
func (a *App) detectToolVersion(name, path string) string {
	ver := getVersion(name, path)
	if ver == "" {
		ver = extractVersionFromPath(path)
	}
	if ver == "" {
		ver = "local"
	}
	return sanitizeVersionStr(ver)
}

// detectLocalToolPath tries to find a tool on the system PATH and
// common install directories WITHOUT spawning any subprocesses.
// It returns the path to the tool binary, or "" if not found.
func detectLocalToolPath(name string, shims []string, shimDir string) string {
	return detectLocalToolPathIndexed(name, shims, shimDir, nil)
}

func detectLocalToolPathIndexed(name string, shims []string, shimDir string, byBase map[string]hostscan.Hit) string {
	searchNames := shims
	if len(searchNames) == 0 {
		searchNames = []string{name}
	}
	if runtime.GOOS == "windows" {
		expanded := make([]string, 0, len(searchNames)*2)
		for _, s := range searchNames {
			expanded = append(expanded, s, s+".exe")
		}
		searchNames = expanded
	}

	// 1. Check environment variables first.
	if envPath := toolmeta.CheckEnvVars(name); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	// 2. PATH lookup via hostscan (skips xdev shim copies).
	if byBase != nil {
		if path, ok := hostscan.FirstInIndex(searchNames, byBase); ok {
			return path
		}
	} else if path, ok := hostscan.FirstOnPATH(searchNames, shimDir); ok {
		return path
	}

	// 3. Check common install directories.
	commonPaths := toolmeta.CommonInstallPaths(name)
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 4. Check npm global bin dirs. npm-installed CLIs (codex, opencode,
	//    claude-code, ...) may live in the npm global bin even when that
	//    dir is not on PATH. Precise per-file check (never returns a bare
	//    directory), so this cannot produce false positives for tools
	//    that are not actually npm-installed.
	if p := detectInNpmGlobalBins(searchNames); p != "" {
		return p
	}

	return ""
}

// detectInNpmGlobalBins looks for any of the given shim names inside npm's
// global bin directory (Windows: %APPDATA%\npm; Unix: ~/.npm-global/bin,
// ~/npm/bin, ~/.local/bin). Returns the first existing file, or "".
func detectInNpmGlobalBins(searchNames []string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var dirs []string
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			dirs = append(dirs, filepath.Join(appData, "npm"))
		}
	} else {
		dirs = append(dirs,
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, "npm", "bin"),
			filepath.Join(home, ".local", "bin"),
		)
	}
	for _, dir := range dirs {
		for _, s := range searchNames {
			for _, cand := range []string{filepath.Join(dir, s), filepath.Join(dir, s+".cmd")} {
				if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
					return cand
				}
			}
		}
	}
	return ""
}

// isInsideDir 委托 hostscan.InsideDir（xdev 目录 vs PATH 的判定只在一处）。
func isInsideDir(path, dir string) bool {
	return hostscan.InsideDir(path, dir)
}

// checkEnvVars checks well-known environment variables for tool locations.
// Returns the path to the tool binary if found.
//（Spec 033：实现移入 internal/toolmeta.CheckEnvVars，此包裹函数已删除）

// getBinNamesForTool / checkEnvVars 已收口到 internal/toolmeta
// （Spec 033 R1 / R4）：查询走 toolmeta.BinNames / toolmeta.CheckEnvVars，
// android-* 子目录特判逻辑在 toolmeta/paths.go。

// getVersion attempts to get the version string from a tool binary.
// Uses a 2-second timeout to prevent hanging on unresponsive tools (e.g. Java).
// If path is a directory, it will try to find the binary inside it.
func getVersion(name, path string) string {
	start := time.Now()

	// If path is a directory, try to find the binary inside it
	binPath := path
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		binPath = findBinaryInDir(name, path)
		if binPath == "" {
			logDebug("[getVersion] %s: no binary found in directory %s", name, path)
			return ""
		}
	} else if err != nil {
		logDebug("[getVersion] %s: path %s does not exist", name, path)
		return ""
	}

	// 版本参数查 toolmeta 表（Spec 033 R2 收口）
	flags := toolmeta.VersionArg(name)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, flags...)
	// On Windows, prevent each exec.Command from flashing a console
	// window.  Without this, probing 35 tools creates dozens of
	// visible cmd.exe popups that flash at random screen positions.
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logDebug("[getVersion] %s (%s) failed in %v: %v", name, binPath, time.Since(start), err)
		return ""
	}

	// Parse version from output: iterate lines in order and return the
	// first line that yields an extractable version. This fixes tools whose
	// first output line is a banner/separator (e.g. `gradle -version` prints
	// "------------------------------------------------------------" first,
	// then "Gradle 9.4.1") — previously the raw separator became the version
	// and later failed SafeName during migration ("unsafe-version-blocked").
	ver := toolmeta.ExtractVersionFromOutput(string(output))
	logDebug("[getVersion] %s → %q (in %v)", name, ver, time.Since(start))
	return ver
}

// extractVersionFromOutput / extractVersion / stripWindowsSuffix / isVersionLike
// 已收口到 internal/toolmeta（Spec 033 R3）：gui 侧统一调用
// toolmeta.ExtractVersionFromOutput / toolmeta.ExtractVersion /
// toolmeta.StripWindowsSuffix / toolmeta.IsVersionLike。

// findBinaryInDir finds the executable binary for a tool inside a directory.
// It checks bin/, the root directory, and common naming patterns.
func findBinaryInDir(name, dir string) string {
	// 二进制名查 toolmeta 表（Spec 033 R1 收口）
	binNames := toolmeta.BinNames(name)

	for _, binName := range binNames {
		candidates := []string{
			filepath.Join(dir, "bin", binName),
			filepath.Join(dir, binName),
		}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(dir, "bin", binName+".exe"),
				filepath.Join(dir, "bin", binName+".cmd"),
				filepath.Join(dir, binName+".exe"),
				filepath.Join(dir, binName+".cmd"),
			)
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
	}
	return ""
}

// dirSizeFast recursively calculates directory size using os.ReadDir,
// which is faster than filepath.Walk because it reads directory entries
// in bulk rather than calling os.Lstat on every file individually.
func dirSizeFast(path string, size *int64) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			dirSizeFast(filepath.Join(path, e.Name()), size)
		} else {
			info, err := e.Info()
			if err == nil {
				*size += info.Size()
			}
		}
	}
}

// getCommonInstallPaths 已收口到 internal/toolmeta.CommonInstallPaths
// （Spec 033 R4）：Windows/darwin/linux + nvm/pyenv/sdkman 探测逻辑在 toolmeta/paths.go。

// sanitizeVersionStr 清理版本号字符串，使其适合作为目录名
func sanitizeVersionStr(ver string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, ver)
}

// findLocalBinaryForShim finds the local binary path for a specific shim name.
func findLocalBinaryForShim(toolName, shimName, localPath string) string {
	// If localPath is a binary, check if it matches the shim
	if !strings.HasSuffix(localPath, string(os.PathSeparator)) {
		base := filepath.Base(localPath)
		// Strip extension
		if runtime.GOOS == "windows" {
			base = strings.TrimSuffix(base, ".exe")
		}
		if base == shimName {
			return localPath
		}
	}

	// Look in the tool's bin directory
	dir := localPath
	fi, err := os.Stat(localPath)
	if err == nil && !fi.IsDir() {
		dir = filepath.Dir(localPath)
		if filepath.Base(dir) == "bin" {
			dir = filepath.Dir(dir)
		}
	}

	// Check bin/ subdirectory
	candidates := []string{
		filepath.Join(dir, "bin", shimName),
		filepath.Join(dir, "bin", shimName+".exe"),
		filepath.Join(dir, "bin", shimName+".cmd"),
		filepath.Join(dir, shimName),
		filepath.Join(dir, shimName+".exe"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// extractVersionFromPath tries to extract a version number from a file path.
// For example: "C:\Program Files\Java\jdk-17.0.2" -> "17.0.2"
//
//	"D:\Env\Python\Python312" -> "3.12"
//	"D:\Env\node-v18.20.4" -> "18.20.4"
func extractVersionFromPath(path string) string {
	// Walk up the path components and try to extract a version from each
	dir := path
	for {
		base := filepath.Base(dir)
		if ver := toolmeta.ExtractVersion(base); ver != "" {
			return ver
		}
		// Try special patterns before going up
		if ver := extractSpecialVersionPattern(base); ver != "" {
			return ver
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// extractSpecialVersionPattern handles non-standard version patterns in directory names.
// Examples: "Python312" → "3.12", "jdk17" → "17", "go1.22" → "1.22",
//
//	"node-v18.20.4-win-x64" → "18.20.4", "gcc-13.2.0" → "13.2.0"
func extractSpecialVersionPattern(s string) string {
	sLower := strings.ToLower(s)

	// Pattern: Python312 → 3.12, Python311 → 3.11, Python310 → 3.10
	if strings.HasPrefix(sLower, "python") {
		rest := s[6:] // after "python"
		if len(rest) >= 2 && rest[0] >= '0' && rest[0] <= '9' {
			// Python312 → 3.12
			if len(rest) == 2 {
				return string(rest[0]) + "." + string(rest[1])
			}
			// Python3120 → 3.12.0
			if len(rest) >= 3 && rest[1] >= '0' && rest[1] <= '9' {
				return string(rest[0]) + "." + rest[1:]
			}
		}
	}

	// Pattern: jdk-17, jdk-11, jdk17, jdk11
	if strings.HasPrefix(sLower, "jdk") {
		rest := s[3:]
		rest = strings.TrimPrefix(rest, "-")
		rest = strings.TrimPrefix(rest, "_")
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			if ver := toolmeta.ExtractVersion(rest); ver != "" {
				return ver
			}
			// jdk17 → 17
			return rest
		}
	}

	// Pattern: node-v18.20.4-win-x64 → 18.20.4
	if strings.HasPrefix(sLower, "node") {
		rest := s[4:]
		rest = strings.TrimPrefix(rest, "-")
		rest = strings.TrimPrefix(rest, "v")
		if ver := toolmeta.ExtractVersion(rest); ver != "" {
			return ver
		}
	}

	// Pattern: go1.22.0 → 1.22.0
	if strings.HasPrefix(sLower, "go") && len(s) > 2 && s[2] >= '0' && s[2] <= '9' {
		if ver := toolmeta.ExtractVersion(s[2:]); ver != "" {
			return ver
		}
	}

	return ""
}

// FixLocalVersions scans the install directory for tools with version "local"
// and attempts to detect their real version number. If detected, the directory
// is renamed from <tool>/local to <tool>/<real_version> and the current junction
// is updated accordingly. Returns the list of fixed tools.
func (a *App) FixLocalVersions() []FixedVersion {
	cfg := a.getCfg()
	installDir := cfg.Core.InstallDir
	currentDir := filepath.Join(cfg.Core.XMDevDir, "current")

	var fixed []FixedVersion

	entries, err := os.ReadDir(installDir)
	if err != nil {
		return fixed
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		toolName := entry.Name()
		localPath := filepath.Join(installDir, toolName, "local")

		fi, err := os.Stat(localPath)
		if err != nil || !fi.IsDir() {
			continue
		}

		// Found a "local" version directory — try to detect real version
		detectedVer := a.detectToolVersion(toolName, localPath)
		// Also try reading the junction target path for version hints
		if detectedVer == "local" {
			currentLink := filepath.Join(currentDir, toolName)
			if target, err := os.Readlink(currentLink); err == nil {
				if ver := extractVersionFromPath(target); ver != "" {
					detectedVer = sanitizeVersionStr(ver)
				}
			}
		}

		if detectedVer == "local" {
			logInfo("[FixLocalVersions] %s: cannot detect version, keeping 'local'", toolName)
			continue
		}

		newPath := filepath.Join(installDir, toolName, detectedVer)

		// Check if the target directory already exists
		if _, err := os.Stat(newPath); err == nil {
			logInfo("[FixLocalVersions] %s: target %s already exists, skipping", toolName, detectedVer)
			continue
		}

		// Rename the directory
		if err := os.Rename(localPath, newPath); err != nil {
			logWarn("[FixLocalVersions] %s: rename failed: %v", toolName, err)
			continue
		}

		logInfo("[FixLocalVersions] %s: renamed local → %s", toolName, detectedVer)

		// Update the current junction if it was pointing to "local"
		currentLink := filepath.Join(currentDir, toolName)
		if target, err := os.Readlink(currentLink); err == nil {
			// Check if the current junction points to the "local" directory
			if filepath.Base(target) == "local" && filepath.Dir(target) == filepath.Join(installDir, toolName) {
				os.Remove(currentLink)
				if err := createJunctionForFix(currentLink, newPath); err != nil {
					logWarn("[FixLocalVersions] %s: failed to update current junction: %v", toolName, err)
				} else {
					logInfo("[FixLocalVersions] %s: updated current junction → %s", toolName, detectedVer)
				}
			}
		}

		fixed = append(fixed, FixedVersion{
			Tool:   toolName,
			OldVer: "local",
			NewVer: detectedVer,
		})
	}

	// Refresh cache after fixing
	if len(fixed) > 0 {
		a.refreshAndSaveLocalCache()
	}

	return fixed
}

// FixedVersion represents a tool whose "local" version was renamed to a real version.
type FixedVersion struct {
	Tool   string `json:"tool"`
	OldVer string `json:"oldVer"`
	NewVer string `json:"newVer"`
}

// createJunctionForFix creates a Windows junction (directory reparse point).
func createJunctionForFix(link, target string) error {
	return util.CreateJunction(link, target)
}
