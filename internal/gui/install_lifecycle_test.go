package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

type waitingConfirmDesktop struct {
	desktopRuntime
	opened chan struct{}
}

func (d *waitingConfirmDesktop) Emit(string, any) {}
func (d *waitingConfirmDesktop) Confirm(ctx context.Context, _, _ string) (bool, error) {
	close(d.opened)
	<-ctx.Done()
	return false, ctx.Err()
}
func TestInstallSessionCancellationInterruptsConfirmation(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: home, CacheDir: filepath.Join(home, "cache"), InstallDir: filepath.Join(home, "install")}}
	reg := plugin.NewRegistry()
	tool := &manifest.Tool{Name: "confirmation", Latest: "1.0.0", VersionList: []manifest.Version{{Version: "1.0.0", Unverified: true}}}
	reg.Register(&plugin.Generic{M: tool})
	app := NewAppWithOptions(cfg, nil, reg, RunOptions{NoEnvWrite: true})
	app.ctx = context.Background()
	desktop := &waitingConfirmDesktop{opened: make(chan struct{})}
	app.desktop = desktop
	id, err := app.StartInstall(InstallSpec{Tool: tool.Name, Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-desktop.opened:
	case <-time.After(time.Second):
		t.Fatal("confirmation not opened")
	}
	if err := app.CancelInstall(id); err != nil {
		t.Fatal(err)
	}
	app.installSessions.mu.Lock()
	s := app.installSessions.sessions[id]
	app.installSessions.mu.Unlock()
	awaitInstall(t, s)
	view, err := app.GetInstallSession(id)
	if err != nil || view.Status != "cancelled" {
		t.Fatalf("cancel outcome: %+v %v", view, err)
	}
	if _, err := os.Stat(cfg.Core.InstallDir); !os.IsNotExist(err) {
		t.Fatalf("cancelled confirmation wrote install directory: %v", err)
	}
}

func TestPackageManagerCommandObservesApplicationContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{rootCtx: ctx, rootCancel: cancel}
	command := "sleep 30"
	executionEnv := "bash"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
		executionEnv = "powershell"
	}
	done := make(chan error, 1)
	go func() {
		done <- app.installPackageManagerViaScriptContext(ctx, pmDef{name: "fixture", displayName: "Fixture", platform: runtime.GOOS, executionEnv: executionEnv, installCommand: command})
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled process reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process ignored cancellation")
	}
}
func TestInstallShutdownWaitsForWorkers(t *testing.T) {
	app := &App{}
	root, cancel := context.WithCancel(context.Background())
	app.rootCtx = root
	app.rootCancel = cancel
	s, err := app.launchInstallSession(InstallSpec{Tool: "fixture"}, "fixture", func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	app.shutdown(root)
	select {
	case <-s.done:
		if !errors.Is(s.err, context.Canceled) {
			t.Fatal(s.err)
		}
	default:
		t.Fatal("shutdown returned before worker cleanup")
	}
}
