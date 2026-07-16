// app_local_detect.go implements local tool detection for the GUI.
//
// ScanLocalEnvironments scans the system for development tools that are
// already installed but not managed by xdev. It checks:
//   - Tools on the system PATH (via exec.LookPath)
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

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/shim"
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
	var result []ToolInfo
	for _, t := range tools {
		localVer, localPath, _ := a.getLocalCached(t.Name())
		if localVer != "" || localPath != "" {
			result = append(result, a.pluginToToolInfo(t))
		}
	}
	return result
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

	// 首次访问，触发后台刷新并返回零值
	if atomic.CompareAndSwapInt32(&a.dashRefreshing, 0, 1) {
		go a.refreshDashboardStats()
	}
	return DashboardStats{}
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
func (a *App) ApplyUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := update.Check(ctx, version.Version)
	if err != nil {
		return err
	}
	if !result.HasUpdate {
		return fmt.Errorf("no update available")
	}
	return update.Apply(ctx, result)
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
}

// GetPackageManagers returns available package managers with their status.
func (a *App) GetPackageManagers() []PackageManagerInfo {
	pms := []pmDef{
		{"npx", "NPX", "Node 包执行器 - 运行 npm 包", "npx", []string{"--version"}, "node", "随 Node.js 自动安装"},
		{"npm", "NPM", "Node 包管理器", "npm", []string{"--version"}, "node", "随 Node.js 自动安装"},
		{"yarn", "Yarn", "快速、可靠的依赖管理工具", "yarn", []string{"--version"}, "node", "需先安装 Node.js，然后运行 corepack enable yarn"},
		{"pnpm", "PNPM", "快速、节省磁盘的包管理器", "pnpm", []string{"--version"}, "node", "需先安装 Node.js，然后运行 corepack enable pnpm"},
		{"uvx", "UVX", "通过 UV 运行 Python 工具", "uvx", []string{"--version"}, "uv", ""},
		{"uv", "UV", "极速 Python 包安装器", "uv", []string{"--version"}, "uv", ""},
		{"pip", "PIP", "Python 包安装器", "pip", []string{"--version"}, "python", "随 Python 自动安装"},
		{"pipx", "PIPX", "在隔离环境中安装运行 Python 应用", "pipx", []string{"--version"}, "", "通过 pip install pipx 或 uv tool install pipx 安装"},
		{"cargo", "Cargo", "Rust 包管理器", "cargo", []string{"--version"}, "rust", "随 Rust 自动安装"},
		{"maven", "Maven", "Java 构建自动化工具", "mvn", []string{"--version"}, "maven", "随 Maven 自动安装"},
		{"brew", "Homebrew", "macOS/Linux 包管理器", "brew", []string{"--version"}, "", "访问 https://brew.sh 安装"},
		{"scoop", "Scoop", "Windows 命令行安装器", "scoop", []string{"--version"}, "", "访问 https://scoop.sh 安装"},
		{"choco", "Chocolatey", "Windows 包管理器", "choco", []string{"--version"}, "", "访问 https://chocolatey.org 安装"},
		{"dotnet", ".NET CLI", ".NET 开发工具", "dotnet", []string{"--version"}, "dotnet", ""},
		{"go", "Go 工具链", "Go 模块和构建工具", "go", []string{"version"}, "go", ""},
	}

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
	return results
}

// detectPackageManager detects a single package manager's status.
func (a *App) detectPackageManager(pm pmDef) PackageManagerInfo {
	// 对于附属工具（detectCmd != toolName，如 yarn/pnpm 是 node 的附属工具），
	// xdev 不能直接安装它们，需要先安装父工具再启用。
	// CanInstall 仅对 xdev 可以直接安装的工具为 true。
	canInstall := pm.toolName != "" && pm.detectCmd == pm.toolName
	info := PackageManagerInfo{
		Name:        pm.name,
		DisplayName: pm.displayName,
		Description: pm.description,
		Category:    "package-manager",
		CanInstall:  canInstall,
		ToolName:    pm.toolName,
		InstallHint: pm.installHint,
	}

	// Check if available via PATH
	shimDir := resolveShimDir()
	installDir := a.getCfg().Core.InstallDir
	if path, err := exec.LookPath(pm.detectCmd); err == nil {
		isXdevManaged := false
		if shimDir != "" && isInsideDir(path, shimDir) {
			isXdevManaged = true
		}
		if installDir != "" && isInsideDir(path, installDir) {
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
							// 尝试获取附属工具的实际版本号
							actualVer := a.getSubToolVersion(pm.detectCmd, pm.toolName)
							if actualVer != "" {
								info.Version = actualVer
							} else {
								// getSubToolVersion 失败时，尝试直接执行工具获取版本
								ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
								cmd := exec.CommandContext(ctx, pm.detectCmd, pm.detectArgs...)
								util.HideWindow(cmd)
								if output, err := cmd.CombinedOutput(); err == nil {
									line := strings.TrimSpace(string(output))
									if idx := strings.Index(line, "\n"); idx > 0 {
										line = line[:idx]
									}
									if v := extractVersion(line); v != "" {
										info.Version = v
									} else {
										info.Version = "随 " + pm.toolName + " " + activeVer + " 附带"
									}
								} else {
									info.Version = "随 " + pm.toolName + " " + activeVer + " 附带"
								}
								cancel()
							}
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
						if v := extractVersion(outputStr); v != "" {
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
						if v := extractVersion(outputStr); v != "" {
							info.Version = v
						} else {
							info.Version = outputStr
						}
					}
				}
			} else {
				info.Installed = true
				info.LocalPath = path
				// Get version (with short timeout)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				cmd := exec.CommandContext(ctx, path, pm.detectArgs...)
				util.HideWindow(cmd)
				if output, err := cmd.CombinedOutput(); err == nil {
					outputStr := strings.TrimSpace(string(output))
					if pm.name == "gradle" {
						for _, line := range strings.Split(outputStr, "\n") {
							line = strings.TrimSpace(line)
							if strings.HasPrefix(line, "Gradle ") {
								info.Version = strings.TrimPrefix(line, "Gradle ")
								break
							}
						}
					} else {
						line := outputStr
						if idx := strings.Index(line, "\n"); idx > 0 {
							line = line[:idx]
						}
						info.Version = extractVersion(line)
						if info.Version == "" {
							info.Version = line
						}
					}
				}
				cancel()
			}
		}
	}

	// Also check if installed via xdev (for tools not found on PATH)
	// 但对于附属工具（detectCmd != toolName），需要验证附属工具可执行文件是否真的存在
	if !info.Installed && pm.toolName != "" {
		if activeVer, installed := a.currentVersion(pm.toolName); installed {
			if pm.detectCmd != pm.toolName {
				// 附属工具：验证可执行文件是否存在
				parentDir := a.currentLink(pm.toolName)
				if target, ok := shim.FindInDir(parentDir, pm.detectCmd); ok {
					info.Installed = true
					info.LocalPath = target
					// 尝试获取版本号
					actualVer := a.getSubToolVersion(pm.detectCmd, pm.toolName)
					if actualVer != "" {
						info.Version = actualVer
					} else {
						info.Version = "随 " + pm.toolName + " " + activeVer + " 附带"
					}
				}
			} else {
				info.Installed = true
				info.Version = activeVer
				link := a.currentLink(pm.toolName)
				if target, err := os.Readlink(link); err == nil {
					info.LocalPath = target
				}
			}
		}
	}

	return info
}

// getSubToolVersion 获取附属工具的实际版本号
// 例如 yarn/pnpm 在 node 安装目录内，尝试执行该工具获取版本号
func (a *App) getSubToolVersion(subToolCmd, parentToolName string) string {
	parentDir := a.currentLink(parentToolName)

	// 在父工具目录中查找子工具可执行文件
	var subToolPath string
	if target, ok := shim.FindInDir(parentDir, subToolCmd); ok {
		subToolPath = target
	}

	if subToolPath == "" {
		// Windows: check Scripts subdirectory (pip, pipx, etc.)
		if runtime.GOOS == "windows" {
			scriptsDir := filepath.Join(parentDir, "Scripts")
			if target, ok := shim.FindInDir(scriptsDir, subToolCmd); ok {
				subToolPath = target
			}
		}
		// Unix: check bin subdirectory
		if subToolPath == "" {
			binDir := filepath.Join(parentDir, "bin")
			if target, ok := shim.FindInDir(binDir, subToolCmd); ok {
				subToolPath = target
			}
		}
	}

	if subToolPath == "" {
		return ""
	}

	// 执行子工具获取版本号
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, subToolPath, "--version")
	util.HideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err == nil {
		line := strings.TrimSpace(string(output))
		if idx := strings.Index(line, "\n"); idx > 0 {
			line = line[:idx]
		}
		return extractVersion(line)
	}
	return ""
}

// InstallPackageManager installs a package manager via xdev.
// toolName is the xdev tool name (e.g. "uv", "node", "python").
func (a *App) InstallPackageManager(toolName string) error {
	logInfo("[InstallPackageManager] installing %s", toolName)
	p, ok := a.registry.Get(toolName)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", toolName)
	}

	// Use the default/latest version
	ver := p.DefaultVersion()
	if ver == "" {
		ver = "latest"
	}

	return a.InstallTool(toolName, ver)
}

// UpdatePackageManager updates a package manager to the latest version.
func (a *App) UpdatePackageManager(toolName string) error {
	logInfo("[UpdatePackageManager] updating %s", toolName)

	// Get the latest available version
	versions := a.GetAvailableVersions(toolName)
	if len(versions) == 0 {
		return fmt.Errorf("no versions available for %s", toolName)
	}

	latestVer := versions[0].Version // first is latest
	return a.InstallTool(toolName, latestVer)
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
func detectLocalToolPath(name string, shims []string) string {
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
	if envPath := checkEnvVars(name); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	// 2. Try exec.LookPath (pure Go, no subprocess).
	//    Skip results that point into the xdev shim directory,
	//    because those are xdev binary copies, not the real tool.
	shimDir := resolveShimDir()
	for _, cmdName := range searchNames {
		if path, err := exec.LookPath(cmdName); err == nil {
			if shimDir != "" && isInsideDir(path, shimDir) {
				logDebug("[detectLocalToolPath] skipping shim path %s for %s", path, name)
				continue
			}
			return path
		}
	}

	// 3. Check common install directories.
	commonPaths := getCommonInstallPaths(name)
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// resolveShimDir returns the xdev shim directory from the current
// config, or "" if unavailable.
func resolveShimDir() string {
	cfg, err := config.Load("")
	if err != nil {
		return ""
	}
	return cfg.Core.ShimDir
}

// isInsideDir checks whether the given path is inside the
// specified directory.  Both paths are cleaned and compared
// case-insensitively on Windows.
func isInsideDir(path, dir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		cleanPath = strings.ToLower(cleanPath)
		cleanDir = strings.ToLower(cleanDir)
	}
	// Ensure dir ends with separator for prefix matching
	if !strings.HasSuffix(cleanDir, string(os.PathSeparator)) {
		cleanDir += string(os.PathSeparator)
	}
	return strings.HasPrefix(cleanPath, cleanDir)
}

// checkEnvVars checks well-known environment variables for tool locations.
// Returns the path to the tool binary if found.
func checkEnvVars(name string) string {
	envMap := map[string][]string{
		"go":                     {"GOROOT"},
		"java":                   {"JAVA_HOME"},
		"dotnet":                 {"DOTNET_ROOT"},
		"android-platform-tools": {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"android-build-tools":    {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"android-platform":       {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"android-ndk":            {"ANDROID_NDK_HOME", "NDK_HOME"},
		"android-cmdline-tools":  {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"android-emulator":       {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"node":                   {"NVM_HOME"},
		"python":                 {"PYTHON_HOME"},
		"rust":                   {"RUSTUP_HOME"},
		"flutter":                {"FLUTTER_ROOT"},
		"dart":                   {"DART_SDK"},
		"gradle":                 {"GRADLE_HOME"},
		"maven":                  {"MAVEN_HOME", "M2_HOME"},
		"cmake":                  {"CMAKE_ROOT"},
		"llvm":                   {"LLVM_HOME"},
		"typescript":             {"TYPESCRIPT_HOME"},
	}

	envVars, ok := envMap[name]
	if !ok {
		return ""
	}

	for _, envVar := range envVars {
		val := os.Getenv(envVar)
		if val == "" {
			continue
		}
		// Try to find the binary within the env var directory
		binNames := getBinNamesForTool(name)
		for _, bin := range binNames {
			candidate := filepath.Join(val, "bin", bin)
			if runtime.GOOS == "windows" {
				candidateExe := candidate + ".exe"
				if _, err := os.Stat(candidateExe); err == nil {
					return candidateExe
				}
			}
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			// Also try directly in the env var directory
			candidate = filepath.Join(val, bin)
			if runtime.GOOS == "windows" {
				candidateExe := candidate + ".exe"
				if _, err := os.Stat(candidateExe); err == nil {
					return candidateExe
				}
			}
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		// For Android tools, check subdirectories
		if name == "android-platform-tools" {
			candidate := filepath.Join(val, "platform-tools")
			if runtime.GOOS == "windows" {
				if _, err := os.Stat(filepath.Join(candidate, "adb.exe")); err == nil {
					return filepath.Join(candidate, "adb.exe")
				}
			} else {
				if _, err := os.Stat(filepath.Join(candidate, "adb")); err == nil {
					return filepath.Join(candidate, "adb")
				}
			}
		}
		if name == "android-build-tools" {
			candidate := filepath.Join(val, "build-tools")
			if entries, err := os.ReadDir(candidate); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						return filepath.Join(candidate, e.Name())
					}
				}
			}
		}
		if name == "android-ndk" {
			candidate := filepath.Join(val, "ndk")
			if entries, err := os.ReadDir(candidate); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						return filepath.Join(candidate, e.Name())
					}
				}
			}
			// Also check ANDROID_NDK_HOME directly
			if fi, err := os.Stat(val); err == nil && fi.IsDir() {
				return val
			}
		}
		if name == "android-emulator" {
			candidate := filepath.Join(val, "emulator")
			if runtime.GOOS == "windows" {
				if _, err := os.Stat(filepath.Join(candidate, "emulator.exe")); err == nil {
					return filepath.Join(candidate, "emulator.exe")
				}
			} else {
				if _, err := os.Stat(filepath.Join(candidate, "emulator")); err == nil {
					return filepath.Join(candidate, "emulator")
				}
			}
		}
	}
	return ""
}

// getBinNamesForTool returns the binary names to look for when checking
// environment variables for a tool.
func getBinNamesForTool(name string) []string {
	switch name {
	case "go":
		return []string{"go"}
	case "java":
		return []string{"java", "javac"}
	case "dotnet":
		return []string{"dotnet"}
	case "node":
		return []string{"node"}
	case "python":
		return []string{"python", "python3"}
	case "rust":
		return []string{"rustc", "cargo"}
	case "flutter":
		return []string{"flutter"}
	case "dart":
		return []string{"dart"}
	case "gradle":
		return []string{"gradle"}
	case "maven":
		return []string{"mvn"}
	case "cmake":
		return []string{"cmake"}
	case "llvm":
		return []string{"clang"}
	case "typescript":
		return []string{"tsc"}
	default:
		return []string{name}
	}
}

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

	versionFlags := map[string][]string{
		"python":  {"--version"},
		"python3": {"--version"},
		"node":    {"--version"},
		"go":      {"version"},
		"java":    {"--version"},
		"rustc":   {"--version"},
		"cargo":   {"--version"},
		"dotnet":  {"--version"},
		"php":     {"--version"},
		"ruby":    {"--version"},
		"lua":     {"-v"},
		"perl":    {"--version"},
		"dart":    {"--version"},
		"kotlin":  {"--version"},
		"bun":     {"--version"},
		"deno":    {"--version"},
		"zig":     {"version"},
		"julia":   {"--version"},
		"ghc":     {"--version"},
		"scala":   {"--version"},
		"cmake":   {"--version"},
		"tsc":     {"--version"},
		"gcc":     {"--version"},
		"clang":   {"--version"},
		"cjc":     {"--version"},
		"make":    {"--version"},
		"ninja":   {"--version"},
		"gradle":  {"-version"},
		"mvn":     {"-version"},
		"maven":   {"-version"},
		"ant":     {"-version"},
		"haskell": {"--version"},
		"erl":     {"-version"},
		"adb":     {"version"},
		"flutter": {"--version"},
		"git":     {"--version"},
		"mingw":   {"--version"},
	}

	flags, ok := versionFlags[name]
	if !ok {
		flags = []string{"--version"}
	}

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

	// Parse version from output - take first line, extract version number
	line := strings.TrimSpace(string(output))
	if idx := strings.Index(line, "\n"); idx > 0 {
		line = line[:idx]
	}

	// Try to extract a semver-like pattern
	ver := extractVersion(line)
	if ver != "" {
		logDebug("[getVersion] %s → %s (in %v)", name, ver, time.Since(start))
		return ver
	}
	// Return the whole first line if no version pattern found (truncated)
	if len(line) > 60 {
		line = line[:60] + "..."
	}
	logDebug("[getVersion] %s → %q (raw, in %v)", name, line, time.Since(start))
	return line
}

// findBinaryInDir finds the executable binary for a tool inside a directory.
// It checks bin/, the root directory, and common naming patterns.
func findBinaryInDir(name, dir string) string {
	// Map tool names to their binary names
	binaryNames := map[string][]string{
		"java":       {"java", "javac"},
		"node":       {"node", "npm"},
		"python":     {"python", "python3"},
		"go":         {"go"},
		"dotnet":     {"dotnet"},
		"rust":       {"rustc", "cargo"},
		"flutter":    {"flutter"},
		"dart":       {"dart"},
		"gradle":     {"gradle"},
		"maven":      {"mvn"},
		"cmake":      {"cmake"},
		"llvm":       {"clang"},
		"ruby":       {"ruby"},
		"php":        {"php"},
		"perl":       {"perl"},
		"git":        {"git"},
		"deno":       {"deno"},
		"bun":        {"bun"},
		"zig":        {"zig"},
		"lua":        {"lua"},
		"make":       {"make"},
		"ninja":      {"ninja"},
		"mingw":      {"gcc", "g++"},
		"erlang":     {"erl"},
		"haskell":    {"ghc"},
		"julia":      {"julia"},
		"kotlin":     {"kotlinc"},
		"scala":      {"scala"},
		"sbt":        {"sbt"},
		"ant":        {"ant"},
		"adb":        {"adb"},
		"uv":         {"uv"},
		"typescript": {"tsc"},
	}

	binNames, ok := binaryNames[name]
	if !ok {
		binNames = []string{name}
	}

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

// extractVersion tries to extract a version number from a string.
func extractVersion(s string) string {
	// Look for patterns like "3.12.7", "v18.0.0", "go1.22.0"
	s = strings.TrimSpace(s)
	// Remove common prefixes
	for _, prefix := range []string{"v", "go", "yarn ", "pnpm ", "gradle ", "npm ", "pip ", "cmake version ", "gnu make ", "apache maven ", "apache ant(tm) version ", "dart sdk version: ", "git version ", "(gcc) ", "version "} {
		if strings.HasPrefix(strings.ToLower(s), prefix) && len(s) > len(prefix) {
			rest := s[len(prefix):]
			if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				s = rest
				break
			}
		}
	}

	// Find the first semver-like pattern (e.g. 3.12.7, 18.0.0, 1.22)
	parts := strings.Fields(s)
	for _, p := range parts {
		if isVersionLike(p) {
			// Strip Windows-style suffix like ".windows.1" from "2.47.0.windows.1"
			if stripped := stripWindowsSuffix(p); stripped != "" {
				return stripped
			}
			return p
		}
	}
	return ""
}

// stripWindowsSuffix removes Windows-specific version suffixes like ".windows.1"
// from strings like "2.47.0.windows.1", returning "2.47.0".
func stripWindowsSuffix(s string) string {
	idx := strings.Index(s, ".windows.")
	if idx > 0 {
		return s[:idx]
	}
	return s
}

// isVersionLike checks if a string looks like a version number.
func isVersionLike(s string) bool {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(strings.ToLower(s), "go")
	// Strip Windows-style suffix like ".windows.1" before counting dots
	if idx := strings.Index(s, ".windows."); idx > 0 {
		s = s[:idx]
	}
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return false
	}
	dots := strings.Count(s, ".")
	return dots >= 1 && dots <= 3
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

// getCommonInstallPaths returns platform-specific common install paths for a tool.
func getCommonInstallPaths(name string) []string {
	var paths []string
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")

		switch name {
		case "python", "python3":
			paths = append(paths,
				filepath.Join(localAppData, "Programs", "Python"),
			)
		case "node":
			paths = append(paths,
				filepath.Join(programFiles, "nodejs"),
				filepath.Join(localAppData, "nvm"),
			)
		case "go":
			paths = append(paths,
				filepath.Join(programFiles, "Go"),
				filepath.Join(home, "go"),
				filepath.Join(home, "sdk", "go"),
			)
		case "java":
			paths = append(paths,
				filepath.Join(programFiles, "Java"),
				filepath.Join(programFilesX86, "Java"),
			)
		case "dotnet":
			paths = append(paths,
				filepath.Join(programFiles, "dotnet"),
			)
		case "ruby":
			paths = append(paths,
				filepath.Join(programFiles, "Ruby"),
			)
		case "git":
			paths = append(paths,
				filepath.Join(programFiles, "Git"),
			)
		case "mingw":
			paths = append(paths,
				filepath.Join(localAppData, "Programs", "mingw64"),
				filepath.Join(localAppData, "Programs", "mingw32"),
				filepath.Join(programFiles, "mingw64"),
				filepath.Join(programFiles, "mingw32"),
				filepath.Join(home, "scoop", "apps", "mingw"),
				filepath.Join("C:", "msys64", "mingw64"),
				filepath.Join("C:", "msys64", "mingw32"),
			)
		case "flutter":
			paths = append(paths,
				filepath.Join(localAppData, "flutter"),
			)
		case "android-platform-tools":
			paths = append(paths,
				filepath.Join(home, "AppData", "Local", "Android", "Sdk", "platform-tools"),
			)
		case "typescript":
			// npm global install paths
			paths = append(paths,
				filepath.Join(localAppData, "npm", "node_modules", "typescript", "bin"),
				filepath.Join(programFiles, "nodejs", "node_modules", "typescript", "bin"),
			)
		}
	} else if runtime.GOOS == "darwin" {
		switch name {
		case "python", "python3":
			paths = append(paths,
				"/usr/local/bin/python3",
				"/opt/homebrew/bin/python3",
			)
		case "node":
			paths = append(paths,
				"/usr/local/bin/node",
				"/opt/homebrew/bin/node",
			)
		case "go":
			paths = append(paths,
				"/usr/local/go/bin/go",
				"/opt/homebrew/bin/go",
			)
		case "java":
			paths = append(paths,
				"/usr/local/opt/openjdk/bin/java",
				"/Library/Java/JavaVirtualMachines",
			)
		case "flutter":
			paths = append(paths,
				filepath.Join(home, "development", "flutter"),
			)
		case "typescript":
			paths = append(paths,
				"/usr/local/lib/node_modules/typescript/bin",
				"/opt/homebrew/lib/node_modules/typescript/bin",
			)
		}
	} else {
		// Linux
		switch name {
		case "python", "python3":
			paths = append(paths, "/usr/bin/python3", "/usr/local/bin/python3")
		case "node":
			paths = append(paths, "/usr/bin/node", "/usr/local/bin/node")
		case "go":
			paths = append(paths, "/usr/local/go/bin/go", "/usr/bin/go")
		case "java":
			paths = append(paths, "/usr/bin/java", "/usr/local/bin/java")
		case "typescript":
			paths = append(paths,
				"/usr/local/lib/node_modules/typescript/bin",
				"/usr/lib/node_modules/typescript/bin",
			)
		}
	}

	// NVM / pyenv / sdkman etc.
	switch name {
	case "node":
		nvmDir := filepath.Join(home, ".nvm")
		if _, err := os.Stat(nvmDir); err == nil {
			paths = append(paths, nvmDir)
		}
	case "python", "python3":
		pyenvDir := filepath.Join(home, ".pyenv")
		if _, err := os.Stat(pyenvDir); err == nil {
			paths = append(paths, pyenvDir)
		}
	case "java":
		sdkmanDir := filepath.Join(home, ".sdkman")
		if _, err := os.Stat(sdkmanDir); err == nil {
			paths = append(paths, sdkmanDir)
		}
	}

	return paths
}

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
		if ver := extractVersion(base); ver != "" {
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
			if ver := extractVersion(rest); ver != "" {
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
		if ver := extractVersion(rest); ver != "" {
			return ver
		}
	}

	// Pattern: go1.22.0 → 1.22.0
	if strings.HasPrefix(sLower, "go") && len(s) > 2 && s[2] >= '0' && s[2] <= '9' {
		if ver := extractVersion(s[2:]); ver != "" {
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
