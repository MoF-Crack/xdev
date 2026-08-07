// app_initconfig_test.go contains the F-18 deadlock regression test:
// InitConfig must not call a.getCfg() while holding the cfgMu write lock.
package gui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
)

// TestInitConfig_NoDeadlock guards against F-18: InitConfig held the cfgMu
// write lock and then called a.getCfg() (which takes an RLock). RWMutex is
// not reentrant, so first-run setup deadlocked. The fix uses a.cfg directly
// inside the locked section. A deadlock would hang InitConfig forever, so we
// run it in a goroutine with a timeout guard.
func TestInitConfig_NoDeadlock(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: tmp}}
	app := &App{cfg: cfg}

	// InitConfig 会调用 InitLogger 打开 <tmp>/logs/xdev-gui.log，
	// 测试结束前必须关闭日志句柄，否则 TempDir 清理会失败（句柄占用）。
	t.Cleanup(CloseLogger)

	// InitConfig 会把 <tmp>/current/xdev 追加到真实用户 PATH（Windows），
	// 测试结束前撤销该副作用。
	xdevBinDir := filepath.Join(tmp, "current", "xdev")
	t.Cleanup(func() { _ = env.RemoveFromUserPath(xdevBinDir) })

	done := make(chan error, 1)
	go func() { done <- app.InitConfig(tmp) }()

	select {
	case err := <-done:
		// InitConfig 返回即证明未死锁；随后 getCfg() 也必须立即可用。
		if err != nil {
			t.Logf("InitConfig returned error (not a deadlock): %v", err)
		}
		_ = app.getCfg()
	case <-time.After(15 * time.Second):
		t.Fatal("InitConfig deadlocked (cfgMu write lock + getCfg RLock)")
	}
}
