// extra.go defines `xdev self` (update/uninstall/version), `xdev tui`,
// and `xdev gui` commands.
//
// # self update
//
// Checks for a newer version of xdev and applies the update if available.
// Returns a non-zero exit code if the update check itself fails (not just
// if no update is available).
//
// # self uninstall
//
// Removes all shims, cleans shell RC hooks, removes environment variable
// fragments, removes current/ symlinks, and optionally deletes the entire
// data directory. The user must manually delete the binary afterwards.
//
// # tui / gui
//
// Launch the terminal UI (Bubble Tea) or desktop GUI (Wails) respectively.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/gui"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/tui"
	"github.com/xmdemt/xdev/internal/update"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the xdev binary itself",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "update",
			Short: "Update xdev to the latest version",
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := context.Background()
				v := version.Version
				fmt.Printf("Current version: %s\n", v)
				fmt.Println("Checking for updates...")

				result, err := update.Check(ctx, v)
					if err != nil {
						fmt.Printf("Error checking for updates: %v\n", err)
						return fmt.Errorf("update check failed: %w", err)
					}

				if !result.HasUpdate {
					fmt.Printf("Already up to date (v%s)\n", result.LatestVersion)
					return nil
				}

				fmt.Printf("Update available: v%s -> v%s\n", result.CurrentVersion, result.LatestVersion)
				fmt.Println("Downloading update...")

				if err := update.Apply(ctx, result); err != nil {
					return fmt.Errorf("self update: %w", err)
				}

				fmt.Printf("Successfully updated to v%s! Please restart xdev.\n", result.LatestVersion)
				return nil
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Remove xdev and all its shims",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg := Cfg()

				// Invalidate the cross-process home cache so the
				// next invocation re-discovers the home directory.
				config.InvalidateHomeCache()

				// 1. Remove all shims
				fmt.Printf("Removing shims from %s...\n", cfg.Core.ShimDir)
				if err := os.RemoveAll(cfg.Core.ShimDir); err != nil {
					fmt.Printf("Warning: failed to remove shim directory: %v\n", err)
				}

				// 2. Remove shell RC injection
				fmt.Println("Cleaning up shell configuration...")
				shellInfo := env.DetectShell()
				if err := env.RemoveShellRC(shellInfo); err != nil {
					fmt.Printf("Warning: failed to clean shell RC: %v\n", err)
				}

				// 3. Remove env fragments
				fmt.Printf("Removing env fragments from %s...\n", cfg.Core.EnvDir)
				if err := os.RemoveAll(cfg.Core.EnvDir); err != nil {
					fmt.Printf("Warning: failed to remove env directory: %v\n", err)
				}

				// 4. Remove current symlinks
				currentDir := filepath.Join(cfg.Core.XMDevDir, "current")
				fmt.Printf("Removing current symlinks from %s...\n", currentDir)
				if err := os.RemoveAll(currentDir); err != nil {
					fmt.Printf("Warning: failed to remove current directory: %v\n", err)
				}

				// 5. Ask about the full data directory
				fmt.Printf("\nRemaining data in %s:\n", cfg.Core.XMDevDir)
				fmt.Println("  - install/   (installed tool versions)")
				fmt.Println("  - cache/     (downloaded archives)")
				fmt.Println("  - downloads/ (downloaded installers)")
				fmt.Println("  - config.yaml")
				fmt.Print("\nRemove all data? (y/N): ")

				var answer string
				fmt.Scanln(&answer)
				if answer == "y" || answer == "Y" {
					if err := os.RemoveAll(cfg.Core.XMDevDir); err != nil {
						fmt.Printf("Warning: failed to remove data directory: %v\n", err)
					} else {
						fmt.Printf("Removed %s\n", cfg.Core.XMDevDir)
					}
				}

				// 6. Remove the binary itself
				exe, err := os.Executable()
				if err != nil {
					fmt.Printf("Warning: could not locate binary: %v\n", err)
					return nil
				}
				fmt.Printf("\nTo complete uninstall, remove the binary:\n  %s\n", exe)
				fmt.Println("xdev has been uninstalled.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "version",
			Short: "Print the xdev version",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Printf("xdev %s (XDev - Development Environment Manager)\n", version.Version)
				return nil
			},
		},
	)
	return cmd
}

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, reg := loadCatalogAndRegistry()
			cfg := Cfg()
			p := tui.NewProgram(cfg, cat, reg)
			_, err := p.Run()
			return err
		},
	}
}

func newGuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gui",
		Short: "Launch the cross-platform desktop GUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, reg := loadCatalogAndRegistry()
			cfg := Cfg()
			app := gui.NewApp(cfg, cat, reg)
			webviewDir := filepath.Join(cfg.Core.XMDevDir, "cache", "webview2")
			return gui.Run(app, webviewDir)
		},
	}
}

// NewGUIApp creates a GUI App instance.  Exported so main.go can
// use it when launching the GUI directly (binary named xdev-gui*).
func NewGUIApp(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry) *gui.App {
	return gui.NewApp(cfg, cat, reg)
}

// RunGUI starts the Wails GUI application.  Exported so main.go can
// use it when launching the GUI directly.
func RunGUI(app *gui.App, webviewDir string) error {
	return gui.Run(app, webviewDir)
}

// forwardExecResult writes the stdout/stderr of an ExecResult to the
// corresponding process streams.
func forwardExecResult(result *util.ExecResult) {
	if result.Stdout != "" {
		os.Stdout.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		os.Stderr.WriteString(result.Stderr)
	}
}

// warnOllamaUnavailable prints the standard warning when the Ollama
// AI backend is not running.
func warnOllamaUnavailable() {
	fmt.Println("⚠ Ollama is not running. Start it with: ollama serve")
	fmt.Println("  Install Ollama: https://ollama.com")
}
