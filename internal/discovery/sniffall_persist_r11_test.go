package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

// newSniffAllTestScheduler 构造一个不触网的 Scheduler，缓存落在给定的 home 下。
//
// **注入空 catalog**：本组用例只关心「合并写是否落盘」，不关心嗅探本身。若不注入，
// `SniffAll` 会去嗅探真实 manifest 里的**全部**工具（实测单次 70~80 秒且依赖网络），
// 既慢又不确定。注入空 catalog 后 tools 为空、循环体不执行，但合并写**仍照常走完整
// 路径**（读缓存 → 合并 → 写回）—— 正是被测点。
func newSniffAllTestScheduler(t *testing.T, home string) *Scheduler {
	t.Helper()
	cfg := &config.SniffConfig{
		Strategy:       "manual",
		Interval:       "24h",
		Concurrency:    1,
		RateLimitPerHr: 60,
	}
	s := NewScheduler(cfg, home, home)
	s.loadCatalog = func(string) (*manifest.Catalog, error) {
		return &manifest.Catalog{}, nil
	}
	return s
}

// 第十一轮审计（Round 10 报告待办 8）：嗅探缓存损坏后 `SniffAll` **报成功但从不落盘**。
//
// 缺陷现场：`sniffAllWithForce` 的合并写用 `withCacheLock`，而 `withCacheLock` 在
// 读失败（缓存损坏/不可读）时**正确地**返回错误、拒绝写空缓存（Round 10 P2-1 的修复）。
// 但调用方把这个错误**只记日志、不返回**：
//
//	mergeErr := s.withCacheLock(func(cache *SniffCache) error { ... })
//	if mergeErr != nil {
//		log.Printf("[discovery] SniffAll: merge write failed: %v", mergeErr)   // ← 只 log
//	}
//	if len(errs) > 0 { return fmt.Errorf("sniff errors: %s", ...) }
//	return ctx.Err()                                                          // ← 报成功
//
// 后果：缓存文件损坏时，`xdev sniff --all` / GUI「全部嗅探」**显示成功**，而这一轮的
// 嗅探结果**一个字节都没落盘**。用户以为嗅探过了，下次启动仍然是旧数据 ——
// 与 F-56「读失败绝不覆写」是同一处的两半：写侧守住了，但**调用方把失败吞了**。
//
// 这与 Round 10 P2-2（`saveState` 失败被吞）是**完全相同的形态**：
// 底层函数返回了错误，调用方没检查。修 A-02 的写侧时修了 isolator，这里漏了。

// corruptSniffCache 写一个内容非法、无法解析的 sniff-cache.json。
func corruptSniffCache(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "sniff-cache.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// 非法 JSON：withCacheLock 的 readCacheUnlocked 会解析失败 → 返回错误。
	if err := os.WriteFile(path, []byte("{ this is not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSniffAll_CorruptCacheMustNotReportSuccess 是核心守卫。
//
// 断言：缓存损坏时 `SniffAll` **必须返回错误**（而不是 nil），且**绝不**把损坏文件
// 覆写成空缓存（F-56）。
func TestSniffAll_CorruptCacheMustNotReportSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	cachePath := corruptSniffCache(t, home)
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}

	s := newSniffAllTestScheduler(t, home)

	// SniffAll 走完整合并写路径。即使没有任何工具需要嗅探，合并写仍会尝试读缓存。
	runErr := s.SniffAll(context.Background())

	after, rerr := os.ReadFile(cachePath)
	if rerr != nil {
		t.Fatalf("损坏缓存不应被删除: %v", rerr)
	}
	// F-56：读失败绝不覆写 —— 内容必须逐字节不变。
	if string(before) != string(after) {
		t.Errorf("P2-1/F-56 复现：损坏的嗅探缓存被覆写了\nbefore=%q\nafter =%q", before, after)
	}
	if runErr == nil {
		t.Errorf("待办 8 复现：缓存损坏、本轮结果**一个字节都没落盘**，" +
			"但 SniffAll 返回 nil（报成功）—— 用户以为嗅探过了")
	}
}

// 对照：缓存**正常**时 SniffAll 不得因为这条改动而报错。
func TestSniffAll_HealthyCacheStillSucceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	s := newSniffAllTestScheduler(t, home)
	if err := s.SniffAll(context.Background()); err != nil {
		t.Errorf("缓存健康时 SniffAll 不应报错: %v", err)
	}
}

// 对照：**缓存不存在**（首次运行）是合法状态，不得报错。
// `readCacheUnlocked` 对 os.IsNotExist 返回 (nil, nil)，withCacheLock 随后按空缓存处理。
func TestSniffAll_MissingCacheIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "sniff-cache.json")); err == nil {
		t.Fatal("测试前提：缓存文件不应存在")
	}

	s := newSniffAllTestScheduler(t, home)
	if err := s.SniffAll(context.Background()); err != nil {
		t.Errorf("首次运行（无缓存）时 SniffAll 不应报错: %v", err)
	}
	// 首次运行应当把缓存写出来。
	if _, err := os.Stat(filepath.Join(home, "sniff-cache.json")); err != nil {
		t.Errorf("首次 SniffAll 后应写出缓存: %v", err)
	}
}

// 端到端后果断言：损坏缓存 → SniffAll 报错；修复损坏后 → 能正常落盘。
// 这条钉住「失败是可恢复的、且失败时确实没落盘」。
func TestSniffAll_CorruptThenRepairedPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	cachePath := corruptSniffCache(t, home)

	s := newSniffAllTestScheduler(t, home)
	if err := s.SniffAll(context.Background()); err == nil {
		t.Fatal("损坏缓存时 SniffAll 应返回错误")
	}

	// 人工修复缓存为合法空缓存。
	if err := os.WriteFile(cachePath, []byte(`{"tools":{},"last_sniff_at":"0001-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SniffAll(context.Background()); err != nil {
		t.Fatalf("修复缓存后 SniffAll 应成功: %v", err)
	}
	fi, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Error("修复后 SniffAll 应把缓存写出内容")
	}
}
