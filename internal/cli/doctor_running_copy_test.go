package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// 20261021 审计 P1-3：doctor 必须能发现“PATH 上的 xdev 不是当前这份代码”。
// 这里只驱动纯函数，不执行 cobra 命令，避免进程级初始化把配置解析到真实 home。

func makeSelfHome(t *testing.T, activeVersion string) *config.Config {
	t.Helper()
	home := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   home,
		InstallDir: filepath.Join(home, "install"),
	}}

	target := filepath.Join(cfg.Core.InstallDir, "xdev", activeVersion)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	exeName := "xdev"
	if os.PathSeparator == '\\' {
		exeName = "xdev.exe"
	}
	if err := os.WriteFile(filepath.Join(target, exeName), []byte("stub"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "current"), 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}
	if err := util.CreateJunction(filepath.Join(home, "current", "xdev"), target); err != nil {
		t.Skipf("cannot create junction/symlink on this host: %v", err)
	}
	return cfg
}

func TestRunningCopyStatus_Matching(t *testing.T) {
	cfg := makeSelfHome(t, version.Get())

	ok, infoOnly, msg := runningCopyStatus(cfg, `C:\xdev.exe`)
	if !ok || infoOnly {
		t.Fatalf("matching current/xdev: ok=%v infoOnly=%v msg=%q", ok, infoOnly, msg)
	}
	if !strings.Contains(msg, version.Get()) {
		t.Errorf("message should mention the active version: %q", msg)
	}
}

func TestRunningCopyStatus_MismatchIsFailure(t *testing.T) {
	cfg := makeSelfHome(t, "0.0.0-old")

	ok, infoOnly, msg := runningCopyStatus(cfg, `C:\xdev.exe`)
	if ok || infoOnly {
		t.Fatalf("stale current/xdev must be a failure: ok=%v infoOnly=%v msg=%q", ok, infoOnly, msg)
	}
	if !strings.Contains(msg, "0.0.0-old") || !strings.Contains(msg, "self install") {
		t.Errorf("message must name the stale version and the fix: %q", msg)
	}
}

func TestRunningCopyStatus_MissingLinkIsFailure(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   home,
		InstallDir: filepath.Join(home, "install"),
	}}
	if err := os.MkdirAll(cfg.Core.InstallDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ok, infoOnly, msg := runningCopyStatus(cfg, `C:\xdev.exe`)
	if ok || infoOnly {
		t.Fatalf("missing current/xdev on an initialized home must fail: ok=%v infoOnly=%v msg=%q", ok, infoOnly, msg)
	}
	if !strings.Contains(msg, "missing") || !strings.Contains(msg, "self install") {
		t.Errorf("message must say missing + fix: %q", msg)
	}
}

func TestRunningCopyStatus_UninitializedHomeIsInfo(t *testing.T) {
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   t.TempDir(),
		InstallDir: filepath.Join(t.TempDir(), "install"),
	}}

	ok, infoOnly, msg := runningCopyStatus(cfg, `C:\xdev.exe`)
	if ok || !infoOnly {
		t.Fatalf("uninitialized home must be info-only: ok=%v infoOnly=%v msg=%q", ok, infoOnly, msg)
	}
	if !strings.Contains(msg, "not initialized") {
		t.Errorf("message should tell the user to init: %q", msg)
	}
}

func TestRCBlockHome(t *testing.T) {
	cases := []struct {
		name string
		rc   string
		want string
		ok   bool
	}{
		{"powershell", `# >>> xdev init >>>` + "\n" + `$env:XMDEV_HOME = "E:\XDev"`, `E:\XDev`, true},
		{"posix", `export XMDEV_HOME='/opt/xdev'`, `/opt/xdev`, true},
		{"fish", `set -gx XMDEV_HOME '/opt/fish-xdev'`, `/opt/fish-xdev`, true},
		{"no-block", `export PATH=/usr/bin:$PATH`, ``, false},
		{"empty-value", `export XMDEV_HOME=''`, ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := rcBlockHome([]byte(tc.rc))
			if ok != tc.ok || got != tc.want {
				t.Errorf("rcBlockHome = (%q,%v), want (%q,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSamePath_CaseFoldingOnWindows(t *testing.T) {
	if samePath(`E:\XDev`, `e:\xdev\`) == false {
		t.Skip("case-insensitive comparison is Windows-only")
	}
	if !samePath(filepath.Join("a", "b"), filepath.Join("a", ".", "b")) {
		t.Error("samePath must clean the paths")
	}
}
