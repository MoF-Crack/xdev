//go:build windows

// Ticket 10 (audit-fixes / P0-11 + C-13 / N-11) guard tests: PATH
// read-modify-write serialization and case-insensitive PATH comparisons.
//
// K1 reproduces the lost-write race: CleanupUserPath used to run its whole
// RMW cycle without withUserPathLock (while AddCurrentBinPaths held it),
// so concurrent operations raced the registry and rolled each other back.
package env

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

// K1: concurrent PATH RMW from two goroutines must not lose either side's
// writes. The two sides touch DISJOINT entry sets (A adds a current/bin
// entry, B removes the shim dir in direct mode), so under correct locking
// the final PATH must show BOTH effects; a lost write leaves one missing.
// Loop counts are high enough to make the unlocked race highly probable.
func TestConcurrentPathRMW_NoLostWrite(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	shimDir := `E:\XDev\shims`
	home := `E:\XDev`
	nodeBin := `E:\XDev\current\node`
	if err := WriteUserPath(`C:\Windows\System32;` + shimDir + `;D:\Env\Tools\Git\cmd`); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	const rounds = 20
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := AddCurrentBinPaths([]string{nodeBin}); err != nil {
				t.Errorf("AddCurrentBinPaths round %d: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if _, err := CleanupUserPath(home, shimDir, "direct"); err != nil {
				t.Errorf("CleanupUserPath round %d: %v", i, err)
				return
			}
		}
	}()
	wg.Wait()

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	normalized := strings.ToLower(result)
	if !strings.Contains(normalized, strings.ToLower(nodeBin)) {
		t.Errorf("lost write: entry added by AddCurrentBinPaths (%s) missing from final PATH: %q", nodeBin, result)
	}
	if strings.Contains(normalized, strings.ToLower(shimDir)) {
		t.Errorf("lost write: entry removed by CleanupUserPath (%s) still present in final PATH: %q", shimDir, result)
	}
}

// K2: when the lock location cannot be created, withUserPathLock must
// degrade to the unprotected call AND leave a diagnosable WARN log.
func TestWithUserPathLock_DegradesWithLog(t *testing.T) {
	// 直接注入一个「不可创建」的锁路径：父目录链穿过一个**已存在的文件**
	// （<file>\.xdev\.locks），AcquireFileLock 的 MkdirAll 必然失败。
	// 第四轮复核改造：原先靠 t.Setenv("USERPROFILE", <file>) 让
	// defaultUserPathLockPath 间接撞上该文件 —— 但 TestMain 现在为整包安装
	// 内存 PATH 后端（锁路径已指向临时目录，USERPROFILE 不再参与），用例必须
	// 自己控制锁路径接缝才能确定性地触发降级。
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevLockFn := userPathLockPathFn
	userPathLockPathFn = func() (string, error) {
		return filepath.Join(notADir, ".xdev", ".locks", "userpath.lock"), nil
	}
	t.Cleanup(func() { userPathLockPathFn = prevLockFn })

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ran := false
	err := withUserPathLock(func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("withUserPathLock: %v (degradation must not fail the call)", err)
	}
	if !ran {
		t.Fatal("protected function did not run under degraded lock")
	}
	if out := logBuf.String(); !strings.Contains(out, "userpath lock degraded") {
		t.Errorf("degradation warning missing from log; got: %q", out)
	}
}

// K3: SetWindowsUserPath must dedupe case-insensitively — Windows PATH
// entries compare case-insensitively, so a lowercase variant must not be
// prepended again as a duplicate (C-13 / N-11).
func TestSetWindowsUserPath_CaseInsensitiveDedupe(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	if err := WriteUserPath(`C:\Windows\System32;e:\xdev\shims`); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}
	if err := SetWindowsUserPath(`E:\XDev\shims`); err != nil {
		t.Fatalf("SetWindowsUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	count := strings.Count(strings.ToLower(result), strings.ToLower(`E:\XDev\shims`))
	if count != 1 {
		t.Errorf("shim dir appears %d times in PATH (case-variant duplicate prepend): %q", count, result)
	}
}

// K4: RemoveFromWindowsUserPath must match case-insensitively — a
// case-variant entry (E:\XDEV\SHIMS vs e:\xdev\shims) must be removed.
func TestRemoveFromWindowsUserPath_CaseInsensitive(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	if err := WriteUserPath(`C:\Windows\System32;E:\XDEV\SHIMS`); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}
	if err := RemoveFromWindowsUserPath(`e:\xdev\shims`); err != nil {
		t.Fatalf("RemoveFromWindowsUserPath: %v", err)
	}

	result, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	if strings.Contains(strings.ToLower(result), "xdev\\shims") {
		t.Errorf("case-variant shim entry not removed: %q", result)
	}
}

// K5（票 44 / P2-2）：TransformUserPath 是 GUI PATH 清理的锁内 RMW 入口——
// 与其它持锁 mutator 并发时不得丢写。一半 worker 走 Transform（新路径），
// 一半走 SetWindowsUserPath（既有持锁 mutator）：每条并发写入都必须出现在
// 终态。旧实现（锁外读 + 锁内盲写）在交错下会以「读快照覆盖新值」丢条目。
func TestTransformUserPath_ConcurrentLockedMutators_NoLostWrite(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	if err := WriteUserPath(`C:\Windows\System32`); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			entry := fmt.Sprintf(`C:\t%d`, i)
			_, err := TransformUserPath(func(cur string) (string, bool, error) {
				for _, p := range strings.Split(cur, ";") {
					if strings.EqualFold(strings.TrimSpace(p), entry) {
						return "", false, nil
					}
				}
				if cur == "" {
					return entry, true, nil
				}
				return cur + ";" + entry, true, nil
			})
			if err != nil {
				errCh <- fmt.Errorf("worker t%d: %w", i, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := SetWindowsUserPath(fmt.Sprintf(`C:\s%d`, i)); err != nil {
				errCh <- fmt.Errorf("worker s%d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	final, err := ReadUserPath()
	if err != nil {
		t.Fatalf("ReadUserPath: %v", err)
	}
	lower := strings.ToLower(final)
	for i := 0; i < workers; i++ {
		for _, entry := range []string{fmt.Sprintf(`c:\t%d`, i), fmt.Sprintf(`c:\s%d`, i)} {
			if !strings.Contains(lower, entry) {
				t.Errorf("lost write: concurrent entry %s missing from final PATH: %q", entry, final)
			}
		}
	}
}

// K6（票 44）：TransformUserPath 的 changed/no-op 语义——transform 返回
// changed=false 或 next==current 时不落盘（后端值不变）。
func TestTransformUserPath_NoOpSemantics(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	const initial = `C:\Windows\System32;E:\some\bin`
	if err := WriteUserPath(initial); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	// changed=false → 不写
	changed, err := TransformUserPath(func(cur string) (string, bool, error) {
		return "", false, nil
	})
	if err != nil || changed {
		t.Fatalf("changed=false: changed=%v err=%v, want false/nil", changed, err)
	}
	if got, _ := ReadUserPath(); got != initial {
		t.Errorf("changed=false must not write; PATH = %q, want %q", got, initial)
	}

	// next == current → 不写（幂等）
	changed, err = TransformUserPath(func(cur string) (string, bool, error) {
		return cur, true, nil
	})
	if err != nil || changed {
		t.Fatalf("next==current: changed=%v err=%v, want false/nil（幂等不写）", changed, err)
	}

	// 真变更 → 写入并报 changed=true
	changed, err = TransformUserPath(func(cur string) (string, bool, error) {
		return cur + `;E:\new`, true, nil
	})
	if err != nil || !changed {
		t.Fatalf("real change: changed=%v err=%v, want true/nil", changed, err)
	}
	if got, _ := ReadUserPath(); got != initial+`;E:\new` {
		t.Errorf("transform write = %q, want %q", got, initial+`;E:\new`)
	}

	// transform 报错 → 原值不动、错误透传
	_, err = TransformUserPath(func(cur string) (string, bool, error) {
		return "", false, fmt.Errorf("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("transform error must propagate, got %v", err)
	}
	if got, _ := ReadUserPath(); got != initial+`;E:\new` {
		t.Errorf("failed transform must not write; PATH = %q", got)
	}
}
