package util

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// 第十轮审计 P1-2 守卫：工具锁**不得线程亲和**，且必须在 goroutine 迁移后仍能释放。
//
// 缺陷现场（Windows 命名互斥量）：`CreateMutexW` 的所有权是**线程亲和**的（内核记录
// owner thread），而 Go 的 goroutine 在阻塞后会自由迁移到别的 OS 线程 → `ReleaseMutex`
// 以 `ERROR_NOT_OWNER` 失败，`CloseHandle` 又**不释放所有权**。只要还有另一个进程在
// 等待（持有句柄使内核对象存活），该 mutex 会一直被原线程占有到**线程退出**。
//
// 审计实测：30 次 `Activate` 中 **6 次漏释放（20%）**；跨进程对照显示父进程一直等到
// 超时（3.01s），而同线程 Release 对照只要 302ms。
// 后果：GUI 常驻 + CLI 并发时，CLI 对同一工具的 install **每次阻塞 5 分钟**再失败。
//
// 修复后锁改用 `LockFileEx`（所有权绑到文件句柄而非线程），因此本守卫在
// **每个平台**都必须通过（旧实现在 Windows 上会红）。

// TestAcquireToolLock_ReleaseAfterGoroutineMigration 是核心断言：
// 在一个 goroutine 里获取锁、让出调度（迫使它可能迁移 OS 线程）、再从**另一个**
// goroutine 释放，之后必须能立刻重新获取。
//
// 注意「跨 goroutine 释放」正是旧实现失败的形态：命名互斥量的所有权属于**获取时的
// 那个线程**，而 Go 不保证释放发生在同一线程上。文件锁没有这个概念。
func TestAcquireToolLock_ReleaseAfterGoroutineMigration(t *testing.T) {
	dir := t.TempDir()
	const tool = "migrationtool"

	lock, err := AcquireToolLock(dir, tool, 5*time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// 让出调度并做一点阻塞工作，给 goroutine 迁移 OS 线程的机会
	// （旧实现正是在这一步之后 ReleaseMutex 失败）。
	var wg sync.WaitGroup
	wg.Add(1)
	released := make(chan error, 1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		runtime.Gosched()
		released <- lock.Release()
	}()
	wg.Wait()

	if rerr := <-released; rerr != nil {
		t.Errorf("Release 返回错误（旧命名互斥量在此形态下会报 ERROR_NOT_OWNER）: %v", rerr)
	}

	// 关键：释放后必须**立刻**可再次获取。旧实现下锁会一直被原线程占有，
	// 这里会阻塞到超时。
	start := time.Now()
	lock2, err := AcquireToolLock(dir, tool, 2*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("P1-2 复现：释放后无法重新获取（锁泄漏，旧实现会一直阻塞到超时）: %v (elapsed=%v)", err, elapsed)
	}
	_ = lock2.Release()
	if elapsed > time.Second {
		t.Errorf("重新获取耗时 %v —— 锁未被真正释放", elapsed)
	}
}

// TestAcquireToolLock_NoLeakAcrossManyCycles 复现审计的「30 次 20% 漏释放」压测形态：
// 反复 acquire/release，每次都要求**下一次仍能拿到**。
//
// 旧实现下只要有一次 ReleaseMutex 失败，后续所有 acquire 都会阻塞到超时。
func TestAcquireToolLock_NoLeakAcrossManyCycles(t *testing.T) {
	dir := t.TempDir()
	const tool = "cycletool"
	const cycles = 30

	for i := 0; i < cycles; i++ {
		// 每轮换一个 goroutine 获取、另一个释放，最大化线程迁移的机会。
		var lock *ToolLock
		var err error
		done := make(chan struct{})
		go func() {
			defer close(done)
			lock, err = AcquireToolLock(dir, tool, 2*time.Second)
		}()
		<-done
		if err != nil {
			t.Fatalf("第 %d 轮 acquire 失败（说明前一轮漏释放了锁）: %v", i, err)
		}

		relDone := make(chan error, 1)
		go func() {
			runtime.Gosched()
			relDone <- lock.Release()
		}()
		if rerr := <-relDone; rerr != nil {
			t.Errorf("第 %d 轮 Release 返回错误: %v", i, rerr)
		}
	}
}

// 跨进程互斥仍必须成立：另一个进程持锁时，本进程必须拿不到。
//
// 这一条防止「为了修线程亲和而把锁改成进程内的」这类假修复。
func TestAcquireToolLock_CrossProcessStillExclusive(t *testing.T) {
	dir := t.TempDir()
	const tool = "xproctool"

	holder, err := AcquireToolLock(dir, tool, 2*time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer holder.Release()

	// 用独立句柄模拟另一个进程：锁文件上的排他锁必须与已持有的锁冲突。
	lockPath := filepath.Join(dir, ".locks", tool+".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("锁文件应存在于 %s: %v", lockPath, err)
	}
	other, err := AcquireFileLock(lockPath, 0)
	if err == nil {
		_ = other.Release()
		t.Error("另一个句柄在本进程持锁时拿到了锁 —— 互斥失效")
	}

	// 释放后另一个句柄必须能拿到（锁真的被放开了）。
	if err := holder.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	after, err := AcquireFileLock(lockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("释放后另一个句柄仍拿不到锁: %v", err)
	}
	_ = after.Release()
}

// 不同 xdev home 不得互相争用（锁按 home 隔离）。
func TestAcquireToolLock_DifferentHomesDoNotContend(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	const tool = "sametool"

	lockA, err := AcquireToolLock(dirA, tool, 2*time.Second)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer lockA.Release()

	// 不同 home 下同名工具必须能同时持锁。
	lockB, err := AcquireToolLock(dirB, tool, 2*time.Second)
	if err != nil {
		t.Fatalf("不同 home 的同名工具不应互相阻塞: %v", err)
	}
	_ = lockB.Release()
}

// 锁键里的路径分隔符等字符不得逃出 .locks 目录。
func TestAcquireToolLock_KeyStaysInsideLocksDir(t *testing.T) {
	dir := t.TempDir()
	for _, tool := range []string{"a/b", `a\b`, "..", "normal"} {
		lock, err := AcquireToolLock(dir, tool, 2*time.Second)
		if err != nil {
			t.Errorf("tool=%q: acquire 失败: %v", tool, err)
			continue
		}
		_ = lock.Release()
	}
	// .locks 目录里不得出现逃逸出去的文件（例如 dir 的父目录被写入）。
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "a" || e.Name() == "b" {
			t.Errorf("锁键逃出了 .locks 目录：父目录出现 %s", e.Name())
		}
	}
	_ = fmt.Sprint()
}
