// Package cli wires up the Cobra command tree for xdev.
//
// The root command is created in NewRootCommand and every subcommand
// (install / use / list / etc.) is attached in the same place so that
// the executable can be reused as a "shim" — the same binary is called
// under aliases such as `python`, `node`, `go`, etc., and routes the
// invocation to the underlying real tool.
//
// # Entry Point Flow
//
//  1. main() calls NewRootCommand(name, desc)
//  2. shim.MaybeExec checks if the binary was invoked under a tool alias;
//     if so, it dispatches to the real tool and exits
//  3. Otherwise, the Cobra command tree runs normally
//  4. If no subcommand is specified, the TUI is launched by default
//
// # Global State
//
// The runCtx struct (context.go) holds the loaded config and flag values.
// It is initialized in the Cobra PersistentPreRun callback via initContext().
// Subcommands access it through the Cfg(), Verbose(), NoColor(), and
// NoPathFix() helper functions.
//
// # --no-path-fix Flag
//
// When --no-path-fix is set, the installer skips all registry writes
// (PATH modifications, env var writes, WM_SETTINGCHANGE broadcast).
// This is essential for CI/CD and integration tests that must not
// pollute the host environment.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/tui"
	"github.com/xmdemt/xdev/internal/version"
)

// NewRootCommand builds the full command tree.
func NewRootCommand(name, desc string) *cobra.Command {
	// shim entrypoint: if we were launched under an aliased name,
	// hand control over to the shim dispatcher and return.
	if handled, err := shim.MaybeExec(filepath.Base(os.Args[0])); handled {
		if err != nil {
			if exitErr, ok := err.(*shim.ExitCodeError); ok {
				os.Exit(exitErr.Code)
			}
			fmt.Fprintln(os.Stderr, "shim:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	root := &cobra.Command{
		Use:   name,
		Short: desc,
		Long: desc + `

xdev lets you install, switch and manage multiple versions of
programming language runtimes and development tools, with a unified CLI,
an interactive TUI, and a cross-platform GUI.

Quick start:
  xdev init
  xdev install python@3.12.0
  xdev use python@3.12.0
  xdev doctor

Running xdev without arguments launches the interactive TUI.

Short aliases you can add to your shell rc:
  alias xdev='xdev'`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
		Version:           version.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			// No subcommand specified — launch TUI by default
			cat, reg := loadCatalogAndRegistry()
			cfg := Cfg()
			p := tui.NewProgram(cfg, cat, reg)
			_, err := p.Run()
			return err
		},
	}

	// global flags
	var (
		flagXMDevDir  string
		flagVerbose   bool
		flagNoColor   bool
		flagNoPathFix bool
	)
	root.PersistentFlags().StringVar(&flagXMDevDir, "xmdevenv-dir", "",
		"override the xdev home directory (default: $XMDEV_HOME or ~/.xdev)")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose log output")
	root.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colored output")
	root.PersistentFlags().BoolVar(&flagNoPathFix, "no-path-fix", false,
		"do not auto-prompt to fix PATH issues during install/use")

	// bind persistent pre-run so every subcommand can rely on cfg being loaded
	cobra.OnInitialize(func() {
		initContext(flagXMDevDir, flagVerbose, flagNoColor, flagNoPathFix)
		// Trigger Scheduler.OnStartup per configured strategy (FR-006).
		// This runs on every command invocation but is idempotent —
		// interval checks last_sniff_at, startup launches async, etc.
		cfg := Cfg()
		s := discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)
		s.OnStartup(context.Background())
		sniffScheduler = s
	})

	// register subcommands
	root.AddCommand(
		newInitCmd(),
		newConfigCmd(),
		newProfileCmd(),
		newAvailableCmd(),
		newSearchCmd(),
		newListCmd(),
		newCurrentCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newUseCmd(),
		newUpgradeCmd(),
		newWhichCmd(),
		newEnvCmd(),
		newShellCmd(),
		newDoctorCmd(),
		newSelfCmd(),
		newTuiCmd(),
		newGuiCmd(),
		newRefreshCmd(),
		newKeygenCmd(),
		newSignManifestsCmd(),
		newFillSHA256Cmd(),
		newHookCmd(),
		newActivateCmd(),
		newDeactivateCmd(),
		// Environment isolation
		newIsolateCmd(),
		// Remote development
		newRemoteCmd(),
		// Plugin management
		newPluginCmd(),
		// Manifest management
		newManifestCmd(),
		// Version
		newVersionCmd(),
		// Shell completion
		newCompletionCmd(),
		// Tool discovery sniffing (011)
		newSniffCmd(),
		// Available updates (011)
		newUpdatesCmd(),
	)

	return root
}
