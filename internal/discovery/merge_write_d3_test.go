package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

// writeToolYAML 落一个最小工具清单（Catalog 的字段不可导出，只能经真实文件）。
func writeToolYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s.yaml: %v", name, err)
	}
}

// toolKeys 返回缓存里的工具名（仅用于失败信息）。
func toolKeys(m map[string]ToolSniffResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 第八轮审计 D3 守卫：SniffAll 的**合并写**必须与 updateCacheEntryLocked 落在
// 同一把独占锁内（F-56 当时只修了后者）。
//
// 缺陷现场：合并写是 `LoadCache()`（共享锁，在 s.mu 之外）→ 改内存 →
// `SaveCache()`（此时才取独占锁）——两把锁之间没有覆盖整个 RMW 周期，因此在
// 「读」与「写」之间由另一个写入者落地的条目会被本轮过时快照**整份覆盖**。
// 修复后整个合并逻辑走 withCacheLock（锁内重新读取 → 合并 → 写回）。
//
// 本用例驱动**真实**的 SniffAll：让嗅探器在被调用时（即 SniffAll 已经进入、
// 尚未写回）并发落地一条条目，然后断言它存活。
func TestSniffAll_MergeWriteKeepsConcurrentEntry_D3(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.SniffConfig{Strategy: "manual", Concurrency: 1, RateLimitPerHr: 60}
	s := NewScheduler(cfg, dir, dir)

	// 一个可自动嗅探的工具（通过真实 YAML 落盘，Catalog 字段不可导出）。
	writeToolYAML(t, dir, "alpha", "name: alpha\nauto_sniff: true\n")
	s.loadCatalog = func(d string) (*manifest.Catalog, error) { return manifest.LoadCatalog(d) }

	// 先落地一条「早先」的条目。
	s.mu.Lock()
	if err := s.updateCacheEntryLocked("early", ToolSniffResult{LastSniff: time.Now()}); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()

	// 嗅探器在被调用时并发写入一条新条目 —— 这正是「SniffAll 已经读过缓存、
	// 但还没写回」的窗口。
	s.registry = &SnifferRegistry{sniffers: []Sniffer{&concurrentWriteSniffer{s: s}}}

	if err := s.SniffAll(context.Background()); err != nil {
		t.Fatalf("SniffAll: %v", err)
	}

	after, err := s.LoadCache()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Tools["concurrent"]; !ok {
		t.Errorf("D3 复现：SniffAll 的合并写丢失了并发落地的条目（tools=%v）——"+
			"合并写未与 updateCacheEntryLocked 共用同一把独占锁", toolKeys(after.Tools))
	}
	if _, ok := after.Tools["alpha"]; !ok {
		t.Errorf("本轮嗅探结果未落盘（tools=%v）", toolKeys(after.Tools))
	}
}

// concurrentWriteSniffer 在 Sniff 被调用时并发写入一条缓存条目。
type concurrentWriteSniffer struct{ s *Scheduler }

func (c *concurrentWriteSniffer) Name() string { return "concurrent" }
func (c *concurrentWriteSniffer) Supports(t *manifest.Tool) bool {
	return t != nil && t.Name == "alpha"
}
func (c *concurrentWriteSniffer) Sniff(_ context.Context, _ *manifest.Tool) ([]DiscoveredVersion, error) {
	c.s.mu.Lock()
	_ = c.s.updateCacheEntryLocked("concurrent", ToolSniffResult{LastSniff: time.Now()})
	c.s.mu.Unlock()
	return []DiscoveredVersion{{Version: "1.0.0"}}, nil
}
