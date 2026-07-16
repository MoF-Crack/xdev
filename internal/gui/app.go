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
//   - app_custom_tools.go: GetCustomTools, AddCustomTool, etc.
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
}

// VersionInfo represents a version entry for the frontend.
type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
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

	// Dashboard stats cache
	dashMu         sync.RWMutex
	dashCache      *DashboardStats
	dashExpire     time.Time
	dashRefreshing int32 // atomic: 1 = refresh in progress, prevents concurrent storms

	// Channel for signaling that refreshLocalCache has completed
	localReady     chan struct{}
	localReadyOnce sync.Once

	// cancelRefresh is used to cancel the delayed refreshLocalCache goroutine
	cancelRefresh context.CancelFunc

	// Favorites
	favMu     sync.RWMutex
	favorites []FavoriteInfo

	// Custom tools
	customMu    sync.RWMutex
	customTools []CustomTool

	// Isolation
	isoMu       sync.Mutex
	isoMode     isolator.IsolationMode
	isoInstance isolator.Isolator
}

// toolScanCacheFile 返回工具扫描缓存文件路径
func toolScanCacheFile() string {
	cfg, _ := config.Load("")
	return filepath.Join(cfg.Core.CacheDir, "tool_scan_cache.json")
}

// NewApp creates a new App instance.
func NewApp(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry) *App {
	return &App{
		cfg:         cfg,
		cat:         cat,
		registry:    reg,
		localMap:    make(map[string]localInfo),
		dlMgr:       downloader.NewManager(),
		localReady:  make(chan struct{}),
		favorites:   []FavoriteInfo{},
		customTools: []CustomTool{},
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	logInfo("[startup] Wails OnStartup callback fired")
}

// domReady is called when the DOM is ready.
func (a *App) domReady(ctx context.Context) {
	logInfo("[domReady] Wails OnDomReady callback fired — calling WindowShow()")
	wailsRuntime.WindowShow(ctx)
	logInfo("[domReady] WindowShow() completed, window should now be visible")

	// 先尝试从缓存加载
	a.loadLocalCache()
	a.loadFavorites()
	a.loadCustomTools()

	// 自动检测并修复主目录下缺失的 xdev/xdev-gui 二进制
	go func() {
		time.Sleep(1 * time.Second)
		a.EnsureSelfInstalled()
	}()

	// 自动修复版本号为 "local" 的已迁移工具
	go func() {
		time.Sleep(2 * time.Second)
		if fixed := a.FixLocalVersions(); len(fixed) > 0 {
			logInfo("[domReady] fixed %d local version(s)", len(fixed))
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
// (at least once) or the timeout expires.
func (a *App) waitForLocalCache() {
	// Quick check: already populated?
	a.localMu.RLock()
	populated := len(a.localMap) > 0
	a.localMu.RUnlock()
	if populated {
		return
	}
	// Wait for refreshLocalCache to complete, up to 5 seconds.
	select {
	case <-a.localReady:
		return
	case <-time.After(5 * time.Second):
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

// getCfg returns the config with read lock protection.
func (a *App) getCfg() *config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

// setCfg updates the config with write lock protection.
func (a *App) setCfg(cfg *config.Config) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg = cfg
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
