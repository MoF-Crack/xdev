package shim

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// staleResidueMaxAge 是「该回收的 `.stale-*` 残留」的年龄门槛。
//
// 第十一轮审计（Round 10 报告待办 3）：Round 10 只做了**过滤**（`IsStaleResidueName`），
// 没有做**回收** —— `.stale-*` 会永久占着 `current/` 下的目录。`removeLinkEntry`
// 在 `os.RemoveAll(stale)` 失败时只记 WARNING（那是刻意的：失败不该让安装流程中断），
// 但没有任何后续清扫，于是残留只增不减。
//
// 为什么按**年龄**而不是无条件删：`removeLinkEntry` 先 `os.Rename` 再 `RemoveAll`，
// 中间那一瞬 `.stale-*` 是**活着的临时名**。并发进程（GUI 与 CLI）同时扫描时，
// 无条件删会把另一个进程正在删除的目录抽走。1 小时远大于任何一次激活/卸载的耗时。
var staleResidueMaxAge = time.Hour

// staleResidueDirName 是 `current/` **之外**的归档目录名。
//
// 第十二轮收口（Round 12 报告 §七-③）：把残留的暂存地从 `current/<tool>.stale-<rand>`
// 换成 `<xmdeHome>/.stale-residue/`。原因见 `CollectStaleResidue` 的注释 ——
// 一句话：**归档目录里的条目永远不可能「在途」**，因此可以立刻删；而 `current/`
// 从此根本不会出现 `.stale-*`。
const staleResidueDirName = ".stale-residue"

// defaultResidueSweepInterval 是已启用进程内**后台清扫**的周期。
//
// 60s 是刻意的权衡：残留的年龄门槛是 1 小时，因此 60s 的检查粒度完全够用；
// 而它对「长时间运行的 GUI 宿主」又足够及时（不会让残留累积一小时以上）。
const defaultResidueSweepInterval = 60 * time.Second

// residueSweepInterval 为变量以便测试用极短周期驱动后台清扫。
var residueSweepInterval = defaultResidueSweepInterval

// collectMu 保证同一进程内不会有两个采集器同时往 current/ 里搬东西。
// 用**普通互斥量**而不是「名为 .sweep- 的目录」—— 后者本身就是 current/ 下的污染，
// 而且要处理「持有者崩溃留下永久锁」的问题。
var collectMu sync.Mutex

// 后台清扫器状态：整个进程只启动**一个** goroutine（Start 是幂等的）。
var (
	sweepOnce     sync.Once
	sweepStopOnce sync.Once
	sweepStop     chan struct{}
	sweepStopped  chan struct{}
)

// SetResidueSweepIntervalForTest 让测试用极短周期驱动后台清扫。
// 必须在 StartStaleResidueSweeper 之前调用。
func SetResidueSweepIntervalForTest(d time.Duration) { residueSweepInterval = d }

// ResetResidueSweeperForTest 复位后台清扫器的包级状态（仅测试用）。
func ResetResidueSweeperForTest() {
	if sweepStop != nil {
		select {
		case <-sweepStopped:
		default:
			sweepStopOnce.Do(func() { close(sweepStop) })
			<-sweepStopped
		}
	}
	sweepOnce = sync.Once{}
	sweepStopOnce = sync.Once{}
	sweepStop = nil
	sweepStopped = nil
	residueSweepInterval = defaultResidueSweepInterval
}

// StartStaleResidueSweeper 启动进程内**唯一**的残留清扫 goroutine（幂等）。
//
// 第十二轮收口（Round 12 报告 §七-③）：此前 `.stale-*` 回收有**三个**调用点
// （`buildShimIndexMap` / `Activate` / `Uninstall`），而其中 `buildShimIndexMap` 那条
// 已经证明会引发无限递归（Round 11 修复 2b：`buildShimIndexMap → ReapStaleResidue →
// InvalidateShimIndex → writeDiskShimIndex → buildShimIndexMap`，实测全量测试从
// 1.2 分钟变成 >20 分钟不结束且**没有任何测试失败**）。
//
// 三个调用点等于把同一个地雷铺了三份。本函数把它收成**一个显式生命周期**：
//   - 宿主入口（CLI 或 GUI 启动）调一次 `StartStaleResidueSweeper()`；
//   - 退出时调 `StopStaleResidueSweeper()`（CLI）或让它常驻（GUI）；
//   - 短命进程靠 `removeLinkEntry` 的**归档式回收**保证卫生，不依赖 ticker。
//
// 设计上刻意**不**把回收挂回任何扫描函数：扫描必须保持纯读。否则又会有人
// 在扫描里加副作用，又会长出一条环 —— 这正是 Round 11 那个递归的成因。
func StartStaleResidueSweeper(xmdeHome string) {
	if xmdeHome == "" {
		return
	}
	sweepOnce.Do(func() {
		sweepStop = make(chan struct{})
		sweepStopped = make(chan struct{})
		interval := residueSweepInterval
		go func() {
			defer close(sweepStopped)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-sweepStop:
					return
				case <-ticker.C:
					ReapStaleResidue(xmdeHome)
				}
			}
		}()
		log.Printf("[shim] stale-residue sweeper started (interval %s)", interval)
	})
}

// StopStaleResidueSweeper 停止后台清扫并等待 goroutine 退出（幂等）。
func StopStaleResidueSweeper() {
	if sweepStop == nil {
		return
	}
	sweepStopOnce.Do(func() { close(sweepStop) })
	<-sweepStopped
}

// ReapStaleResidue 是**唯一**的回收入口：完成「采集 → 就地清 → 清归档」三步。
//
// 只在条目**确实过期**（mtime 早于 cutoff）时搬/删，且失败一律忽略（best-effort）——
// 回收是卫生工作，绝不该让调用方（安装/激活/扫描）失败。
//
// 返回搬走 + 删除的条目总数，便于调用方与测试断言。
func ReapStaleResidue(xmdeHome string) int {
	if xmdeHome == "" {
		return 0
	}
	cutoff := time.Now().Add(-staleResidueMaxAge)

	// 1. 先把 current/ 里**已过期**的残留搬进 current/ 之外的归档目录。
	//    必须排在删除之前：搬走之后它们就不再污染 `current/` 枚举与 shim 索引，
	//    而真正的删除可以稍后再试（甚至留给下次启动）。
	collected := CollectStaleResidue(xmdeHome)
	// 2. 就地删除 current/ 里剩余的过期残留（历史遗留，或搬运失败但本可删的）。
	purgedCurrent := purgeStaleInCurrent(xmdeHome, cutoff)
	// 3. 清空归档目录里**已过期**的条目。归档目录里的东西已经不在 current/ 下，
	//    不需要「是否在途」的判断 —— 只要够旧就可以删。
	//
	//    这一步**不计入返回值**：被清掉的正是第 1 步刚搬进去的那些条目，
	//    已被 `collected` 计过一次。若这里再计一次，同一个残留会被算成 2 个 ——
	//    调用方（以及守卫）看到的「条目数」会凭空翻倍。真正的删除量仍在日志里。
	purgedArchive := purgeArchive(xmdeHome, cutoff)

	// 语义：返回「被处理的**不同**条目数」= 从 current/ 搬走的 + 就地删掉的。
	total := collected + purgedCurrent
	if total > 0 || purgedArchive > 0 {
		log.Printf("[shim] reaped %d stale residue entr(ies) under %s (archived=%d purgedInPlace=%d archivePurged=%d)",
			total, xmdeHome, collected, purgedCurrent, purgedArchive)
	}
	return total
}

// CollectStaleResidue 把 `current/` 下**已过期**的 `.stale-*` / `.tmp-*` 残留
// `os.Rename` 到 `current/` 之外的归档目录（`<xmdeHome>/.stale-residue/`）。
//
// # 为什么需要这一步（本轮最关键的设计取舍）
//
// `removeLinkEntry` 原先的 rename 目标是 `current/<tool>.stale-<rand>` ——
// 也就是**残留留在了 `current/` 里**。这产生了一组互相矛盾的需求：
//
//	回收方要「够旧就删」；而并发写方可能刚 rename 完、正要去 RemoveAll。
//	为保证不误删在途条目，回收方只能按年龄等待（1 小时）——
//	于是刚产生的残留必然在 current/ 里停留一小时，期间它仍然是一个
//	需要被所有扫描方过滤掉的污染源。
//
// 换一个 rename 目标（搬到 current/ **之外**）就把这对矛盾解开了：
//   - 归档目录里的条目**永远不可能在途**（写方已经放弃它、或它在 current/ 里已经
//     老到不会有人在处理）；
//   - 因此归档目录里的东西可以安全删除；
//   - 而 `current/` 从此**根本不会**出现 `.stale-*`（除历史遗留）。
//
// 返回搬走的条目数。
func CollectStaleResidue(xmdeHome string) int {
	collectMu.Lock()
	defer collectMu.Unlock()

	currentDir := filepath.Join(xmdeHome, "current")
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-staleResidueMaxAge)
	moved := 0
	for _, e := range entries {
		if !isStaleResidueName(e.Name()) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(cutoff) {
			continue // 在途（或无法判定）—— 不动
		}
		dst, derr := archivePath(xmdeHome, e.Name())
		if derr != nil {
			continue
		}
		if rerr := os.Rename(filepath.Join(currentDir, e.Name()), dst); rerr == nil {
			moved++
		}
		// rename 失败（跨卷/被占用）：留给 purgeStaleInCurrent 就地删。
	}
	return moved
}

// archivePath 供包内采集器使用。
func archivePath(xmdeHome, name string) (string, error) {
	return ResidueArchivePath(xmdeHome, name)
}

// ResidueArchivePath 返回 `<xmdeHome>/.stale-residue/` 下一个**唯一**的目标路径，
// 供把残留归档到 `current/` **之外**使用（`installer.removeLinkEntry` 直接调用）。
//
// 导出原因：`removeLinkEntry` 是残留的**唯一产生者**，它必须把 rename 目标放在
// `current/` 之外 —— 否则「刚产生的残留」又要等 1 小时才可能被回收，而它在那一
// 小时内仍是被所有扫描方必须过滤的污染源。把路径构造放在这里（而不是在 installer
// 里再写一遍）保证两处用的是**同一个**归档目录名。
//
// 时间戳 + 原名字：避免两次归档撞名（撞名会让后一次 rename 覆盖前一次、丢掉证据）。
func ResidueArchivePath(xmdeHome, name string) (string, error) {
	dir := filepath.Join(xmdeHome, staleResidueDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 36)+"-"+name), nil
}

// purgeStaleInCurrent 就地删除 `current/` 下早于 cutoff 的 xdev 残留条目。
func purgeStaleInCurrent(xmdeHome string, cutoff time.Time) int {
	currentDir := filepath.Join(xmdeHome, "current")
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !isStaleResidueName(e.Name()) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if RemoveResidueEntry(filepath.Join(currentDir, e.Name())) {
			removed++
		}
	}
	return removed
}

// purgeArchive 删除归档目录里早于 cutoff 的条目。
func purgeArchive(xmdeHome string, cutoff time.Time) int {
	dir := filepath.Join(xmdeHome, staleResidueDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if RemoveResidueEntry(filepath.Join(dir, e.Name())) {
			removed++
		}
	}
	return removed
}

// RemoveResidueEntry 删除一个残留条目：链接用 `os.Remove`（只解开 reparse point），
// 其余用 `os.RemoveAll`。
//
// 与 `removeLinkEntry` 的「绝不顺着 reparse point 递归」同一条铁律。
// 失败时尝试清掉只读位再重试一次（平台相关部分见 `clearReadOnly`）。
func RemoveResidueEntry(full string) bool {
	if li, lerr := os.Lstat(full); lerr == nil && li.Mode()&os.ModeSymlink != 0 {
		return os.Remove(full) == nil
	}
	if err := os.RemoveAll(full); err == nil {
		return true
	}
	clearReadOnly(full) // 兜底：清只读位再试一次
	return os.RemoveAll(full) == nil
}
