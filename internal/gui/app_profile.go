// app_profile.go implements environment profile export/import for the GUI.
//
// Profiles capture the complete xdev environment state as a JSON file:
// installed tools, active versions, PATH mode, proxy, mirrors, env vars,
// favorites, and custom tools. This enables team-wide environment
// synchronization or personal backup/restore.
//
// Provides:
//   - ExportProfile: save the current environment to a JSON file
//   - ImportProfile: load and apply a previously exported profile
//
// Import applies settings in order: PATH mode → proxy → mirrors →
// install tools → activate versions → env vars → favorites → custom tools.
package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// ExportProfile exports the current environment profile to a JSON file.
// The profile includes: installed tools, active versions, environment variables,
// PATH mode, proxy settings, and mirror configuration.
func (a *App) ExportProfile(filePath string) error {
	// Security (F-11): 只允许导出为 .json 文件，且目标目录必须存在（可写）。
	if strings.ToLower(filepath.Ext(filePath)) != ".json" {
		return fmt.Errorf("profile export path must end with .json: %q", filePath)
	}
	parent := filepath.Dir(filePath)
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return fmt.Errorf("export directory does not exist: %s", parent)
	}

	cfg := a.getCfg()

	profile := map[string]interface{}{
		"version":  version.Version,
		"exported": time.Now().Format(time.RFC3339),
		"pathMode": cfg.GetPathMode(),
		"proxy":    cfg.Proxy,
		"mirrors":  cfg.Mirror,
		"tools":    []map[string]string{},
		"envVars":  []map[string]string{},
	}

	// Collect installed tools and active versions
	registry := a.getRegistry()
	if registry != nil {
		tools := profile["tools"].([]map[string]string)
		for _, name := range registry.Names() {
			activeVer, _ := a.currentVersion(name)
			installed := a.installedVersions(name)
			for _, ver := range installed {
				// If version is "local", try to detect the real version
				exportVer := ver
				if ver == "local" {
					installPath := filepath.Join(cfg.Core.InstallDir, name, ver)
					if detected := getVersion(name, installPath); detected != "" {
						exportVer = sanitizeVersionStr(detected)
						logInfo("[ExportProfile] %s: detected real version %s (was 'local')", name, exportVer)
					} else if detected := extractVersionFromPath(installPath); detected != "" {
						exportVer = sanitizeVersionStr(detected)
						logInfo("[ExportProfile] %s: extracted version %s from path (was 'local')", name, exportVer)
					}
				}
				entry := map[string]string{
					"tool":    name,
					"version": exportVer,
					"active":  "",
				}
				if ver == activeVer {
					entry["active"] = "true"
				}
				tools = append(tools, entry)
			}
		}
		profile["tools"] = tools
	}

	// Collect user environment variables set by xdev
	envVars := profile["envVars"].([]map[string]string)
	userEnvVars := a.GetEnvVars()
	for _, ev := range userEnvVars {
		if ev.Source == "user" {
			envVars = append(envVars, map[string]string{
				"name":  ev.Name,
				"value": ev.Value,
				"level": "user",
			})
		}
	}
	profile["envVars"] = envVars

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	return nil
}

// profileMaxSize 是导入配置文件的大小上限（10 MB），防止读取任意超大文件。
const profileMaxSize = 10 << 20

// installToolForProfile 是 ImportProfile 的安装入口（默认 = 真实安装）。
//
// 包级可替换的理由：GUI 单测没有 Wails 运行时上下文，而安装成功路径会经
// wailsRuntime.EventsEmit 发进度事件（ctx 缺失时 Wails 直接 log.Fatalf
// 结束整个测试进程），所以守卫 29-2「未安装的 active 项不产生假错误」无法
// 在测试里走真实下载/激活。测试注入内存实现后，UseVersion 这一侧的行为
//（本票的修复点）仍然走真实代码。
var installToolForProfile = func(a *App, tool, version string) error {
	return a.InstallTool(tool, version)
}

// ImportProfile imports an environment profile from a JSON file.
// It installs the specified tool versions, sets active versions,
// configures environment variables, proxy, and mirrors.
func (a *App) ImportProfile(filePath string) ImportResult {
	result := ImportResult{
		Success:   false,
		Message:   "",
		Installed: 0,
		Failed:    0,
		Errors:    []string{},
	}

	// Security (F-11): 校验路径 —— 必须是 .json 常规文件且大小受限，
	// 防止读取任意路径/设备文件。
	if strings.ToLower(filepath.Ext(filePath)) != ".json" {
		result.Message = fmt.Sprintf("profile import path must end with .json: %q", filePath)
		return result
	}
	fi, err := os.Stat(filePath)
	if err != nil {
		result.Message = fmt.Sprintf("读取文件失败: %v", err)
		return result
	}
	if !fi.Mode().IsRegular() {
		result.Message = fmt.Sprintf("profile path is not a regular file: %q", filePath)
		return result
	}
	if fi.Size() > profileMaxSize {
		result.Message = fmt.Sprintf("profile file too large (%d bytes, limit %d)", fi.Size(), profileMaxSize)
		return result
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		result.Message = fmt.Sprintf("读取文件失败: %v", err)
		return result
	}

	var profile struct {
		Version  string             `json:"version"`
		PathMode string             `json:"pathMode"`
		Proxy    config.ProxyConfig `json:"proxy"`
		Mirrors  map[string]string  `json:"mirrors"`
		Tools    []struct {
			Tool    string `json:"tool"`
			Version string `json:"version"`
			Active  string `json:"active"`
		} `json:"tools"`
		EnvVars []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Level string `json:"level"`
		} `json:"envVars"`
	}

	if err := json.Unmarshal(data, &profile); err != nil {
		result.Message = fmt.Sprintf("解析文件失败: %v", err)
		return result
	}

	errors := []string{}
	installed := 0
	failed := 0

	// Apply PATH mode（F-19/D17：写锁内操作实时配置）
	if profile.PathMode != "" {
		if err := a.mutateCfg(func(cfg *config.Config) error {
			return cfg.SetPathMode(profile.PathMode)
		}); err != nil {
			errors = append(errors, fmt.Sprintf("set path mode: %v", err))
		}
	}

	// Apply proxy settings
	if err := a.mutateCfg(func(cfg *config.Config) error {
		return cfg.SetProxy(profile.Proxy.HTTP, profile.Proxy.HTTPS, profile.Proxy.NoProxy, profile.Proxy.Enabled)
	}); err != nil {
		errors = append(errors, fmt.Sprintf("set proxy: %v", err))
	}

	// Apply mirror settings.
	// 票 16（P1-17 / P2-15）：profile 是不可信的用户输入 JSON，mirrors 必须
	// 逐项用 util.IsSafeHTTPSExternalURL 校验后才写入 cfg.Mirror：明文 http、
	// localhost、内网/保留地址等不安全项一律丢弃，并把该项计入 result.Errors
	// （保持既有 result 结构，逐项追加错误消息）。
	if len(profile.Mirrors) > 0 {
		safeMirrors := make(map[string]string, len(profile.Mirrors))
		for name, mirrorURL := range profile.Mirrors {
			if err := util.IsSafeHTTPSExternalURL(mirrorURL); err != nil {
				errors = append(errors, fmt.Sprintf("mirror %s: %v", name, err))
				continue
			}
			safeMirrors[name] = mirrorURL
		}
		// 只在校验后仍有合法项时落盘；全部被拒绝时保留用户既有 mirrors，
		// 避免 profile 里的一批坏 URL 反而清空配置。
		if len(safeMirrors) > 0 {
			if err := a.mutateCfg(func(cfg *config.Config) error {
				cfg.Mirror = safeMirrors
				return cfg.Save()
			}); err != nil {
				errors = append(errors, fmt.Sprintf("set mirrors: %v", err))
			}
		}
	}

	// Install tools
	registry := a.getRegistry()
	if registry != nil {
		for _, t := range profile.Tools {
			if _, ok := registry.Get(t.Tool); ok {
				// Check if already installed
				installedVersions := a.installedVersions(t.Tool)
				alreadyInstalled := false
				for _, v := range installedVersions {
					if v == t.Version {
						alreadyInstalled = true
						break
					}
				}

				// B-06（票 29 / P1-19）：本项若由下面的 InstallTool 安装
				// （后台安装路径），就绝不能再调 UseVersion —— 安装本身已经
				// 完成激活，而旧代码会对这个「刚下安装意图、版本目录尚未落地」
				// 的版本调用 UseVersion → Activate 在缺失的 installPath 上失败
				// → result.Errors 里出现一条假错误（导入报告「失败」但工具其实
				// 装好了）。UseVersion 只用于切换**已安装**版本。
				justInstalled := false
				if !alreadyInstalled {
					if err := installToolForProfile(a, t.Tool, t.Version); err != nil {
						failed++
						errors = append(errors, fmt.Sprintf("%s@%s: %v", t.Tool, t.Version, err))
						continue
					}
					justInstalled = true
				}

				installed++

				// Set active version
				if t.Active == "true" && !justInstalled {
					if err := a.UseVersion(t.Tool, t.Version); err != nil {
						errors = append(errors, fmt.Sprintf("%s use %s: %v", t.Tool, t.Version, err))
					}
				}
			} else {
				failed++
				errors = append(errors, fmt.Sprintf("%s: 未知工具", t.Tool))
			}
		}
	}

	// Set environment variables. Profiles are untrusted user-supplied
	// JSON: only syntactically valid, non-protected names may be written.
	for _, e := range profile.EnvVars {
		if e.Level != "user" {
			continue
		}
		if err := util.ValidateEnvVarName(e.Name); err != nil {
			errors = append(errors, fmt.Sprintf("env %s: %v", e.Name, err))
			continue
		}
		if err := env.SetUserEnvVar(e.Name, e.Value); err != nil {
			errors = append(errors, fmt.Sprintf("env %s: %v", e.Name, err))
		}
	}

	result.Success = true
	result.Installed = installed
	result.Failed = failed
	result.Errors = errors
	result.Message = fmt.Sprintf("导入完成: %d 个工具已安装, %d 个失败", installed, failed)

	return result
}
