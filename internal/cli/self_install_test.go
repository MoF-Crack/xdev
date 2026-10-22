package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// 20261021 审计 P1-3：CLI 需要一个显式入口把“当前运行的二进制”安装进
// install/xdev/<version> 并切换 current/xdev。否则用户手工替换根副本后，
// PATH 上的 current/xdev 可能长期停留在旧版本（本机实测停在 0.3.3-rc.2）。
func TestSelfCmd_HasInstallSubcommand(t *testing.T) {
	found := false
	for _, c := range newSelfCmd().Commands() {
		if c.Name() == "install" {
			found = true
			if c.Short == "" {
				t.Error("self install must have a Short description")
			}
			if c.Long == "" {
				t.Error("self install must document that the NEW binary has to run it")
			}
		}
	}
	if !found {
		t.Fatal("self install subcommand is missing")
	}
}

func TestRunSelfInstall_InstallsAndSwitchesCurrent(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   tmp,
			InstallDir: filepath.Join(tmp, "install"),
		},
	}

	if err := runSelfInstall(cfg); err != nil {
		t.Fatalf("runSelfInstall: %v", err)
	}

	exeName := "xdev"
	if runtime.GOOS == "windows" {
		exeName = "xdev.exe"
	}
	target := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version, exeName)
	if fi, err := os.Stat(target); err != nil {
		t.Fatalf("installed binary missing at %s: %v", target, err)
	} else if fi.IsDir() {
		t.Fatalf("installed binary path is a directory: %s", target)
	}

	// current/xdev 必须指向刚安装的版本。ActiveVersion 会跟随链接并确认
	// 目标存在，正好覆盖“悬空/旧目标”两种失败形态。
	active, ok := util.ActiveVersion(tmp, "xdev")
	if !ok {
		t.Fatalf("current/xdev is not active after self install")
	}
	if active != version.Version {
		t.Errorf("current/xdev active version = %q, want %q", active, version.Version)
	}

	// 幂等：第二次执行不得报错，也不得改变 active。
	if err := runSelfInstall(cfg); err != nil {
		t.Fatalf("runSelfInstall (second): %v", err)
	}
	if again, ok := util.ActiveVersion(tmp, "xdev"); !ok || again != version.Version {
		t.Errorf("after second run active = %q (ok=%v), want %q", again, ok, version.Version)
	}
}
