//go:build windows

// A-11（第九轮，P1）：withUserPathLock 不可重入 → 嵌套调用静默降级。
//
// 缺陷原形：util.AcquireFileLock 是 OS 字节范围锁（Windows LockFileEx /
// Unix flock），同进程重复加锁会失败。嵌套调用时内层会阻塞满 10s 超时，
// 然后走 err != nil 分支 —— 记 "userpath lock degraded" 警告并**不再由内层
// 获取任何跨进程序列化**，内层临界区只剩外层那把锁在罩着；外层临界区一结束，
// 内层剩余部分即无锁（票 10/44 要防的 PATH 读-改-写丢写面），并额外造成
// GUI 假死 10 秒。实测红证据：elapsed=10.03s + degraded 警告。
//
// 说明（第九轮复核更正）：把「内层无锁」写成「内层临界区完全无跨进程序列化」
// 并不精确 —— 实测（见报告）内层 body 仍在**外层**持锁窗口内，用非阻塞
// AcquireFileLock 探测到的是「锁仍被本进程持有」。真正可观测的危害是
// 「10s 阻塞 + 降级警告 + 外层释放后内层不再有任何锁」，本文件的断言即按此
// 三条可观测事实写，不夸大。
//
// 修复：withUserPathLock 记录「持有者 goroutine id + 深度」，同 goroutine
// 的嵌套调用直接在外层临界区内执行 fn（外层已持跨进程锁，语义安全）；
// 不同 goroutine 的调用**不走**可重入快路（那是真正的并发争用，保持既有
// 的「10s 超时后降级」契约，且既有并发守卫继续压真实锁）。
//
// 宿主安全：本文件只操作临时目录下的锁文件，不触碰注册表；包级 TestMain
// （userenv_hermetic_test.go）已为整包安装内存 PATH/环境后端。
package env

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// a11InstallLockPath 把锁路径指向临时目录（避免在开发者真实 ~/.xdev/.locks
// 下建文件），并捕获日志用于「是否降级」的断言。返回锁路径与日志缓冲。
func a11InstallLockPath(t *testing.T) (lockPath string, logBuf *bytes.Buffer) {
	t.Helper()

	lockPath = filepath.Join(t.TempDir(), "userpath.lock")
	prevLockFn := userPathLockPathFn
	userPathLockPathFn = func() (string, error) { return lockPath, nil }
	t.Cleanup(func() { userPathLockPathFn = prevLockFn })

	logBuf = &bytes.Buffer{}
	prevOut := log.Writer()
	log.SetOutput(logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	return lockPath, logBuf
}

// a11AssertLockHeldByThisProcess 断言锁文件**当前被本进程持有**：以 0 超时
// （非阻塞）再取一次必然失败。这把「内层确实处在持锁窗口内」变成可观测事实。
func a11AssertLockHeldByThisProcess(t *testing.T, lockPath, when string) {
	t.Helper()
	fl, err := util.AcquireFileLock(lockPath, 0)
	if err == nil {
		fl.Release()
		t.Errorf("%s: 锁文件 %s 竟然可被再次获取 —— 该临界区实际处于**无锁**状态（A-11 缺陷原形）", when, lockPath)
	}
}

// TestWithUserPathLock_NestedCallDoesNotDegrade 是 A-11 的主守卫。
//
// 断言四件事：
//  1. 嵌套调用**不阻塞**（< 2s；缺陷原形实测 10.03s）；
//  2. 内层 fn **真的执行**（不是被跳过/降级吞掉）；
//  3. 嵌套**未触发降级警告**（缺陷原形的可观测标志）；
//  4. 内层被登记为同一持有者的嵌套（深度 +1）—— 即它走的是可重入快路，
//     而不是「重复加锁失败后降级」那条路。
func TestWithUserPathLock_NestedCallDoesNotDegrade(t *testing.T) {
	lockPath, logBuf := a11InstallLockPath(t)

	var innerRan bool
	var outerDepthSeen, innerDepthSeen int

	start := time.Now()
	err := withUserPathLock(func() error {
		outerDepthSeen = userPathLockDepth
		// 外层确实持锁：此刻本进程再取同一把锁必须失败。
		a11AssertLockHeldByThisProcess(t, lockPath, "外层临界区内")

		return withUserPathLock(func() error {
			innerRan = true
			innerDepthSeen = userPathLockDepth
			// 内层同样是**持锁**状态（缺陷原形下这里是「已降级、无锁」）。
			a11AssertLockHeldByThisProcess(t, lockPath, "内层嵌套临界区内")
			return nil
		})
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("嵌套 withUserPathLock 返回错误: %v", err)
	}
	if !innerRan {
		t.Fatal("内层 fn 未执行 —— 嵌套调用被吞掉")
	}
	// 2s 阈值：缺陷原形实测 10.08s（阻塞满 10s 争用窗口），修复后 ~1ms。
	if elapsed >= 2*time.Second {
		t.Errorf("嵌套调用耗时 %v（>= 2s）—— 仍在阻塞等待跨进程锁（A-11 缺陷原形：内层重复加锁失败前阻塞满 10s）", elapsed)
	}
	if outerDepthSeen < 1 {
		t.Errorf("外层临界区内 userPathLockDepth=%d, want >= 1（外层未登记为持有者）", outerDepthSeen)
	}
	if innerDepthSeen != outerDepthSeen+1 {
		t.Errorf("内层 userPathLockDepth=%d, want %d（内层未被视为同一持有者的嵌套）", innerDepthSeen, outerDepthSeen+1)
	}
	if out := logBuf.String(); strings.Contains(out, "userpath lock degraded") {
		t.Errorf("嵌套调用触发了降级警告（内层临界区无跨进程序列化）: %q", strings.TrimSpace(out))
	}
}

// TestWithUserPathLock_NestedIsStillSerialized 断言「不降级」之外，嵌套期间
// 跨进程锁**始终由本进程持有**，且退出后正确释放（无泄漏）。
//
// 这是「修复没有把锁削弱成摆设」的关键回归守卫：缺陷原形下内层降级 = 锁在
// 内层窗口内实际是空闲的，其它进程可以长驱直入。
func TestWithUserPathLock_NestedIsStillSerialized(t *testing.T) {
	lockPath, logBuf := a11InstallLockPath(t)

	var inOuter, inInner bool
	err := withUserPathLock(func() error {
		inOuter = true
		a11AssertLockHeldByThisProcess(t, lockPath, "外层")
		return withUserPathLock(func() error {
			inInner = true
			a11AssertLockHeldByThisProcess(t, lockPath, "内层")
			return nil
		})
	})
	if err != nil {
		t.Fatalf("嵌套 withUserPathLock: %v", err)
	}
	if !inOuter || !inInner {
		t.Fatalf("外层/内层临界区未全部执行: outer=%v inner=%v", inOuter, inInner)
	}

	if out := logBuf.String(); strings.Contains(out, "userpath lock degraded") {
		t.Errorf("嵌套期间出现降级警告: %q", strings.TrimSpace(out))
	}

	// 全部退出后锁必须已释放（否则后续任何 PATH 写入都会白等 10s）。
	fl, err := util.AcquireFileLock(lockPath, 0)
	if err != nil {
		t.Fatalf("嵌套返回后锁未释放（泄漏）: %v", err)
	}
	fl.Release()
}

// TestWithUserPathLock_CrossProcessStillExclusive 是 A-11 修复的**反向**守卫：
// 修复不得削弱真实互斥。两条独立路径都要压：
//  1. withUserPathLock 持锁期间，独立的 AcquireFileLock 必须失败；
//  2. 两个独立 AcquireFileLock 之间仍互斥。
func TestWithUserPathLock_CrossProcessStillExclusive(t *testing.T) {
	lockPath, _ := a11InstallLockPath(t)

	// 1. withUserPathLock 仍然取真实锁（可重入快路只对**同 goroutine** 生效，
	//    不能把锁整体绕过）。
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withUserPathLock(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	if fl, err := util.AcquireFileLock(lockPath, 0); err == nil {
		fl.Release()
		t.Error("withUserPathLock 持锁期间，独立 AcquireFileLock 竟然成功 —— 互斥被削弱")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("withUserPathLock: %v", err)
	}

	// 2. 释放后互斥语义不变：第一个成功、第二个失败。
	first, err := util.AcquireFileLock(lockPath, 0)
	if err != nil {
		t.Fatalf("释放后首个 AcquireFileLock 必须成功: %v", err)
	}
	if second, err2 := util.AcquireFileLock(lockPath, 200*time.Millisecond); err2 == nil {
		second.Release()
		t.Error("同进程内两个独立 AcquireFileLock 不互斥 —— 跨进程序列化前提被破坏")
	}
	first.Release()
}

// TestWithUserPathLock_OtherGoroutineIsNotTreatedAsNested 守住可重入快路的
// 边界：**不同 goroutine** 绝不能被当作「嵌套」放行（否则两个 goroutine 会
// 同时进入临界区，等于重新引入丢写）。
//
// 同时断言该形态沿用既有契约：不是降级、也不是并发，而是真正互斥排队。
func TestWithUserPathLock_OtherGoroutineIsNotTreatedAsNested(t *testing.T) {
	_, logBuf := a11InstallLockPath(t)

	var concurrent, maxConcurrent int32
	var ran int32
	const workers = 4

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := withUserPathLock(func() error {
				atomic.AddInt32(&ran, 1)
				n := atomic.AddInt32(&concurrent, 1)
				for {
					old := atomic.LoadInt32(&maxConcurrent)
					if n <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
				return nil
			}); err != nil {
				t.Errorf("withUserPathLock: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&ran); got != workers {
		t.Errorf("执行了 %d 个临界区, want %d", got, workers)
	}
	if got := atomic.LoadInt32(&maxConcurrent); got != 1 {
		t.Errorf("临界区内最大并发 = %d, want 1 —— 不同 goroutine 被误判为可重入嵌套（互斥失效）", got)
	}
	if out := logBuf.String(); strings.Contains(out, "userpath lock degraded") {
		t.Errorf("同进程不同 goroutine 的正常排队不应降级: %q", strings.TrimSpace(out))
	}
}
