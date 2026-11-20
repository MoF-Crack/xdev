//go:build desktop

// Package gui adapts the XDev application to the Wails 3 desktop runtime.
package gui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	buildassets "github.com/xmdemt/xdev/build"
	"github.com/xmdemt/xdev/frontend"
)

type wailsDesktop struct {
	app    *application.App
	window *application.WebviewWindow
}

func (d *wailsDesktop) Emit(name string, data any) { d.app.Event.Emit(name, data) }
func (d *wailsDesktop) Quit()                      { d.app.Quit() }
func (d *wailsDesktop) Show() {
	if d.window.IsMinimised() {
		d.window.UnMinimise()
	}
	d.window.Show()
}
func (d *wailsDesktop) ChooseDirectory(title string) (string, error) {
	return d.app.Dialog.OpenFile().SetTitle(title).CanChooseDirectories(true).CanChooseFiles(false).AttachToWindow(d.window).PromptForSingleSelection()
}
func (d *wailsDesktop) ChooseFile(title, filters string) (string, error) {
	dialog := d.app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(true).CanChooseDirectories(false).AttachToWindow(d.window)
	if filters != "" {
		dialog.AddFilter("Files", filters)
	}
	return dialog.PromptForSingleSelection()
}
func (d *wailsDesktop) Confirm(ctx context.Context, title, message string) (bool, error) {
	return awaitDesktopConfirmation(ctx, func(answer func(bool)) {
		dialog := d.app.Dialog.Warning().SetTitle(title).SetMessage(message).AttachToWindow(d.window)
		dialog.AddButton("Yes").OnClick(func() { answer(true) })
		dialog.AddButton("No").SetAsDefault().SetAsCancel().OnClick(func() { answer(false) })
		dialog.Show()
	})
}

// Run initialises the native runtime only after the entry point has selected
// GUI mode. CLI, TUI and managed-tool dispatch never call application.New.
func Run(app *App, webviewDir string) error {
	if webviewDir != "" {
		if err := os.MkdirAll(webviewDir, 0o755); err != nil {
			return fmt.Errorf("create WebView data directory: %w", err)
		}
	}
	assets, err := fs.Sub(frontend.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("frontend assets: %w", err)
	}
	native := application.New(application.Options{
		Name:        "XDev",
		Icon:        buildassets.AppIcon,
		Description: "Development Environment Manager",
		Services:    []application.Service{application.NewService(app)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Windows:     application.WindowsOptions{WebviewUserDataPath: webviewDir},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	window := native.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "XDev - Development Environment Manager",
		Width: 1440, Height: 900, MinWidth: 800, MinHeight: 600,
		BackgroundColour: application.RGBA{Red: 16, Green: 21, Blue: 34, Alpha: 255},
		Frameless:        true, Hidden: true,
		Linux: application.LinuxWindow{Icon: buildassets.AppIcon},
	})
	app.desktop = &wailsDesktop{app: native, window: window}
	var ready sync.Once
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		ready.Do(func() { app.domReady(app.rootContext()) })
	})
	return native.Run()
}

// ServiceStartup and ServiceShutdown are framework lifecycle hooks, excluded
// from generated callable bindings by Wails. They own background cancellation.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}
func (a *App) ServiceShutdown() error {
	a.shutdown(a.rootContext())
	return nil
}
