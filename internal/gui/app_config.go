// app_config.go is the prefs module behind the Wails App adapter
// (Spec 029 B2). It implements configuration management for the GUI.
//
// Provides:
//   - GetConfig: returns the full configuration as JSON
//   - GetMirrorConfig / SetMirrorConfig: per-tool mirror URLs
//   - GetPathMode / SetPathMode: PATH management mode (shim/direct/auto)
//   - GetProxyConfig / SetProxyConfig: HTTP/HTTPS proxy settings
//   - GetDrives: enumerate available drives (Windows only)
//   - OpenInFileExplorer: open a directory in the OS file manager
//   - SelectDirectory: native directory picker dialog
//   - ValidateDirectory: check if a directory is suitable for xdev home
//
// Mirror speed testing (SpeedTestMirror) runs concurrent HEAD requests
// against configured mirrors and returns latency rankings.
package gui

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

// GetMirrorConfig returns the current mirror configuration.
func (a *App) GetMirrorConfig() map[string]string {
	cfg := a.getCfg()
	return cfg.Mirror
}

// SetMirrorConfig updates the mirror configuration and saves.
func (a *App) SetMirrorConfig(mirrors map[string]string) error {
	for tool, rawURL := range mirrors {
		if err := util.IsSafeHTTPSExternalURL(rawURL); err != nil {
			return fmt.Errorf("mirror URL for %s is unsafe: %w", tool, err)
		}
	}
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg.Mirror = mirrors
	return a.cfg.Save()
}

// RunDiagnostics performs basic health checks and returns results.
func (a *App) RunDiagnostics() []DiagnosticResult {
	cfg := a.getCfg()
	var results []DiagnosticResult

	// Check directories exist
	dirs := map[string]string{
		"XMDevDir":    cfg.Core.XMDevDir,
		"InstallDir":  cfg.Core.InstallDir,
		"ShimDir":     cfg.Core.ShimDir,
		"CacheDir":    cfg.Core.CacheDir,
		"DownloadDir": cfg.Core.DownloadDir,
		"EnvDir":      cfg.Core.EnvDir,
	}
	for name, dir := range dirs {
		_, err := os.Stat(dir)
		dr := DiagnosticResult{
			Check: name,
			Path:  dir,
			OK:    err == nil,
		}
		if err != nil {
			dr.Error = err.Error()
		}
		results = append(results, dr)
	}

	// Check shim directory is in PATH
	inPath := false
	pathEnv := os.Getenv("PATH")
	for _, p := range filepath.SplitList(pathEnv) {
		if p == cfg.Core.ShimDir {
			inPath = true
			break
		}
	}
	results = append(results, DiagnosticResult{
		Check: "ShimDir in PATH",
		Path:  cfg.Core.ShimDir,
		OK:    inPath,
	})

	return results
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() ConfigInfo {
	cfg := a.getCfg()
	return ConfigInfo{
		XMDevDir:    cfg.Core.XMDevDir,
		InstallDir:  cfg.Core.InstallDir,
		ShimDir:     cfg.Core.ShimDir,
		CacheDir:    cfg.Core.CacheDir,
		DownloadDir: cfg.Core.DownloadDir,
		PathMode:    cfg.GetPathMode(),
		Proxy: ProxyInfo{
			HTTP:    cfg.Proxy.HTTP,
			HTTPS:   cfg.Proxy.HTTPS,
			NoProxy: cfg.Proxy.NoProxy,
			Enabled: cfg.Proxy.Enabled,
		},
	}
}

// GetPathMode returns the current PATH management mode ("shim" or "direct").
func (a *App) GetPathMode() string {
	cfg := a.getCfg()
	return cfg.GetPathMode()
}

// GetMigrationMode returns the current local-tool migration mode
// ("junction" or "move"), defaulting to "junction".
func (a *App) GetMigrationMode() string {
	cfg := a.getCfg()
	return cfg.GetMigrationMode()
}

// SetMigrationMode persists the local-tool migration mode ("junction" or
// "move") and saves the config. The mode is read back by the migration
// entry points (MigrateLocalToolByJunction / MigrateLocalToolByMove).
func (a *App) SetMigrationMode(mode string) error {
	if err := a.mutateCfg(func(cfg *config.Config) error {
		return cfg.SetMigrationMode(mode)
	}); err != nil {
		return err
	}
	logInfo("[SetMigrationMode] migration mode set to %s", mode)
	return nil
}

// SetPathMode switches the PATH management mode and migrates the PATH entries.
func (a *App) SetPathMode(mode string) error {
	cfg := a.getCfg()
	oldMode := cfg.GetPathMode()
	if mode == oldMode {
		return nil
	}
	if mode != "shim" && mode != "direct" {
		// 2026-10-25（P0-4）：与 config.SetPathMode 保持同一套拒绝语义。
		// "auto" 单独给提示：它不是非法值，而是**尚未实现**，
		// 笼统报 "invalid" 会让用户以为自己拼错了。
		if mode == "auto" {
			return fmt.Errorf("path mode 'auto' is not supported yet: " +
				"per-project switching via the cd hook is not implemented (use 'shim')")
		}
		return fmt.Errorf("invalid path mode: %s (must be 'shim' or 'direct')", mode)
	}

	logInfo("[SetPathMode] switching from %s to %s", oldMode, mode)

	// 获取所有已激活的工具
	currentDir := filepath.Join(cfg.Core.XMDevDir, "current")
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return fmt.Errorf("failed to read current directory: %w", err)
	}

	var activeTools []struct {
		name string
		p    plugin.Plugin
	}
	for _, e := range entries {
		// 第十轮审计 P3-8（P3）：current/ 下可能残留 xdev 自己的旁路条目
		// （`.stale-*` 来自 removeLinkEntry 的重命名，`.tmp-*` 来自 Activate
		// 的临时链接名）。它们不是工具，绝不能被当成"已激活工具"参与
		// PATH 迁移 —— 否则会把残留目录的 bin 路径写进 PATH，或为其创建
		// shim。过滤规则与 shim 反向索引共用同一份定义。
		if shim.IsStaleResidueName(e.Name()) {
			continue
		}
		// 检查是否是有效的 Junction/目录。
		//
		// 第十轮审计附带发现（**P0 级**，由本项守卫暴露）：不能用 `e.IsDir()`。
		// `current/<tool>` 在 Windows 上是**目录联接（junction）**，而
		// `os.ReadDir` 的 `DirEntry.IsDir()` 对重解析点返回 **false**
		// （本机实测：`IsDir()=false`、`Type=?---------`；同一路径 `os.Stat`
		// 的 `IsDir()` 为 **true**）。于是旧代码的 `!e.IsDir() { continue }`
		// 会**跳过每一个真实工具** —— `SetPathMode("direct")` 静默地一个 bin
		// 路径都不加，用户切到 direct 后 PATH 里没有工具，且没有任何报错。
		//
		// 正确判据是「**跟随**链接后确实是目录」，即 `os.Stat`（而非 Lstat）。
		// 这同时保留原有语义：悬空链接会被 `os.Stat` 判为不存在而跳过。
		linkPath := filepath.Join(currentDir, e.Name())
		fi, statErr := os.Stat(linkPath)
		if statErr != nil || !fi.IsDir() {
			continue
		}
		if p, ok := a.registry.Get(e.Name()); ok {
			activeTools = append(activeTools, struct {
				name string
				p    plugin.Plugin
			}{name: e.Name(), p: p})
		}
	}

	if mode == "direct" {
		// shim → direct: 移除 shim 目录，添加 current/<tool>/bin 路径
		// 1. 为每个活跃工具添加 current/<tool>/bin 到 PATH
		for _, at := range activeTools {
			toolCurrentDir := filepath.Join(cfg.Core.XMDevDir, "current", at.name)
			binPaths := env.ToolBinPaths(at.name, toolCurrentDir)
			if len(binPaths) > 0 {
				if err := env.AddCurrentBinPaths(binPaths); err != nil {
					logWarn("[SetPathMode] failed to add %s bin paths: %v", at.name, err)
				} else {
					logInfo("[SetPathMode] added %s bin paths to PATH", at.name)
				}
			}
		}
		// 2. 从 PATH 中移除 shim 目录
		if err := env.RemoveFromUserPath(cfg.Core.ShimDir); err != nil {
			logWarn("[SetPathMode] failed to remove shim dir from PATH: %v", err)
		} else {
			logInfo("[SetPathMode] removed shim dir from PATH")
		}
		// 3. 删除 shim 文件（节省磁盘空间）
		for _, at := range activeTools {
			for _, shimName := range at.p.Shims() {
				shimPath := filepath.Join(cfg.Core.ShimDir, shimName)
				os.Remove(shimPath)
				if runtime.GOOS == "windows" {
					os.Remove(shimPath + ".exe")
				}
			}
		}
		logInfo("[SetPathMode] removed shim files")
	} else {
		// direct → shim: 移除 current/<tool>/bin 路径，添加 shim 目录，创建 shim 文件
		// 1. 从 PATH 中移除所有 current/ 前缀的条目
		if err := env.RemoveAllCurrentPaths(currentDir); err != nil {
			logWarn("[SetPathMode] failed to remove current paths from PATH: %v", err)
		} else {
			logInfo("[SetPathMode] removed current paths from PATH")
		}
		// 2. 将 shim 目录添加到 PATH 最前面
		if err := env.SetWindowsUserPath(cfg.Core.ShimDir); err != nil {
			logWarn("[SetPathMode] failed to add shim dir to PATH: %v", err)
		} else {
			logInfo("[SetPathMode] added shim dir to PATH")
		}
		// 3. 为每个活跃工具创建 shim 文件
		ins := a.newInstaller(cfg)
		for _, at := range activeTools {
			for _, shimName := range at.p.Shims() {
				shimExe := ins.ResolveShimExe()
				if err := shim.InstallShim(cfg.Core.ShimDir, shimName, shimExe); err != nil {
					logWarn("[SetPathMode] failed to create shim %s: %v", shimName, err)
				} else {
					logInfo("[SetPathMode] created shim: %s", shimName)
				}
			}
		}
	}

	// 保存配置（F-19/D17：写锁内操作实时配置，快照上的修改不会落盘）
	if err := a.mutateCfg(func(c *config.Config) error {
		return c.SetPathMode(mode)
	}); err != nil {
		logWarn("[SetPathMode] failed to save config: %v", err)
	}
	shim.InvalidateShimIndex()
	env.BroadcastEnvironmentChange()

	logInfo("[SetPathMode] mode switched to %s", mode)
	return nil
}

// GetProxyConfig returns the current proxy configuration.
func (a *App) GetProxyConfig() ProxyInfo {
	cfg := a.getCfg()
	return ProxyInfo{
		HTTP:    cfg.Proxy.HTTP,
		HTTPS:   cfg.Proxy.HTTPS,
		NoProxy: cfg.Proxy.NoProxy,
		Enabled: cfg.Proxy.Enabled,
	}
}

// SetProxyConfig updates proxy settings and reconfigures the HTTP client.
func (a *App) SetProxyConfig(http, https, noProxy string, enabled bool) error {
	if err := a.mutateCfg(func(cfg *config.Config) error {
		return cfg.SetProxy(http, https, noProxy, enabled)
	}); err != nil {
		return err
	}
	remote.ConfigureHTTPClient(a.getCfg().GetProxyURL("https"))
	return nil
}

// TestMirrorConnectivity 测试镜像源连通性
func (a *App) TestMirrorConnectivity(mirrorURL string) ProxyTestResult {
	// Security (N-4): Validate the URL to prevent SSRF — the frontend
	// could supply an internal/loopback URL to probe the network.
	if err := util.IsSafeExternalURL(mirrorURL); err != nil {
		return ProxyTestResult{
			Success: false,
			Message: fmt.Sprintf("URL 不安全: %v", err),
		}
	}
	result := ProxyTestResult{
		Success: false,
		Message: "",
		Latency: 0,
	}

	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: util.SecureRedirectCheck(3),
	}

	start := time.Now()
	resp, err := client.Head(mirrorURL)
	latency := time.Since(start).Milliseconds()
	result.Latency = latency

	if err != nil {
		result.Message = fmt.Sprintf("连接失败: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Success = true
		result.Message = fmt.Sprintf("连接成功 (HTTP %d, %dms)", resp.StatusCode, latency)
	} else {
		result.Message = fmt.Sprintf("HTTP %d (%dms)", resp.StatusCode, latency)
	}
	return result
}

// TestProxy tests the proxy connection by making a HEAD request.
func (a *App) TestProxy(proxyURL, testURL string) ProxyTestResult {
	if testURL == "" {
		testURL = "https://www.google.com"
	}
	// Security (N-5): Validate the test URL to prevent SSRF via the
	// proxy — the frontend could supply an internal/loopback URL.
	if err := util.IsSafeExternalURL(testURL); err != nil {
		return ProxyTestResult{
			Success: false,
			Message: fmt.Sprintf("测试 URL 不安全: %v", err),
		}
	}
	result := ProxyTestResult{
		Success: false,
		Message: "",
		Latency: 0,
	}

	if err := util.ValidateProxyURL(proxyURL); err != nil {
		result.Message = fmt.Sprintf("无效的代理地址: %v", err)
		return result
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		result.Message = fmt.Sprintf("无效的代理地址: %v", err)
		return result
	}

	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) { return parsed, nil },
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     transport,
		CheckRedirect: util.SecureRedirectCheck(3),
	}

	start := time.Now()
	resp, err := client.Head(testURL)
	latency := time.Since(start).Milliseconds()
	result.Latency = latency

	if err != nil {
		result.Message = fmt.Sprintf("连接失败: %v", err)
		if isProxyConnError(err) {
			// The proxy itself is unreachable — give the user an
			// actionable hint instead of a raw dial error. Clash Verge
			// (mihomo) commonly listens on mixed-port 7897, not 7890.
			result.Message = fmt.Sprintf(
				"连接失败: 代理 %s 不可达。Clash Verge 当前常用 mixed-port 为 7897（不是 7890）。请在设置中修改端口，或运行「探测本地代理」。",
				parsed.Host)
		} else {
			// Security (F-15): 错误消息中回显的代理地址去除 userinfo/query，
			// 防止代理 URL 中的凭据泄露。
			result.Message = strings.ReplaceAll(result.Message, proxyURL, util.SanitizeURL(proxyURL))
		}
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Success = true
		result.Message = fmt.Sprintf("连接成功 (HTTP %d, %dms)", resp.StatusCode, latency)
	} else {
		result.Message = fmt.Sprintf("HTTP %d (%dms)", resp.StatusCode, latency)
	}
	return result
}

// isProxyConnError reports whether the error indicates the proxy itself
// was unreachable (dead port), as opposed to a downstream failure.
// Thin alias over util.IsProxyError (Spec 021 D4).
func isProxyConnError(err error) bool {
	return util.IsProxyError(err)
}

// ProbeLocalProxies returns candidate proxy URLs that accept HTTP CONNECT
// on localhost. Candidates cover common Clash Verge / V2Ray / sing-box
// mixed-ports (7890, 7891, 7897, 7899, 10809, 20171, 6152, 1080, 8080).
// Each candidate is TCP-dialed with a 300ms timeout; open ports are then
// verified with a HEAD request through the proxy (3s timeout).
func (a *App) ProbeLocalProxies() []ProxyProbeResult {
	ports := []int{7890, 7891, 7897, 7899, 10809, 20171, 6152, 1080, 8080}
	results := make([]ProxyProbeResult, 0, len(ports))
	for _, port := range ports {
		results = append(results, probeLocalProxy(port))
	}
	return results
}

// probeLocalProxy probes a single localhost proxy port.
func probeLocalProxy(port int) ProxyProbeResult {
	res := ProxyProbeResult{
		URL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		Port: port,
	}

	// Step 1: is the port even listening?
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		res.Message = "端口未监听"
		return res
	}
	conn.Close()

	// Step 2: verify it actually proxies HTTPS (HTTP CONNECT).
	proxyURL, _ := url.Parse(res.URL)
	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) { return proxyURL, nil },
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	start := time.Now()
	resp, err := client.Head("https://api.github.com")
	res.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Message = fmt.Sprintf("端口已监听，但代理请求失败: %v", err)
		return res
	}
	defer resp.Body.Close()

	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	res.Message = fmt.Sprintf("HTTP %d (%dms)", resp.StatusCode, res.LatencyMs)
	return res
}

// GetCacheBreakdown 遍历 CacheDir，按文件扩展名分类统计大小
func (a *App) GetCacheBreakdown() CacheBreakdown {
	cfg := a.getCfg()
	var breakdown CacheBreakdown

	entries, err := os.ReadDir(cfg.Core.CacheDir)
	if err != nil {
		return breakdown
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size := info.Size()
		name := entry.Name()
		breakdown.TotalCount++

		// 分类规则单一来源：cache_classify.go 的 cacheEntryCategory。
		switch cacheEntryCategory(name) {
		case "archives":
			breakdown.Archives += size
		case "shaFiles":
			breakdown.ShaFiles += size
		case "partFiles":
			breakdown.PartFiles += size
		case "scanCache":
			breakdown.ScanCache += size
		default:
			breakdown.OtherFiles += size
		}
	}

	return breakdown
}

// CleanCache 按分类删除缓存文件
// category: "archives" | "shaFiles" | "partFiles" | "all"
//
// 第七轮审计 F-06（P1）：本函数曾与 CleanCacheCategory 语义分叉——它的
// `case "all"` 直接 `shouldDelete = true`，把 CacheDir 下的**每一个**文件都
// 删掉，而 CacheDir 里同时住着**非缓存的用户状态文件**：favorites.json（收藏）、
// ignored_tools.json（更新忽略列表）、custom_tools.json（自定义工具注册）、
// keystore.dat（TOFU 密钥库）、shim-index.v1、sniff-cache.json、
// tool_scan_cache.json。用户点一次「清理所有缓存」即静默清空这些数据，
// 且确认文案只说「需要重新下载」。
//
// 现在收敛为**单一实现**：直接委托给 CleanCacheCategory（分类规则来自
// cache_classify.go 的 cacheEntryCategory 单一来源），"all" 只删真正的缓存
// （归档 / .sha256 / .part），元数据归入 "otherFiles" 而**不被 "all" 删除**。
// 保留本入口是为了不破坏既有 Wails 绑定（前端历史调用点仍可达）。
func (a *App) CleanCache(category string) error {
	return a.CleanCacheCategory(category)
}
