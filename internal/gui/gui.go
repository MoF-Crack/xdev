// Package gui implements the Wails-based desktop GUI for xdev.
//
// The GUI is built with Wails v2 (Go backend) + Vue 3 (frontend).
// Communication between Go and JavaScript happens via:
//   - Wails bindings: exported methods on the App struct
//   - Wails events: real-time updates (install progress, etc.)
//
// # Startup
//
// The Run() function creates a Wails application with:
//   - Custom dark theme (Naive UI integration)
//   - Frameless window with custom title bar
//   - Single-instance enforcement (prevents multiple GUI windows)
//   - Parent process monitoring (auto-closes if launcher dies)
//
// Platform-specific process helpers live in gui_proc_windows.go /
// gui_proc_other.go so non-Windows CI can compile the package.
package gui

import (
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/xmdemt/xdev/frontend"
)

// Run starts the Wails GUI application.
// webviewDir is the directory where WebView2 stores its user data.
// If empty, WebView2 uses its default location.
func Run(app *App, webviewDir string) error {
	logInfo("[Run] GUI starting, webviewDir=%s", webviewDir)

	// Ensure the WebView2 data directory exists before launching.
	if webviewDir != "" {
		if err := os.MkdirAll(webviewDir, 0o755); err != nil {
			logError("[Run] failed to create webviewDir %s: %v", webviewDir, err)
		} else {
			logInfo("[Run] webviewDir ensured: %s", webviewDir)
		}
	}

	// Kill stale WebView2 orphan processes *before* Wails starts,
	// so they don't appear as ghost windows during startup.
	prewarmWebView2()

	logInfo("[Run] calling wails.Run() with StartHidden=true, Width=1024, Height=768")
	start := time.Now()

	err := wails.Run(&options.App{
		Title:            "XDev - Development Environment Manager",
		Width:            1024,
		Height:           768,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 10, G: 22, B: 40, A: 255},
		AssetServer: &assetserver.Options{
			Assets: frontend.DistFS,
		},
		OnStartup:  app.startup,
		OnDomReady: app.domReady,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		// StartHidden: true hides the main window during WebView2
		// initialisation.  Without this, WebView2's child processes
		// (GPU, Renderer, Network) briefly flash visible 800x1000
		// windows on screen.  We show the window in domReady after
		// the Hide+Show hack has completed.
		StartHidden: true,
		// Frameless removes the native Windows title bar so we can
		// use a custom title bar that matches the dark theme and
		// prevents double-click-to-close bugs on the native title bar.
		Frameless: true,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			WebviewUserDataPath:  webviewDir,
		},
	})

	if err != nil {
		logError("[Run] wails.Run() returned error after %v: %v", time.Since(start), err)
	} else {
		logInfo("[Run] wails.Run() exited normally after %v", time.Since(start))
	}

	return err
}
