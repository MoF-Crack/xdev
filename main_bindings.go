//go:build bindings

package main

import (
	"embed"
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

// Wails 3 discovers application.NewService registrations through static source
// analysis. Generating bindings does not initialise configuration or launch GUI.
func main() {}
