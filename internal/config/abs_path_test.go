package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 33（P1-27 / E-08）：home 路径绝对化（包内单测）
// ---------------------------------------------------------------------------

// TC-33-1e resolveXMDevDir 返回绝对路径：无论输入是相对路径、~ 展开还是
// 环境变量，返回值都必须是绝对路径（否则后续 Join 出的 install/shim/cache
// 会随进程 CWD 漂移）。
func TestResolveXMDevDir_AlwaysAbsolute(t *testing.T) {
	// 绝对输入保持不变（清理形式）。
	abs := t.TempDir()
	got, err := resolveXMDevDir(abs)
	if err != nil {
		t.Fatalf("resolveXMDevDir(%q): %v", abs, err)
	}
	if got != abs {
		t.Errorf("absolute input: got %q, want %q（绝对路径零影响）", got, abs)
	}

	// 相对输入 → 绝对（相对当前工作目录解析）。
	got, err = resolveXMDevDir(filepath.Join("relative", "xdev-home"))
	if err != nil {
		t.Fatalf("relative input: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("relative input resolved to %q, want absolute path", got)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, cwd) {
		t.Errorf("relative input resolved to %q, want under CWD %q", got, cwd)
	}

	// ~ 与 ~/sub 展开后同样绝对。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	got, err = resolveXMDevDir("~")
	if err != nil {
		t.Fatalf("~: %v", err)
	}
	if got != home {
		t.Errorf("resolveXMDevDir(~) = %q, want %q", got, home)
	}
	got, err = resolveXMDevDir("~/xdev-test-home")
	if err != nil {
		t.Fatalf("~/xdev-test-home: %v", err)
	}
	if want := filepath.Join(home, "xdev-test-home"); got != want {
		t.Errorf("resolveXMDevDir(~/xdev-test-home) = %q, want %q", got, want)
	}

	// $XMDEV_HOME 相对值 → 绝对。
	t.Setenv("XMDEV_HOME", filepath.Join("env", "relative-home"))
	got, err = resolveXMDevDir("")
	if err != nil {
		t.Fatalf("XMDEV_HOME relative: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("XMDEV_HOME relative resolved to %q, want absolute", got)
	}
}

// TC-33-1f expandHome 的边界：空串不得被 filepath.Abs 变成 CWD。
func TestExpandHome_EmptyStaysEmpty(t *testing.T) {
	got, err := expandHome("")
	if err != nil {
		t.Fatalf("expandHome(\"\"): %v", err)
	}
	if got != "" {
		t.Errorf("expandHome(\"\") = %q, want \"\"（不得变成 CWD）", got)
	}

	got, err = expandHome(filepath.Join("rel", "dir"))
	if err != nil {
		t.Fatalf("expandHome(rel/dir): %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expandHome(rel/dir) = %q, want absolute", got)
	}
}

// TC-33-1g Load 之后所有派生目录均为绝对路径（端到端）。
func TestLoadDerivedPathsAreAbsolute(t *testing.T) {
	cfg, err := Load(filepath.Join("rel-home", "xdev"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for field, v := range map[string]string{
		"XMDevDir":    cfg.Core.XMDevDir,
		"DownloadDir": cfg.Core.DownloadDir,
		"InstallDir":  cfg.Core.InstallDir,
		"ShimDir":     cfg.Core.ShimDir,
		"CacheDir":    cfg.Core.CacheDir,
		"EnvDir":      cfg.Core.EnvDir,
		"ManifestDir": cfg.Core.ManifestDir,
	} {
		if !filepath.IsAbs(v) {
			t.Errorf("Core.%s = %q, want absolute path", field, v)
		}
	}
}
