package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 第八轮审计 A-09 守卫：`InvalidateHomeCache()` 必须同时失效**进程内**的
// sync.Once 缓存，而不只是删磁盘缓存文件。
//
// 缺陷现场：旧实现只 `os.Remove` 两个缓存文件，而 `homeOnce` 已 fire、
// `homeVal` 仍是旧值 —— 长驻进程（GUI）里「改 home → 失效缓存 → 下次重新发现」
// 的期望不成立：实测 `ResolveHomeDir()`=home1 → 改 `XMDEV_HOME`=home2 →
// `InvalidateHomeCache()` → **仍返回 home1**（只有测试专用的
// `ResetHomeDirForTest` 能刷新）。
//
// 生产可达点：gui/app_migration.go 的迁移收尾、cli/extra.go、cli/init.go ——
// 此后同进程的 shim 索引读写与自更新继续用旧 home。
func TestInvalidateHomeCache_ResetsInProcessOnce(t *testing.T) {
	home1 := t.TempDir()
	home2 := t.TempDir()

	// 隔离进程内状态，测试结束后恢复（避免影响同包其它用例）。
	ResetHomeDirForTest()
	t.Cleanup(ResetHomeDirForTest)

	t.Setenv("XMDEV_HOME", home1)
	if got := ResolveHomeDir(); got != home1 {
		t.Fatalf("test setup: ResolveHomeDir() = %q, want %q", got, home1)
	}

	// 换 home 并声明缓存失效（这正是 GUI 迁移收尾做的事）。
	t.Setenv("XMDEV_HOME", home2)
	InvalidateHomeCache()

	got := ResolveHomeDir()
	if got != home2 {
		t.Errorf("A-09 复现：InvalidateHomeCache() 后 ResolveHomeDir() = %q, want %q —— "+
			"进程内 sync.Once 未重置，长驻进程会继续用旧 home", got, home2)
	}
}

// 对照：InvalidateHomeCache 仍必须删除磁盘缓存文件（原有语义不得丢）。
func TestInvalidateHomeCache_StillRemovesDiskCache(t *testing.T) {
	ResetHomeDirForTest()
	t.Cleanup(ResetHomeDirForTest)

	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	_ = ResolveHomeDir() // 触发写入

	p := homeCachePath()
	// 主动写一份缓存文件（homeCachePath 的目录可能尚不存在）。
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	InvalidateHomeCache()

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("磁盘缓存文件未被删除（err=%v）", err)
	}
}
