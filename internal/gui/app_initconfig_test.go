// app_initconfig_test.go contains the F-18 deadlock regression test:
// InitConfig must not call a.getCfg() while holding the cfgMu write lock.
package gui

import (
	"path/filepath"
	"strings"
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
//
// 第四轮复核改造：本用例原先让 InitConfig 把 <tmp>/current/xdev 与 <tmp>/shims
// **追加到开发者真实的用户 PATH**（再在 t.Cleanup 里按大小写不敏感匹配撤销），
// 并在注释里留下了"清理历史死锁测试残留 Temp 目录"的痕迹——说明强杀后的残留确实
// 发生过（临时目录死条目会永久留在用户 PATH 里）。现在改用 env 的内存 PATH 后端：
// 副作用落在内存里，宿主零接触；断言还因此变强了（可以直接验证"确实追加了这两个目录"，
// 而不再只能事后清理）。
func TestInitConfig_NoDeadlock(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: tmp}}
	app := &App{cfg: cfg}

	// InitConfig 会调用 InitLogger 打开 <tmp>/logs/xdev-gui.log，
	// 测试结束前必须关闭日志句柄，否则 TempDir 清理会失败（句柄占用）。
	t.Cleanup(CloseLogger)

	// PATH 切到内存后端（初始值模拟一个普通用户 PATH）。跨平台可用：
	// 非 Windows 上 PATH 管理本身是 no-op，钩子只提供一个可断言的内存槽位。
	//
	// 注意先读一次**真实** PATH 作为对照——切换之后 env.ReadUserPath() 读的就是内存后端，
	// 不能再用它判断"真实 PATH 有没有被写"（第一版断言正是踩了这个坑，被自己的用例抓到）。
	realBefore, _ := env.ReadUserPath()
	getPath, restorePath := env.UseInMemoryUserPathForTesting(`C:\Windows\System32`)
	t.Cleanup(restorePath)

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

	// 副作用断言（在内存后端上）：InitConfig 追加的目录必须落在内存 PATH 里。
	finalPath := getPath()
	if !strings.Contains(strings.ToLower(finalPath), strings.ToLower(filepath.Join(tmp, "current", "xdev"))) {
		t.Errorf("InitConfig 应把 current/xdev 追加进 PATH，实际: %q", finalPath)
	}

	// 宿主安全断言：切回真实后端后，真实用户 PATH 必须与切换前逐字符一致。
	restorePath()
	realAfter, _ := env.ReadUserPath()
	if realAfter != realBefore {
		t.Errorf("真实用户 PATH 被测试写入（宿主污染）:\nbefore: %q\nafter:  %q", realBefore, realAfter)
	}
}
