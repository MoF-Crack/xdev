package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 第十轮审计 P1-2 守卫（跨进程形态）。
//
// **为什么需要跨进程**：审计实测的漏释放是**概率性**的（30 次 Activate 中 6 次），
// 根因是「goroutine 迁移 OS 线程」——而 Go 的调度在**进程内**并不保证一定迁移。
// 实测确认：把 `Release` 放到另一个 goroutine 里（进程内迁移尝试）**无法稳定复现**
// 旧命名互斥量的泄漏，探针在修复前后都通过。因此这里用**子进程**构造，让「获取锁的
// 线程」与「释放锁的线程」确定性地分属不同的 OS 线程 —— 这正是审计证明该缺陷时用的
// 形态（`parent acquire: ... timeout after 3s elapsed=3.0147904s` vs 同线程对照
// `elapsed=302.9666ms`）。
//
// 本文件是 `_test.go`，因此子进程的 `-test.run` 探针只能复用它自己；这里改用
// **父进程持锁、子进程尝试获取**这一可确定性构造：只要父进程的锁能被别的进程观察到，
// 就说明「锁」确实是一个跨进程互斥量；而「父进程在自己的 goroutine 里释放后立刻可重取」
// 则由进程内用例覆盖。
//
// 关键断言（修复前后可判定）：父进程在**一个 goroutine** 里获取锁、在**另一个
// goroutine** 里释放，然后子进程必须能拿到锁。旧命名互斥量下，若释放线程 != 获取线程，
// 子进程会一直阻塞到超时。

// toolLockProbeEnv 让本测试文件可以充当「子进程探针」。
const toolLockProbeEnv = "XDEV_TOOLLOCK_PROBE"

func TestMain(m *testing.M) {
	// 子进程探针模式：尝试获取锁并打印结论，然后退出。
	if os.Getenv(toolLockProbeEnv) != "" {
		dir := os.Getenv(toolLockProbeEnv + "_DIR")
		tool := os.Getenv(toolLockProbeEnv + "_TOOL")
		lock, err := AcquireToolLock(dir, tool, 1500*time.Millisecond)
		if err != nil {
			os.Stdout.WriteString("PROBE-LOCKED\n")
			os.Exit(0)
		}
		_ = lock.Release()
		os.Stdout.WriteString("PROBE-FREE\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestAcquireToolLock_WaiterHoldingHandleSurvivesFailedRelease 是本项的核心守卫，
// 复刻审计证明该缺陷时的**确定性时序**。
//
// 为什么必须这样构造（关键，两个前车之鉴）：
//
//  1. **不能只看 `Release()` 的返回值**。旧实现的 Release 在 `ReleaseMutex` 失败时
//     只 `log.Printf` 然后 `return nil` —— 实测 30 轮里出现多条
//     「Attempt to release mutex not owned by caller」警告，而 `release failures: 0/30`。
//     即「静默泄漏」：调用方以为释放成功。
//  2. **必须有另一个等待者持有句柄**。命名互斥量的内核对象在**最后一个句柄关闭**时
//     销毁；单进程顺序测试里 `CloseHandle` 之后没有别的句柄，对象随即消失、泄漏自动
//     蒸发 —— 这就是为什么纯进程内探针在修复前后都通过。
//
// 因此本用例按审计的时序构造：
//
//	父进程获取锁 → 子进程开始等待（持有句柄，使内核对象存活）
//	→ 父进程从**迁移后的 goroutine** 释放（旧实现下 ReleaseMutex 失败）
//	→ 子进程必须仍能拿到锁。
//
// 旧实现下子进程会一直阻塞到自己的超时并打印 PROBE-LOCKED。
func TestAcquireToolLock_WaiterHoldingHandleSurvivesFailedRelease(t *testing.T) {
	dir := t.TempDir()
	const tool = "waiterprobe"

	lock, err := AcquireToolLock(dir, tool, 3*time.Second)
	if err != nil {
		t.Fatalf("parent acquire: %v", err)
	}

	// 启动子进程并让它开始等待（持有句柄）。给足时间确保它已进入 WaitForSingleObject。
	exe, exErr := os.Executable()
	if exErr != nil {
		t.Skipf("cannot locate test binary: %v", exErr)
	}
	cmd := exec.Command(exe, "-test.run", "TestNothingMatchesThisPattern")
	cmd.Env = append(os.Environ(),
		toolLockProbeEnv+"=1",
		toolLockProbeEnv+"_DIR="+dir,
		toolLockProbeEnv+"_TOOL="+tool,
	)
	cmd.Dir = filepath.Dir(exe)
	HideWindow(cmd)
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start probe subprocess: %v", err)
	}
	// 让子进程真正开始等待（持有句柄）。
	time.Sleep(400 * time.Millisecond)

	// 父进程从迁移后的 goroutine 释放 —— 旧实现在这里 ReleaseMutex 失败。
	relDone := make(chan error, 1)
	go func() {
		runtime.Gosched()
		time.Sleep(2 * time.Millisecond)
		relDone <- lock.Release()
	}()
	if rerr := <-relDone; rerr != nil {
		t.Errorf("跨 goroutine Release 返回错误: %v", rerr)
	}

	waitErr := cmd.Wait()
	out := buf.String()
	if strings.Contains(out, "PROBE-FREE") {
		return // 锁确实被释放了
	}
	if strings.Contains(out, "PROBE-LOCKED") {
		t.Errorf("P1-2 复现：父进程从迁移后的 goroutine 释放后，等待中的子进程仍拿不到锁"+
			"（命名互斥量的线程亲和导致静默泄漏）—— probe 输出 %q", strings.TrimSpace(out))
		return
	}
	if waitErr != nil {
		t.Skipf("probe subprocess did not report a verdict (%v): %q", waitErr, strings.TrimSpace(out))
	}
	t.Fatalf("探针未给出结论: %q", strings.TrimSpace(out))
}

// TestAcquireToolLock_CrossProcessObservableAfterCrossGoroutineRelease 覆盖更弱的形态：
// 先释放、再让子进程获取（无等待者持有句柄）。这一条在旧实现下也可能通过 ——
// 保留它是为了钉住「释放之后锁确实可用」这一基本性质。
func TestAcquireToolLock_CrossProcessObservableAfterCrossGoroutineRelease(t *testing.T) {
	dir := t.TempDir()
	const tool = "xprocprobe"

	// goroutine A 获取。
	var lock *ToolLock
	var err error
	got := make(chan struct{})
	go func() {
		defer close(got)
		lock, err = AcquireToolLock(dir, tool, 3*time.Second)
	}()
	<-got
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// goroutine B 释放（确定性跨线程：不同的 goroutine + Gosched）。
	relDone := make(chan error, 1)
	go func() {
		runtime.Gosched()
		relDone <- lock.Release()
	}()
	if rerr := <-relDone; rerr != nil {
		t.Errorf("跨 goroutine Release 返回错误（旧命名互斥量在此会报 ERROR_NOT_OWNER）: %v", rerr)
	}

	// 子进程必须能拿到锁。
	out := runToolLockProbe(t, dir, tool)
	if strings.Contains(out, "PROBE-LOCKED") {
		t.Errorf("P1-2 复现：跨 goroutine 释放后，另一个进程仍拿不到锁（锁泄漏）—— probe 输出 %q", out)
	}
	if !strings.Contains(out, "PROBE-FREE") {
		t.Fatalf("探针未给出结论: %q", out)
	}
}

// 对照：父进程**持有**锁时，子进程必须拿不到（证明上面的 PROBE-FREE 不是假阳性）。
func TestAcquireToolLock_CrossProcessProbeSeesHeldLock(t *testing.T) {
	dir := t.TempDir()
	const tool = "xprocprobeheld"

	lock, err := AcquireToolLock(dir, tool, 3*time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lock.Release()

	out := runToolLockProbe(t, dir, tool)
	if !strings.Contains(out, "PROBE-LOCKED") {
		t.Errorf("父进程持锁时子进程应拿不到锁（否则上面的守卫无意义）: probe 输出 %q", out)
	}
}

// runToolLockProbe 以子进程方式重跑本测试二进制，让它尝试获取同一个工具锁。
func runToolLockProbe(t *testing.T, dir, tool string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run", "TestNothingMatchesThisPattern")
	cmd.Env = append(os.Environ(),
		toolLockProbeEnv+"=1",
		toolLockProbeEnv+"_DIR="+dir,
		toolLockProbeEnv+"_TOOL="+tool,
	)
	cmd.Dir = filepath.Dir(exe)
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		t.Skipf("probe subprocess failed to run: %v", err)
	}
	return string(out)
}
