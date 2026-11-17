package gui

import (
	"context"
	"fmt"
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"sync"
)

// desktopRuntime is the desktop-only seam. Business operations use their own
// contexts; emitting an event never requires a framework context or a live UI.
type desktopRuntime interface {
	Emit(name string, data any)
	Show()
	ChooseDirectory(title string) (string, error)
	ChooseFile(title, filters string) (string, error)
	Confirm(ctx context.Context, title, message string) (bool, error)
}

// RunOptions carries policies already resolved by the command entry point.
type RunOptions struct{ NoEnvWrite bool }

func NewAppWithOptions(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry, opts RunOptions) *App {
	app := NewApp(cfg, cat, reg)
	app.noEnvWrite = opts.NoEnvWrite
	return app
}

func (a *App) emitEvent(name string, data any) {
	if a.desktop != nil {
		a.desktop.Emit(name, data)
	}
}

func (a *App) confirmInstall(ctx context.Context, tool, version string) (bool, error) {
	if a.desktop == nil {
		return false, fmt.Errorf("installation confirmation requires a desktop session")
	}
	return a.desktop.Confirm(ctx, "Unverified Version", fmt.Sprintf("Version %s@%s was discovered by sniffing.\n\nThe archive URL has not been verified — downloads may fail.\n\nContinue with installation?", tool, version))
}

func (a *App) chooseDirectory(title string) (string, error) {
	if a.desktop == nil {
		return "", fmt.Errorf("directory selection requires a desktop session")
	}
	return a.desktop.ChooseDirectory(title)
}

func (a *App) chooseFile(title, filters string) (string, error) {
	if a.desktop == nil {
		return "", fmt.Errorf("file selection requires a desktop session")
	}
	return a.desktop.ChooseFile(title, filters)
}

// Native dialogs can complete synchronously (Windows) or later (macOS).
// The buffered one-shot response works for both and shutdown releases waiters.
func awaitDesktopConfirmation(ctx context.Context, show func(func(bool))) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	answers := make(chan bool, 1)
	var once sync.Once
	// Show can itself block on Windows; run it off the calling worker so the
	// session can still be cancelled while the native modal remains open.
	go show(func(value bool) { once.Do(func() { answers <- value }) })
	select {
	case answer := <-answers:
		return answer, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
