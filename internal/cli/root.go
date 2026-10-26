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
			// Spec 034: 交互入口启动自检（静默降级，不阻塞）
			if err := runSelfCheck(); err != nil {
				return err
			}
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
		// 票 34（P1-30 / E-11）：此处只构建 Scheduler 句柄（供
		// ShutdownSniffScheduler 刷缓存，startupCancel==nil 时 Shutdown 秒退），
		// 不再无条件调用 OnStartup——`xdev hook`/`xdev env`/一次性子命令
		// 此前也会 spawn 后台嗅探。启动嗅探改由三个交互入口经
		// PersistentPreRunE 显式挂载（见 mountStartupSniff / isInteractiveEntry）。
		cfg := Cfg()
		sniffScheduler = discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)
	})

	// 票 34（P1-30 / E-11）：startup / interval 策略的启动嗅探只挂交互入口
	// （无参 TUI = root 自身、xdev tui、xdev gui）。用命令身份判定而不是在
	// 各 RunE 里逐点插入，使「恰好三入口」在单一位置可见（守卫测试
	// startup_sniff_test.go 直接断言本函数与命令名清单）。
	// 零参 GUI（main.go 的 len(os.Args)<=1 → runGUI）不走 Cobra，保持无嗅探。
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if isInteractiveEntry(cmd) {
			mountStartupSniff()
		}
		return nil
	}

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
		// Package managers (2026-10-25：定义已数据化到
		// manifests/package-managers.yaml，CLI 与 GUI 共享同一数据源)
		newPMCmd(),
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

// ---------------------------------------------------------------------------
// 票 34（P1-30 / E-11）：启动嗅探的三入口挂载
// ---------------------------------------------------------------------------

// interactiveEntryNames 是「交互入口」子命令名清单。无参调用（root 自身
// 的 RunE，默认 TUI）由 isInteractiveEntry 的 HasParent()==false 分支覆盖，
// 不在此列表中。
var interactiveEntryNames = []string{"tui", "gui"}

// isInteractiveEntry reports whether the command being executed is one of the
// three interactive entry points (root TUI / `xdev tui` / `xdev gui`).
// Everything else — including the cd-hook path (`xdev hook`, `xdev env`) and
// every one-shot subcommand — must not trigger a startup sniff.
func isInteractiveEntry(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if !cmd.HasParent() {
		return true // 无子命令：root 的 RunE 启动 TUI
	}
	name := cmd.Name()
	for _, n := range interactiveEntryNames {
		if name == n {
			return true
		}
	}
	return false
}

// startupSniffMounted guards against a second mount when a command tree is
// executed more than once in the same process (tests / embedding).
var startupSniffMounted bool

// mountStartupSniff triggers Scheduler.OnStartup for the configured strategy.
// Only called from an interactive entry (idempotent).
func mountStartupSniff() {
	if sniffScheduler == nil || startupSniffMounted {
		return
	}
	startupSniffMounted = true
	sniffScheduler.OnStartup(context.Background())
}

// HasStartupSniff reports whether this process launched (or is about to
// launch) a background startup sniff, so main.go can replace its
// unconditional 100ms wait.
//
// 票 34（P1-30 / E-11）：语义 = 「交互入口已挂载」∧「策略条件成立」。
// interval 策略的 TTL 到期条件由 Scheduler.StartupSniffPending 计入
// （与 onStartupInterval 的实际 spawn 判定共用 intervalElapsed）；
// 缓存新鲜、strategy=manual/service、以及 `xdev hook`/`xdev env` 等非交互
// 命令都返回 false（一次性子命令不再付出 100ms）。
//
// 注意：调用方须在 ShutdownSniffScheduler 之前取本值——Shutdown 会把
// sniff-cache 的 last_sniff_at 刷成 now，令 interval 判定失真。
func HasStartupSniff() bool {
	if sniffScheduler == nil || !startupSniffMounted {
		return false
	}
	return sniffScheduler.StartupSniffPending()
}
