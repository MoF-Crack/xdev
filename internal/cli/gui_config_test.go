package cli

import (
	"errors"
	"github.com/xmdemt/xdev/internal/config"
	"testing"
)

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
