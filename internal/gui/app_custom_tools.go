// app_custom_tools.go implements custom tool management for the GUI.
//
// Custom tools allow users to add tools that are not in the built-in
// manifest catalog. A custom tool definition includes:
//   - Name and display name
//   - Download URL template (with {{version}} placeholder)
//   - Archive format (zip, tar.gz, etc.)
//   - Shim names
//   - Environment variables
//
// Custom tools are stored in custom_tools.json in the config directory.
// The InstallCustomTool method downloads and installs using the same
// installer pipeline as built-in tools.
//
// Provides:
//   - GetCustomTools: list all custom tool definitions
//   - AddCustomTool: add a new custom tool definition
//   - RemoveCustomTool: remove a custom tool definition
//   - InstallCustomTool: download and install a custom tool
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/extractor"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

// CustomTool 自定义工具信息
type CustomTool struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	InstallPath string `json:"installPath"`
	Shims       string `json:"shims"`   // 逗号分隔的 shim 名
	EnvVars     string `json:"envVars"` // KEY=VALUE 格式，逗号分隔
	AddedAt     string `json:"addedAt"`
}

// GetCustomTools 返回自定义工具列表
func (a *App) GetCustomTools() []CustomTool {
	a.customMu.RLock()
	defer a.customMu.RUnlock()
	result := make([]CustomTool, len(a.customTools))
	copy(result, a.customTools)
	return result
}

// AddCustomTool 添加自定义工具
func (a *App) AddCustomTool(toolJson string) error {
	var tool CustomTool
	if err := json.Unmarshal([]byte(toolJson), &tool); err != nil {
		return fmt.Errorf("invalid tool data: %v", err)
	}
	if tool.Name == "" || tool.DisplayName == "" || tool.DownloadURL == "" {
		return fmt.Errorf("name, displayName and downloadUrl are required")
	}
	if err := util.IsSafeHTTPSExternalURL(tool.DownloadURL); err != nil {
		return fmt.Errorf("download URL is unsafe: %w", err)
	}
	// Security (C-3): Validate tool name is a safe path component to prevent
	// path traversal when the name is used in filepath.Join for install dirs.
	if _, err := util.SafeName(tool.Name); err != nil {
		return fmt.Errorf("tool name is invalid: %w", err)
	}
	if tool.Version != "" {
		if _, err := util.SafeName(tool.Version); err != nil {
			return fmt.Errorf("tool version is invalid: %w", err)
		}
	}
	// 检查是否与内置工具冲突
	if _, ok := a.registry.Get(tool.Name); ok {
		return fmt.Errorf("tool name %q conflicts with a built-in tool", tool.Name)
	}
	a.customMu.Lock()
	defer a.customMu.Unlock()
	// 检查是否已存在
	for _, t := range a.customTools {
		if t.Name == tool.Name {
			return fmt.Errorf("custom tool %q already exists", tool.Name)
		}
	}
	tool.AddedAt = time.Now().Format("2006-01-02")
	a.customTools = append(a.customTools, tool)
	a.saveCustomTools()
	return nil
}

// RemoveCustomTool 移除自定义工具
func (a *App) RemoveCustomTool(toolName string) error {
	a.customMu.Lock()
	defer a.customMu.Unlock()
	for i, t := range a.customTools {
		if t.Name == toolName {
			a.customTools = append(a.customTools[:i], a.customTools[i+1:]...)
			a.saveCustomTools()
			return nil
		}
	}
	return fmt.Errorf("custom tool %q not found", toolName)
}

// InstallCustomTool 安装自定义工具（下载并解压到 install 目录）
func (a *App) InstallCustomTool(toolName string) error {
	a.customMu.RLock()
	var tool *CustomTool
	for i := range a.customTools {
		if a.customTools[i].Name == toolName {
			tool = &a.customTools[i]
			break
		}
	}
	if tool == nil {
		a.customMu.RUnlock()
		return fmt.Errorf("custom tool %q not found", toolName)
	}
	downloadURL := tool.DownloadURL
	version := tool.Version
	if version == "" {
		version = "latest"
	}
	a.customMu.RUnlock()

	// Security (C-3): Validate toolName and version are safe path components
	// before using them in filepath.Join for install/cache directories.
	if _, err := util.SafeName(toolName); err != nil {
		return fmt.Errorf("tool name %q is not a safe path component: %w", toolName, err)
	}
	if _, err := util.SafeName(version); err != nil {
		return fmt.Errorf("version %q is not a safe path component: %w", version, err)
	}

	cfg := a.getCfg()
	installDir := filepath.Join(cfg.Core.InstallDir, toolName, version)
	if _, err := os.Stat(installDir); err == nil {
		return fmt.Errorf("version %s already installed", version)
	}

	// 从 URL 推断压缩格式
	format := guessArchiveFormat(downloadURL)

	// 下载文件
	cachePath := filepath.Join(cfg.Core.CacheDir, toolName+"-"+version+"."+format)
	dlOpts := downloader.Options{
		URL:        downloadURL,
		Dest:       cachePath,
		MaxRetries: 3,
	}
	if err := downloader.Download(context.Background(), dlOpts); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}

	// 解压
	os.MkdirAll(installDir, 0755)
	if err := extractor.Extract(cachePath, installDir, format, 0); err != nil {
		os.RemoveAll(installDir)
		return fmt.Errorf("extract failed: %v", err)
	}

	// 创建 current junction
	currentLink := filepath.Join(cfg.Core.InstallDir, toolName, "current")
	os.Remove(currentLink)
	if err := createJunction(currentLink, installDir); err != nil {
		return fmt.Errorf("create junction failed: %v", err)
	}

	// 创建 current/<toolName> junction（与内置工具一致的结构）
	currentToolLink := filepath.Join(cfg.Core.XMDevDir, "current", toolName)
	os.Remove(currentToolLink)
	if err := createJunction(currentToolLink, installDir); err != nil {
		logWarn("[InstallCustomTool] create current junction failed: %v", err)
	}

	// 创建 shim 文件
	if tool.Shims != "" {
		shimNames := strings.Split(tool.Shims, ",")
		shimExe := a.resolveShimExe()
		for _, shimName := range shimNames {
			shimName = strings.TrimSpace(shimName)
			if shimName == "" {
				continue
			}
			if err := shim.InstallShim(cfg.Core.ShimDir, shimName, shimExe); err != nil {
				logWarn("[InstallCustomTool] shim install failed for %s: %v", shimName, err)
			}
		}
	}

	// 设置环境变量
	if tool.EnvVars != "" {
		envPairs := strings.Split(tool.EnvVars, ",")
		for _, pair := range envPairs {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 {
				if err := a.SetEnvVar(toolName, parts[0], parts[1], "user"); err != nil {
					logWarn("[InstallCustomTool] set env var %s failed: %v", parts[0], err)
				}
			}
		}
	}

	// 更新安装路径
	a.customMu.Lock()
	for i := range a.customTools {
		if a.customTools[i].Name == toolName {
			a.customTools[i].InstallPath = installDir
			break
		}
	}
	a.saveCustomTools()
	a.customMu.Unlock()

	a.refreshAndSaveLocalCache()
	return nil
}

// saveCustomTools 保存自定义工具列表到文件
// Caller must hold a.customMu lock.
func (a *App) saveCustomTools() {
	if err := a.saveJSONToCache("custom_tools.json", a.customTools); err != nil {
		logWarn("[saveCustomTools] marshal error: %v", err)
	}
}

// loadCustomTools 从文件加载自定义工具列表
func (a *App) loadCustomTools() {
	var tools []CustomTool
	if err := a.loadJSONFromCache("custom_tools.json", &tools); err != nil {
		return
	}
	a.customTools = tools
}

// createJunction 创建 Windows 目录联接（Junction），Unix 上创建符号链接
func createJunction(link, target string) error {
	return util.CreateJunction(link, target)
}

// resolveShimExe 确定用于创建 shim 的 xdev 二进制路径
func (a *App) resolveShimExe() string {
	cfg := a.getCfg()
	currentXdevExe := filepath.Join(cfg.Core.XMDevDir, "current", "xdev", "xdev.exe")
	if runtime.GOOS != "windows" {
		currentXdevExe = filepath.Join(cfg.Core.XMDevDir, "current", "xdev", "xdev")
	}
	if fi, err := os.Stat(currentXdevExe); err == nil && !fi.IsDir() {
		return currentXdevExe
	}
	homeExe := filepath.Join(cfg.Core.XMDevDir, "xdev.exe")
	if runtime.GOOS != "windows" {
		homeExe = filepath.Join(cfg.Core.XMDevDir, "xdev")
	}
	if fi, err := os.Stat(homeExe); err == nil && !fi.IsDir() {
		return homeExe
	}
	exe, err := os.Executable()
	if err != nil {
		return "xdev"
	}
	return exe
}

// guessArchiveFormat 从 URL 推断压缩包格式
func guessArchiveFormat(url string) string {
	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".zip") {
		return "zip"
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
		return "tar.xz"
	}
	if strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") {
		return "tar.bz2"
	}
	if strings.HasSuffix(lower, ".tar.zst") || strings.HasSuffix(lower, ".tzst") {
		return "tar.zst"
	}
	if strings.HasSuffix(lower, ".7z") {
		return "7z"
	}
	if strings.HasSuffix(lower, ".tar") {
		return "tar"
	}
	// 默认使用 zip
	return "zip"
}
