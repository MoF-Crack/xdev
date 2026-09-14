// Package config handles the on-disk xdev configuration.
//
// A single YAML file at $XMDEV_HOME/config.yaml controls every aspect
// of the tool: where tools are downloaded, where they are unpacked,
// which mirrors to prefer, and how PATH manipulation should behave.
//
// # Configuration Loading
//
// The primary entry point is Load(xmdeDir), which:
//  1. Resolves the xdev home directory via resolveXMDevDir()
//  2. Reads config.yaml (or returns defaults if the file doesn't exist)
//  3. Fills in any empty path fields relative to the home directory
//  4. Runs legacy path migration once (via sync.Once)
//
// # Home Directory Resolution
//
// The home directory is resolved in this priority order:
//  1. Explicit --xmdevenv-dir flag (passed as xmdeDir argument)
//  2. $XMDEV_HOME environment variable
//  3. last_selection.json (user's previous interactive choice)
//  4. Smart Windows fallback (D:-J: drive scan, ≥20 GiB free, avoid C:)
//  5. Existing installation scan on non-C drives
//  6. Platform default (~/.xdev, ~/Library/XDev, ~/.local/share/xdev)
//
// Set XMDEV_FORCE_HOME=1 to bypass the smart Windows heuristic.
//
// # Thread Safety
//
// Config.Save() is protected by a sync.Mutex. All public mutation
// methods (SetProxy, SetPathMode, etc.) acquire the lock before
// modifying fields and calling saveLocked(). The saveLocked() method
// uses an atomic write pattern (write to .tmp, then os.Rename) to
// prevent corruption from concurrent writes or process crashes.
//
// # Legacy Migration
//
// The package handles automatic migration from legacy paths:
//   - XimuLocalDev\xmdevenvmt → XDev
//   - XimuLocalDev\xdev → XDev
//   - .xmdevenvmt → .xdev
//
// Migration is guarded by migrateOnce (sync.Once) to ensure it only
// runs once per process, even if Load() is called concurrently.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
	"gopkg.in/yaml.v3"
)

// migrateOnce ensures the legacy path migration in Load() only runs once
// even if Load is called concurrently from multiple goroutines.
var migrateOnce sync.Once

// SniffConfig controls the automatic tool-discovery sniffing behaviour
// (spec 011-tool-discovery-automanage).
type SniffConfig struct {
	Strategy        string `yaml:"strategy"`               // interval|startup|service|manual
	Interval        string `yaml:"interval"`               // default 24h
	Concurrency     int    `yaml:"concurrency"`            // default 3
	RateLimitPerHr  int    `yaml:"rate_limit_per_hour"`    // default 60
	AutoArchiveFill bool   `yaml:"auto_archive_fill"`      // default true
	LastSniffAt     string `yaml:"last_sniff_at"`          // ISO timestamp, maintained by scheduler
	GitHubToken     string `yaml:"github_token,omitempty"` // optional GitHub API token (env GITHUB_TOKEN/GH_TOKEN also checked)
}

// CustomToolConfig represents a user-defined custom tool added through the GUI.
type CustomToolConfig struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name,omitempty"`
	SourceURL   string `yaml:"source_url"`
	SourceType  string `yaml:"source_type"` // github|html|npm
	Pattern     string `yaml:"pattern,omitempty"`
	Shim        string `yaml:"shim,omitempty"`
	EnvVar      string `yaml:"env_var,omitempty"`
}

// MigrationConfig controls how local tools are migrated into xdev
// management. Mode "junction" creates a Windows junction pointing at the
// original installation (no file copy); "move" moves the directory under
// the xdev install directory. Defaults to "junction".
type MigrationConfig struct {
	MigrationMode string `yaml:"migration_mode"` // "junction" | "move", default "junction"
}

// Config is the in-memory representation of the user's config file.
//
// The on-disk file is YAML (not TOML despite the original package doc).
// Most tooling in the Go ecosystem speaks YAML and the file is small
// and human-edited, making YAML the pragmatic choice.
//
// All mutation methods (SetProxy, SetPathMode, Save) are protected by
// the unexported mu mutex for thread safety.
//
// Security (H-12): The Integrity field holds an HMAC-SHA256 computed over
// the core configuration fields (paths, mirrors) using a machine-specific
// key. This detects tampering with sensitive settings like mirror URLs,
// shim_dir, or manifest_dir, which could redirect downloads or tool
// invocations. Verification failures are logged as warnings but do not
// prevent loading — the user may be legitimately editing the file.
type Config struct {
	Core        CoreConfig             `yaml:"core"`                   // Paths and global toggles
	Path        PathConfig             `yaml:"path"`                   // PATH management mode
	Proxy       ProxyConfig            `yaml:"proxy"`                  // HTTP/HTTPS proxy settings
	Mirror      map[string]string      `yaml:"mirror"`                 // Per-tool mirror URLs
	Plugin      map[string]interface{} `yaml:"plugin"`                 // Plugin-specific settings
	Aliases     map[string]string      `yaml:"aliases"`                // Command aliases
	Sniff       SniffConfig            `yaml:"sniff"`                  // Tool-discovery sniffing (011)
	Migration   MigrationConfig        `yaml:"migration"`              // Local-tool migration mode
	CustomTools []CustomToolConfig     `yaml:"custom_tools,omitempty"` // User-defined custom tools
	Integrity   string                 `yaml:"integrity,omitempty"`    // Security (H-12): HMAC-SHA256 of sensitive fields
	mu          sync.Mutex             `yaml:"-"`                      // protects Save and field mutations
}

// CoreConfig holds the high-level paths and global toggles.
// All directory paths default to subdirectories of XMDevDir if left empty
// (resolved by ResolvePaths). LogLevel defaults to "info", SelfUpdate
// defaults to true, and ManifestTTL defaults to "24h".
type CoreConfig struct {
	XMDevDir    string `yaml:"xmdevenv_dir"` // Root xdev home directory
	DownloadDir string `yaml:"download_dir"` // Where archives are cached
	InstallDir  string `yaml:"install_dir"`  // Where tools are extracted to
	ShimDir     string `yaml:"shim_dir"`     // Where shim binaries live
	CacheDir    string `yaml:"cache_dir"`    // Manifest cache directory
	EnvDir      string `yaml:"env_dir"`      // Environment variable fragment directory
	ManifestDir string `yaml:"manifest_dir"` // Tool manifest YAML directory
	LogLevel    string `yaml:"log_level"`    // Logging verbosity: "debug"|"info"|"warn"|"error"
	SelfUpdate  bool   `yaml:"self_update"`  // Whether to auto-check for xdev updates
	// SelfUpdateMode 启动自检发现新版本时的默认行为（Spec 034）：
	// "prompt"（默认，三选：更新/忽略并继续/退出）| "auto"（自动更新）| "notify"（仅提示）
	SelfUpdateMode string `yaml:"self_update_mode"`
	// SelfUpdateStale 启动时是否把 XMDevDir 根下旧版本 xdev CLI 副本替换为当前版本（默认 true）
	SelfUpdateStale  bool     `yaml:"self_update_stale"`
	ManifestTTL      string   `yaml:"manifest_ttl"`      // Cache TTL for remote version lists (Go duration)
	MirrorPreference []string `yaml:"mirror_preference"` // User-preferred mirror URLs, highest priority
	// ArchiveRetention 下载归档的保留策略：
	// "immediate" 安装成功立即删除 | "7d" 保留 7 天 | "30d" 保留 30 天 | "forever" 永久保留
	ArchiveRetention string `yaml:"archive_retention"`
}

// PathConfig controls how the tool interacts with the user's PATH.
// Mode "shim" uses a single shims/ directory; "direct" adds each tool's
// current/<tool>/bin to PATH; "auto" uses the cd hook for per-project switching.
type PathConfig struct {
	Mode                string `yaml:"mode"`                   // "shim" | "direct" | "auto"
	InjectToShellRC     bool   `yaml:"inject_to_shell_rc"`     // Auto-inject cd hook into shell RC
	InjectToWindowsPath bool   `yaml:"inject_to_windows_path"` // Add shim dir to Windows user PATH
	ShellRC             string `yaml:"shell_rc"`               // Custom shell RC file path (auto-detected if empty)
}

// ProxyConfig holds HTTP/HTTPS proxy settings for downloads.
// Supports HTTP, HTTPS, and SOCKS5 proxies.
type ProxyConfig struct {
	HTTP    string `yaml:"http"`     // e.g. "http://127.0.0.1:7890"
	HTTPS   string `yaml:"https"`    // e.g. "http://127.0.0.1:7890"
	NoProxy string `yaml:"no_proxy"` // e.g. "localhost,127.0.0.1,.example.com"
	Enabled bool   `yaml:"enabled"`  // global toggle
}

// GetProxyURL returns the proxy URL for the given scheme (http/https).
// Returns empty string if proxy is disabled or not configured.
func (c *Config) GetProxyURL(scheme string) string {
	if !c.Proxy.Enabled {
		return ""
	}
	switch strings.ToLower(scheme) {
	case "https":
		if c.Proxy.HTTPS != "" {
			return c.Proxy.HTTPS
		}
		return c.Proxy.HTTP
	case "http":
		if c.Proxy.HTTP != "" {
			return c.Proxy.HTTP
		}
		return c.Proxy.HTTPS
	default:
		return c.Proxy.HTTP
	}
}

// SetProxy updates proxy settings and saves the config atomically.
// The enabled flag acts as a global toggle; when false, GetProxyURL
// returns "" regardless of the configured URLs.
func (c *Config) SetProxy(http, https, noProxy string, enabled bool) error {
	if err := util.ValidateProxyURL(http); err != nil {
		return fmt.Errorf("http proxy: %w", err)
	}
	if err := util.ValidateProxyURL(https); err != nil {
		return fmt.Errorf("https proxy: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Proxy.HTTP = http
	c.Proxy.HTTPS = https
	c.Proxy.NoProxy = noProxy
	c.Proxy.Enabled = enabled
	return c.saveLocked()
}

// Clone 返回配置的深拷贝快照（Spec 021 D17 / F-19）：复制所有 map/slice
// 字段并重置内部互斥锁。调用方可安全读取副本，不受其他协程对原配置
// 的并发修改影响；副本上的修改不会写回原配置。
func (c *Config) Clone() *Config {
	cp := Config{
		Core:      c.Core,
		Path:      c.Path,
		Proxy:     c.Proxy,
		Sniff:     c.Sniff,
		Migration: c.Migration,
		Integrity: c.Integrity,
	}
	cp.Mirror = make(map[string]string, len(c.Mirror))
	for k, v := range c.Mirror {
		cp.Mirror[k] = v
	}
	cp.Plugin = make(map[string]interface{}, len(c.Plugin))
	for k, v := range c.Plugin {
		cp.Plugin[k] = v
	}
	cp.Aliases = make(map[string]string, len(c.Aliases))
	for k, v := range c.Aliases {
		cp.Aliases[k] = v
	}
	cp.Core.MirrorPreference = append([]string(nil), c.Core.MirrorPreference...)
	cp.CustomTools = append([]CustomToolConfig(nil), c.CustomTools...)
	return &cp
}

// GetPathMode returns the PATH management mode, defaulting to "shim".
// Valid values: "shim" (single shim directory in PATH), "direct"
// (each tool's current/<tool>/bin added to PATH directly), or "auto"
// (cd hook + shim, automatically switches versions per-project).
func (c *Config) GetPathMode() string {
	if c.Path.Mode == "" {
		return "shim"
	}
	switch c.Path.Mode {
	case "shim", "direct", "auto":
		return c.Path.Mode
	default:
		return "shim"
	}
}

// SetPathMode updates the PATH management mode and saves the config.
//
// 票 33（P1-28 / E-09）：枚举单一化——只接受 shim | direct | auto。
// "symlink" 是历史遗留值（无任何实现），此处与 cli/config.go setPathField
// 同源拒绝；存量 config.yaml 里的 "symlink" 由 GetPathMode 静默降级为
// "shim"（保持现状，不破坏既有安装）。
func (c *Config) SetPathMode(mode string) error {
	if mode != "shim" && mode != "direct" && mode != "auto" {
		return fmt.Errorf("invalid path mode: %q", mode)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Path.Mode = mode
	return c.saveLocked()
}

// GetArchiveRetention returns the download archive retention policy,
// defaulting to "immediate". Valid values: "immediate" (delete archives
// right after a successful install), "7d"/"30d" (keep for 7/30 days,
// cleaned up at startup), or "forever" (never auto-delete).
func (c *Config) GetArchiveRetention() string {
	if c.Core.ArchiveRetention == "" {
		return "immediate"
	}
	switch c.Core.ArchiveRetention {
	case "immediate", "7d", "30d", "forever":
		return c.Core.ArchiveRetention
	default:
		return "immediate"
	}
}

// SetArchiveRetention updates the archive retention policy and saves the
// config. Valid values: "immediate" | "7d" | "30d" | "forever".
func (c *Config) SetArchiveRetention(v string) error {
	switch v {
	case "immediate", "7d", "30d", "forever":
	default:
		return fmt.Errorf("invalid archive retention policy: %q", v)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Core.ArchiveRetention = v
	return c.saveLocked()
}

// GetMigrationMode returns the local-tool migration mode, defaulting to
// "junction". Valid values: "junction" (create a Windows junction pointing
// at the original directory) or "move" (move the directory under xdev).
func (c *Config) GetMigrationMode() string {
	if c.Migration.MigrationMode == "" {
		return "junction"
	}
	switch c.Migration.MigrationMode {
	case "junction", "move":
		return c.Migration.MigrationMode
	default:
		return "junction"
	}
}

// SetMigrationMode updates the local-tool migration mode and saves the config.
func (c *Config) SetMigrationMode(mode string) error {
	if mode != "junction" && mode != "move" {
		return fmt.Errorf("invalid migration mode: %q", mode)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Migration.MigrationMode = mode
	return c.saveLocked()
}

// Load reads and returns the Config stored at xmdeDir.
// If xmdeDir is empty, the home directory is resolved via the full
// precedence chain (see resolveXMDevDir). If config.yaml does not
// exist, defaults are returned without error — the caller (typically
// the `init` command) decides whether to persist them via Save().
// Legacy path migration runs at most once per process via sync.Once.
func Load(xmdeDir string) (*Config, error) {
	xmdeDir, err := resolveXMDevDir(xmdeDir)
	if err != nil {
		return nil, err
	}
	cfg := defaults(xmdeDir)
	path := filepath.Join(xmdeDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file yet - return defaults.  Caller decides
			// whether to call Save() to persist them (the `init`
			// command does so).
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Security (H-12): Verify config integrity if HMAC is present.
	// A mismatch indicates the config was tampered with or edited
	// outside of xdev. To protect against malicious redirection of
	// downloads/shims, the security-sensitive fields (paths, mirrors,
	// path mode) are reset to safe defaults. Non-sensitive fields
	// (e.g., AI config) are preserved to support legitimate cross-
	// machine migration. The user is prompted to reconfigure via
	// `xdev config set` which regenerates the HMAC.
	if cfg.Integrity != "" {
		expectedHMAC := cfg.computeIntegrityHMAC()
		if expectedHMAC != cfg.Integrity {
			safeDefaults := defaults(xmdeDir)
			fmt.Fprintln(os.Stderr, "⚠ WARNING: config.yaml integrity check FAILED — the file may have been tampered with or edited outside xdev.")
			fmt.Fprintln(os.Stderr, "  Security-sensitive fields (paths, mirrors, path mode) have been reset to safe defaults to protect against malicious redirection.")
			fmt.Fprintln(os.Stderr, "  Run `xdev config set` to reconfigure and regenerate the integrity hash.")
			log.Printf("[config] WARNING: integrity check failed — sensitive fields (paths, mirrors, path mode) reset to defaults")
			cfg.Core.InstallDir = safeDefaults.Core.InstallDir
			cfg.Core.ShimDir = safeDefaults.Core.ShimDir
			cfg.Core.ManifestDir = safeDefaults.Core.ManifestDir
			cfg.Core.DownloadDir = safeDefaults.Core.DownloadDir
			cfg.Mirror = safeDefaults.Mirror
			cfg.Path.Mode = safeDefaults.Path.Mode
			cfg.Integrity = ""
		}
	}

	// Security (H-12): Validate mirror URLs use HTTPS.
	for tool, mirrorURL := range cfg.Mirror {
		if mirrorURL != "" && !strings.HasPrefix(mirrorURL, "https://") {
			log.Printf("[config] WARNING: mirror URL for %q is not HTTPS: %q — this could allow MITM attacks", tool, mirrorURL)
		}
	}

	// SniffConfig defaults: when the sniff section is absent or has empty
	// strategy, the user has no explicit sniff config — fill defaults.
	if cfg.Sniff.Strategy == "" {
		cfg.Sniff.Strategy = "interval"
	}
	if cfg.Sniff.Interval == "" {
		cfg.Sniff.Interval = "24h"
	}
	if cfg.Sniff.Concurrency <= 0 {
		cfg.Sniff.Concurrency = 3
	}
	if cfg.Sniff.RateLimitPerHr <= 0 {
		cfg.Sniff.RateLimitPerHr = 300
	}
	// AutoArchiveFill only gets its default from defaults(); if Strategy was
	// empty (no sniff section at all) we already filled it above.
	// If the user explicitly set Strategy but omitted AutoArchiveFill, the
	// zero false is kept (user can explicitly set it to true if desired).

	// Migration mode default: junction when the migration section is absent
	// or its mode is empty.
	if cfg.Migration.MigrationMode == "" {
		cfg.Migration.MigrationMode = "junction"
	}

	// Spec 034: self-update defaults — 旧 config 文件缺字段时回填；
	// 未知枚举在决策层按 prompt 容错。
	if cfg.Core.SelfUpdateMode == "" {
		cfg.Core.SelfUpdateMode = "prompt"
	}

	// Re-resolve paths in case the user only wrote partial settings.
	cfg.ResolvePaths(xmdeDir)

	// Check if XMDevDir contains a legacy path and auto-migrate.
	// Use sync.Once to prevent concurrent Load calls from both
	// triggering migration and double-saving.
	migrateOnce.Do(func() {
		if migrated := migrateLegacyPathValue(cfg.Core.XMDevDir); migrated != cfg.Core.XMDevDir {
			cfg.Core.XMDevDir = migrated
			cfg.ResolvePaths(migrated)
			// Update config.yaml with new paths
			UpdateConfigPaths(migrated)
			// Save the updated config
			cfg.Save()
			InvalidateHomeCache()
		}
	})

	return cfg, nil
}

// computeIntegrityHMAC computes an HMAC-SHA256 over the security-sensitive
// fields of the config using the machine-specific key. This allows detection
// of tampering with paths, mirror URLs, or other settings that could redirect
// downloads or tool invocations.
func (c *Config) computeIntegrityHMAC() string {
	// Build a deterministic representation of security-sensitive fields
	type integrityData struct {
		InstallDir  string            `json:"install_dir"`
		ShimDir     string            `json:"shim_dir"`
		ManifestDir string            `json:"manifest_dir"`
		DownloadDir string            `json:"download_dir"`
		Mirror      map[string]string `json:"mirror"`
		PathMode    string            `json:"path_mode"`
	}
	snapshot := integrityData{
		InstallDir:  c.Core.InstallDir,
		ShimDir:     c.Core.ShimDir,
		ManifestDir: c.Core.ManifestDir,
		DownloadDir: c.Core.DownloadDir,
		Mirror:      make(map[string]string),
		PathMode:    c.Path.Mode,
	}
	// Sort mirror keys for deterministic output
	for k, v := range c.Mirror {
		snapshot.Mirror[k] = v
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return util.ComputeHMAC(data)
}

// Save writes the config to disk in YAML form using an atomic write
// pattern (write to .tmp, then os.Rename) to prevent corruption.
// Security (H-12): Before saving, an HMAC of sensitive fields is computed
// and stored in the Integrity field so tampering can be detected on next load.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Compute and store integrity HMAC before saving
	c.Integrity = c.computeIntegrityHMAC()
	return c.saveLocked()
}

// saveLocked writes the config to disk in YAML form using atomic write.
// The caller must hold c.mu. The atomic pattern (tmp + rename) ensures
// that a crash mid-write leaves the original file intact.
func (c *Config) saveLocked() error {
	if err := os.MkdirAll(c.Core.XMDevDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path := filepath.Join(c.Core.XMDevDir, "config.yaml")
	if err := util.AtomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// EnsureDirs creates every directory the tool needs.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.Core.XMDevDir,
		c.Core.DownloadDir,
		c.Core.InstallDir,
		c.Core.ShimDir,
		c.Core.CacheDir,
		c.Core.EnvDir,
		c.Core.ManifestDir,
		filepath.Join(c.Core.XMDevDir, "current"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// defaults constructs a Config populated with sensible defaults for
// the current operating system.
func defaults(xmdeDir string) *Config {
	c := &Config{
		Core: CoreConfig{
			XMDevDir:        xmdeDir,
			DownloadDir:     filepath.Join(xmdeDir, "downloads"),
			InstallDir:      filepath.Join(xmdeDir, "install"),
			ShimDir:         filepath.Join(xmdeDir, "shims"),
			CacheDir:        filepath.Join(xmdeDir, "cache"),
			EnvDir:          filepath.Join(xmdeDir, "env"),
			ManifestDir:     filepath.Join(xmdeDir, "manifests"),
			LogLevel:        "info",
			SelfUpdate:      true,
			SelfUpdateMode:  "prompt",
			SelfUpdateStale: true,
			ManifestTTL:     "24h",
		},
		Path: PathConfig{
			Mode:                "shim",
			InjectToShellRC:     true,
			InjectToWindowsPath: true,
		},
		Mirror:  defaultMirrors(),
		Plugin:  map[string]interface{}{},
		Aliases: map[string]string{},
		Sniff: SniffConfig{
			Strategy:        "interval",
			Interval:        "24h",
			Concurrency:     3,
			RateLimitPerHr:  300,
			AutoArchiveFill: true,
		},
		Migration: MigrationConfig{
			MigrationMode: "junction",
		},
	}
	c.ResolvePaths(xmdeDir)
	return c
}

// ResolvePaths fills in any empty path fields relative to the
// xdev root.  Exposed (capitalised) so that the CLI layer can
// re-derive download/install/shim sub-dirs after the user picks a
// new home directory in the interactive prompt.
func (c *Config) ResolvePaths(xmdeDir string) {
	if c.Core.XMDevDir == "" {
		c.Core.XMDevDir = xmdeDir
	}
	if c.Core.DownloadDir == "" {
		c.Core.DownloadDir = filepath.Join(c.Core.XMDevDir, "downloads")
	}
	if c.Core.InstallDir == "" {
		c.Core.InstallDir = filepath.Join(c.Core.XMDevDir, "install")
	}
	if c.Core.ShimDir == "" {
		c.Core.ShimDir = filepath.Join(c.Core.XMDevDir, "shims")
	}
	if c.Core.CacheDir == "" {
		c.Core.CacheDir = filepath.Join(c.Core.XMDevDir, "cache")
	}
	if c.Core.EnvDir == "" {
		c.Core.EnvDir = filepath.Join(c.Core.XMDevDir, "env")
	}
	if c.Core.ManifestDir == "" {
		c.Core.ManifestDir = filepath.Join(c.Core.XMDevDir, "manifests")
	}
}

// resolveXMDevDir walks the precedence chain:
//  1. explicit argument (non-empty)
//  2. $XMDEV_HOME
//  3. smart fallback that tries to avoid a nearly-full C: drive
//
// On Windows, if the user did not set XMDEV_HOME and the resolved
// home directory would live on a drive with less than
// MinFreeBytesDefault bytes free, we transparently switch to
// <largest-fixed-drive>:\XDev.  This keeps large
// language runtimes and Android SDKs off a cramped system disk.
//
// Set the environment variable XMDEV_FORCE_HOME=1 to bypass the
// heuristic and always use ~/.xdev.
//
// 票 33（P1-27 / E-08）：返回前统一 filepath.Abs——本函数是所有路径的
// 单一来源（config.yaml / last_selection.json / 缓存 / 驱动器扫描），
// 一旦返回相对路径，后续 Join 出的 shim/install/cache 路径都会相对 CWD
// 漂移（跨进程注册表与缓存写入落到错误位置）。归一失败（CWD 不可用等
// 极端情况）时保留原值，不因归一失败而拒绝启动。
func resolveXMDevDir(explicit string) (string, error) {
	dir, err := resolveXMDevDirRaw(explicit)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	abs, absErr := filepath.Abs(dir)
	if absErr != nil {
		return dir, nil
	}
	return abs, nil
}

// resolveXMDevDirRaw implements the precedence chain documented above;
// callers must use resolveXMDevDir so the absolute-path invariant holds.
func resolveXMDevDirRaw(explicit string) (string, error) {
	if explicit != "" {
		return expandHome(explicit)
	}
	if v := os.Getenv("XMDEV_HOME"); v != "" {
		return expandHome(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	if os.Getenv("XMDEV_FORCE_HOME") == "1" {
		return filepath.Join(home, ".xdev"), nil
	}

	// Check last_selection.json for a previously chosen install dir.
	// This takes priority over smart fallback so that the user's
	// explicit choice is respected even after config.yaml is deleted.
	//
	// Security (L-3): The file may contain an HMAC field for integrity
	// verification. If the HMAC is present but does not verify, the
	// selection is treated as tampered and this path is skipped (falls
	// through to smart fallback below).
	lastSelPath := filepath.Join(home, ".xdev", "last_selection.json")
	if data, err := os.ReadFile(lastSelPath); err == nil {
		var sel struct {
			InstallDir string `json:"installDir"`
			Drive      string `json:"drive"`
			HMAC       string `json:"hmac,omitempty"`
		}
		if json.Unmarshal(data, &sel) == nil && sel.InstallDir != "" {
			// Verify HMAC if present (L-3 integrity check).
			// The HMAC is computed over {installDir, drive} to match the
			// GUI's computeLastSelectionHMAC function.
			if sel.HMAC != "" {
				hmacData, _ := json.Marshal(struct {
					InstallDir string `json:"installDir"`
					Drive      string `json:"drive"`
				}{sel.InstallDir, sel.Drive})
				if !util.VerifyHMAC(hmacData, sel.HMAC) {
					log.Printf("[config] WARNING: last_selection.json HMAC verification failed — file may have been tampered with, ignoring stored selection")
					// Fall through to smart fallback
					sel.InstallDir = ""
				}
			}
			if sel.InstallDir != "" {
				if fi, err := os.Stat(sel.InstallDir); err == nil && fi.IsDir() {
					// Check if the stored path is a legacy path that needs migration
					migrated := migrateLegacyPathValue(sel.InstallDir)
					if migrated != sel.InstallDir {
						// Update last_selection.json with the new path (with fresh HMAC)
						newHMACData, _ := json.Marshal(struct {
							InstallDir string `json:"installDir"`
							Drive      string `json:"drive"`
						}{migrated, sel.Drive})
						newSel := struct {
							InstallDir string `json:"installDir"`
							Drive      string `json:"drive"`
							HMAC       string `json:"hmac,omitempty"`
						}{
							InstallDir: migrated,
							Drive:      sel.Drive,
							HMAC:       util.ComputeHMAC(newHMACData),
						}
						if newData, err := json.MarshalIndent(newSel, "", "  "); err == nil {
							os.WriteFile(lastSelPath, newData, 0o644)
						}
						// Also update config.yaml if it exists in the old dir
						UpdateConfigPaths(migrated)
						InvalidateHomeCache()
						// Check if the migrated path exists
						if fi2, err2 := os.Stat(migrated); err2 == nil && fi2.IsDir() {
							return migrated, nil
						}
					}
					return sel.InstallDir, nil
				}
			}
		}
	}

	// Smart fallback on Windows to avoid the C: drive.
	if runtime.GOOS == "windows" {
		if alt := saferWindowsHome(); alt != "" {
			// Auto-migrate: if the new XDev path doesn't exist but a
			// legacy xmdevenvmt directory does, rename it in-place.
			if _, err := os.Stat(alt); err != nil {
				tryMigrateLegacy(alt)
			}
			return alt, nil
		}
		// Last resort before C: drive: scan all non-C drives for
		// any existing xdev installation.
		if found := scanForExistingInstall(); found != "" {
			return found, nil
		}
	}
	// Platform-specific default home directory
	newPath := getDefaultHomeDir()
	// Auto-migrate legacy directory to new path
	if _, err := os.Stat(newPath); err != nil {
		legacy := getLegacyDir()
		if legacy != "" {
			if fi, err2 := os.Stat(legacy); err2 == nil && fi.IsDir() {
				os.Rename(legacy, newPath)
			}
		}
	}
	return newPath, nil
}

// getDefaultHomeDir returns the platform-specific default xdev home directory:
//   - Windows: ~/.xdev (smart fallback to non-system drive handled above)
//   - macOS:   ~/Library/XDev/
//   - Linux:   ~/.local/share/xdev/ (XDG Base Directory Specification)
func getDefaultHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/", ".xdev")
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "XDev")
	case "linux":
		// Respect XDG_DATA_HOME if set
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "xdev")
		}
		return filepath.Join(home, ".local", "share", "xdev")
	default:
		return filepath.Join(home, ".xdev")
	}
}

// getLegacyDir returns the legacy directory path for auto-migration:
//   - Windows: ~/.xmdevenvmt
//   - macOS:   none (first release on macOS)
//   - Linux:   ~/.xdev (pre-XDG path)
func getLegacyDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, ".xmdevenvmt")
	case "linux":
		return filepath.Join(home, ".xdev")
	default:
		return ""
	}
}

// tryMigrateLegacy attempts to rename a legacy xmdevenvmt directory
// to the new XDev path.  For example, E:\XimuLocalDev\xmdevenvmt → E:\XDev.
// It also updates the config.yaml inside to reflect the new paths.
// If the rename fails (e.g. cross-device), it falls back to a no-op
// and the user will be prompted to re-initialise.
func tryMigrateLegacy(newPath string) {
	vol := filepath.VolumeName(newPath)
	if vol == "" {
		return
	}
	legacies := []string{
		filepath.Join(vol, "XimuLocalDev", "xmdevenvmt"),
		filepath.Join(vol, "XimuLocalDev", "xdev"),
	}
	for _, legacy := range legacies {
		fi, err := os.Stat(legacy)
		if err != nil || !fi.IsDir() {
			continue
		}
		// Verify it looks like an xdev home
		looksValid := false
		if _, err := os.Stat(filepath.Join(legacy, "install")); err == nil {
			looksValid = true
		}
		if _, err := os.Stat(filepath.Join(legacy, "config.yaml")); err == nil {
			looksValid = true
		}
		if !looksValid {
			continue
		}
		// Attempt rename
		if err := os.Rename(legacy, newPath); err != nil {
			// Cross-device rename won't work; skip silently.
			// The user will be prompted to init a new directory.
			continue
		}
		// Update config.yaml with new paths if it exists
		UpdateConfigPaths(newPath)
		// Invalidate home cache so the next call picks up the new path
		InvalidateHomeCache()
		return
	}
}

// UpdateConfigPaths rewrites the config.yaml in the given directory
// to replace any legacy xmdevenvmt paths with the new xdev paths.
func UpdateConfigPaths(dir string) {
	configPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	// Replace legacy path patterns in the YAML content
	content := string(data)
	content = strings.ReplaceAll(content, "XimuLocalDev\\xmdevenvmt", "XDev")
	content = strings.ReplaceAll(content, "XimuLocalDev/xmdevenvmt", "XDev")
	content = strings.ReplaceAll(content, "XimuLocalDev\\xdev", "XDev")
	content = strings.ReplaceAll(content, "XimuLocalDev/xdev", "XDev")
	content = strings.ReplaceAll(content, ".xmdevenvmt", ".xdev")
	if err := util.AtomicWriteFile(configPath, []byte(content), 0o644); err != nil {
		// 票 40（P2-33 / N-18）：文案原先误写成 last_selection.json，而这里
		// 写的是 configPath（<home>/config.yaml）——出错时日志必须指向真正
		// 失败的文件，否则用户会去排查一个从未被写过的文件。
		log.Printf("[config] WARNING: failed to save config.yaml: %v", err)
	}
}

// migrateLegacyPathValue checks if a path contains legacy directory names
// and returns the updated path. For example:
//
//	E:\XimuLocalDev\xmdevenvmt → E:\XDev
//	E:\XimuLocalDev\xdev → E:\XDev
//
// If no legacy pattern is found, the original path is returned unchanged.
func migrateLegacyPathValue(dir string) string {
	normalized := strings.ReplaceAll(strings.ToLower(dir), "/", "\\")
	legacyPatterns := []struct {
		pattern     string
		replacement func(string) string
	}{
		{`\ximulocaldev\xmdevenvmt`, func(vol string) string { return vol + "\\XDev" }},
		{`\ximulocaldev\xdev`, func(vol string) string { return vol + "\\XDev" }},
	}
	for _, lp := range legacyPatterns {
		if strings.Contains(normalized, lp.pattern) {
			vol := filepath.VolumeName(dir)
			newPath := lp.replacement(vol)
			// If the legacy dir exists, try to rename/migrate it
			if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
				if _, err := os.Stat(newPath); err == nil {
					// newPath already exists, move remaining contents
					for _, sub := range []string{"install", "cache", "shims", "env", "downloads", "manifests", "current", "logs", "config.yaml"} {
						src := filepath.Join(dir, sub)
						dst := filepath.Join(newPath, sub)
						if _, err := os.Stat(src); err == nil {
							os.Rename(src, dst)
						}
					}
					// Try to remove the now-empty legacy dir
					os.Remove(dir)
					// Also try to remove the parent XimuLocalDev if empty
					parent := filepath.Dir(dir)
					os.Remove(parent)
				} else if err := os.Rename(dir, newPath); err != nil {
					// Rename failed, keep original
					return dir
				}
			}
			// 更新用户 PATH 中的旧 shim 目录为新路径
			env.UpdateShimDirInUserPath(dir, newPath)
			return newPath
		}
	}
	return dir
}

// migrateLegacyPath checks for legacy installation directories
// that existed before the rename from xmdevenvmt to xdev.
// Given a new path like "E:\XDev", it checks for:
//   - <drive>\XimuLocalDev\xmdevenvmt (legacy)
//   - <drive>\XimuLocalDev\xdev
func migrateLegacyPath(newPath string) string {
	vol := filepath.VolumeName(newPath)
	if vol == "" {
		return ""
	}
	legacies := []string{
		filepath.Join(vol, "XimuLocalDev", "xmdevenvmt"),
		filepath.Join(vol, "XimuLocalDev", "xdev"),
	}
	for _, l := range legacies {
		if fi, err := os.Stat(l); err == nil && fi.IsDir() {
			// Only return if it looks like an xdev home (has install/ or config.yaml)
			if _, err := os.Stat(filepath.Join(l, "install")); err == nil {
				return l
			}
			if _, err := os.Stat(filepath.Join(l, "config.yaml")); err == nil {
				return l
			}
		}
	}
	return ""
}

// MinFreeBytesDefault is the threshold below which a drive is
// considered "too small" for storing toolchains and Android SDKs.
// 20 GiB is enough to fit a full Android SDK (~10 GiB) plus one or
// two language runtimes.
const MinFreeBytesDefault int64 = 20 * (1 << 30)

// DriveInfo describes one fixed drive the user could choose.
type DriveInfo struct {
	Letter  string // e.g. "D:"
	Path    string // e.g. "D:\\"
	Free    int64  // bytes available
	Total   int64  // total bytes
	Mounted bool
}

// ScanDrives enumerates fixed drive letters A: through Z: and
// returns a slice with their free-space information.
//
// On non-Windows platforms the returned slice is empty.  Callers
// should always check len(result) before doing anything drive-
// specific.
func ScanDrives() []DriveInfo {
	if runtime.GOOS != "windows" {
		return nil
	}
	out := make([]DriveInfo, 0, 26)
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		info := DriveInfo{Letter: drive, Path: drive + string(os.PathSeparator)}
		if free, ok := DriveFreeBytes(drive); ok {
			info.Free = free
			info.Mounted = true
		}
		// Total size is best-effort; if Win32 fails we leave it 0.
		// We piggy-back on a second syscall to keep the helper
		// signature simple.
		if total, ok := driveTotalBytes(drive); ok {
			info.Total = total
		}
		out = append(out, info)
	}
	return out
}

// driveTotalBytes asks the kernel for the total bytes on a drive.
// Falls back to 0 when the call fails.
func driveTotalBytes(drive string) (int64, bool) {
	if runtime.GOOS != "windows" {
		return 0, false
	}
	if !strings.HasSuffix(drive, ":") {
		drive += ":"
	}
	return driveTotalBytesImpl(drive)
}

// DriveFreeBytes returns the available bytes on a Windows drive.
//
// We prefer the Win32 GetDiskFreeSpaceExW API (it works even when
// fsutil is blocked by policy) and fall back to running fsutil when
// the syscall is unavailable.
func DriveFreeBytes(drive string) (int64, bool) {
	if runtime.GOOS != "windows" {
		return 0, false
	}
	if !strings.HasSuffix(drive, ":") {
		drive += ":"
	}
	if free, ok := winFreeBytes(drive); ok {
		return free, true
	}
	// Fallback: invoke fsutil through a shell.  Some hardened
	// systems block this so we tolerate the failure.
	cmd := exec.Command("cmd", "/c", "fsutil", "volume", "diskfree", drive)
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "total bytes free") {
			continue
		}
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		num := strings.TrimSpace(line[idx+1:])
		num = strings.ReplaceAll(num, ",", "")
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// winFreeBytes queries GetDiskFreeSpaceExW via the win32 syscall.
//
// We use the uintptr-based entry point so we don't have to pull in
// golang.org/x/sys/windows just for this one call.
func winFreeBytes(drive string) (int64, bool) {
	return winFreeBytesImpl(drive)
}

// saferWindowsHome picks a fixed Windows drive that has at least
// MinFreeBytesDefault bytes free, prefers D:, then E:, F:... and
// falls back to the user home only when no other drive is
// available.
//
// Policy: by default the xdev home is never created on C:.
// Language runtimes, Android SDKs and build caches routinely grow
// past 5-10 GiB, which is dangerous to put on the system disk.  If
// you really want to live on C:, set XMDEV_FORCE_HOME=1 and the
// caller will fall back to %USERPROFILE%\.xdev.
func saferWindowsHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	homeDrive := filepath.VolumeName(home)
	_ = homeDrive // referenced for documentation; we never auto-keep C:
	candidates := []string{"D:", "E:", "F:", "G:", "H:", "I:", "J:"}
	var best string
	var bestFree int64
	for _, d := range candidates {
		free, ok := DriveFreeBytes(d)
		if !ok || free < MinFreeBytesDefault {
			continue
		}
		if free > bestFree {
			best = d
			bestFree = free
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(best+string(os.PathSeparator), "XDev")
}

// scanForExistingInstall searches all non-C drives for an existing
// xdev installation.  This is used as a last resort before falling
// back to the C: drive.
func scanForExistingInstall() string {
	candidates := []string{"D:", "E:", "F:", "G:", "H:", "I:", "J:"}
	paths := []string{
		"XDev",
	}
	for _, drive := range candidates {
		for _, p := range paths {
			full := filepath.Join(drive+string(os.PathSeparator), p)
			fi, err := os.Stat(full)
			if err != nil || !fi.IsDir() {
				continue
			}
			// Verify it looks like an xdev home
			if _, err := os.Stat(filepath.Join(full, "install")); err == nil {
				return full
			}
			if _, err := os.Stat(filepath.Join(full, "config.yaml")); err == nil {
				return full
			}
		}
	}
	return ""
}

// ResolveHomeDir returns the xdev home directory using the same
// precedence chain as resolveXMDevDir but without requiring an
// explicit argument.  This is the public API for packages that
// need to locate the xdev home (e.g. shim, env) without importing
// the full config loading machinery.
//
// Priority:
//  1. $XMDEV_HOME
//  2. Smart Windows fallback (avoids C: drive)
//  3. Legacy path migration
//  4. ~/.xdev (last resort)
//
// The result is cached in-process via sync.Once and across
// processes via a small cache file on the local disk, avoiding
// repeated drive scanning on Windows.
var (
	homeOnce sync.Once
	homeVal  string
)

// ResetHomeDirForTest resets the cached home directory so that the
// next call to ResolveHomeDir re-reads the XMDEV_HOME environment
// variable.  This is intended ONLY for use in tests.
func ResetHomeDirForTest() {
	homeOnce = sync.Once{}
	homeVal = ""
}

func ResolveHomeDir() string {
	homeOnce.Do(func() {
		// If XMDEV_HOME is set, use it directly (no scanning needed).
		if v := os.Getenv("XMDEV_HOME"); v != "" {
			homeVal = v
			return
		}
		// Try to read from cross-process cache file.
		homeVal = readHomeCache()
		if homeVal != "" {
			return
		}
		// Full resolution with drive scanning.
		dir, _ := resolveXMDevDir("")
		homeVal = dir
		writeHomeCache(homeVal)
	})
	return homeVal
}

// InvalidateHomeCache deletes the cross-process home directory cache.
// Call this from `xdev init` and `xdev self uninstall` so the next
// invocation re-discovers the correct home.  It removes both the
// primary cache and any legacy LOCALAPPDATA cache.
func InvalidateHomeCache() {
	os.Remove(homeCachePath())
	os.Remove(fallbackCachePath())
}

// homeCachePath returns the path to the cross-process home cache file.
// When the xdev home directory is already resolved, the cache is stored
// there instead of LOCALAPPDATA to avoid writing to the C: drive.
// Only when the home directory is not yet determined does it fall back
// to LOCALAPPDATA.
func homeCachePath() string {
	if homeVal != "" {
		return filepath.Join(homeVal, ".home.cache")
	}
	return fallbackCachePath()
}

// fallbackCachePath returns the legacy LOCALAPPDATA-based cache path.
// Used as a fallback when the xdev home directory is not yet resolved,
// and for cleaning up old cache files during migration.
func fallbackCachePath() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, "xdev", "home.cache")
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "xdev", "home.cache")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".xdev", "home.cache")
}

// readHomeCache reads the cached home directory from disk.
// Returns "" if the cache doesn't exist or is invalid.
// It tries the primary cache path first, then falls back to the
// LOCALAPPDATA-based legacy path for backward compatibility.
func readHomeCache() string {
	// Try primary cache path (xdev home dir if resolved, else LOCALAPPDATA).
	if val := readCacheFile(homeCachePath()); val != "" {
		return val
	}
	// On Windows, also try the likely xdev home directory for a cache
	// file written by a previous run. This avoids always reading from
	// LOCALAPPDATA (C: drive) when a non-system-drive home exists.
	if runtime.GOOS == "windows" && homeVal == "" {
		if alt := saferWindowsHome(); alt != "" {
			if val := readCacheFile(filepath.Join(alt, ".home.cache")); val != "" {
				return val
			}
		}
	}
	// Try the legacy LOCALAPPDATA path directly (for caches written by
	// older versions that always used LOCALAPPDATA).
	if fallback := fallbackCachePath(); fallback != homeCachePath() {
		if val := readCacheFile(fallback); val != "" {
			return val
		}
	}
	return ""
}

// readCacheFile reads and validates a single cache file.
// Returns the cached home directory or "" if invalid.
//
// 票 43：写入端的内容是 `dir + "\n" + os.Getenv("XMDEV_HOME")`，而生产环境的
// 正常情形是 XMDEV_HOME **未设置**（home 是「被发现」的，不是配置的），此时
// 文件只有一行。本函数原先要求 `len(lines) >= 2`，于是单行缓存永远被拒收——
// 跨进程缓存从未生效：每个 xdev/shim/CLI/GUI 进程都重新全盘扫描
// （saferWindowsHome 对 8 个盘符求剩余空间 / scanForExistingInstall 逐盘 stat）
// 并重写该文件。现在接受单行形式（缺第二行 = 记录时环境变量为空），并对读到的
// 两行做 TrimSpace（Windows 写出的 CRLF 同样能对上）。
func readCacheFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}
	cachedHome := strings.TrimSpace(lines[0])
	cachedEnv := ""
	if len(lines) >= 2 {
		cachedEnv = strings.TrimSpace(lines[1])
	}
	if cachedHome == "" {
		return ""
	}
	if os.Getenv("XMDEV_HOME") != cachedEnv {
		return ""
	}
	if fi, err := os.Stat(cachedHome); err != nil || !fi.IsDir() {
		return ""
	}
	return cachedHome
}

// PeekCachedHomeDir returns the xdev home directory that previous runs
// recorded, WITHOUT performing discovery and WITHOUT writing anything.
//
// It exists for host-pollution auditing (internal/testkit/hostsnapshot): a
// test suite must be able to watch the developer's real xdev home for writes
// even when XMDEV_HOME is pinned to a temp directory for the test process.
// Unlike ResolveHomeDir it never calls writeHomeCache, and unlike
// readHomeCache it does not invalidate an entry when the current process's
// XMDEV_HOME differs from the value recorded in the cache file.
func PeekCachedHomeDir() string {
	if homeVal != "" {
		return homeVal
	}
	var candidates []string
	if v := os.Getenv("XMDEV_HOME"); v != "" {
		candidates = append(candidates, filepath.Join(v, ".home.cache"))
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "xdev", "home.cache"))
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "xdev", "home.cache"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".xdev", "home.cache"))
	}
	for _, c := range candidates {
		if dir := rawCachedDir(c); dir != "" {
			return dir
		}
	}
	if runtime.GOOS == "windows" {
		if dir := scanForExistingInstall(); dir != "" {
			return dir
		}
	}
	return ""
}

// rawCachedDir reads the home directory recorded in a cache file without
// validating it against the current XMDEV_HOME. Returns "" when the file is
// missing/malformed or when the recorded directory no longer exists.
func rawCachedDir(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}
	dir := strings.TrimSpace(lines[0])
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}

// writeHomeCache atomically writes the home directory cache file.
// When the cache is written to the xdev home directory (non-C: drive),
// it also removes the legacy LOCALAPPDATA cache to avoid stale entries.
//
// 票 31（P1-24 收敛核验）：本处原为「固定 path+".tmp" + os.WriteFile」克隆，
// 多进程（GUI/shim/CLI 并发启动）会互相截断同一中间文件，rename 方拿到
// 半截内容或直接失败。改用唯名临时文件（CreateTemp）+ 权限还原 + rename；
// 与 internal/util.AtomicWriteFile 的 CreateTemp 版本同型（该本体由
// internal/util 域负责，见票 31 报告的白名单外清单）。
func writeHomeCache(dir string) {
	path := homeCachePath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	content := dir + "\n" + os.Getenv("XMDEV_HOME")
	if err := writeFileCreateTemp(path, []byte(content), 0o644); err != nil {
		log.Printf("[config] WARNING: write home cache %s: %v", path, err)
	}
	// Clean up legacy LOCALAPPDATA cache if we're now writing elsewhere.
	if fallback := fallbackCachePath(); fallback != path {
		os.Remove(fallback)
	}
}

// writeFileCreateTemp writes data to path atomically using a uniquely named
// temporary file in the same directory (CreateTemp gives the 0600 default,
// so the requested perm is restored explicitly; on Windows the read-only bit
// is not part of FileMode perm, so there is no side effect).
// 票 31: 固定 "<path>.tmp" 会被并发写者互相截断，唯名临时文件消除该竞态。
func writeFileCreateTemp(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write tmp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync tmp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close tmp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod tmp %s: %w", tmpName, err)
	}
	if err := renameWithRetry(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// renameWithRetry renames with a short bounded retry window.
//
// Windows 上多个进程/goroutine 同时以 MoveFileEx(REPLACE_EXISTING) 覆盖同一
// 目标时会偶发 ERROR_ACCESS_DENIED（已知行为，不是调用方的错）：直接冒泡
// 会把这个正常的并发写当失败。重试窗口 ~55ms，超时后原样返回错误。
func renameWithRetry(oldPath, newPath string) error {
	const attempts = 10
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * time.Millisecond)
	}
	return err
}

// expandHome replaces a leading "~" or "~/" with the user's home dir,
// and（票 33 / P1-27 / E-08）normalises the result to an absolute path so
// that a relative --xmdevenv-dir / $XMDEV_HOME never propagates into the
// rest of the config (which would resolve relative to the process CWD).
func expandHome(p string) (string, error) {
	out := p
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		switch {
		case p == "~":
			out = home
		case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\"):
			out = filepath.Join(home, p[2:])
		}
	}
	if out == "" {
		return "", nil
	}
	if abs, err := filepath.Abs(out); err == nil {
		return abs, nil
	}
	return out, nil
}

// IsWindows reports whether we are on Windows.  Small helper to avoid
// scattering runtime.GOOS checks through the codebase.
func IsWindows() bool { return runtime.GOOS == "windows" }

// IsMac reports whether we are on macOS.
func IsMac() bool { return runtime.GOOS == "darwin" }

// IsLinux reports whether we are on Linux.
func IsLinux() bool { return runtime.GOOS == "linux" }

// defaultMirrors returns the built-in mirror table.
//
// The first entry for each tool is the "fast" choice for users in
// China; users elsewhere can override via `config set mirror.X url`.
func defaultMirrors() map[string]string {
	return map[string]string{
		"python":  "https://mirrors.aliyun.com/python",
		"node":    "https://npmmirror.com/mirrors/node",
		"go":      "https://goproxy.cn",
		"java":    "https://download.oracle.com/java",
		"rust":    "https://rsproxy.cn",
		"android": "https://redirector.gvt1.com/edgedl/android/repository",
		"flutter": "https://storage.flutter-io.cn",
		"maven":   "https://mirrors.aliyun.com/maven",
	}
}
