// app_tool_query.go implements tool browsing and version query methods
// for the GUI frontend.
//
// Provides:
//   - GetCategories: list all tool categories with counts
//   - GetToolsByCategory: list tools in a category with install status
//   - GetInstalledVersions: list installed versions for a tool
//   - GetAvailableVersions: list remote versions for a tool (with caching)
//   - SearchTools: search tools by name/description
//   - GetToolDetail: get detailed info for a single tool
//   - RefreshToolVersions: force-refresh remote version list
//
// Version queries are cached in-memory (cacheVersions) with a TTL from
// config.Core.ManifestTTL (default 24h). This avoids hitting remote APIs
// on every panel switch.
package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/plugin"
)

// pluginToToolInfo converts a plugin.Plugin to a ToolInfo struct,
// populating common fields including local cache info.
func (a *App) pluginToToolInfo(p plugin.Plugin) ToolInfo {
	activeVer, installed := a.currentVersion(p.Name())
	localVer, localPath, localSource := a.getLocalCached(p.Name())
	return ToolInfo{
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

	// Fixed display order
	order := []string{"language", "build", "mobile", "system", "local", "package-manager"}
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
// Waits for the local tool cache to be populated before returning
// so that localVersion and localPath are always available.
func (a *App) GetToolsByCategory(category string) []ToolInfo {
	a.waitForLocalCache()

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
			result = append(result, ToolInfo{
				Name:         pm.Name,
				DisplayName:  pm.DisplayName,
				Category:     "package-manager",
				Description:  pm.Description,
				Installed:    pm.Installed,
				LocalVersion: pm.Version,
				LocalPath:    pm.LocalPath,
				CanInstall:   pm.CanInstall,
				ToolName:     pm.ToolName,
				InstallHint:  pm.InstallHint,
			})
		}
		return result
	}

	// Handle "language" category - 添加没有 manifest 的编程语言
	if category == "language" {
		// 先获取 registry 中已有的 language 分类工具
		tools := a.registry.All()
		var result []ToolInfo
		for _, t := range tools {
			if t.Category() != "language" {
				continue
			}
			result = append(result, a.pluginToToolInfo(t))
		}

		// 添加没有 manifest 的编程语言
		extraLangs := []ToolInfo{
			{Name: "typescript", DisplayName: "TypeScript", Category: "language", Description: "JavaScript 的超集，添加了静态类型", CanInstall: true, InstallHint: "通过 npm install -g typescript 安装"},
			{Name: "c", DisplayName: "C", Category: "language", Description: "通用编程语言", CanInstall: false, InstallHint: "安装 MinGW 或 LLVM/Clang 以获取 C 编译器"},
			{Name: "cpp", DisplayName: "C++", Category: "language", Description: "C 语言的扩展，支持面向对象编程", CanInstall: false, InstallHint: "安装 MinGW 或 LLVM/Clang 以获取 C++ 编译器"},
			{Name: "cangjie", DisplayName: "仓颉", Category: "language", Description: "华为仓颉编程语言", CanInstall: false, InstallHint: "访问华为开发者网站下载仓颉 SDK"},
		}
		for i := range extraLangs {
			lang := &extraLangs[i]
			switch lang.Name {
			case "typescript":
				if path, err := exec.LookPath("tsc"); err == nil {
					lang.Installed = true
					lang.LocalPath = path
					if ver := getVersion("tsc", path); ver != "" {
						lang.LocalVersion = ver
					}
				}
			case "c", "cpp":
				// 优先检查 gcc，其次检查 clang
				if path, err := exec.LookPath("gcc"); err == nil {
					lang.Installed = true
					lang.LocalPath = path
					if ver := getVersion("gcc", path); ver != "" {
						lang.LocalVersion = ver
					}
				} else if path, err := exec.LookPath("clang"); err == nil {
					lang.Installed = true
					lang.LocalPath = path
					if ver := getVersion("clang", path); ver != "" {
						lang.LocalVersion = ver
					}
				}
			case "cangjie":
				if path, err := exec.LookPath("cjc"); err == nil {
					lang.Installed = true
					lang.LocalPath = path
					if ver := getVersion("cjc", path); ver != "" {
						lang.LocalVersion = ver
					}
				}
			}
			result = append(result, *lang)
		}
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
func (a *App) GetInstalledVersions(toolName string) []VersionInfo {
	tools := a.registry.All()
	for _, t := range tools {
		if t.Name() == toolName {
			versions := a.installedVersions(toolName)
			activeVer, _ := a.currentVersion(toolName)
			var result []VersionInfo
			for _, v := range versions {
				result = append(result, VersionInfo{
					Version:   v,
					Installed: true,
					Active:    v == activeVer,
				})
			}
			return result
		}
	}
	return nil
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

	var result []VersionInfo
	for _, v := range t.VersionList {
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
