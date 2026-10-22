// context.go holds the global run context for CLI commands.
//
// The cli package is intentionally small — heavy lifting lives in
// internal/config, internal/manifest, internal/resolver, etc. This
// file only holds the in-memory snapshot of "what is the user asking
// for in this particular invocation".
//
// # Thread Safety
//
// The global context is initialized once in the Cobra PersistentPreRun
// callback and then read-only for the rest of the command execution.
// No synchronization is needed because Cobra runs commands sequentially.
package cli

import (
	"fmt"
	"os"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/remote"
)

// Global scheduler reference for startup-strategy lifecycle management.
// Set by root.go's OnInitialize and used by ShutdownSniffScheduler.
var sniffScheduler *discovery.Scheduler

// Global context for the running command.
//
// The cli package is intentionally small - heavy lifting lives in
// internal/config, internal/manifest, internal/resolver, etc.  This
// file only holds the in-memory snapshot of "what is the user asking
// for in this particular invocation".

type runCtx struct {
	cfg         *config.Config
	xmdeDir     string // --xmdevenv-dir flag value, "" if not set
	verbose     bool
	noColor     bool
	noPathFix   bool
	initialized bool
}

var globalCtx runCtx

// Cfg returns the loaded configuration, panicking if the global
// initialization was somehow skipped.  All subcommands are expected
// to run after Cobra's PersistentPreRun, so this should be safe.
func Cfg() *config.Config {
	if !globalCtx.initialized {
		// Fall back to default config so that we never panic on direct
		// library use (e.g. tests or embedding).
		c, err := config.Load("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "xdev: failed to load config: %v\n", err)
			os.Exit(2)
		}
		globalCtx.cfg = c
		globalCtx.initialized = true
	}
	return globalCtx.cfg
}

func Verbose() bool   { return globalCtx.verbose }
func NoColor() bool   { return globalCtx.noColor }
func NoPathFix() bool { return globalCtx.noPathFix }

// XMDevDirOverride returns the value of the --xmdevenv-dir flag
// from the global flag set, or "" when not provided.  Subcommands
// can use this to detect whether the user already chose a home
// before falling back to interactive prompting.
func XMDevDirOverride() string { return globalCtx.xmdeDir }

// ForceHome reports whether the user set XMDEV_FORCE_HOME=1.
func ForceHome() bool { return os.Getenv("XMDEV_FORCE_HOME") == "1" }

// loadCatalogAndRegistry is a convenience helper that loads the
// manifest catalog and builds a plugin registry from it in one call.
func loadCatalogAndRegistry() (*manifest.Catalog, *plugin.Registry) {
	cat := loadCatalog()
	reg := loadRegistry(cat)
	return cat, reg
}

// unsupportedToolErr returns a standard error for an unknown tool name.
func unsupportedToolErr(name string) error {
	return fmt.Errorf("unsupported tool %q (run 'xdev available')", name)
}

func initContext(xmdeDir string, verbose, noColor, noPathFix bool) {
	dir := xmdeDir
	if dir == "" {
		dir = os.Getenv("XMDEV_HOME")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		// Config load failure is fatal; surface a friendly message.
		// Avoid importing ui/lipgloss here to keep init simple.
		os.Stderr.WriteString("xdev: failed to load config: " + err.Error() + "\n")
		os.Exit(2)
	}
	globalCtx.cfg = cfg
	globalCtx.xmdeDir = xmdeDir
	globalCtx.verbose = verbose
	globalCtx.noColor = noColor
	globalCtx.noPathFix = noPathFix
	globalCtx.initialized = true

	// Configure HTTP client proxy from config (fixes sniff failing behind proxy).
	remote.ConfigureHTTPClient(cfg.GetProxyURL("https"))

	// Configure GitHub API token from config file (env GITHUB_TOKEN/GH_TOKEN
	// is read by effectiveGitHubToken() automatically each request).
	if cfg.Sniff.GitHubToken != "" {
		remote.ConfigureGitHubToken(cfg.Sniff.GitHubToken)
	}
}

// ShutdownSniffScheduler shuts down the global sniff scheduler.
// Called from main.go after root.Execute() returns, so the
// startup-strategy background goroutine can persist partial results
// before process exit (SC-002).
//
// 第十一轮审计（Round 10 报告待办 6）：**不再吞掉 Shutdown 的错误**。
//
// Round 10 把 `Scheduler.Shutdown()` 的签名改成了 `error`（P2-1：读/写失败一律上报，
// 不再 `_ =` 吞掉），但这里的调用方仍然忽略返回值 —— 于是「Shutdown 刷缓存失败」
// 在 CLI 侧依然是**静默**的，用户看不到任何提示。这与 Round 10 修掉的
// `saveState` 吞错、以及本轮修掉的 `SniffAll` 吞错是同一形态的第三次出现。
//
// 这里返回 error 而不是在包内 log：调用方是 `main.go`，只有它知道该以什么方式
// 呈现（写 stderr / 影响退出码）。`main.go` 的现有契约是「退出码反映子进程真实
// 退出码」（票 33），因此**不得**因为一次收尾刷盘失败就改写退出码 —— 见那边的用法。
func ShutdownSniffScheduler() error {
	if sniffScheduler == nil {
		return nil
	}
	if err := sniffScheduler.Shutdown(); err != nil {
		return fmt.Errorf("shutdown sniff scheduler: %w", err)
	}
	return nil
}
