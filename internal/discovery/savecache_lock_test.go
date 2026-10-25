package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// 审查 S-01 回归：SaveCache 抢不到独占锁时**不得**写盘。
//
// 背景：旧实现是 `if lockErr == nil { defer lock.Release() }` —— 锁不可得就
// 静默继续写盘，与同文件 withCacheLock（第十轮 P2-1）的「抢不到锁绝不写盘」
// 策略矛盾。SaveCache 是整份快照覆盖语义，并发时后写者会覆盖前写者。
func TestSaveCacheRefusesWriteWithoutLock(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "sniff-cache.json")

	// 预置已有内容，用于判断是否被覆盖
	original := &SniffCache{Tools: map[string]ToolSniffResult{}}
	originalData, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}
	if err := os.WriteFile(cachePath, originalData, 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	s := &Scheduler{
		cachePath: cachePath,
		limiter:   newRateLimiter(60),
	}

	// 另一个持有者占住独占锁
	holder, err := util.AcquireFileLock(cachePath+".lock", time.Second)
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}
	defer holder.Release()

	incoming := &SniffCache{Tools: map[string]ToolSniffResult{
		"probe-tool": {Versions: []string{"9.9.9"}, LastSniff: time.Now()},
	}}
	writeErr := s.SaveCache(incoming)

	if writeErr == nil {
		t.Error("SaveCache 抢锁失败时返回了 nil，期望非 nil 错误（拒绝无锁写）")
	}

	afterData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache after: %v", err)
	}
	if string(afterData) != string(originalData) {
		t.Error("SaveCache 抢锁失败时仍改写了缓存文件，期望保持原样")
	}
}

// 审查 S-01 回归：能拿到锁时 SaveCache 仍应正常工作（避免修复过度）。
func TestSaveCacheWritesWhenLockAvailable(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "sniff-cache.json")

	s := &Scheduler{
		cachePath: cachePath,
		limiter:   newRateLimiter(60),
	}
	incoming := &SniffCache{Tools: map[string]ToolSniffResult{
		"probe-tool": {Versions: []string{"1.2.3"}, LastSniff: time.Now()},
	}}
	if err := s.SaveCache(incoming); err != nil {
		t.Fatalf("SaveCache 无竞争时应成功: %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var got SniffCache
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	if _, ok := got.Tools["probe-tool"]; !ok {
		t.Error("缓存未包含写入的条目")
	}
}
