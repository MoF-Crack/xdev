// app_tool_query.go is the catalog-query module behind the Wails App
// adapter (Spec 029 B2). It implements tool browsing and version query.
//
// Provides:
//   - GetCategories: list all tool categories with counts
//   - GetToolsByCategory: list tools in a category with install status
//   - GetInstalledVersions: list installed versions for a tool
//   - GetAvailableVersions: list manifest versions for a tool (sorted newest-first)
//
// GetToolsByCategory is cached in-memory per category (toolListCacheTTL).
// Available versions come from the in-memory catalog, not a remote TTL cache.
package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// devToolsCategoryNames 列出归并到「开发工具」(development_tools) 的所有
// 旧分类名。Spec 024 把 ide/agent/harness 三个扁平分类合并为单一的
// development_tools 分类，归并层确保磁盘 manifest 残留旧 category 字段
// 时这些工具仍能在「开发工具」下显示（兜底迁移过程）。
var devToolsCategoryNames = map[string]bool{
	"ide":               true,
	"agent":             true,
	"harness":           true,
	"development_tools": true,
}

// isDevToolsCategory 报告 category 是否属于「开发工具」归并集合。
func isDevToolsCategory(category string) bool {
	return devToolsCategoryNames[category]
}

// pluginToToolInfo converts a plugin.Plugin to a ToolInfo struct,
// populating common fields including local cache info.
func (a *App) pluginToToolInfo(p plugin.Plugin) ToolInfo {
	activeVer, installed := a.currentVersion(p.Name())
	localVer, localPath, localSource := a.getLocalCached(p.Name())
	info := ToolInfo{
		Name:         p.Name(),
		DisplayName:  p.DisplayName(),
		Category:     p.Category(),
		Description:  p.Description(),
		DefaultVer:   p.DefaultVersion(),
		ActiveVer:    activeVer,
		Installed:    installed,
		LocalVersion: localVer,
		LocalPath:    localPath,
		Source:       localSource,
	}
	// 形态信息来自 manifest（Spec 023）。manifest 可能为 nil
	// （Lua 插件/测试桩）——nil 安全，留空即可（omitempty）。
	if m := p.Manifest(); m != nil {
		info.FormFactor = m.EffectiveFormFactor()
		info.GUIBinary = m.GUIBinary
		// Spec 025：是否需要按形态区分下载（多形态独立 archive 工具才为 true）。
		info.HasFormSpecificArchives = m.HasFormSpecificArchives()
		info.AvailableForms = m.AvailableArchiveForms()
	}
	return info
}

// GetCategories returns all tool categories with counts.
func (a *App) GetCategories() []CategoryInfo {
	tools := a.registry.All()
	catMap := make(map[string]int)
	for _, t := range tools {
		catMap[t.Category()]++
	}

	// Add "local" category - count tools that have local installations
	a.localMu.RLock()
	localCount := 0
	for _, t := range tools {
		if info, ok := a.localMap[t.Name()]; ok && info.Path != "" {
			localCount++
		}
	}
	a.localMu.RUnlock()
	if localCount > 0 {
		catMap["local"] = localCount
	}

	// Add "package-manager" category (always shown)
	catMap["package-manager"] = 0 // count is populated by GetPackageManagers

	// 确保 "language" 分类始终存在（包含虚拟编程语言）
	if _, ok := catMap["language"]; !ok {
		catMap["language"] = 0
	}

	// Spec 024：把旧分类 ide/agent/harness 的计数归并到 development_tools。
	// 这样即使磁盘 manifest 残留旧 category 字段，GetCategories 也只输出
	// development_tools 一个入口（归并层兜底迁移过程）。
	devCount := 0
	for name, count := range catMap {
		if name != "development_tools" && isDevToolsCategory(name) {
			devCount += count
			delete(catMap, name)
		}
	}
	// 确保 development_tools 条目始终存在（即使计数为 0，也要出现在分类列表中，
	// 以便前端渲染入口——与 language 分类始终存在的处理一致）。
	if _, ok := catMap["development_tools"]; !ok {
		catMap["development_tools"] = 0
	}
	catMap["development_tools"] += devCount

	// Fixed display order (Spec 024: ide/agent/harness 合并为 development_tools)
	order := []string{"language", "build", "development_tools", "mobile", "system", "local", "package-manager"}
	var result []CategoryInfo
	for _, name := range order {
		if count, ok := catMap[name]; ok {
			result = append(result, CategoryInfo{Name: name, Count: count})
		}
	}
	// Append any remaining categories not in the fixed order
	for name, count := range catMap {
		found := false
		for _, o := range order {
			if o == name {
				found = true
				break
			}
		}
		if !found {
			result = append(result, CategoryInfo{Name: name, Count: count})
		}
	}
	return result
}

// GetLogPath returns the path to the GUI log file, so the frontend
// can display it to the user for debugging purposes.
func (a *App) GetLogPath() string {
	return LogPath()
}

// GetDebugInfo returns diagnostic information about the internal state.
// Used for troubleshooting when the GUI shows no categories.
func (a *App) GetDebugInfo() DebugInfo {
	cfg := a.getCfg()
	tools := a.registry.All()
	catTools := a.cat.Tools()
	regNames := make([]string, 0, len(tools))
	for _, t := range tools {
		regNames = append(regNames, t.Name())
	}
	catNames := make([]string, 0, len(catTools))
	for _, t := range catTools {
		catNames = append(catNames, t.Name)
	}
	return DebugInfo{
		XMDevDir:      cfg.Core.XMDevDir,
		ManifestDir:   cfg.Core.ManifestDir,
		CacheDir:      cfg.Core.CacheDir,
		RegistryCount: len(tools),
		CatalogCount:  len(catTools),
		RegistryNames: regNames,
		CatalogNames:  catNames,
	}
}

// GetToolsByCategory returns tools filtered by category.
//
// Results are cached per category for toolListCacheTTL so that
// switching between 编程语言/包管理器 pages doesn't re-spawn
// subprocesses for version detection every time. The cache is cleared
// by InvalidateToolsCache after install/uninstall/use-version.
func (a *App) GetToolsByCategory(category string) []ToolInfo {
	// TTL cache lookup — fast path avoids waitForLocalCache + subprocess scans.
	if cached, ok := a.toolCacheGet(category); ok {
		return cached
	}
	result := a.computeToolsByCategory(category)
	a.toolCachePut(category, result)
	return result
}

// toolListCacheTTL 工具列表缓存有效期。
const toolListCacheTTL = 60 * time.Second

// toolCacheGet 返回指定分类的缓存工具列表；未命中或过期时 ok=false。
func (a *App) toolCacheGet(category string) ([]ToolInfo, bool) {
	a.toolsCacheMu.Lock()
	defer a.toolsCacheMu.Unlock()
	if a.toolsCache == nil {
		return nil, false
	}
	entry, ok := a.toolsCache[category]
	if !ok || time.Now().After(entry.Expire) {
		return nil, false
	}
	return entry.Entries, true
}

// toolCachePut 写入指定分类的工具列表缓存。
func (a *App) toolCachePut(category string, entries []ToolInfo) {
	a.toolsCacheMu.Lock()
	defer a.toolsCacheMu.Unlock()
	if a.toolsCache == nil {
		a.toolsCache = make(map[string]toolsCacheEntry)
	}
	a.toolsCache[category] = toolsCacheEntry{Entries: entries, Expire: time.Now().Add(toolListCacheTTL)}
}

// InvalidateToolsCache 清除全部工具列表缓存，使下次 GetToolsByCategory
// 重新扫描。安装/卸载/切换版本/修复后调用，确保列表反映最新状态。
func (a *App) InvalidateToolsCache() {
	a.toolsCacheMu.Lock()
	a.toolsCache = nil
	a.toolsCacheMu.Unlock()
}

// computeToolsByCategory 实际的分类工具列表计算（无缓存）。
func (a *App) computeToolsByCategory(category string) []ToolInfo {
	a.waitForLocalCache()

	// Handle "development_tools" category - Spec 024 归并 ide/agent/harness。
	// 返回归并集合（ide/agent/harness/development_tools）内的全部工具，
	// 确保磁盘 manifest 残留旧 category 字段时工具仍能在「开发工具」下显示。
	//
	// 注意：只对 "development_tools" 触发归并。直接查询旧分类名
	// （ide/agent/harness）时走下方 regular filtering 精确匹配——
	// 保持向后兼容契约（外部调用者/测试可能直接查 "ide" 期望只返回
	// category==ide 的工具，而非全部 dev-tools）。
	if category == "development_tools" {
		tools := a.registry.All()
		var result []ToolInfo
		for _, t := range tools {
			if !isDevToolsCategory(t.Category()) {
				continue
			}
			result = append(result, a.pluginToToolInfo(t))
		}
		return result
	}

	// Handle "local" category - show only tools with local installations
	if category == "local" {
		tools := a.registry.All()
		var result []ToolInfo
		for _, t := range tools {
			_, localPath, _ := a.getLocalCached(t.Name())
			if localPath == "" {
				continue
			}
			result = append(result, a.pluginToToolInfo(t))
		}
		return result
	}

		// Handle "package-manager" category - return as ToolInfo for compatibility
		if category == "package-manager" {
			pms := a.GetPackageManagers()
			var result []ToolInfo
			for _, pm := range pms {
				ti := ToolInfo{
					Name:                    pm.Name,
					DisplayName:             pm.DisplayName,
					Category:                "package-manager",
					Description:             pm.Description,
					Installed:               pm.Installed,
					LocalVersion:            pm.Version,
					LocalPath:               pm.LocalPath,
					CanInstall:              pm.CanInstall,
					ToolName:                pm.ToolName,
					InstallHint:             pm.InstallHint,
					HasUpdate:               pm.HasUpdate,
					RequiresAdmin:           pm.RequiresAdmin, // Spec 025 L3 阻塞项3
					InstallCommand:          pm.InstallCommand,
					EnableCommand:           pm.EnableCommand, // Spec 026: 附属工具启用命令
					OfferSubToolInstall:     pm.OfferSubToolInstall,
				}
				// Spec 026: 传递可用安装方式（pipx 的 pip/uv 选项）
				if len(pm.InstallMethods) > 0 {
					ti.InstallMethods = make([]InstallMethodInfo, len(pm.InstallMethods))
					for j, m := range pm.InstallMethods {
						ti.InstallMethods[j] = InstallMethodInfo{Name: m.Name, Command: m.Command}
					}
				}
				// Spec 027: 传递多实例列表（pip 的 xdev 管理 + 系统安装）
				if len(pm.Instances) > 0 {
					ti.Instances = make([]PMInstanceInfo, len(pm.Instances))
					copy(ti.Instances, pm.Instances)
				}
				result = append(result, ti)
			}
			return result
		}

	// Handle "language" category
	if category == "language" {
		tools := a.registry.All()
		var result []ToolInfo
		for _, t := range tools {
			if t.Category() != "language" {
				continue
			}
			result = append(result, a.pluginToToolInfo(t))
		}
		// C/C++ 不是独立工具——它们是 MinGW/LLVM/Clang 的使用场景。
		// 用户直接在「构建工具」分类安装 MinGW 或 LLVM/Clang 获取 C/C++ 编译器。
		return result
	}

	// Regular category filtering
	tools := a.registry.All()
	var result []ToolInfo
	for _, t := range tools {
		if category != "" && t.Category() != category {
			continue
		}
		result = append(result, a.pluginToToolInfo(t))
	}
	return result
}

// GetInstalledVersions returns installed versions for a tool.
// Each entry carries the integrity status (Intact/Integrity) so the
// frontend can flag incomplete installations and offer a repair action.
func (a *App) GetInstalledVersions(toolName string) []VersionInfo {
	tools := a.registry.All()
	cfg := a.getCfg()
	for _, t := range tools {
		if t.Name() == toolName {
			versions := a.installedVersions(toolName)
			activeVer, _ := a.currentVersion(toolName)
			var result []VersionInfo
			for _, v := range versions {
				vi := VersionInfo{
					Version:   v,
					Installed: true,
					Active:    v == activeVer,
				}
				integrity := installer.VerifyInstallIntegrity(t, filepath.Join(cfg.Core.InstallDir, toolName, v))
				vi.Intact = integrity.OK
				if !integrity.OK {
					vi.Integrity = integrity.Reason
				}
				result = append(result, vi)
			}
			return result
		}
	}
	return nil
}

// CheckInstallIntegrity verifies whether an installed version is complete
// (non-empty directory and all shim binaries present). It is safe to call
// for versions that are not installed — OK will simply be false.
func (a *App) CheckInstallIntegrity(toolName, version string) IntegrityInfo {
	info := IntegrityInfo{Tool: toolName, Version: version}
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		info.Reason = err.Error()
		return info
	}
	cfg := a.getCfg()
	installPath := p.InstallDir(cfg.Core.InstallDir, version)
	info.Path = installPath
	if fi, err := os.Stat(installPath); err != nil || !fi.IsDir() {
		info.Reason = "not installed"
		return info
	}
	integrity := installer.VerifyInstallIntegrity(p, installPath)
	info.OK = integrity.OK
	info.Reason = integrity.Reason
	info.Missing = integrity.Missing
	return info
}

// GetAvailableVersions returns all available versions from the manifest for a tool.
func (a *App) GetAvailableVersions(toolName string) []VersionInfo {
	t, ok := a.cat.Tool(toolName)
	if !ok {
		return nil
	}
	installed := a.installedVersions(toolName)
	activeVer, _ := a.currentVersion(toolName)
	installedSet := make(map[string]bool)
	for _, v := range installed {
		installedSet[v] = true
	}

	versions := append([]manifest.Version(nil), t.VersionList...)
	manifest.SortVersionsByRelease(versions)

	var result []VersionInfo
	for _, v := range versions {
		result = append(result, VersionInfo{
			Version:   v.Version,
			Installed: installedSet[v.Version],
			Active:    v.Version == activeVer,
		})
	}
	return result
}

// isInsideNodeDir 检查路径是否在某个 Node.js 安装目录内
func isInsideNodeDir(path string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(filepath.Clean(path)), "/", "\\")
	if !strings.Contains(normalized, "\\node\\") && !strings.Contains(normalized, "\\nodejs\\") {
		return false
	}
	// 额外检查：向上遍历父目录，确认存在 node.exe/node 可执行文件
	dir := filepath.Dir(filepath.Clean(path))
	for {
		nodeExe := ""
		if runtime.GOOS == "windows" {
			nodeExe = filepath.Join(dir, "node.exe")
		} else {
			nodeExe = filepath.Join(dir, "node")
		}
		if _, err := os.Stat(nodeExe); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		// 只在包含 node/nodejs 的目录层级范围内检查
		parentLower := strings.ReplaceAll(strings.ToLower(parent), "/", "\\")
		if !strings.Contains(parentLower, "\\node") && !strings.Contains(parentLower, "\\nodejs") {
			break
		}
		dir = parent
	}
	// 路径匹配但未找到 node 可执行文件，返回 false
	return false
}
