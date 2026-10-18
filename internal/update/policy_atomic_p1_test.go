package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// 第九轮审计 P1-1 守卫：writeJSONAtomic 的 rename 必须有界重试。
//
// 缺陷现场：这是仓库里**第三份**原子写实现（另两份是 util.AtomicWriteFile、
// config.writeFileCreateTemp），而收敛只对齐了「唯名临时文件 + Sync + Chmod」，
// 漏了 rename 重试 —— policy.go 结尾是裸 os.Rename。
//
// Windows 上多写者同时以 MoveFileEx(REPLACE_EXISTING) 覆盖同一目标会偶发
// ERROR_ACCESS_DENIED，且这是**瞬态**的（审计实测：首次尝试 563.7µs 即失败，
// 释放句柄后同一次 rename 成功）。裸 os.Rename 把它当硬失败，审计实测
// 12 写者 × 60 轮 + 6 并发读者 = **696/720（96.7%）失败**；同构压测走
// util.AtomicWriteFile 为 0 失败。
//
// 后果：自我更新节流缓存写不进去 → 每次启动都重查远端（可能触 API 限流）。
//
// 判定口径：**只断言「污染为 0」**，不断言「零失败」。并发读者会让 Windows 的
// rename 偶发失败，即使有重试也可能在极端负载下耗尽窗口 —— 这与仓库既有先例
// （internal/config/atomicwrite_convergence_test.go:151-160、
// internal/util/fs_test.go 的 A-10 守卫）一致。

// policyPayload 生成可自校验的变长 JSON 负载。
func policyPayload(i int) map[string]interface{} {
	return map[string]interface{}{
		"marker": fmt.Sprintf("[%03d]", i),
		"body":   string(make([]byte, 0)), // 占位，下面用填充
		"pad":    repeatChar(byte('a'+i%26), 64+i*8),
	}
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// isCompletePolicy 判定读回的内容是否为「某一次完整写入」。
func isCompletePolicy(data []byte) bool {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	marker, ok := m["marker"].(string)
	if !ok || len(marker) != 5 {
		return false
	}
	var n int
	if _, err := fmt.Sscanf(marker, "[%03d]", &n); err != nil {
		return false
	}
	pad, ok := m["pad"].(string)
	if !ok {
		return false
	}
	want := repeatChar(byte('a'+n%26), 64+n*8)
	return pad == want
}

// 并发写同一 policy 路径：读回内容必须始终是某一次完整写入（无撕裂/污染）。
func TestWriteJSONAtomic_ConcurrentWritersNoTornContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policy.cache")

	const writers = 12
	const rounds = 60

	var wg sync.WaitGroup
	var mu sync.Mutex
	var torn []string
	var renameErrs int

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				i := w*rounds + r
				if err := writeJSONAtomic(target, policyPayload(i)); err != nil {
					mu.Lock()
					renameErrs++
					mu.Unlock()
					continue
				}
				data, err := os.ReadFile(target)
				if err != nil {
					mu.Lock()
					renameErrs++
					mu.Unlock()
					continue
				}
				if !isCompletePolicy(data) {
					mu.Lock()
					if len(torn) < 5 {
						torn = append(torn, fmt.Sprintf("len=%d head=%q", len(data), headBytes(data, 20)))
					}
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if renameErrs > 0 {
		t.Logf("（参考）Windows 目标竞争导致的写/读失败 %d 次 —— 有重试后应为少数瞬态", renameErrs)
	}
	if len(torn) > 0 {
		t.Errorf("writeJSONAtomic 出现撕裂/污染：\n  %s", joinLines(torn))
	}
}

// 核心红→绿断言：**瞬态的 rename 竞争必须被重试吸收**。
//
// 这是确定性地复现审计的实测现象（首次尝试 563.7µs 即失败，释放句柄后同一次
// rename 成功）。构造方式：Windows 上 Go 的 os.Open 只传
// FILE_SHARE_READ|FILE_SHARE_WRITE（**不含 FILE_SHARE_DELETE**），因此持有目标
// 的读句柄会让对它的 rename 失败（ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION）
// —— 这正是并发读者造成瞬态失败的真实机制。
//
//   - 有重试（窗口约 55ms）：句柄在 10ms 后释放，重试成功 → PASS
//   - 无重试（裸 os.Rename）：微秒级即失败并在 10ms 后报错 → FAIL
//
// 非 Windows 上「rename 覆盖被打开的文件」本来就能成功，该用例无意义 → skip。
func TestWriteJSONAtomic_TransientlyBusyTargetIsRetried(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("rename-over-open-file only fails on Windows (no FILE_SHARE_DELETE)")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "busy.cache")
	if err := writeJSONAtomic(target, policyPayload(0)); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// 持有读句柄 → 阻塞对 target 的 rename。
	h, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		// 远比重试窗口（约 55ms）短，但远长于一次 rename 的耗时。
		time.Sleep(10 * time.Millisecond)
		_ = h.Close()
		close(done)
	}()

	werr := writeJSONAtomic(target, policyPayload(7))
	<-done
	if werr != nil {
		t.Fatalf("瞬态占用未被重试吸收（rename 缺少有界重试）: %v", werr)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !isCompletePolicy(data) {
		t.Errorf("重试后内容不是完整写入: len=%d head=%q", len(data), headBytes(data, 20))
	}
}

// 单写者连续覆盖：不得因瞬态竞争冒泡失败。
func TestWriteJSONAtomic_SingleWriterRepeatedUpdatesNeverFail(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policy-single.cache")

	// 单写者连续覆盖：每次 rename 的目标刚刚被自己创建，仍可能因外部句柄
	// 短暂占用而失败 —— 有重试则不会冒泡。
	for i := 0; i < 200; i++ {
		if err := writeJSONAtomic(target, policyPayload(i)); err != nil {
			t.Fatalf("第 %d 次覆盖写失败（说明 rename 缺少瞬态重试）: %v", i, err)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !isCompletePolicy(data) {
		t.Errorf("最终内容不是完整写入: len=%d head=%q", len(data), headBytes(data, 20))
	}
}

// 输出格式保持不变：json.NewEncoder 会补一个尾换行（10 vs 9 字节）。
// 这解释为什么不能把本函数朴素替换成 util.AtomicWriteFile。
func TestWriteJSONAtomic_KeepsEncoderTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policy-nl.cache")

	if err := writeJSONAtomic(target, map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Errorf("输出应以换行结尾（Encoder 语义），got %q", string(data))
	}
	// 内容必须可解析回来。
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["a"] != "b" {
		t.Errorf("round-trip 失败: %v", m)
	}
}

// 失败路径不得残留临时文件（含 rename 失败）。
func TestWriteJSONAtomic_NoResidue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policy-ok.cache")
	if err := writeJSONAtomic(target, policyPayload(1)); err != nil {
		t.Fatal(err)
	}
	// 目标为目录时 rename 必失败 —— 断言返回错误且不留临时文件。
	badDir := filepath.Join(dir, "policy-bad.cache")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(badDir, policyPayload(2)); err == nil {
		t.Error("目标是目录时必须返回错误")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == "policy-ok.cache" || name == "policy-bad.cache" {
			continue
		}
		t.Errorf("残留文件: %s", name)
	}
}

func headBytes(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}
