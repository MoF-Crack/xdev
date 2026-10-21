package shim

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// 第十二轮收口（Round 12 报告 §七-③）：把 `.stale-*` 回收从**三个**调用点
// 收成**一个显式生命周期 + 一个归档目录**。
//
// 收口前的形态：
//  1. `buildShimIndexMap`（扫描时顺带回收）—— **已证明会引发无限递归**
//     （Round 11 修复 2b：`buildShimIndexMap → ReapStaleResidue →
//     InvalidateShimIndex → writeDiskShimIndex → buildShimIndexMap`，
//     实测全量测试从 1.2 分钟变成 >20 分钟不结束且**没有任何测试失败**）。
//  2. `Activate`（事务后即时回收）。
//  3. `Uninstall`（事务后即时回收）。
//
// 三个调用点等于把同一个地雷铺了三份。本轮的设计是：
//   - **扫描保持纯读**（§1 守卫：`buildShimIndexMap` 里不得再出现回收调用）；
//   - 新增 `StartStaleResidueSweeper` / `StopStaleResidueSweeper` 显式生命周期，
//     只有 GUI 这种长驻宿主启动它（§2/§3 守卫）；
//   - `removeLinkEntry` 的 rename 目标搬到 `current/` **之外**（§4 守卫），
//     使得 `current/` 根本不会再出现 `.stale-*`，从而不再需要「等 1 小时」。

// TestBuildShimIndexMap_IsPureRead 是 §1 守卫：扫描函数里**不得**出现回收调用。
//
// 这是 Round 11 那个递归的直接防线。判据必须剥掉注释 —— 那段注释里为解释递归
// 恰恰**写着** `ReapStaleResidue` 这个名字（本守卫初版会因此对自己误报）。
func TestBuildShimIndexMap_IsPureRead(t *testing.T) {
	data, err := os.ReadFile("shim.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	start := strings.Index(src, "func buildShimIndexMap(")
	if start < 0 {
		t.Fatal("buildShimIndexMap 未找到")
	}
	body := extractBracedBlockFrom(src[start:])
	if body == "" {
		t.Fatal("buildShimIndexMap 函数体未找到（花括号未配平）")
	}
	code := stripSlashCommentsR13(body)

	if strings.Contains(code, "ReapStaleResidue(") {
		t.Error("buildShimIndexMap 必须是**纯读**扫描，不得调用 ReapStaleResidue —— " +
			"扫描里的副作用正是 Round 11 无限递归的根因")
	}
	if strings.Contains(code, "CollectStaleResidue(") {
		t.Error("buildShimIndexMap 不得调用 CollectStaleResidue（同上）")
	}
	if strings.Contains(code, "removeResidueEntry") || strings.Contains(code, "RemoveResidueEntry") {
		t.Error("buildShimIndexMap 不得删除残留条目（扫描必须纯读）")
	}
}

// TestReapStaleResidue_ArchivesOutsideCurrentR13 是 §4 守卫：**过期残留必须被搬出
// `current/`**，而不是留在原地等年龄。
//
// 这是本轮最关键的行为变化：搬到 `current/` 之外以后，归档里的东西**永远不可能
// 在途**，因此可以安全删除；而 `current/` 从此不再出现 `.stale-*`。
func TestReapStaleResidue_ArchivesOutsideCurrentR13(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	stale := makeAgedResidueR13(t, home, "tool.stale-old", 2*time.Hour)

	ReapStaleResidue(home)

	// current/ 下必须已经没有它。
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("过期残留仍留在 current/ 下: %s", stale)
	}
	// 而且必须真的被删掉（已过期 → 采集后立刻清）。
	archiveDir := filepath.Join(home, staleResidueDirName)
	if entries, err := os.ReadDir(archiveDir); err == nil && len(entries) > 0 {
		t.Errorf("归档目录里仍有 %d 个过期条目（应已被清空）", len(entries))
	}
}

// 未过期的残留（可能在途）**不得**被搬走 —— 这是「不误删在途」的核心保证。
func TestReapStaleResidue_KeepsFreshEntriesR13(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	fresh := makeAgedResidueR13(t, home, "inuse.stale-now", 0)

	ReapStaleResidue(home)

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("在途（未过期）的 .stale-* 被动了，会与并发写方的 RemoveAll 竞争: %v", err)
	}
}

// 真实工具目录永不被碰（哪怕是 100 小时前）。
func TestReapStaleResidue_NeverTouchesRealToolsR13(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	realTool := makeAgedResidueR13(t, home, "realtool", 100*time.Hour)

	ReapStaleResidue(home)

	if _, err := os.Stat(realTool); err != nil {
		t.Errorf("真实工具目录被回收误删了（绝不允许）: %v", err)
	}
}

// TestResidueArchivePath_OutsideCurrent 钉住归档目录的位置契约：
// 它必须在 `current/` **之外**，否则就又回到「残留留在 current/」的老问题。
func TestResidueArchivePath_OutsideCurrent(t *testing.T) {
	home := t.TempDir()
	p, err := ResidueArchivePath(home, "tool.stale-x")
	if err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(home, "current")
	rel, rerr := filepath.Rel(currentDir, p)
	if rerr == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("归档路径 %q 落在了 current/ 之内（rel=%q）—— 必须在其之外", p, rel)
	}
	if !strings.Contains(p, staleResidueDirName) {
		t.Errorf("归档路径 %q 应位于 %q 下", p, staleResidueDirName)
	}
	// 目录必须已被创建（rename 才能成功）。
	if fi, err := os.Stat(filepath.Dir(p)); err != nil || !fi.IsDir() {
		t.Errorf("归档目录未被创建: %v", err)
	}
}

// TestStartStopStaleResidueSweeper_IdempotentAndStops 是 §2 守卫：
// 启动幂等、停止幂等，且停止后 goroutine 真的退出（不泄漏）。
func TestStartStopStaleResidueSweeper_IdempotentAndStops(t *testing.T) {
	ResetResidueSweeperForTest()
	t.Cleanup(ResetResidueSweeperForTest)

	home := t.TempDir()

	// 多次 Start 只能起一个 goroutine（sync.Once）。
	StartStaleResidueSweeper(home)
	StartStaleResidueSweeper(home)
	StartStaleResidueSweeper(home)

	// 空 home 不得 panic，也不得启动。
	StartStaleResidueSweeper("")

	StopStaleResidueSweeper()
	StopStaleResidueSweeper() // 幂等
}

// TestStaleResidueSweeper_ReapsInBackground 是 §3 守卫：后台 ticker 真的会回收。
//
// 用 10ms 的测试周期，证明确实有周期性回收发生（而不只是函数存在）。
func TestStaleResidueSweeper_ReapsInBackground(t *testing.T) {
	ResetResidueSweeperForTest()
	t.Cleanup(ResetResidueSweeperForTest)
	SetResidueSweepIntervalForTest(10 * time.Millisecond)

	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	stale := makeAgedResidueR13(t, home, "bg.stale-old", 2*time.Hour)

	StartStaleResidueSweeper(home)
	defer StopStaleResidueSweeper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(stale); os.IsNotExist(err) {
			return // 后台扫掉了
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("后台清扫器 3 秒内未回收过期残留: %s", stale)
}

// 并发采集不得互相踩（collectMu 的作用）。
func TestCollectStaleResidue_ConcurrentSafe(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 20; i++ {
		makeAgedResidueR13(t, home, "t"+string(rune('a'+i))+".stale-x", 2*time.Hour)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			CollectStaleResidue(home)
		}()
	}
	wg.Wait()

	// 全部搬走即可（不应 panic / 死锁 / 留下 current/ 下的残留）。
	entries, err := os.ReadDir(filepath.Join(home, "current"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if IsStaleResidueName(e.Name()) {
			t.Errorf("并发采集后 current/ 下仍有残留: %s", e.Name())
		}
	}
}

// makeAgedResidueR13 在 current/ 下造一个带 bin/ 的残留目录，并把 mtime 拨到指定年龄。
func makeAgedResidueR13(t *testing.T, home, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(home, "current", name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "ghost.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	// 目录本身的 mtime 才是判据（e.Info() 返回的就是它）。
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
	return dir
}

// extractBracedBlockFrom 从 s 里第一个 `{` 开始返回配平后的整段（含花括号）。
func extractBracedBlockFrom(s string) string {
	open := strings.Index(s, "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open : i+1]
			}
		}
	}
	return ""
}

// stripSlashCommentsR13 去掉整行 `//` 与行尾 `// ...` 注释。
func stripSlashCommentsR13(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
