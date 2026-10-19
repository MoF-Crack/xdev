package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 第十轮审计 P2-1 守卫：`<base>.tmp-*` 崩溃残留必须被回收，且**不得**误删在途写者。
//
// 缺陷现场：A-10 把固定 `<path>.tmp` 迁移到 `os.CreateTemp(dir, base+".tmp-*")`，
// 消除了跨写者污染，但把「崩溃残留」从**有界 1** 变成**每次中断 1**，而全仓没有回收者。
// 审计实测：50 次中断写 → 50 个孤儿；**成功写入也不回收**；硬杀后留下
// `state.cache.tmp-3564375558`。
//
// 修复：`AtomicWriteFile` 发布成功后顺带做一次**按年龄**的有界清扫。
// 年龄门槛（默认 1 小时）远大于任何一次原子写的耗时，因此不会误伤在途写者 ——
// 这一点是本守卫的重点（无条件删会把别的写者的临时文件抽走）。

// TestSweepStaleTempFiles_RemovesAgedOrphans：过期的孤儿必须被回收。
func TestSweepStaleTempFiles_RemovesAgedOrphans(t *testing.T) {
	dir := t.TempDir()
	base := "state.cache"

	// 造 3 个「陈旧」孤儿，把 mtime 拨到 2 小时前。
	var orphans []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, base+".tmp-orphan"+string(rune('a'+i)))
		if err := os.WriteFile(p, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		orphans = append(orphans, p)
	}

	removed := sweepStaleTempFiles(dir, base)
	if removed != len(orphans) {
		t.Errorf("sweepStaleTempFiles 删除 %d 个，want %d", removed, len(orphans))
	}
	for _, p := range orphans {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("P2-1 复现：陈旧孤儿未被回收: %s", filepath.Base(p))
		}
	}
}

// **核心安全断言**：新近的临时文件（模拟**在途写者**）绝不能被删。
func TestSweepStaleTempFiles_KeepsInFlightWriters(t *testing.T) {
	dir := t.TempDir()
	base := "state.cache"

	// 刚创建（mtime=now）→ 视为在途写者。
	inFlight := filepath.Join(dir, base+".tmp-inflight")
	if err := os.WriteFile(inFlight, []byte("being written"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed := sweepStaleTempFiles(dir, base)
	if removed != 0 {
		t.Errorf("sweepStaleTempFiles 删除了 %d 个文件，want 0（在途写者不得被删）", removed)
	}
	if _, err := os.Stat(inFlight); err != nil {
		t.Errorf("P2-1 复现：在途写者的临时文件被误删: %v", err)
	}
}

// 只处理**同一个 base 前缀**的临时文件，不得碰其它文件的临时文件。
func TestSweepStaleTempFiles_IgnoresOtherBases(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)

	otherBase := filepath.Join(dir, "other.cache.tmp-x")
	if err := os.WriteFile(otherBase, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(otherBase, old, old); err != nil {
		t.Fatal(err)
	}

	// 同名但非临时文件的目标文件：绝不能被删。
	target := filepath.Join(dir, "state.cache")
	if err := os.WriteFile(target, []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}

	removed := sweepStaleTempFiles(dir, "state.cache")
	if removed != 0 {
		t.Errorf("sweepStaleTempFiles 删除了 %d 个，want 0（只应处理同 base 的 .tmp-*）", removed)
	}
	if _, err := os.Stat(otherBase); err != nil {
		t.Errorf("别的 base 的临时文件被误删: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("P2-1 复现：**目标文件本身**被误删: %v", err)
	}
}

// 端到端：反复成功写入后，目录里不应累积临时文件（正常路径本来就自清理，
// 这里同时验证清扫没有把目标或无关文件删掉）。
func TestAtomicWriteFile_RepeatedWritesLeaveNoTempAccumulation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.cache")

	// 预置一个陈旧孤儿，模拟「上次进程被硬杀」。
	orphan := filepath.Join(dir, "state.cache.tmp-crashresidue")
	if err := os.WriteFile(orphan, []byte("crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if err := AtomicWriteFile(target, []byte("data"), 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.cache" {
			t.Errorf("P2-1 复现：目录里仍有残留 %s（陈旧孤儿应在首次成功写入后被回收）", e.Name())
		}
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Errorf("目标内容 = %q, want data", string(got))
	}
}

// 注入较短的年龄窗口时，稍旧的孤儿也应被回收 —— 证明门槛是**可配置的变量**
// 而非硬编码，便于运维按需收紧。
func TestSweepStaleTempFiles_HonoursConfigurableAge(t *testing.T) {
	dir := t.TempDir()
	base := "cfg.cache"
	p := filepath.Join(dir, base+".tmp-x")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 拨到 10 秒前。
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}

	// 默认 1 小时门槛下不该被删。
	if n := sweepStaleTempFiles(dir, base); n != 0 {
		t.Errorf("默认门槛下删除了 %d 个，want 0", n)
	}

	// 收紧到 1 秒后应被回收。
	prev := staleTempAge
	staleTempAge = time.Second
	defer func() { staleTempAge = prev }()

	if n := sweepStaleTempFiles(dir, base); n != 1 {
		t.Errorf("收紧门槛后删除了 %d 个，want 1", n)
	}
}
