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
