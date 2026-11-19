package cli

import (
	"errors"
	"github.com/xmdemt/xdev/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGUIEntrypointLeavesStartupSelfUpdateToDesktop(t *testing.T) {
	oldContext, oldLegacy, oldConfigured := globalCtx, guiLauncher, configuredGUILauncher
	t.Cleanup(func() { globalCtx = oldContext; guiLauncher = oldLegacy; configuredGUILauncher = oldConfigured })
	cache := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: t.TempDir(), CacheDir: cache, SelfUpdate: true, SelfUpdateMode: "prompt"}}
	globalCtx = runCtx{cfg: cfg, initialized: true, noPathFix: true}
	// A previous check older than 24 hours would cause the old GUI CLI path to
	// consume the record before starting the actual desktop.
	checkFile := filepath.Join(cache, "self_update_last_check.json")
	oldRecord := []byte(`{"checkedAt":"` + time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339) + `"}`)
	if err := os.WriteFile(checkFile, oldRecord, 0o600); err != nil {
		t.Fatal(err)
	}
	SetConfiguredGUILauncher(func(*config.Config, bool) error {
		got, err := os.ReadFile(checkFile)
		if err != nil || string(got) != string(oldRecord) {
			t.Fatalf("CLI consumed desktop startup update: %s %v", got, err)
		}
		return nil
	})
	if err := newGuiCmd().RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredGUILauncherPreservesResolvedContext(t *testing.T) {
	oldContext, oldLegacy, oldConfigured := globalCtx, guiLauncher, configuredGUILauncher
	t.Cleanup(func() { globalCtx = oldContext; guiLauncher = oldLegacy; configuredGUILauncher = oldConfigured })
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: t.TempDir(), SelfUpdate: false}}
	globalCtx = runCtx{cfg: cfg, initialized: true, noPathFix: true}
	wantErr := errors.New("launch failed")
	SetConfiguredGUILauncher(func(got *config.Config, noWrite bool) error {
		if got != cfg || got.Core.XMDevDir != cfg.Core.XMDevDir {
			t.Fatal("GUI reloaded or lost the explicit home")
		}
		if !noWrite {
			t.Fatal("GUI lost no-path-fix write policy")
		}
		return wantErr
	})
	if err := newGuiCmd().RunE(nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("launch error = %v", err)
	}
}
