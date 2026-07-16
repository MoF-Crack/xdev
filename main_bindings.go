//go:build bindings

package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/xmdemt/xdev/frontend"
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
	// In bindings mode, Wails compiles the binary with -tags bindings
	// and runs it to generate frontend binding files.  We need to
	// provide the same Bind list that the production GUI uses so that
	// all exported methods are discovered by the binding generator.
	cfg, _ := config.Load("")
	if cfg == nil {
		cfg = &config.Config{}
	}
	cat, _ := manifest.LoadCatalogCached(cfg.Core.ManifestDir, cfg.Core.CacheDir)
	if cat == nil {
		cat = &manifest.Catalog{}
	}
	app := gui.NewApp(cfg, cat, nil)

	err := wails.Run(&options.App{
		AssetServer: &assetserver.Options{
			Assets: frontend.DistFS,
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		panic(err)
	}
}
