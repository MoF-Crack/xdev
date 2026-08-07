// app.go defines the App struct and all data types used for Wails
// frontend-backend communication.
//
// # App Struct
//
// The App struct is the central backend object exposed to the Vue 3 frontend
// via Wails bindings. Every exported method on App becomes callable from
// JavaScript as `window.go.main.App.<Method>()`. The struct holds references
// to the config, plugin registry, manifest catalog, and runtime state.
//
// # Data Types
//
// The following types are used as JSON responses to frontend calls:
//   - ToolInfo: tool metadata for the browse panel
//   - VersionInfo: version entry for the version list
//   - CategoryInfo: category with tool count
//   - InstallProgress: real-time installation progress
//   - DebugInfo: diagnostic information for the settings page
//
// # Method Organization
//
// App methods are spread across multiple files for maintainability:
//   - app.go: core types, startup/shutdown, caching, debug
//   - app_tool_query.go: GetCategories, GetToolsByCategory, etc.
//   - app_install.go: InstallTool, UseVersion, UninstallVersion, etc.
//   - app_local_detect.go: ScanLocalEnvironments, ScanLocalTools
//   - app_migration.go: IsInitialized, InitConfig, MigrateLocalTool, etc.
//   - app_config.go: GetConfig, SetMirrorConfig, GetPathMode, etc.
//   - app_env_vars.go: GetEnvVars, SetEnvVar, DeleteEnvVar, etc.
//   - app_project.go: GetProjectConfig, SetProjectTool, RunProjectTask, etc.
//   - app_favorites.go: GetFavorites, AddFavorite, RemoveFavorite
//   - app_profile.go: ExportProfile, ImportProfile
//
// # Concurrency
//
// The App struct uses a sync.RWMutex for thread-safe access to shared state
// (local tool cache, download state). Long-running operations (install,
// scan) run in goroutines and report results via Wails runtime events.
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/isolator"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// ToolInfo represents a tool's basic information for the frontend.
type ToolInfo struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	DefaultVer   string `json:"defaultVersion"`
	ActiveVer    string `json:"activeVersion"`
	Installed    bool   `json:"installed"`
	LocalVersion string `json:"localVersion"` // detected on system PATH
	LocalPath    string `json:"localPath"`    // path to local binary
	CanInstall   bool   `json:"canInstall"`   // true if xdev can install this (package-manager)
	ToolName     string `json:"toolName"`     // xdev tool name for install (package-manager)
	InstallHint  string `json:"installHint"`  // hint for how to install (package-manager)
	Source       string `json:"source"`       // "xdev" 或 "local"
	// HasUpdate mirrors PackageManagerInfo.HasUpdate for the package-manager
	// category: true only when a strictly newer version is available.
	HasUpdate bool `json:"hasUpdate"`
	// FormFactor 工具形态（gui/cli/tui），来自 manifest（Spec 023）。
	FormFactor []string `json:"formFactor,omitempty"`
	// GUIBinary GUI 主二进制相对路径（form_factor 含 gui 时）。
	GUIBinary string `json:"guiBinary,omitempty"`
	// HasFormSpecificArchives 报告该工具是否有按形态区分的独立 archive
	// （Spec 025）。true 时前端安装流程显示形态选择器，让用户选择下载
	// 哪个形态对应的 archive（如 OpenCode 的 Desktop App vs CLI）。
	// false 时（单包多形态工具如 VS Code）直接安装，不显示选择器。
	HasFormSpecificArchives bool `json:"hasFormSpecificArchives,omitempty"`
	// RequiresAdmin 报告该包管理器安装是否需要管理员权限（Spec 025）。
	// 仅对 script-installable 包管理器（choco）有意义；前端按钮文案
	// 改为「以管理员安装」并附 tooltip。
	RequiresAdmin bool `json:"requiresAdmin,omitempty"`
	// Spec 026: 附属工具动态安装支持（与 PackageManagerInfo 对齐）。
	EnableCommand    string              `json:"enableCommand,omitempty"`
	InstallMethods   []InstallMethodInfo `json:"installMethods,omitempty"`
	// Spec 027: 多实例检测（与 PackageManagerInfo 对齐）。
	Instances []PMInstanceInfo `json:"instances,omitempty"`
}

// VersionInfo represents a version entry for the frontend.
type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	// Intact is true when an installed version passes the integrity
	// check (non-empty dir, all shim binaries present). It is false for
	// incomplete installations. Always true for non-installed versions.
	Intact bool `json:"intact"`
	// Integrity is a human-readable reason when Intact is false.
	Integrity string `json:"integrity,omitempty"`
}

// IntegrityInfo is the result of CheckInstallIntegrity for a single
// installed version.
type IntegrityInfo struct {
	Tool    string   `json:"tool"`
	Version string   `json:"version"`
	Path    string   `json:"path"`
	OK      bool     `json:"ok"`
	Reason  string   `json:"reason"`
	Missing []string `json:"missing"`
}

// CategoryInfo represents a tool category.
type CategoryInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// InstallProgress represents installation progress for the frontend.
type InstallProgress struct {
	TaskID  string  `json:"taskId"`
	Tool    string  `json:"tool"`
	Version string  `json:"version"`
	Phase   string  `json:"phase"`
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed"`
	ETA     string  `json:"eta"`
}

// localInfo caches the result of local tool detection.
type localInfo struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Source  string `json:"source"` // "xdev" 或 "local"
}

// ──────────────────────────────────────────────────────
// Strongly-typed return types (replacing map[string]interface{})
// ──────────────────────────────────────────────────────

// DebugInfo contains debug/diagnostic information about the xdev environment.
type DebugInfo struct {
	XMDevDir      string   `json:"xmdevDir"`
	ManifestDir   string   `json:"manifestDir"`
	CacheDir      string   `json:"cacheDir"`
	RegistryCount int      `json:"registryCount"`
	CatalogCount  int      `json:"catalogCount"`
	RegistryNames []string `json:"registryNames"`
	CatalogNames  []string `json:"catalogNames"`
}

// InstalledToolInfo represents an installed tool with its version info.
type InstalledToolInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Category    string `json:"category"`
}

// DashboardStats contains dashboard statistics for the frontend.
type DashboardStats struct {
	TotalTools     int                 `json:"totalTools"`
	InstalledCount int                 `json:"installedCount"`
	InstallSize    int64               `json:"installSize"`
	CacheSize      int64               `json:"cacheSize"`
	ShimCount      int                 `json:"shimCount"`
	XMDevDir       string              `json:"xmdevDir"`
	InstallDir     string              `json:"installDir"`
	ShimDir        string              `json:"shimDir"`
	CacheDir       string              `json:"cacheDir"`
	DownloadDir    string              `json:"downloadDir"`
	InstalledTools []InstalledToolInfo `json:"installedTools"`
}

// UpdateCheckResult contains the result of checking for updates.
type UpdateCheckResult struct {
	HasUpdate bool   `json:"hasUpdate"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Error     string `json:"error,omitempty"`
}

// DiagnosticResult represents a single diagnostic check result.
type DiagnosticResult struct {
	Check string `json:"check"`
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ConfigInfo contains the current configuration for the frontend.
type ConfigInfo struct {
	XMDevDir    string    `json:"xmdevDir"`
	InstallDir  string    `json:"installDir"`
	ShimDir     string    `json:"shimDir"`
	CacheDir    string    `json:"cacheDir"`
	DownloadDir string    `json:"downloadDir"`
	PathMode    string    `json:"pathMode"`
	Proxy       ProxyInfo `json:"proxy"`
}

// ProxyInfo contains proxy configuration details.
type ProxyInfo struct {
	HTTP    string `json:"http"`
	HTTPS   string `json:"https"`
	NoProxy string `json:"noProxy"`
	Enabled bool   `json:"enabled"`
}

// ProxyTestResult contains the result of testing a proxy connection.
type ProxyTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Latency int64  `json:"latency"`
}

// ProxyProbeResult is the outcome of probing a candidate local proxy port.
type ProxyProbeResult struct {
	URL       string `json:"url"`       // candidate proxy URL, e.g. http://127.0.0.1:7897
	Port      int    `json:"port"`      // probed port
	OK        bool   `json:"ok"`        // true when the port proxies HTTPS successfully
	LatencyMs int64  `json:"latencyMs"` // HEAD latency through the proxy (0 when port closed)
	Message   string `json:"message"`   // human-readable outcome
}

// ImportResult contains the result of importing a profile.
type ImportResult struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Installed int      `json:"installed"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
}

// MigrationResult represents the result of migrating a single local tool.
type MigrationResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "success", "failed", "skipped"
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// LocalToolInfo represents a locally installed tool detected on the system.
type LocalToolInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	Managed     bool   `json:"managed"`       // true if also managed by xdev
	ActiveVer   string `json:"activeVersion"` // xdev active version (if managed)
}

// PackageManagerInfo represents a package manager tool.
type PackageManagerInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version"`
	LocalPath   string `json:"localPath"`
	CanInstall  bool   `json:"canInstall"`  // true if xdev can install this
	ToolName    string `json:"toolName"`    // xdev tool name for install (e.g. "uv")
	InstallHint string `json:"installHint"` // hint for how to install (e.g. "Install Node.js")
	// Spec 025: script install 支持。RequiresAdmin 为 true 时，前端按钮文案
	// 改为「以管理员安装」并提示用户。
	RequiresAdmin bool `json:"requiresAdmin"`
	// Spec 026: 附属工具动态安装支持。
	// EnableCommand 非空时，该工具是"可启用附属工具"（如 yarn/pnpm 通过
	// corepack enable 启用）。前端显示「启用」按钮，点击后调 InstallSubTool。
	EnableCommand string `json:"enableCommand,omitempty"`
	// InstallMethods 列出可用的安装方式（如 pipx 可通过 pip 或 uv 安装）。
	// 长度 > 1 时前端点「安装」弹选项对话框；长度 == 1 时直接用唯一方法。
	InstallMethods []InstallMethodInfo `json:"installMethods,omitempty"`
	// HasUpdate is true only when an installed, xdev-managed package
	// manager has a strictly newer version available (semver compare).
	// The frontend shows the "Update" button only when this is true.
	HasUpdate bool `json:"hasUpdate"`
	// Spec 027: 多实例检测——列出所有检测到的实例（xdev 管理的 + 系统安装的）。
	// 前端详情抽屉显示为多版本列表，非 xdev 管理实例旁显示「迁移」按钮。
	Instances []PMInstanceInfo `json:"instances,omitempty"`
}

// PMInstanceInfo 描述包管理器的一个检测实例（Spec 027）。
type PMInstanceInfo struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Managed bool   `json:"managed"` // true = xdev 管理的，false = 系统安装的
	Source  string `json:"source"`  // "xdev" 或 "local" 或 "system"
}

// InstallMethodInfo 描述附属工具的一种安装方式（Spec 026），供前端展示。
type InstallMethodInfo struct {
	Name    string `json:"name"`    // 方法标识（如 "pip" / "uv"）
	Command string `json:"command"` // 执行命令（如 "pip install pipx"）
}

// DownloadTaskInfo 前端展示用的下载任务信息
type DownloadTaskInfo struct {
	ID           string  `json:"id"`
	Tool         string  `json:"tool"`
	Version      string  `json:"version"`
	Status       string  `json:"status"`
	Progress     float64 `json:"progress"`
	Speed        string  `json:"speed"`
	ETA          string  `json:"eta"`
	Downloaded   int64   `json:"downloaded"`
	Total        int64   `json:"total"`
	ErrorMessage string  `json:"errorMessage,omitempty"`
}

// CacheBreakdown 缓存分类统计信息
type CacheBreakdown struct {
	Archives   int64 `json:"archives"`   // 下载的压缩包大小
	ShaFiles   int64 `json:"shaFiles"`   // .sha256 文件大小
	PartFiles  int64 `json:"partFiles"`  // .part 临时文件大小
	ScanCache  int64 `json:"scanCache"`  // tool_scan_cache.json 大小
	OtherFiles int64 `json:"otherFiles"` // 其他文件大小
	TotalCount int   `json:"totalCount"` // 文件总数
}

// AboutInfo 关于页面信息
type AboutInfo struct {
	AppName      string `json:"appName"`
	Version      string `json:"version"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	WailsVersion string `json:"wailsVersion"`
	Platform     string `json:"platform"`
	Arch         string `json:"arch"`
	RepoURL      string `json:"repoURL"`
	License      string `json:"license"`
}

// regMu protects Windows registry operations (setWindowsEnvVar,
// deleteWindowsEnvVar) from concurrent access during parallel migrations.
var regMu sync.Mutex

// App is the Wails backend struct that exposes methods to the frontend.
type App struct {
	ctx      context.Context
	cfg      *config.Config
	cfgMu    sync.RWMutex // protects cfg for concurrent read/write
	cat      *manifest.Catalog
	registry *plugin.Registry
	localMu  sync.RWMutex
	localMap map[string]localInfo // cached local tool detection results
	dlMgr    *downloader.Manager  // 下载管理器

	// rootCtx 是应用级根上下文（F-20 / D18）：在 startup 创建、shutdown
	// 取消。所有后台任务（安装轮询、延迟刷新等）应派生自它，以便应用
	// 退出时立即停止，而不是继续空转（如 1 小时的安装轮询）。
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// Dashboard stats cache
	dashMu         sync.RWMutex
	dashCache      *DashboardStats
	dashExpire     time.Time
	dashRefreshing int32 // atomic: 1 = refresh in progress, prevents concurrent storms

	// Size breakdown caches (Spec 019) — TTL'd so repeated drawer opens
	// don't rescan the (potentially multi-GB) install/cache trees on
	// every call. Cleared via InvalidateSizeBreakdown after
	// install/uninstall/use/cache-clean operations.
	sizeBreakdownMu  sync.Mutex
	installSizeCache []DiskUsageEntry
	installSizeExp   time.Time
	cacheSizeCache   []DiskUsageEntry
	cacheSizeExp     time.Time

	// Channel for signaling that refreshLocalCache has completed
	localReady     chan struct{}
	localReadyOnce sync.Once

	// cancelRefresh is used to cancel the delayed refreshLocalCache goroutine
	cancelRefresh context.CancelFunc

	// Favorites
	favMu     sync.RWMutex
	favorites []FavoriteInfo

	// Ignored tools
	ignored   []IgnoredToolInfo
	ignoredMu sync.RWMutex

	// Tool list cache (per category, TTL) — avoids re-spawning subprocesses
	// for version detection on every category switch (编程语言/包管理器).
	// Cleared via InvalidateToolsCache after install/uninstall/use.
	toolsCacheMu   sync.Mutex
	toolsCache     map[string]toolsCacheEntry
	toolsCacheOnce sync.Once

	// Isolation
	isoMu       sync.Mutex
	isoMode     isolator.IsolationMode
	isoInstance isolator.Isolator

	// Spec 025: pendingFormFilter 用于 InstallToolWithForm 传递形态筛选
	// 到 InstallToolWithOptions 的 archive 选择点。非空时 ArchiveForWithForm
	// 优先匹配该形态的 archive。每次安装完成后清空（一次性使用）。
	pendingFormFilter   string
	pendingFormFilterMu sync.Mutex
}

// toolsCacheEntry 缓存某分类的工具列表结果。
type toolsCacheEntry struct {
	Entries []ToolInfo
	Expire  time.Time
}

// toolScanCacheFile 返回工具扫描缓存文件路径
func toolScanCacheFile() string {
	cfg, _ := config.Load("")
	return filepath.Join(cfg.Core.CacheDir, "tool_scan_cache.json")
}

// NewApp creates a new App instance.
func NewApp(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry) *App {
	return &App{
		cfg:        cfg,
		cat:        cat,
		registry:   reg,
		localMap:   make(map[string]localInfo),
		dlMgr:      downloader.NewManager(),
		localReady: make(chan struct{}),
		favorites:  []FavoriteInfo{},
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// 创建应用级根上下文（F-20 / D18）：shutdown 时取消，所有后台任务
	// 由此派生以便退出时立即停止。
	a.rootCtx, a.rootCancel = context.WithCancel(context.Background())
	logInfo("[startup] Wails OnStartup callback fired")

	// Configure HTTP client proxy from config (fixes sniff failing behind proxy).
	cfg := a.getCfg()
	if cfg != nil {
		remote.ConfigureHTTPClient(cfg.GetProxyURL("https"))
		// Configure GitHub API token from config file (env GITHUB_TOKEN/GH_TOKEN
		// is read by effectiveGitHubToken() automatically each request).
		if cfg.Sniff.GitHubToken != "" {
			remote.ConfigureGitHubToken(cfg.Sniff.GitHubToken)
		}
	}

	// Log token status (without exposing the token value).
	if remote.GitHubTokenConfigured() {
		logInfo("[startup] GitHub API token: configured")
	} else {
		logInfo("[startup] GitHub API token: not configured — rate limit ~60 req/h")
	}

	// Spec 020: 启动兜底清理过期归档（7d/30d 保留策略）。
	if removed := a.CleanupExpiredArchives(); removed > 0 {
		logInfo("[startup] cleaned up %d expired archive file(s)", removed)
	}
}

// domReady is called when the DOM is ready.
func (a *App) domReady(ctx context.Context) {
	logInfo("[domReady] Wails OnDomReady callback fired — calling WindowShow()")
	wailsRuntime.WindowShow(ctx)
	logInfo("[domReady] WindowShow() completed, window should now be visible")

	// 先尝试从缓存加载
	a.loadLocalCache()
	a.loadFavorites()
	a.loadIgnored()

	// 自动检测并修复主目录下缺失的 xdev/xdev-gui 二进制
	go func() {
		select {
		case <-time.After(1 * time.Second):
			a.EnsureSelfInstalled()
		case <-a.rootContext().Done():
		}
	}()

	// 自动修复版本号为 "local" 的已迁移工具
	go func() {
		select {
		case <-time.After(2 * time.Second):
			if fixed := a.FixLocalVersions(); len(fixed) > 0 {
				logInfo("[domReady] fixed %d local version(s)", len(fixed))
			}
		case <-a.rootContext().Done():
		}
	}()

	// 使用可取消的 context，在 shutdown 时取消延迟 goroutine
	refreshCtx, cancel := context.WithCancel(context.Background())
	a.cancelRefresh = cancel
	go func() {
		logInfo("[domReady] scheduling refreshLocalCache in 15 seconds")
		select {
		case <-time.After(15 * time.Second):
			logInfo("[domReady] starting delayed refreshLocalCache")
			a.refreshLocalCache()
			logInfo("[domReady] refreshLocalCache completed")
		case <-refreshCtx.Done():
			logInfo("[domReady] delayed refreshLocalCache cancelled")
		}
	}()
}

// shutdown 在应用退出时清理资源
func (a *App) shutdown(ctx context.Context) {
	logInfo("[shutdown] 取消应用根上下文（停止所有后台任务）...")
	if a.rootCancel != nil {
		a.rootCancel()
	}
	logInfo("[shutdown] 取消延迟刷新任务...")
	if a.cancelRefresh != nil {
		a.cancelRefresh()
	}
	logInfo("[shutdown] 清理下载任务...")
	if a.dlMgr != nil {
		a.dlMgr.CleanupOnExit()
	}
	logInfo("[shutdown] 清理完成")
}

// saveLocalCache 将本地工具扫描结果保存到缓存文件
func (a *App) saveLocalCache() {
	cfg := a.getCfg()
	os.MkdirAll(cfg.Core.CacheDir, 0o755)

	a.localMu.RLock()
	data, err := json.Marshal(a.localMap)
	a.localMu.RUnlock()

	if err != nil {
		logWarn("[saveLocalCache] 序列化缓存失败: %v", err)
		return
	}
	cachePath := filepath.Join(cfg.Core.CacheDir, "tool_scan_cache.json")
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		logWarn("[saveLocalCache] 写入缓存文件失败: %v", err)
	} else {
		logInfo("[saveLocalCache] 缓存已保存到 %s", cachePath)
	}
}

// loadLocalCache 从缓存文件加载本地工具扫描结果
func (a *App) loadLocalCache() bool {
	var cached map[string]localInfo
	if err := a.loadJSONFromCache("tool_scan_cache.json", &cached); err != nil {
		return false
	}

	a.localMu.Lock()
	if a.localMap == nil {
		a.localMap = make(map[string]localInfo)
	}
	for k, v := range cached {
		a.localMap[k] = v
	}
	a.localMu.Unlock()

	logInfo("[loadLocalCache] 从缓存加载了 %d 个工具信息", len(cached))
	return true
}

// refreshLocalCache scans all tools and caches local detection results.
func (a *App) refreshLocalCache() {
	tools := a.registry.All()
	newMap := make(map[string]localInfo, len(tools))
	start := time.Now()
	logInfo("[refreshLocalCache] scanning %d tools for local installations", len(tools))

	found := 0
	for _, t := range tools {
		// 1. Check xdev-managed installation first.
		if activeVer, installed := a.currentVersion(t.Name()); installed {
			link := a.currentLink(t.Name())
			if target, err := os.Readlink(link); err == nil {
				newMap[t.Name()] = localInfo{Version: activeVer, Path: target, Source: "xdev"}
				found++
				logDebug("[refreshLocalCache] xdev %s v%s at %s", t.Name(), activeVer, target)
				continue
			}
		}

		// 2. Check system-local installation.
		path := detectLocalToolPath(t.Name(), t.Shims())
		if path != "" {
			newMap[t.Name()] = localInfo{Version: "", Path: path, Source: "local"}
			found++
			logDebug("[refreshLocalCache] local %s at %s", t.Name(), path)
		}
	}

	a.localMu.Lock()
	a.localMap = newMap
	a.localMu.Unlock()
	a.saveLocalCache() // 保存缓存到文件
	logInfo("[refreshLocalCache] done in %v: found %d/%d tools locally", time.Since(start), found, len(tools))

	// Signal that the local cache has been populated
	a.localReadyOnce.Do(func() {
		close(a.localReady)
	})
}

// waitForLocalCache waits until the local cache has been populated
// (at least once) or a short timeout expires.
//
// 优化（工具列表加载提速）：原实现最长阻塞 5 秒，而 domReady 延迟
// 15 秒才触发 refreshLocalCache——用户在这期间打开「编程语言/包管理器」
// 页面会白等 5 秒。改为快速路径：localMap 未填充时最多等 500ms，
// 未就绪则直接返回（localVersion/localPath 暂缺，由后台刷新补充，
// 前端 refreshTools 可手动刷新）。
func (a *App) waitForLocalCache() {
	// Quick check: already populated?
	a.localMu.RLock()
	populated := len(a.localMap) > 0
	a.localMu.RUnlock()
	if populated {
		return
	}
	// Wait for refreshLocalCache to complete, up to 500ms.
	select {
	case <-a.localReady:
		return
	case <-time.After(500 * time.Millisecond):
		return
	}
}

// refreshAndSaveLocalCache refreshes the local cache and saves it to disk.
func (a *App) refreshAndSaveLocalCache() {
	a.refreshLocalCache()
	a.saveLocalCache()
}

// mustGetPlugin returns the plugin for the given tool name, or an error if not found.
func (a *App) mustGetPlugin(toolName string) (plugin.Plugin, error) {
	p, ok := a.registry.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("tool %q not found", toolName)
	}
	return p, nil
}

// newInstaller creates a new Installer using the app's registry.
func (a *App) newInstaller(cfg *config.Config) *installer.Installer {
	return installer.New(cfg, a.registry)
}

// currentLink returns the path to the current/<toolName> symlink.
func (a *App) currentLink(toolName string) string {
	cfg := a.getCfg()
	return filepath.Join(cfg.Core.XMDevDir, "current", toolName)
}

// saveJSONToCache marshals v as JSON and writes it to a file in the cache directory.
func (a *App) saveJSONToCache(filename string, v any) error {
	cfg := a.getCfg()
	path := filepath.Join(cfg.Core.CacheDir, filename)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// loadJSONFromCache reads and unmarshals a JSON file from the cache directory into v.
func (a *App) loadJSONFromCache(filename string, v any) error {
	cfg := a.getCfg()
	path := filepath.Join(cfg.Core.CacheDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// getRegistry returns the plugin registry.
func (a *App) getRegistry() *plugin.Registry {
	return a.registry
}

// getCfg 返回当前配置的深拷贝快照（F-19 / D17）。
//
// 访问模型（已评审确定）：
//   - 读取方：一律使用 getCfg() 获得快照，读锁在快照复制完成即释放，
//     之后对嵌套字段（Proxy/Sniff/Mirror/CustomTools 等）的读取不再
//     受其他协程并发修改影响（不存在锁外读可变指针的竞态）。
//   - 写入方：必须在 cfgMu 写锁内操作实时配置 —— 使用 mutateCfg，
//     或（如 InitConfig / SetMirrorConfig）直接在 a.cfgMu.Lock 段内
//     修改 a.cfg。禁止通过 getCfg() 的结果修改配置（快照修改无效）。
func (a *App) getCfg() *config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.Clone()
}

// mutateCfg 在 cfgMu 写锁内对实时配置执行 fn，用于需要调用 Config
// 修改方法或直接改字段后保存的写入方（F-19 / D17）。
func (a *App) mutateCfg(fn func(cfg *config.Config) error) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return fn(a.cfg)
}

// setCfg updates the config with write lock protection.
func (a *App) setCfg(cfg *config.Config) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg = cfg
}

// rootContext 返回应用根上下文（F-20 / D18）；未初始化（如单元测试或
// 未走 startup 的调用）时退回 context.Background()，保证可安全调用。
func (a *App) rootContext() context.Context {
	if a.rootCtx != nil {
		return a.rootCtx
	}
	return context.Background()
}

// getLocalCached returns cached local info for a tool.
func (a *App) getLocalCached(name string) (string, string, string) {
	a.localMu.RLock()
	info, ok := a.localMap[name]
	a.localMu.RUnlock()
	if !ok {
		return "", "", ""
	}
	return info.Version, info.Path, info.Source
}

// currentVersion reads the current/<tool> symlink to determine the
// active version. Returns ("", false) when no active version exists.
func (a *App) currentVersion(toolName string) (string, bool) {
	return util.ActiveVersion(a.getCfg().Core.XMDevDir, toolName)
}

// installedVersions lists version directories under install/<tool>.
func (a *App) installedVersions(toolName string) []string {
	cfg := a.getCfg()
	dir := filepath.Join(cfg.Core.InstallDir, toolName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// logInfo/logWarn/logError are defined in log.go

// GetAboutInfo 返回关于页面信息
func (a *App) GetAboutInfo() AboutInfo {
	return AboutInfo{
		AppName:      "XDev",
		Version:      version.Get(),
		BuildDate:    version.BuildDate,
		GoVersion:    runtime.Version(),
		WailsVersion: "v2.12.0",
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		RepoURL:      "https://github.com/xmdemt/xdev",
		License:      "MIT",
	}
}

// EnsureSelfInstalled checks if xdev CLI and GUI binaries exist in the
// XDev home directory (e.g., X:\XDev\). If missing, it copies them from
// the current executable's location. The install/ directory is reserved
// for xdev self-management (version switching, global PATH via junction).
func (a *App) EnsureSelfInstalled() {
	cfg := a.getCfg()
	if cfg == nil || cfg.Core.XMDevDir == "" {
		return
	}

	homeDir := cfg.Core.XMDevDir

	// Determine expected binary names
	cliName := "xdev"
	guiName := "xdev-gui"
	if runtime.GOOS == "windows" {
		cliName = "xdev.exe"
		guiName = "xdev-gui.exe"
	}

	cliPath := filepath.Join(homeDir, cliName)
	guiPath := filepath.Join(homeDir, guiName)

	cliMissing := false
	guiMissing := false

	if _, err := os.Stat(cliPath); err != nil {
		cliMissing = true
	}
	if _, err := os.Stat(guiPath); err != nil {
		guiMissing = true
	}

	if !cliMissing && !guiMissing {
		logInfo("[EnsureSelfInstalled] xdev binaries in home dir are intact")
		return
	}

	logInfo("[EnsureSelfInstalled] detected missing binaries in home dir: cli=%v gui=%v, repairing...",
		cliMissing, guiMissing)

	// Get current executable path as source
	exe, err := os.Executable()
	if err != nil {
		logWarn("[EnsureSelfInstalled] failed to get current exe path: %v", err)
		return
	}
	exeDir := filepath.Dir(exe)

	// Copy xdev.exe to home directory
	if cliMissing {
		srcCli := filepath.Join(exeDir, "xdev.exe")
		if runtime.GOOS != "windows" {
			srcCli = filepath.Join(exeDir, "xdev")
		}
		// If the current exe IS xdev (not xdev-gui), use it directly
		if filepath.Base(exe) == cliName {
			srcCli = exe
		}
		if _, err := os.Stat(srcCli); err == nil {
			if err := util.CopyFile(srcCli, cliPath); err != nil {
				logWarn("[EnsureSelfInstalled] failed to copy %s: %v", cliName, err)
			} else {
				logInfo("[EnsureSelfInstalled] copied %s to %s", cliName, homeDir)
			}
		} else {
			logWarn("[EnsureSelfInstalled] source %s not found at %s", cliName, srcCli)
		}
	}

	// Copy xdev-gui.exe to home directory
	if guiMissing {
		srcGui := filepath.Join(exeDir, guiName)
		if _, err := os.Stat(srcGui); err == nil {
			if err := util.CopyFile(srcGui, guiPath); err != nil {
				logWarn("[EnsureSelfInstalled] failed to copy %s: %v", guiName, err)
			} else {
				logInfo("[EnsureSelfInstalled] copied %s to %s", guiName, homeDir)
			}
		} else {
			logWarn("[EnsureSelfInstalled] source %s not found at %s", guiName, srcGui)
		}
	}

	// Also ensure xdev is installed in install/ directory for self-management
	installDir := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version)
	installCli := filepath.Join(installDir, cliName)
	if _, err := os.Stat(installCli); err != nil {
		logInfo("[EnsureSelfInstalled] also installing to install/ directory for self-management")
		if _, _, err := installer.InstallSelf(cfg); err != nil {
			logWarn("[EnsureSelfInstalled] failed to install to install/ directory: %v", err)
		}
	}
}
