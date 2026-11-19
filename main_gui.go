//go:build !bindings

// Package main（单二进制改造）：GUI 启动器，供 main.go 无参数分支调用。
// 不再有独立 xdev-gui 二进制——GUI/TUI/CLI 共用 main.go 入口。
package main

import (
	"fmt"
	"log"
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
	if err := runGUIWithConfig(cfg, false); err != nil {
		fmt.Fprintln(os.Stderr, "GUI error:", err)
		os.Exit(1)
	}
}

// runGUIWithConfig preserves the command's explicit home and environment policy.
func runGUIWithConfig(cfg *config.Config, noEnvWrite bool) error {
	return runGUIWithRuntime(cfg, noEnvWrite, guiStartupRuntime{
		run:    gui.Run,
		detach: detachGUIConsole,
		report: reportGUIStartupError,
	})
}

// Dependencies are private so startup ordering can be tested without detaching
// the test runner's console or starting a native WebView.
type guiStartupRuntime struct {
	run    func(*gui.App, string) error
	detach func() error
	report func(error)
}

func runGUIWithRuntime(cfg *config.Config, noEnvWrite bool, desktop guiStartupRuntime) (runErr error) {
	if cfg == nil || cfg.Core.XMDevDir == "" {
		return fmt.Errorf("cannot start GUI without a resolved XDev home")
	}

	// Logging must be ready before detaching the Windows console or starting
	// background work. First-run setup needs startup diagnostics as well.
	previousLogOutput := log.Writer()
	gui.InitLogger(filepath.Join(cfg.Core.XMDevDir, "logs"))
	defer func() {
		gui.CloseLogger()
		log.SetOutput(previousLogOutput)
	}()
	// Maintenance builds use a no-op here so their missing-runtime error still
	// reaches the caller's terminal. CLI/TUI/shim never enter this function.
	if err := desktop.detach(); err != nil {
		log.Printf("[gui] could not detach the GUI console: %v", err)
	}
	defer func() {
		if runErr != nil {
			log.Printf("[gui] startup failed: %v", runErr)
			desktop.report(runErr)
		}
	}()

	cat, err := manifest.LoadCatalogCached(cfg.Core.ManifestDir, cfg.Core.CacheDir)
	if err != nil {
		cat = &manifest.Catalog{}
	}
	reg := cli.LoadRegistry(cat)
	app := gui.NewAppWithOptions(cfg, cat, reg, gui.RunOptions{NoEnvWrite: noEnvWrite})
	webviewDir := filepath.Join(cfg.Core.XMDevDir, "cache", "webview2")

	// A long-running GUI owns one sweeper; short CLI invocations reclaim stale
	// links during activation/uninstall instead. Stop it before closing the log.
	shim.StartStaleResidueSweeper(cfg.Core.XMDevDir)
	defer shim.StopStaleResidueSweeper()

	return desktop.run(app, webviewDir)
}
