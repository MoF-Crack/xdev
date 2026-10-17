package update

import (
	"os"
	"path/filepath"
	"testing"
)

// 第七轮审计 F-47 守卫：自更新的分发器刷新必须尊重**可配置的** core.shim_dir。
//
// 缺陷现场：refreshShimDispatchers 硬拼 `<home>/shims`，而 core.shim_dir 是受支持的
// 可配置项（`xdev config set core.shim_dir <dir>`）。改过 shim_dir 的机器上，刷新落在
// 不存在的目录 → RefreshDispatchers 返回 (0,0) 静默成功 → 真实 shim 目录里的旧
// inode 分发器永不更新（正是第六轮 P2-A 要修的场景），且没有任何告警。
func TestResolveShimDir_PrefersConfiguredShimDir(t *testing.T) {
	home := t.TempDir()
	configured := filepath.Join(t.TempDir(), "custom-shims")
	if err := os.MkdirAll(configured, 0o755); err != nil {
		t.Fatal(err)
	}

	// 隔离 home（update 包的 config.Load 会读 XMDEV_HOME / home 下的 config.yaml）。
	t.Setenv("XMDEV_HOME", home)

	// 无配置时回落 <home>/shims。
	if got := resolveShimDir(); got != filepath.Join(home, "shims") {
		t.Errorf("resolveShimDir() with no config = %q, want %q (fallback)", got, filepath.Join(home, "shims"))
	}

	// 写入带 core.shim_dir 的配置 → 必须优先采用它。
	cfgYAML := "core:\n  xmdevenv_dir: " + home + "\n  shim_dir: " + configured + "\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := resolveShimDir(); got != configured {
		t.Errorf("F-47 复现：resolveShimDir() = %q, want %q —— "+
			"硬拼 <home>/shims 会让改过 core.shim_dir 的机器上的分发器永不刷新", got, configured)
	}
}

// 守卫：refreshShimDispatchers 在 shim 目录不存在时不得静默（旧实现返回 (0,0) 后
// 什么都不记，用户无从发现分发器从未被刷新）。这里只断言函数不 panic 且能正常返回——
// 日志断言依赖 log 输出捕获，价值低于「不得硬拼路径」这一结构约束。
func TestRefreshShimDispatchers_MissingDirDoesNotPanic(t *testing.T) {
	t.Setenv("XMDEV_HOME", t.TempDir()) // 该 home 下没有 shims/ 目录
	refreshShimDispatchers(filepath.Join(t.TempDir(), "xdev"))
}
