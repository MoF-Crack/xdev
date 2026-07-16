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
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

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
}
