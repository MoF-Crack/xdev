//go:build !bindings

package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/cli"
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/gui"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/signer"
)

//go:embed manifests/*.yaml
var manifestFS embed.FS

//go:embed manifests/*.yaml.sig
var sigFS embed.FS

const (
	toolName = "xdev"
	toolDesc = "XDev - Development Environment Manager - " +
		"Cross-platform dev environment manager (CLI/TUI/GUI)"
)

func init() {
	manifest.InitEmbedded(manifestFS)
	signer.SetEmbeddedSignaturesFS(sigFS)
}

func main() {
	selfName := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")

	// If the binary is named "xdev-gui*" (e.g. xdev-gui.exe or
	// xdev-gui-1.0.0.exe), launch the GUI directly without going
	// through the CLI command tree.  This avoids the Cobra "no
	// subcommand" error that causes the window to flash and exit.
	if selfName == "xdev-gui" || strings.HasPrefix(selfName, "xdev-gui-") {
		runGUI()
		return
	}

	root := cli.NewRootCommand(toolName, toolDesc)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// runGUI launches the Wails GUI application directly.
func runGUI() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}
	cat, err := manifest.LoadCatalogCached(cfg.Core.ManifestDir, cfg.Core.CacheDir)
	if err != nil {
		cat = &manifest.Catalog{}
	}
	reg := cli.LoadRegistry(cat)
	app := cli.NewGUIApp(cfg, cat, reg)
	webviewDir := filepath.Join(cfg.Core.XMDevDir, "cache", "webview2")

	// Initialise the GUI logger.  If the app has already been
	// initialised (config.yaml exists), open the log file now.
	// Otherwise, log messages are buffered until InitConfig is
	// called from the frontend, at which point the log file is
	// created in the newly-set-up directory.
	if app.IsInitialized() {
		logDir := filepath.Join(cfg.Core.XMDevDir, "logs")
		gui.InitLogger(logDir)
	}
	defer gui.CloseLogger()

	if err := cli.RunGUI(app, webviewDir); err != nil {
		fmt.Fprintln(os.Stderr, "GUI error:", err)
		os.Exit(1)
	}
}
