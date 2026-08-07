// app_cfg_snapshot_test.go tests the config snapshot access model
// (Spec 021 D17 / F-19): getCfg() returns an independent copy, so reader
// mutations cannot corrupt the live config, and writer flows persist.
package gui

import (
	"context"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// TestCfgSnapshotIndependence verifies getCfg() returns a deep copy:
// mutating the snapshot (nested fields, maps, slices) leaves the live
// config untouched.
func TestCfgSnapshotIndependence(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:         base,
			MirrorPreference: []string{"https://primary.example"},
		},
		Mirror: map[string]string{"go": "https://goproxy.cn"},
	}
	app := &App{cfg: cfg}

	snap := app.getCfg()
	snap.Core.XMDevDir = "/mutated"
	snap.Core.MirrorPreference[0] = "https://mutated.example"
	snap.Mirror["go"] = "mutated"

	if app.cfg.Core.XMDevDir != base {
		t.Errorf("live XMDevDir was mutated via snapshot: %q", app.cfg.Core.XMDevDir)
	}
	if app.cfg.Core.MirrorPreference[0] != "https://primary.example" {
		t.Errorf("live MirrorPreference was mutated via snapshot: %v", app.cfg.Core.MirrorPreference)
	}
	if app.cfg.Mirror["go"] != "https://goproxy.cn" {
		t.Errorf("live Mirror was mutated via snapshot: %v", app.cfg.Mirror)
	}

	// 第二次获取的快照与第一次相互独立。
	snap2 := app.getCfg()
	snap2.Mirror["go"] = "second"
	if app.cfg.Mirror["go"] != "https://goproxy.cn" {
		t.Errorf("live Mirror was mutated via second snapshot: %v", app.cfg.Mirror)
	}
	if snap.Mirror["go"] == "second" {
		t.Error("snapshots must be independent of each other")
	}
}

// TestMutateCfgPersists verifies mutateCfg operates on the live config:
// changes made inside the callback are visible to subsequent getCfg().
func TestMutateCfgPersists(t *testing.T) {
	base := t.TempDir()
	app := &App{cfg: &config.Config{Core: config.CoreConfig{XMDevDir: base}}}

	if err := app.mutateCfg(func(cfg *config.Config) error {
		cfg.Core.ArchiveRetention = "30d"
		return nil
	}); err != nil {
		t.Fatalf("mutateCfg failed: %v", err)
	}

	if got := app.getCfg().GetArchiveRetention(); got != "30d" {
		t.Errorf("GetArchiveRetention() = %q after mutateCfg, want %q", got, "30d")
	}
}

// TestRootContextCancel verifies the app root context (F-20 / D18):
// rootContext() is usable before startup (falls back to Background), and
// cancelling the root context propagates to Done() so background tasks
// (install polling, delayed refresh) stop promptly on shutdown.
func TestRootContextCancel(t *testing.T) {
	app := &App{}

	// 未初始化（单元测试场景）：退回 Background，不可取消但可安全使用。
	if app.rootContext() == nil {
		t.Fatal("rootContext() should never be nil")
	}

	// 模拟 startup 初始化根上下文。
	app.rootCtx, app.rootCancel = context.WithCancel(context.Background())
	if app.rootContext().Err() != nil {
		t.Fatal("root context should start not-cancelled")
	}

	// shutdown 语义：取消后 Done() 立即关闭。
	app.rootCancel()
	select {
	case <-app.rootContext().Done():
		// OK
	default:
		t.Fatal("root context Done() should be closed after cancel")
	}
}
