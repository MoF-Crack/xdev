//go:build !bindings

package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/gui"
)

func guiStartupConfig(t *testing.T) *config.Config {
	t.Helper()
	home := t.TempDir()
	return &config.Config{Core: config.CoreConfig{XMDevDir: home, ManifestDir: filepath.Join(home, "manifests"), CacheDir: filepath.Join(home, "cache"), ShimDir: filepath.Join(home, "shims"), InstallDir: filepath.Join(home, "install")}}
}

func TestGUIStartupLogsBeforeConsoleDetachAndRestoresCaller(t *testing.T) {
	cfg := guiStartupConfig(t)
	original := log.Writer()
	var caller bytes.Buffer
	log.SetOutput(&caller)
	t.Cleanup(func() { log.SetOutput(original) })
	detached, ran := false, false
	err := runGUIWithRuntime(cfg, true, guiStartupRuntime{
		detach: func() error {
			detached = true
			path := filepath.Join(cfg.Core.XMDevDir, "logs", "xdev-gui.log")
			if gui.LogPath() != path {
				t.Fatalf("logger is not ready before console detach: %q", gui.LogPath())
			}
			log.Print("startup-before-detach-marker")
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), "startup-before-detach-marker") {
				t.Fatalf("early log was lost: %s %v", data, err)
			}
			return nil
		},
		run: func(_ *gui.App, webview string) error {
			ran = true
			if !detached || webview != filepath.Join(cfg.Core.XMDevDir, "cache", "webview2") {
				t.Fatalf("startup inputs: detached=%v path=%q", detached, webview)
			}
			data, err := os.ReadFile(filepath.Join(cfg.Core.XMDevDir, "logs", "xdev-gui.log"))
			if err != nil || !strings.Contains(string(data), "stale-residue sweeper started") {
				t.Fatalf("sweeper log did not reach GUI log: %s %v", data, err)
			}
			return nil
		},
		report: func(err error) { t.Fatalf("successful GUI reported %v", err) },
	})
	if err != nil || !ran {
		t.Fatalf("run=%v error=%v", ran, err)
	}
	if caller.Len() != 0 {
		t.Fatalf("GUI leaked logs into original terminal: %s", caller.String())
	}
	if log.Writer() != &caller {
		t.Fatal("GUI startup did not restore caller logger")
	}
	log.Print("caller-after-GUI-marker")
	if !strings.Contains(caller.String(), "caller-after-GUI-marker") {
		t.Fatal("caller logging broken after GUI closed")
	}
}

func TestGUIStartupReportsRuntimeErrorWhileLogIsOpen(t *testing.T) {
	cfg := guiStartupConfig(t)
	expected := errors.New("fixture WebView unavailable")
	reports := 0
	err := runGUIWithRuntime(cfg, true, guiStartupRuntime{
		detach: func() error { return nil },
		run:    func(*gui.App, string) error { return expected },
		report: func(err error) {
			reports++
			if !errors.Is(err, expected) {
				t.Fatalf("wrong report: %v", err)
			}
			data, readErr := os.ReadFile(filepath.Join(cfg.Core.XMDevDir, "logs", "xdev-gui.log"))
			if readErr != nil || !strings.Contains(string(data), expected.Error()) {
				t.Fatalf("runtime error lost before visible report: %s %v", data, readErr)
			}
		},
	})
	if !errors.Is(err, expected) || reports != 1 {
		t.Fatalf("runtime error=%v reports=%d", err, reports)
	}
}

func TestGUIStartupDetachFailureDoesNotPreventDesktop(t *testing.T) {
	cfg := guiStartupConfig(t)
	ran := false
	err := runGUIWithRuntime(cfg, true, guiStartupRuntime{
		detach: func() error { return errors.New("fixture detach failure") },
		run:    func(*gui.App, string) error { ran = true; return nil },
		report: func(err error) { t.Fatalf("nonfatal detach reported startup error: %v", err) },
	})
	data, _ := os.ReadFile(filepath.Join(cfg.Core.XMDevDir, "logs", "xdev-gui.log"))
	if err != nil || !ran || !strings.Contains(string(data), "fixture detach failure") {
		t.Fatalf("detach outcome ran=%v err=%v log=%s", ran, err, data)
	}
}
