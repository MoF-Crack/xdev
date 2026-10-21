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
	"github.com/xmdemt/xdev/internal/shim"
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

	// 第十二轮收口（Round 12 报告 §七-③）：GUI 是**长驻宿主**，在这里启动进程内
	// **唯一**的 `.stale-*` 残留清扫 goroutine（幂等，60s 一次）。
	//
	// 为什么只有 GUI 需要它：CLI 是短命进程，ticker 还没来得及触发进程就退出了；
	// 而 CLI 的卫生由 `Activate`/`Uninstall` 后的一次即时回收 + `removeLinkEntry`
	// 的**归档式改名**（把残留搬到 current/ 之外）共同保证 —— 后者让 `current/`
	// 根本不会再出现 `.stale-*`。GUI 可能连续驻留数天，且用户在其中反复
	// install/uninstall，因此需要后台兜底。
	shim.StartStaleResidueSweeper(cfg.Core.XMDevDir)
	defer shim.StopStaleResidueSweeper()

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
