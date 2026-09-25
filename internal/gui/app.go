// app.go defines the App struct and all data types used for Wails
// frontend-backend communication.
//
// # App Struct
//
// App is the Wails desktop adapter (Spec 029 B2). Exported methods stay
// on App so Wails keeps generating the same JS bindings; the
// implementation is grouped into four modules:
//   - catalog query  — app_tool_query.go
//   - install session — app_install.go (InstallSpec)
//   - host scan      — internal/hostscan + app_local_detect.go
//   - prefs / env    — app_config.go, app_env_vars.go, app_profile.go
//
// Every exported method on App becomes callable from JavaScript as
// `window.go.gui.App.<Method>()`. The struct holds references to the
// config, plugin registry, manifest catalog, and runtime state.
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
//   - app_tool_query.go: catalog query (GetCategories, GetToolsByCategory)
//   - app_install.go: install session (InstallSpec + Wails wrappers)
//   - app_local_detect.go: host scan adapter (ScanLocalEnvironments, PMs)
//   - app_migration.go: IsInitialized, InitConfig, MigrateLocalTool, etc.
//   - app_config.go: prefs (GetConfig, SetMirrorConfig, GetPathMode)
//   - app_env_vars.go: prefs (GetEnvVars, SetEnvVar, DeleteEnvVar)
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
	"github.com/xmdemt/xdev/internal/hostscan"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/isolator"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/update"
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
	// InstallCommand 官方安装脚本（scoop/brew/choco），供确认对话框展示。
	InstallCommand string `json:"installCommand,omitempty"`
	// AvailableForms 该工具 archive 上实际标注过的形态（去重），供形态选择器
	// 使用。与 FormFactor 声明不同：OpenCode 声明了 gui/cli/tui，但 archive
	// 目前只有 cli，选择器应只列出 cli。
	AvailableForms []string `json:"availableForms,omitempty"`
	// Spec 026: 附属工具动态安装支持（与 PackageManagerInfo 对齐）。
	EnableCommand  string              `json:"enableCommand,omitempty"`
	InstallMethods []InstallMethodInfo `json:"installMethods,omitempty"`
	// OfferSubToolInstall 为 true 时，即使 Installed=true 也显示启用/安装
	// （仅 yarn/pnpm：PATH 上有系统副本，但 xdev Node 尚未启用）。
	OfferSubToolInstall bool `json:"offerSubToolInstall,omitempty"`
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

// InstallPreview is a cheap preflight for the install button. The
// frontend uses it to decide whether to show a confirmation dialog
// without scanning every category or re-parsing the whole catalog.
type InstallPreview struct {
	Tool                    string        `json:"tool"`
	Version                 string        `json:"version"`
	Unverified              bool          `json:"unverified"`
	HasFormSpecificArchives bool          `json:"hasFormSpecificArchives"`
	AvailableForms          []string      `json:"availableForms,omitempty"`
	Integrity               IntegrityInfo `json:"integrity"`
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
	RequiresAdmin  bool   `json:"requiresAdmin"`
	InstallCommand string `json:"installCommand,omitempty"`
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
	// OfferSubToolInstall 为 true 时，即使 Installed=true 也显示启用/安装
	// （仅 yarn/pnpm：PATH 上有系统副本，但 xdev Node 尚未启用）。
	OfferSubToolInstall bool `json:"offerSubToolInstall,omitempty"`
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

// App is the Wails desktop adapter: a thin exported facade over catalog
// query, install session, host scan, and prefs. New Wails bindings are
// not added in Spec 029; existing wrappers forward into those modules.
type App struct {
	ctx      context.Context
	cfg      *config.Config
	cfgMu    sync.RWMutex // protects cfg for concurrent read/write
	catMu    sync.RWMutex // protects cat for concurrent read/write (reloadCatalog)
	cat      *manifest.Catalog
	registry *plugin.Registry
	localMu  sync.RWMutex
	localMap map[string]localInfo // cached local tool detection results
	// cacheWriteMu 串行化 tool_scan_cache.json 的落盘（票 22 / P1-16 / D-08）。
	// GUI 包有 15 个 saveLocalCache 调用点分布在 7 个文件，且批量迁移是并发
	// 执行的（MigrateAllLocalTools 最多 4 路并发 worker），并发写会让
	// util.AtomicWriteFile 的固定 "<path>.tmp" 临时文件互相截断。用互斥锁
	// 让写请求排队（不是 TryLock 跳过），保证每次调用都真正落盘。
	cacheWriteMu sync.Mutex
	dlMgr        *downloader.Manager // 下载管理器

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
}

// toolsCacheEntry 缓存某分类的工具列表结果。
type toolsCacheEntry struct {
	Entries []ToolInfo
	Expire  time.Time
}

// signalLocalReady 关闭 localReady，让 waitForLocalCache 不再空等。
// loadLocalCache 命中磁盘缓存、以及 refreshLocalCache 完成时都要调用。
func (a *App) signalLocalReady() {
	a.localReadyOnce.Do(func() {
		close(a.localReady)
	})
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

	// Spec 034: 启动 2s 后按配置自检更新（异步、失败静默、24h 限频）。
	go func() {
		select {
		case <-time.After(2 * time.Second):
			a.runStartupSelfCheck()
		case <-a.rootContext().Done():
		}
	}()
}

// domReady is called when the DOM is ready.
func (a *App) domReady(ctx context.Context) {
	logInfo("[domReady] Wails OnDomReady callback fired — calling WindowShow()")
	wailsRuntime.WindowShow(ctx)
	logInfo("[domReady] WindowShow() completed, window should now be visible")

	// 先尝试从缓存加载；命中后立刻 signalLocalReady，避免
	// waitForLocalCache 在 refresh 完成前白等 500ms。
	if a.loadLocalCache() {
		a.signalLocalReady()
	}
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

	// 后台立即刷新本地探测（不再等 15s）。shutdown 取消。
	refreshCtx, cancel := context.WithCancel(a.rootContext())
	a.cancelRefresh = cancel
	go func() {
		select {
		case <-refreshCtx.Done():
			return
		default:
		}
		logInfo("[domReady] starting refreshLocalCache")
		a.refreshLocalCache()
		logInfo("[domReady] refreshLocalCache completed")
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

// saveLocalCache 将本地工具扫描结果保存到缓存文件。
//
// 票 22（P1-16 / D-08）：改为「临时文件 + 原子重命名」写入（util.AtomicWriteFile），
// 并用 cacheWriteMu 串行化并发写。修复前用 os.WriteFile 直接截断写目标文件，
// 并发调用（批量迁移 4 路 worker + 刷新路径）或写中途崩溃会留下空/半截 JSON，
// 下次启动 loadLocalCache 解析失败 → 本地工具状态整体丢失。
// 并发读方（其他进程 / 排障工具）在原子重命名下只会看到「旧的完整内容」或
// 「新的完整内容」，不会看到撕裂状态。
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

	// 排队写（票 22）：所有调用点共用同一把锁，后到者等待而非丢弃。
	a.cacheWriteMu.Lock()
	defer a.cacheWriteMu.Unlock()
	if err := util.AtomicWriteFile(cachePath, data, 0o644); err != nil {
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
	if a.registry == nil {
		a.signalLocalReady()
		return
	}
	tools := a.registry.All()
	newMap := make(map[string]localInfo, len(tools))
	cfg := a.getCfg()
	shimDir := cfg.Core.ShimDir
	start := time.Now()
	logInfo("[refreshLocalCache] scanning %d tools for local installations", len(tools))

	var names []string
	for _, t := range tools {
		if activeVer, installed := a.currentVersion(t.Name()); installed {
			link := a.currentLink(t.Name())
			if target, err := os.Readlink(link); err == nil {
				newMap[t.Name()] = localInfo{Version: activeVer, Path: target, Source: "xdev"}
				continue
			}
		}
		search := t.Shims()
		if len(search) == 0 {
			search = []string{t.Name()}
		}
		names = append(names, search...)
	}
	byBase := hostscan.IndexByBasename(hostscan.Scan(hostscan.Query{Names: names, ShimDir: shimDir}))

	found := len(newMap)
	for _, t := range tools {
		if _, ok := newMap[t.Name()]; ok {
			continue
		}
		path := detectLocalToolPathIndexed(t.Name(), t.Shims(), shimDir, byBase)
		if path != "" {
			newMap[t.Name()] = localInfo{Version: "", Path: path, Source: "local"}
			found++
		}
	}

	a.localMu.Lock()
	a.localMap = newMap
	a.localMu.Unlock()
	a.saveLocalCache()
	logInfo("[refreshLocalCache] done in %v: found %d/%d tools locally", time.Since(start), found, len(tools))
	a.signalLocalReady()
	a.InvalidateToolsCache()
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "localCacheUpdated", len(newMap))
	}
}

// waitForLocalCache waits until the local cache has been populated
// (at least once) or a short timeout expires.
//
// 优化（工具列表加载提速）：localMap 已有缓存则立即返回；否则最多等
// 500ms 等 refreshLocalCache / loadLocalCache 的 signalLocalReady。
// 未就绪则直接返回（localVersion/localPath 暂缺，由后台刷新补充）。
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

// localCacheRefreshHook 是票 22 守卫测试的计数 seam：非 nil 时在每次
// refreshAndSaveLocalCache 完成后被调用一次。生产代码从不赋值（恒为 nil），
// 因此不改变任何生产行为；测试用它断言「批量迁移每批只刷新一次、
// 单工具迁移刷一次」的契约（TestMigrateAllLocalTools_RefreshOncePerBatch）。
var localCacheRefreshHook func()

// refreshAndSaveLocalCache refreshes the local cache and saves it to disk.
//
// 票 22：这是「刷新」的唯一语义单元。批量迁移（MigrateAllLocalTools）在
// 全部 worker 结束后调用一次，单工具迁移在入口（MigrateLocalToolByJunction /
// MigrateLocalToolByMove）调用一次——worker（migrateLocalToolByJunction /
// migrateLocalToolByMove）内部不再刷新，避免 N 个工具触发 N 次全盘扫描。
func (a *App) refreshAndSaveLocalCache() {
	a.refreshLocalCache()
	a.saveLocalCache()
	if localCacheRefreshHook != nil {
		localCacheRefreshHook()
	}
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
	if err := os.MkdirAll(cfg.Core.CacheDir, 0o755); err != nil {
		return err
	}
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

// getCat 返回当前 catalog 指针（票 24 / P1-16 / P2-07）。
//
// 读方一律经由此函数取 cat：reloadCatalog 会在 catMu 写锁内整体替换
// a.cat（嗅探合并后 SetManifest + 换指针），裸读 a.cat 会与之竞争。
// 必须用 RLock（读锁），不能用 Lock——写路径只在 reloadCatalog 中。
// 返回值可能为 nil（catalog 尚未加载 / 加载失败），调用方必须自行
// 做 nil 判断。
func (a *App) getCat() *manifest.Catalog {
	a.catMu.RLock()
	defer a.catMu.RUnlock()
	return a.cat
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
		RepoURL:      "https://github.com/MoF-Crack/xdev",
		License:      "MIT",
	}
}

// EnsureSelfInstalled checks if xdev CLI and GUI binaries exist in the
// XDev home directory (e.g., X:\XDev\). If missing, it copies them from
// the current executable's location. The install/ directory is reserved
// for xdev self-management (version switching, global PATH via junction).
//
// 单二进制模型（Spec 034+ 审计）：homeDir 只维护 xdev 一个副本
// （GUI/TUI/CLI 同一可执行文件），不再复制独立 xdev-gui。
func (a *App) EnsureSelfInstalled() {
	cfg := a.getCfg()
	if cfg == nil || cfg.Core.XMDevDir == "" {
		return
	}

	homeDir := cfg.Core.XMDevDir

	// Determine expected binary names（单二进制：仅 xdev）
	cliName := "xdev"
	if runtime.GOOS == "windows" {
		cliName = "xdev.exe"
	}

	cliPath := filepath.Join(homeDir, cliName)

	cliMissing := false
	if _, err := os.Stat(cliPath); err != nil {
		cliMissing = true
	}

	if !cliMissing {
		// Spec 034: stale 副本替换（副本与当前运行二进制不一致 → 用源覆盖，
		// 镜像一致性语义；冒烟反馈 #5：旧版 1.2.2-alpha 副本因版本号断层
		// 被 CompareSemver 误判「更新」而永不替换）。开关 self_update_stale=false 时跳过。
		if cfg.Core.SelfUpdateStale {
			if src, ok := selfBinarySource(); ok {
				if err := update.ReplaceStaleCLICopy(src, homeDir); err != nil {
					logWarn("[EnsureSelfInstalled] stale replace failed: %v", err)
				} else {
					logInfo("[EnsureSelfInstalled] stale CLI copy check done")
				}
			}
		}
		a.removeObsoleteGUICopy(homeDir)
		a.ensureCurrentSelfInstall(cfg)
		a.refreshShimDispatchers(cfg)
		return
	}

	logInfo("[EnsureSelfInstalled] detected missing binary in home dir: cli=%v, repairing...", cliMissing)

	// Copy xdev.exe to home directory
	if src, ok := selfBinarySource(); ok {
		if err := util.CopyFile(src, cliPath); err != nil {
			logWarn("[EnsureSelfInstalled] failed to copy %s: %v", cliName, err)
		} else {
			logInfo("[EnsureSelfInstalled] copied %s to %s", cliName, homeDir)
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
	a.refreshShimDispatchers(cfg)
}

// ensureCurrentSelfInstall 确保当前版本始终经 install/xdev/<version>/ +
// current/xdev junction 管理（冒烟反馈 #6：junction 长期指向 1.2.2-alpha
// 目录（内含 0.2.0 二进制）——PATH 解析 `xdev` 命中旧版，而 InstallSelf
// 此前只在根副本缺失时才被调用，永不更新）。InstallSelf 幂等：同版本
// 同二进制为 no-op；新版本创建目录并把 junction 切到新版。
func (a *App) ensureCurrentSelfInstall(cfg *config.Config) {
	installCli := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version, "xdev.exe")
	if runtime.GOOS != "windows" {
		installCli = filepath.Join(cfg.Core.InstallDir, "xdev", version.Version, "xdev")
	}
	if _, err := os.Stat(installCli); err == nil {
		return // 当前版本已安装（junction 由 InstallSelf 创建/更新过）
	}
	logInfo("[EnsureSelfInstalled] current version %s not under install/xdev — installing", version.Version)
	if _, _, err := installer.InstallSelf(cfg); err != nil {
		logWarn("[EnsureSelfInstalled] failed to install current version: %v", err)
	}
}

// removeObsoleteGUICopy 清理 homeDir 根的双二进制时代残留 xdev-gui 副本
// （冒烟反馈 #5：E:\XDev\xdev-gui.exe 自 7/10 起无人替换也无人清理）。
// 单二进制模型（Spec 034+）后该文件无任何调用方——PATH 查找按 xdev 名，
// `xdev gui` 子命令进程内启动。删除失败仅记录不阻塞。
func (a *App) removeObsoleteGUICopy(homeDir string) {
	guiName := "xdev-gui"
	if runtime.GOOS == "windows" {
		guiName = "xdev-gui.exe"
	}
	guiPath := filepath.Join(homeDir, guiName)
	if _, err := os.Stat(guiPath); err != nil {
		return // 常见路径：无残留
	}
	if err := os.Remove(guiPath); err != nil {
		logWarn("[EnsureSelfInstalled] failed to remove obsolete %s: %v", guiName, err)
	} else {
		logInfo("[EnsureSelfInstalled] removed obsolete dual-binary leftover %s", guiPath)
	}
}

// refreshShimDispatchers 把 shims/ 下现存分发器副本重新指向当前二进制
// （缺陷④ / shim-alias-case-bug plan §3：shims 在工具 Activate 时硬链接
// 到当时的 xdev，自更新/stale 镜像替换走 rename 落新 inode，旧硬链接持有
// 旧 inode → 分发器版本永远滞后，旧分发器无 PATH 透传/别名修复，继续遮蔽
// 系统真实工具）。幂等自愈：一致时 SameFile no-op，每轮 EnsureSelfInstalled
// 安全调用。
func (a *App) refreshShimDispatchers(cfg *config.Config) {
	src, ok := selfBinarySource()
	if !ok {
		return
	}
	refreshed, failed := shim.RefreshDispatchers(cfg.Core.ShimDir, src)
	if refreshed > 0 || failed > 0 {
		logInfo("[EnsureSelfInstalled] shim dispatchers refreshed=%d failed=%d", refreshed, failed)
	}
}

// selfBinarySource 返回当前可执行文件路径（单二进制模型下，GUI/CLI 同源，
// 复制/替换到 homeDir 的源就是自身）。
func selfBinarySource() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		logWarn("[EnsureSelfInstalled] failed to get current exe path: %v", err)
		return "", false
	}
	return exe, true
}
