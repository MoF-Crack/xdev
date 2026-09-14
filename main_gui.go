//go:build !bindings

// Package main（单二进制改造）：GUI 启动器，供 main.go 无参数分支调用。
// 不再有独立 xdev-gui 二进制——GUI/TUI/CLI 共用 main.go 入口。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xmdemt/xdev/internal/cli"
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/gui"
	"github.com/xmdemt/xdev/internal/manifest"
)

// runGUI 启动桌面 GUI（Wails）。embed manifests/signer 初始化在 main.go。
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
