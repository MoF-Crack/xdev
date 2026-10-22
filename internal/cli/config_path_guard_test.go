package cli

import (
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// ---------------------------------------------------------------------------
// 票 33（P1-27 / E-08、P1-28 / E-09）：home 绝对路径校验 + path.mode 枚举收敛
//
// B0（20261021 审计）：Windows 盘符/UNC 用例已移到
// config_path_guard_windows_test.go（`//go:build windows`），本文件只保留
// 跨平台可编译的用例 —— 否则 Linux/macOS 的 `go vet`/`go test` 会因
// driveFixture 未定义而编译失败。
// ---------------------------------------------------------------------------

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
