// app_custom_tools.go implements the custom tool management methods for the GUI.
//
// Provides:
//   - PreviewCustomToolVersions: discover versions without writing a manifest
//   - AddCustomTool: discover versions, generate manifest, save config
//   - RemoveCustomTool: delete on-disk manifest and config entry
//   - ListCustomTools: return all registered custom tools
package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/util"
)

// CustomToolResult is the return type for AddCustomTool.
type CustomToolResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Versions []string `json:"versions,omitempty"`
}

// CustomToolInfo is the return type for ListCustomTools.
type CustomToolInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	SourceURL   string `json:"sourceUrl"`
	SourceType  string `json:"sourceType"`
}

// customToolRemote builds the manifest RemoteSource for a custom tool out of
// the GUI's single "source URL" field.
//
// B-07（票 29 / P1-20）：自定义工具的 RemoteSource 有**三个构造点**——
// Preview 的版本发现、AddCustomTool 的版本发现、以及落盘 manifest 的持久化
// 块（tool.Remote = …）。三点必须逐字段一致，尤其是 npm 的 Package：
// 只改前两处时「预览能出结果」，但落盘 manifest 缺 package，之后
// sniff/mergeIntoManifest 重新发现版本时仍报 "npm remote: package is
// required"——死功能只修了一半。三点统一走本构造函数，从结构上排除
// 「只改一处」的可能。
func customToolRemote(sourceURL, sourceType, pattern string) manifest.RemoteSource {
	return manifest.RemoteSource{
		Type:    sourceType,
		URL:     sourceURL,
		Pattern: pattern,
		Package: npmPackageFromSource(sourceURL, sourceType),
	}
}

// npmPackageFromSource 推导 npm 远程类型的包名；非 npm 类型返回 ""。
//
// 为什么需要推导：前端自定义工具表单只有「源 URL」一个输入框
// （frontend/src/pages/SettingsPage.vue 的 ct-url，没有 path/package 字段；
// 前端不在本票白名单内，仅记录在报告里），因此 npm 包名只能由服务端从该
// 字段派生。接受带 registry 前缀的 URL 形式
// （"https://registry.npmjs.org/typescript" → "typescript"），作用域包
// （"…/@scope/pkg" → "@scope/pkg"）保留 scope 段。
//
// registry 地址本身不参与请求：discoverNPM 使用固定的 registry base
// （SSRF 姿态，manifest 不得让 xdev 请求任意主机），只有包名进入请求路径。
func npmPackageFromSource(sourceURL, sourceType string) string {
	if sourceType != "npm" {
		return ""
	}
	s := strings.TrimSpace(sourceURL)
	if s == "" {
		return ""
	}
	// 去掉 scheme://host，只保留路径部分（即包名）。
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			s = rest[j+1:]
		} else {
			s = ""
		}
	}
	s = strings.Trim(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return s
}

// PreviewCustomToolVersions discovers available versions from a remote
// source without writing any manifest.  Returns the version list or nil
// on error.
func (a *App) PreviewCustomToolVersions(sourceURL, sourceType, pattern string) []string {
	if sourceURL == "" {
		return nil
	}
	switch sourceType {
	case "html", "json", "npm":
		if err := util.IsSafeExternalURL(sourceURL); err != nil {
			logWarn("[PreviewCustomToolVersions] unsafe URL %q: %v", sourceURL, err)
			return nil
		}
	case "github":
		if strings.Contains(sourceURL, "://") || !strings.Contains(sourceURL, "/") {
			logWarn("[PreviewCustomToolVersions] github source must use owner/repo format, got %q", sourceURL)
			return nil
		}
	}
	if pattern == "" && sourceType == "github" {
		pattern = `v?(\d+\.\d+\.\d+)$`
	}

	rs := customToolRemote(sourceURL, sourceType, pattern)
	versions, err := remote.DiscoverVersions(context.Background(), rs)
	if err != nil {
		logWarn("[PreviewCustomToolVersions] discover failed: %v", err)
		return nil
	}
	return versions
}

// AddCustomTool discovers versions for a custom tool, generates its
// manifest YAML, writes it to disk, saves the config entry, and reloads
// the catalog.  Returns a CustomToolResult with the discovered versions
// on success, or error details on failure.
func (a *App) AddCustomTool(name, displayName, sourceURL, sourceType, pattern, shim, envVar string) *CustomToolResult {
	// --- Validate name ---
	matched, err := regexp.MatchString(`^[a-z0-9-]+$`, name)
	if err != nil || !matched {
		return &CustomToolResult{
			Success: false,
			Message: "tool name must match ^[a-z0-9-]+$ (lowercase letters, digits, hyphens only)",
		}
	}

	// --- B-05（票 29 / P1-18 + N-13）三重存在性门 ---
	//
	// 同名工具可以从三条不同路径混进 catalog/manifest 目录，任一条命中都
	// 必须在**写盘之前**拒绝：
	//   1) catalog 命中 —— 与内置（嵌入）manifest 或已加载的第三方 manifest
	//      同名。嵌入优先的合并语义会把自定义工具追加的 Discovered 版本并进
	//      内置版本表，等于污染内置工具；
	//   2) cfg.CustomTools 命中 —— 上一次添加留下 config 记录（磁盘 yaml 可能
	//      已被手工删除）。旧代码（:211-220）在这里无条件 append，N-13：
	//      config 里出现两条同名记录；
	//   3) 磁盘 manifest 已存在 —— 同名 yaml 未进 catalog（坏 yaml 被 loader
	//      跳过、第三方手工放置）。catalog 检查看不见它，旧代码会用本次生成的
	//      manifest 覆盖掉别人的文件。
	// 第 3 条同时把「回滚只删本次新建的文件」变成平凡真值：越过此门后
	// manifestPath 一定不存在，cfg.Save 失败时的 os.Remove 不可能删掉旧文件。
	cfg := a.getCfg()
	manifestPath := filepath.Join(cfg.Core.ManifestDir, name+".yaml")
	if cat := a.getCat(); cat != nil {
		if _, exists := cat.Tool(name); exists {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("tool %q already exists in the catalog (built-in or loaded manifest); choose another name", name),
			}
		}
	}
	for _, ct := range cfg.CustomTools {
		if ct.Name == name {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("custom tool %q is already registered in the config", name),
			}
		}
	}
	if _, statErr := os.Stat(manifestPath); statErr == nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("manifest %s already exists on disk; remove it before adding %q again", manifestPath, name),
		}
	} else if !os.IsNotExist(statErr) {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("cannot inspect manifest path %s: %v", manifestPath, statErr),
		}
	}

	// --- Validate shim name (F-08 / D11): 空则默认用工具名 ---
	if shim != "" {
		if err := util.ValidateShimName(shim); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("invalid shim name: %v", err),
			}
		}
	}

	// --- Validate envVar name (F-09 / D12): 空则默认 <NAME>_HOME ---
	if envVar != "" {
		if err := util.ValidateEnvVarName(envVar); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("invalid env var: %v", err),
			}
		}
	}

	// --- Validate sourceURL ---
	if sourceURL == "" {
		return &CustomToolResult{Success: false, Message: "source URL is required"}
	}
	switch sourceType {
	case "github":
		if strings.Contains(sourceURL, "://") || !strings.Contains(sourceURL, "/") {
			return &CustomToolResult{
				Success: false,
				Message: "GitHub source must use owner/repo format (e.g. golang/go)",
			}
		}
	case "html", "json", "npm":
		// Validate via the same safety check used by the remote package
		if err := util.IsSafeExternalURL(sourceURL); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("%s source URL is not safe: %v", sourceType, err),
			}
		}
	}

	// --- Default pattern for GitHub ---
	if pattern == "" && sourceType == "github" {
		pattern = `v?(\d+\.\d+\.\d+)$`
	}

	// --- Discover versions ---
	// B-07：与 Preview 及下面的落盘块共用同一构造函数，三点字段完全一致。
	rs := customToolRemote(sourceURL, sourceType, pattern)
	versions, err := remote.DiscoverVersions(context.Background(), rs)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("discover versions: %v", err),
		}
	}
	if len(versions) == 0 {
		return &CustomToolResult{
			Success: false,
			Message: "no versions discovered from source",
		}
	}

	// --- Convert to manifest.DiscoveredVersion ---
	discoveredVersions := make([]manifest.DiscoveredVersion, 0, len(versions))
	for _, v := range versions {
		discoveredVersions = append(discoveredVersions, manifest.DiscoveredVersion{Version: v})
	}

	// --- Generate manifest YAML ---
	yamlBytes, err := manifest.GenerateCustomManifest(name, displayName, shim, envVar, discoveredVersions)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("generate manifest: %v", err),
		}
	}

	// --- Unmarshal, set remote block, remarshal ---
	var tool manifest.Tool
	if err := yaml.Unmarshal(yamlBytes, &tool); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("parse generated manifest: %v", err),
		}
	}
	// B-07（持久化块）：这里是三构造点的第三处——落盘 manifest 的 remote
	// 段。npm 类型必须写出 package，否则工具落盘后再做版本发现
	// （sniff/mergeIntoManifest）仍会因 "npm remote: package is required"
	// 失败，即死功能只修一半。
	tool.Remote = customToolRemote(sourceURL, sourceType, pattern)
	finalYaml, err := yaml.Marshal(&tool)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("marshal manifest: %v", err),
		}
	}

	// --- Write manifest to disk ---
	// 复用三重门已取的配置快照（getCfg 返回深拷贝），保证「门检查的路径」
	// 与「写入的路径」是同一个值。
	if err := os.MkdirAll(cfg.Core.ManifestDir, 0o755); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("create manifest dir: %v", err),
		}
	}
	if err := util.AtomicWriteFile(manifestPath, finalYaml, 0o644); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("write manifest: %v", err),
		}
	}

	// --- Update config ---
	a.cfgMu.Lock()
	a.cfg.CustomTools = append(a.cfg.CustomTools, config.CustomToolConfig{
		Name:        name,
		DisplayName: displayName,
		SourceURL:   sourceURL,
		SourceType:  sourceType,
		Pattern:     pattern,
		Shim:        shim,
		EnvVar:      envVar,
	})
	if err := a.cfg.Save(); err != nil {
		// B-05 回滚：Save 失败必须撤销本次的两处副作用，否则磁盘上留下
		// 一份「config 里没有」的孤儿 manifest，而三重门的第 3 条会让下一次
		// 添加直接报「manifest 已存在」——用户被自己的失败操作卡住。
		//   - 内存：截掉本次 append 的 config 记录（「cfg 未变」）；
		//   - 磁盘：删除本次新建的 manifest。第 3 条门保证进入写入段时
		//     manifestPath 不存在，因此这里的 Remove 不可能删掉既有文件，
		//     「只删本次新建的文件」是平凡真值。
		a.cfg.CustomTools = a.cfg.CustomTools[:len(a.cfg.CustomTools)-1]
		a.cfgMu.Unlock()
		if rmErr := os.Remove(manifestPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logWarn("[custom-tool] rollback of %s failed: %v", manifestPath, rmErr)
		}
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("save config: %v", err),
		}
	}
	a.cfgMu.Unlock()

	// --- Reload catalog + live plugin manifests ---
	if err := a.reloadCatalog(); err != nil {
		logWarn("[custom-tool] reload catalog after add: %v", err)
	}

	return &CustomToolResult{
		Success:  true,
		Message:  fmt.Sprintf("tool %q added with %d versions", name, len(versions)),
		Versions: versions,
	}
}

// RemoveCustomTool deletes the on-disk manifest for a custom tool and
// removes it from the config.  Returns an error if the tool is not
// found.
func (a *App) RemoveCustomTool(name string) error {
	// Validate name (same regex — prevent path traversal)
	matched, err := regexp.MatchString(`^[a-z0-9-]+$`, name)
	if err != nil || !matched {
		return fmt.Errorf("invalid tool name %q", name)
	}

	cfg := a.getCfg()

	// Find and remove from config
	a.cfgMu.Lock()
	idx := -1
	for i, ct := range a.cfg.CustomTools {
		if ct.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.cfgMu.Unlock()
		return fmt.Errorf("custom tool %q not found in config", name)
	}
	a.cfg.CustomTools = append(a.cfg.CustomTools[:idx], a.cfg.CustomTools[idx+1:]...)
	if err := a.cfg.Save(); err != nil {
		a.cfgMu.Unlock()
		return fmt.Errorf("save config: %w", err)
	}
	a.cfgMu.Unlock()

	// Delete manifest file (best-effort — no error if already gone)
	manifestPath := filepath.Join(cfg.Core.ManifestDir, name+".yaml")
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove manifest %s: %w", manifestPath, err)
	}

	if err := a.reloadCatalog(); err != nil {
		logWarn("[custom-tool] reload catalog after remove: %v", err)
	}

	return nil
}

// ListCustomTools returns the list of registered custom tools from config.
func (a *App) ListCustomTools() []CustomToolInfo {
	cfg := a.getCfg()
	out := make([]CustomToolInfo, 0, len(cfg.CustomTools))
	for _, ct := range cfg.CustomTools {
		out = append(out, CustomToolInfo{
			Name:        ct.Name,
			DisplayName: ct.DisplayName,
			SourceURL:   ct.SourceURL,
			SourceType:  ct.SourceType,
		})
	}
	return out
}
