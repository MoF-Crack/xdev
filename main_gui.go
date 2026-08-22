//go:build gui && !bindings

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

func init() {
	manifest.InitEmbedded(manifestFS)
	signer.SetEmbeddedSignaturesFS(sigFS)
}

func main() {
	selfName := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if selfName == "xdev-gui" || strings.HasPrefix(selfName, "xdev-gui-") ||
		selfName == "xdev" || strings.HasPrefix(selfName, "xdev-") {
		runGUI()
		return
	}
	// Unexpected basename: still launch GUI rather than a shim (this
	// binary is the desktop adapter, not the CLI dispatcher).
	runGUI()
}

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
	app := gui.NewApp(cfg, cat, reg)
	webviewDir := filepath.Join(cfg.Core.XMDevDir, "cache", "webview2")

	if app.IsInitialized() {
		logDir := filepath.Join(cfg.Core.XMDevDir, "logs")
		gui.InitLogger(logDir)
	}
	defer gui.CloseLogger()

	if err := gui.Run(app, webviewDir); err != nil {
		fmt.Fprintln(os.Stderr, "GUI error:", err)
		os.Exit(1)
	}
}
