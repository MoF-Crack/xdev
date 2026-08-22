//go:build !bindings && !gui

// Package main is the CLI/TUI/shim entry (Spec 030). The default
// `go build` does not import internal/gui or Wails. GUI lives in
// main_gui.go (tag gui) and main_bindings.go (tag bindings).
package main

import (
	"embed"
	"fmt"
	"os"
	"time"

	"github.com/xmdemt/xdev/internal/cli"
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
	root := cli.NewRootCommand(toolName, toolDesc)

	if err := root.Execute(); err != nil {
		cli.ShutdownSniffScheduler()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cli.ShutdownSniffScheduler()
	time.Sleep(100 * time.Millisecond)
}
