package cli

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// ---------------------------------------------------------------------------
// 票 33（P1-27 / E-08、P1-28 / E-09）：home 绝对路径校验 + path.mode 枚举收敛
// ---------------------------------------------------------------------------

// TC-33-1a `config set core.xmdevenv_dir` 拒绝相对路径 / UNC，接受本地绝对路径。
func TestConfigSet_XMDevDir_RejectsRelativeAndUNC(t *testing.T) {
	rejected := []string{
		"relative/xdev",
		`relative\xdev`,
		"./xdev",
		"../xdev",
		"xdev",
		`C:xdev`,          // Windows 驱动器相对形式
		`\xdev`,           // 根相对（无驱动器）——Windows 下非绝对
		`\\server\share`,  // UNC
		`\\server\share\xdev`,
		"//server/share",  // UNC（POSIX 形态）
		"",
	}
	for _, v := range rejected {
		cfg := &config.Config{}
		if err := configSet(cfg, "core.xmdevenv_dir", v); err == nil {
			t.Errorf("configSet(core.xmdevenv_dir, %q) = nil, want rejection", v)
		}
		if cfg.Core.XMDevDir != "" {
			t.Errorf("configSet(core.xmdevenv_dir, %q) 写入了被拒值 %q", v, cfg.Core.XMDevDir)
		}
	}

	accepted := []string{
		`D:\XDev`,
		`D:/XDev`,
		`C:\Users\tester\.xdev`,
	}
	if runtime.GOOS != "windows" {
		// POSIX 绝对路径只在 POSIX 宿主上是绝对路径（Windows 下 "/opt/xdev"
		// 是根相对路径，正确拒绝）。
		accepted = append(accepted, "/opt/xdev")
	}
	for _, v := range accepted {
		cfg := &config.Config{}
		if err := configSet(cfg, "core.xmdevenv_dir", v); err != nil {
			t.Errorf("configSet(core.xmdevenv_dir, %q) = %v, want nil（绝对路径零影响）", v, err)
			continue
		}
		if cfg.Core.XMDevDir != v {
			t.Errorf("XMDevDir = %q, want %q", cfg.Core.XMDevDir, v)
		}
	}
}

// TC-33-1b init 向导自定义路径分支（promptDriveWith "p"）同样拒绝相对/UNC，
// 并在被拒后重新提示，最终接受绝对路径。
func TestPromptDriveCustomPath_RejectsRelativeAndUNC(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader(
		"p\nrelative\\xdev\n" + // 相对 → 重新提示
			"p\n\\\\server\\share\\xdev\n" + // UNC → 重新提示
			"p\nD:\\XDev\n") // 绝对 → 接受
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Fatal("unexpected Quit")
	}
	if choice.Path != `D:\XDev` {
		t.Errorf("Path = %q, want %q（绝对路径必须原样接受）", choice.Path, `D:\XDev`)
	}
	if !strings.Contains(out.String(), "must be absolute") {
		t.Errorf("缺少相对路径拒绝提示，output: %q", out.String())
	}
	if !strings.Contains(out.String(), "UNC path") {
		t.Errorf("缺少 UNC 拒绝提示，output: %q", out.String())
	}
}

// TC-33-1c path.mode 三值枚举：symlink 拒绝（历史遗留值，全仓无实现）、
// auto 接受；与 config.SetPathMode / GetPathMode 同源。
func TestPathModeEnum_SymlinkRejectedAutoAccepted(t *testing.T) {
	if err := setPathField(&config.PathConfig{}, "mode", "symlink"); err == nil {
		t.Error(`setPathField(mode, "symlink") = nil, want rejection`)
	}
	for _, mode := range []string{"shim", "direct", "auto"} {
		p := &config.PathConfig{}
		if err := setPathField(p, "mode", mode); err != nil {
			t.Errorf("setPathField(mode, %q) = %v, want nil", mode, err)
			continue
		}
		if p.Mode != mode {
			t.Errorf("Mode = %q, want %q", p.Mode, mode)
		}
	}

	// config.SetPathMode 同样拒绝 symlink（同一枚举的第二处定义）。
	c := &config.Config{}
	c.Core.XMDevDir = t.TempDir()
	if err := c.SetPathMode("symlink"); err == nil {
		t.Error(`SetPathMode("symlink") = nil, want rejection`)
	}
	// 存量 "symlink" 由 GetPathMode 降级为 shim（现状保持，不破坏既有安装）。
	c.Path.Mode = "symlink"
	if got := c.GetPathMode(); got != "shim" {
		t.Errorf("GetPathMode() with legacy symlink = %q, want \"shim\"", got)
	}
}

// TC-33-1d 相对 home 经公开入口 config.Load 归一为绝对路径
// （E-08 第二半：resolveXMDevDir/expandHome 返回前 filepath.Abs，
// 防止后续 Join 出的 install/shim/cache 路径随 CWD 漂移）。
// 直接对 resolveXMDevDir/expandHome 的包内单测见 internal/config
// （config_abs_path_test.go）。
func TestLoad_RelativeHomeBecomesAbsolute(t *testing.T) {
	cfg, err := config.Load("relative-home-dir")
	if err != nil {
		t.Fatalf("config.Load(relative): %v", err)
	}
	if !filepath.IsAbs(cfg.Core.XMDevDir) {
		t.Errorf("XMDevDir = %q, want absolute path", cfg.Core.XMDevDir)
	}
	if !filepath.IsAbs(cfg.Core.ShimDir) {
		t.Errorf("ShimDir = %q, want absolute path（派生目录必须一并绝对化）", cfg.Core.ShimDir)
	}
}
