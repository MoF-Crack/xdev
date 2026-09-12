package util

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 40（P2-20 / F-08）：日志轮转加锁 + Windows copy&truncate + 错误不静默
// ---------------------------------------------------------------------------
//
// 旧实现直接 os.Rename 且吞掉 shift 循环与最旧备份删除的错误：Windows 上
// 对仍被其他句柄打开的日志文件 rename 必然失败，轮转因此永不发生却毫无
// 迹象（审计日志无限增长）。新行为：<path>.lock 跨进程互斥；Windows 上
// copy+truncate；失败一律可见。

// writeLog 生成一个内容确定、大小超过 maxSize 的日志文件。
func writeLog(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}
}

// readIfExists 读取文件内容；不存在返回 ("", false)。
func readIfExists(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data), true
}

// TestRotateLog_ShiftsBackupChainAndEmptiesCurrent：超过上限时 .1/.2 链接
// 依次后移，最旧备份被删除，当前文件不再持有旧内容（Unix：被改名移走；
// Windows：被截断为 0）。
func TestRotateLog_ShiftsBackupChainAndEmptiesCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "download_audit.jsonl")
	writeLog(t, path, strings.Repeat("A", 64))        // 当前（> maxSize）
	writeLog(t, path+".1", strings.Repeat("B", 16))   // 一代前
	writeLog(t, path+".2", strings.Repeat("C", 8))    // 二代前
	writeLog(t, path+".3", "OLDEST-SHOULD-DISAPPEAR") // 超出 maxBackups

	if err := RotateLog(path, 10, 3); err != nil {
		t.Fatalf("RotateLog = %v, want nil", err)
	}

	if got, ok := readIfExists(t, path+".1"); !ok || got != strings.Repeat("A", 64) {
		t.Errorf(".1 = %q (exists=%v), want 当前内容被归档", got, ok)
	}
	if got, ok := readIfExists(t, path+".2"); !ok || got != strings.Repeat("B", 16) {
		t.Errorf(".2 = %q (exists=%v), want 旧 .1 后移", got, ok)
	}
	if got, ok := readIfExists(t, path+".3"); !ok || got != strings.Repeat("C", 8) {
		t.Errorf(".3 = %q (exists=%v), want 旧 .2 后移", got, ok)
	}
	if got, ok := readIfExists(t, path+".4"); ok {
		t.Errorf("超出 maxBackups 的备份仍存在: %q", got)
	}
	if got, ok := readIfExists(t, path); ok && got != "" {
		t.Errorf("当前日志仍持有旧内容（轮转未生效）: %d 字节", len(got))
	}
}

// TestRotateLog_NoRotationBelowLimit：未超过上限时不做任何改动。
func TestRotateLog_NoRotationBelowLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	writeLog(t, path, "small")

	if err := RotateLog(path, 1024, 3); err != nil {
		t.Fatalf("RotateLog = %v, want nil", err)
	}
	if got, _ := readIfExists(t, path); got != "small" {
		t.Errorf("未超限的日志被改动: %q", got)
	}
	if _, ok := readIfExists(t, path+".1"); ok {
		t.Error("未超限时不应生成备份")
	}
}

// TestRotateLog_MissingFileIsNoop：文件不存在时是 no-op（既有语义）。
func TestRotateLog_MissingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never-written.jsonl")
	if err := RotateLog(path, 10, 3); err != nil {
		t.Errorf("RotateLog(缺失文件) = %v, want nil", err)
	}
}

// TestRotateLog_ConcurrentRotationsDoNotTear：并发轮转必须串行化——旧实现
// 无锁，多个 goroutine/进程会各自走一遍 shift 循环，备份链相互覆盖
// （-race 在本机不可用，这里用显式并发 + 内容完整性断言替代）。
func TestRotateLog_ConcurrentRotationsDoNotTear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	payload := strings.Repeat("X", 512)
	writeLog(t, path, payload)

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = RotateLog(path, 64, 3)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: RotateLog = %v, want nil", i, err)
		}
	}
	// 备份内容必须是「完整的旧内容」或「空」——绝不能是被并发撕裂的半截。
	for _, suffix := range []string{".1", ".2", ".3"} {
		got, ok := readIfExists(t, path+suffix)
		if !ok {
			continue
		}
		if got != "" && got != payload {
			t.Errorf("%s 内容被撕裂: %d 字节（want 0 或 %d）", suffix, len(got), len(payload))
		}
	}
	// 当前文件同样不得残留半截旧内容。
	if got, ok := readIfExists(t, path); ok && got != "" && got != payload {
		t.Errorf("当前日志内容异常: %d 字节", len(got))
	}
}

// TestRotateLog_WorksWithPlaceholderFixedTmpDir：确定性守卫——实现不得依赖
// 固定 "<dst>.tmp" 中间文件（P1-24 收敛口径）。占位技巧：把固定名预建为
// **目录**；唯名临时文件（os.CreateTemp）完全不受影响，而「写固定
// <dst>.tmp 再 rename」的实现必然失败。Windows 上这条路径正是
// archiveCurrent 的 copy+truncate 分支。
func TestRotateLog_WorksWithPlaceholderFixedTmpDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	writeLog(t, path, strings.Repeat("Q", 128))

	fixedTmp := path + ".1" + ".tmp"
	if err := os.MkdirAll(fixedTmp, 0o755); err != nil {
		t.Fatalf("mkdir placeholder: %v", err)
	}

	if err := RotateLog(path, 64, 3); err != nil {
		t.Fatalf("RotateLog 依赖固定 .tmp 中间文件（被占位即失败）: %v", err)
	}
	if fi, err := os.Stat(fixedTmp); err != nil || !fi.IsDir() {
		t.Errorf("占位目录被破坏: fi=%v err=%v", fi, err)
	}
	if got, ok := readIfExists(t, path+".1"); !ok || got != strings.Repeat("Q", 128) {
		t.Errorf(".1 = %q (exists=%v), want 完整备份", got, ok)
	}
	// 不得残留任何唯名临时文件。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("轮转残留临时文件: %s", e.Name())
		}
	}
}

// TestOpenLogForAppend_ReportsRotationFailure：轮转失败必须留下日志且仍然
// 返回可写句柄（既有契约：轮转失败不阻断审计写入，但必须可见）。
func TestOpenLogForAppend_ReportsRotationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	writeLog(t, path, strings.Repeat("Y", 128))

	// 制造轮转失败：把 <path>.lock 做成一个目录，AcquireFileLock 打不开它。
	if err := os.Mkdir(path+".lock", 0o755); err != nil {
		t.Fatalf("mkdir lock fixture: %v", err)
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	f, err := OpenLogForAppend(path, 64, 3)
	if err != nil {
		t.Fatalf("轮转失败不应阻断打开日志: %v", err)
	}
	defer f.Close()

	if !strings.Contains(buf.String(), "failed to rotate") {
		t.Errorf("轮转失败被静默吞掉，日志输出: %q", buf.String())
	}
	if !strings.Contains(buf.String(), path) {
		t.Errorf("日志未指出失败的路径: %q", buf.String())
	}
}

// TestRotateLog_LockIsReleasedAfterRotation：轮转结束后锁必须释放（否则
// 下一个进程/调用会超时失败）。
func TestRotateLog_LockIsReleasedAfterRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	writeLog(t, path, strings.Repeat("Z", 256))

	for i := 0; i < 3; i++ {
		// 每次都写回超限内容，确保每次都要真正轮转。
		writeLog(t, path, fmt.Sprintf("%s-%d", strings.Repeat("Z", 256), i))
		if err := RotateLog(path, 64, 2); err != nil {
			t.Fatalf("第 %d 次 RotateLog = %v, want nil（锁未释放会超时）", i+1, err)
		}
	}
}
