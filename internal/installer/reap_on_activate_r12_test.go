package installer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/shim"
)

// 第十二轮审计（Round 11 报告 §七-④）：`ReapStaleResidue` 原先**只**挂在
// `buildShimIndexMap` 上 —— 而那个函数只在「重建 shim 反向索引」时才跑。
// 长期不触发索引重建的用户（例如只用 `xdev use` 切版本、从不安装新 shim），
// `.stale-*` 残留永远不会被回收，永久占着 `current/` 下的目录。
//
// 修复：在 `Activate` 与 `Uninstall` 的**成功路径**上各补一次调用 —— 这两条是
// 产生残留的主要路径，且必然经过。best-effort（失败不影响安装/卸载结果）。
//
// 本守卫钉住「激活/卸载确实触发了回收」这个**行为**，而不只是「代码里有个调用」。

// makeAgedStaleResidue 在 current/ 下造一个**已过期**的 `.stale-*` 残留目录。
func makeAgedStaleResidue(t *testing.T, xmdevDir, name string) string {
	t.Helper()
	dir := filepath.Join(xmdevDir, "current", name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "ghost.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 超过默认 1 小时门槛。
	when := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
	return dir
}

// activateFakego 装好 fakego@2.0.0 并激活。
func activateFakego(t *testing.T, ins *Installer, installDir string) *fakePlugin {
	t.Helper()
	installPath := makeFakeInstalledVersion(t, installDir, "fakego", "2.0.0")
	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, installPath, "2.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return p
}

// TestActivate_ReapsStaleResidue 是核心守卫：激活后过期残留必须消失。
func TestActivate_ReapsStaleResidue(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	stale := makeAgedStaleResidue(t, cfg.Core.XMDevDir, "oldtool.stale-abcdef")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("前置条件：残留应存在: %v", err)
	}

	activateFakego(t, ins, cfg.Core.InstallDir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("待办 §七-④ 复现：Activate 后过期残留仍在（%s）—— "+
			"回收只挂在 buildShimIndexMap 上，不重建索引的用户永远不会回收", stale)
	}
}

// 卸载路径同样要回收（卸载是产生残留的另一条主要路径）。
func TestUninstall_ReapsStaleResidue(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "2.0.0")
	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, vA, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// 激活之后再造残留，确保它只会被 Uninstall 回收。
	stale := makeAgedStaleResidue(t, cfg.Core.XMDevDir, "oldtool.stale-unin")

	if err := ins.Uninstall(p, "1.0.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("待办 §七-④ 复现：Uninstall 后过期残留仍在（%s）", stale)
	}
}

// **安全对照**：真实工具目录绝不能被这次新增的回收误删。
func TestActivate_DoesNotReapRealToolDirs(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	// 一个很旧的**真实**工具目录（名字不含 .stale-/.tmp-）。
	realTool := filepath.Join(cfg.Core.XMDevDir, "current", "realoldtool")
	if err := os.MkdirAll(realTool, 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-100 * time.Hour)
	if err := os.Chtimes(realTool, when, when); err != nil {
		t.Fatal(err)
	}

	activateFakego(t, ins, cfg.Core.InstallDir)

	if _, err := os.Stat(realTool); err != nil {
		t.Errorf("真实工具目录被回收误删了（绝不允许）: %v", err)
	}
}

// 未过期的残留（可能在途）不得被激活流程删掉。
func TestActivate_KeepsFreshStaleResidue(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	fresh := filepath.Join(cfg.Core.XMDevDir, "current", "inuse.stale-now")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	// mtime 就是现在 → 未过期。

	activateFakego(t, ins, cfg.Core.InstallDir)

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("在途（未过期）的 .stale-* 被误删，会与并发进程的 RemoveAll 竞争: %v", err)
	}
}

// 直接用 shim 包的 API 复核一次，确保「按年龄」的判据与 installer 的调用一致。
func TestReapStaleResidue_ConsistentFromInstallerPackage(t *testing.T) {
	_, cfg, _ := newTestInstaller(t)

	aged := makeAgedStaleResidue(t, cfg.Core.XMDevDir, "t.stale-aged")
	n := shim.ReapStaleResidue(cfg.Core.XMDevDir)
	if n != 1 {
		t.Errorf("ReapStaleResidue 回收 %d 个, want 1", n)
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Error("过期残留未被回收")
	}
}
