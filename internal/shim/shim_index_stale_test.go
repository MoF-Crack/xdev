package shim

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// 守卫测试 28-3（审计 C-04 / P1-15）：磁盘索引命中后目标已消失（陈旧索引）
// 时，必须用 buildShimIndexMap 的现扫结果回答——绝不返回死路径，也绝不
// 回落到 sync.Once 内存索引（内存索引可能正是陈旧路径的来源，回环）。

type rawIndexEntry struct {
	alias    string
	toolName string
	path     string
}

// writeRawDiskIndex 按 shim-index.v1 的磁盘格式手写一份索引，便于构造
// "索引指向已删除路径"的场景。
func writeRawDiskIndex(t *testing.T, home string, entries ...rawIndexEntry) {
	t.Helper()
	path := diskShimIndexPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("xdev-shim-index\tv1\n")
	for _, e := range entries {
		sb.WriteString(e.alias + "\t" + e.toolName + "\t" + e.path + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pinMemoryIndexAsBuilt 把包级内存索引钉成"已构建（sync.Once 已花费）但
// 内容是陈旧死路径"的状态：这样任何"回落内存索引"的实现都会拿到死路径，
// 而只有真正现扫 current/* 才能得到活路径。
func pinMemoryIndexAsBuilt(alias, deadPath, toolName string) {
	shimIndexMu.Lock()
	shimIndex = map[string]shimEntry{alias: {path: deadPath, toolName: toolName}}
	shimIndexOnce = sync.Once{}
	shimIndexMu.Unlock()
	shimIndexOnce.Do(func() {}) // 标记为"已构建"（sync.Once 无法重置为已花费，Do 空函数即可）
}

// resetShimIndexState 归还包级状态，避免污染同包其它测试。
func resetShimIndexState() {
	shimIndexMu.Lock()
	shimIndex = nil
	shimIndexOnce = sync.Once{}
	shimIndexMu.Unlock()
}

// setupStaleIndexHome 建立一个 home：current/node/bin 下有活着的 node 与
// npm，同时磁盘索引里 npm 指向一条已被删除的路径。
func setupStaleIndexHome(t *testing.T) (home, liveNpm, deadPath string) {
	t.Helper()
	home = t.TempDir()
	binDir := filepath.Join(home, "current", "node", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeName := "node" + testExeSuffix()
	if err := os.WriteFile(filepath.Join(binDir, nodeName), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	npmName := "npm"
	if testExeSuffix() != "" {
		npmName = "npm.cmd"
	}
	liveNpm = filepath.Join(binDir, npmName)
	if err := os.WriteFile(liveNpm, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 陈旧路径：同目录下一个从未存在（或已被删除）的文件。
	deadPath = filepath.Join(binDir, "npm-deleted"+testExeSuffix())
	if _, err := os.Stat(deadPath); err == nil {
		t.Fatalf("test precondition failed: %s must not exist", deadPath)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	t.Cleanup(resetShimIndexState)
	return home, liveNpm, deadPath
}

// TestScanFor_StaleDiskIndexRescansCurrent 是 C-04 的主守卫：
// 磁盘索引（与内存索引）都指向死路径，而 current/* 里 npm 真实存在 ->
// Lookup 必须返回活路径。
func TestScanFor_StaleDiskIndexRescansCurrent(t *testing.T) {
	home, liveNpm, deadPath := setupStaleIndexHome(t)

	writeRawDiskIndex(t, home, rawIndexEntry{alias: "npm", toolName: "node", path: deadPath})
	pinMemoryIndexAsBuilt("npm", deadPath, "node")

	target, toolName, _, err := Lookup("npm", nil)
	if err != nil {
		t.Fatalf("Lookup(npm) with stale disk index: %v", err)
	}
	if target == deadPath {
		t.Fatalf("C-04: shim must not dispatch to the stale path %s", deadPath)
	}
	if target != liveNpm {
		t.Errorf("target = %q, want %q (fresh scan of current/*)", target, liveNpm)
	}
	if toolName != "node" {
		t.Errorf("toolName = %q, want node", toolName)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("returned target must exist: %v", err)
	}
}

// TestScanFor_StaleDiskIndexNoLiveCopyReportsMiss：索引指向死路径，且
// current/* 现扫也找不到该别名 -> 必须"明确未命中"（Lookup 报 not
// installed），而不是把死路径透传给 exec。
func TestScanFor_StaleDiskIndexNoLiveCopyReportsMiss(t *testing.T) {
	home, _, _ := setupStaleIndexHome(t)

	deadPath := filepath.Join(home, "current", "node", "bin", "ghost"+testExeSuffix())
	writeRawDiskIndex(t, home, rawIndexEntry{alias: "ghost", toolName: "node", path: deadPath})
	pinMemoryIndexAsBuilt("ghost", deadPath, "node")

	target, _, _, err := Lookup("ghost", nil)
	if err == nil {
		t.Fatalf("Lookup(ghost) with stale-only index must fail, got target %q", target)
	}
	if target != "" {
		t.Errorf("target = %q, want empty (never a dead path)", target)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %q, want 'not installed' message", err.Error())
	}
}

// TestScanFor_FreshDiskIndexStillShortCircuits：索引路径仍然存在时必须
// 直接采用磁盘索引的答案（C-04 不得退化成"每次都现扫"）。
//
// 判别手法：让磁盘索引把 npm 指向**存在但不同**的路径（node 本体），
// 而内存索引指向死路径。若实现"总是现扫"，会返回现扫得到的 npm 路径而
// 非磁盘索引里的 node 路径，断言即失败。
func TestScanFor_FreshDiskIndexStillShortCircuits(t *testing.T) {
	home, liveNpm, deadPath := setupStaleIndexHome(t)
	nodePath := filepath.Join(home, "current", "node", "bin", "node"+testExeSuffix())

	writeRawDiskIndex(t, home, rawIndexEntry{alias: "npm", toolName: "node", path: nodePath})
	pinMemoryIndexAsBuilt("npm", deadPath, "node")

	target, toolName, _, err := Lookup("npm", nil)
	if err != nil {
		t.Fatalf("Lookup(npm) with fresh disk index: %v", err)
	}
	if target != nodePath {
		t.Errorf("target = %q, want %q (fresh disk index must be trusted as-is, no rescan)", target, nodePath)
	}
	if target == liveNpm {
		t.Errorf("target = %q equals the rescan result — C-04 must not rescan when the indexed path exists", target)
	}
	if toolName != "node" {
		t.Errorf("toolName = %q, want node", toolName)
	}
}

// TestWriteDiskShimIndex_AtomicWriteLeavesNoResidue 覆盖 C-04 的第二部分：
// writeDiskShimIndex 改用 util.AtomicWriteFile（临时文件 + rename）。
// 断言可解析、内容完整、且不留下 .tmp 残留。
func TestWriteDiskShimIndex_AtomicWriteLeavesNoResidue(t *testing.T) {
	home, _, _ := setupStaleIndexHome(t)

	InvalidateShimIndex() // 清内存索引并写盘

	idxPath := diskShimIndexPath(home)
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("disk shim index missing after InvalidateShimIndex: %v", err)
	}
	if !strings.HasPrefix(string(data), "xdev-shim-index\tv1\n") {
		t.Errorf("disk index header corrupted:\n%s", data)
	}
	idx := loadDiskShimIndex(home)
	if idx == nil {
		t.Fatal("loadDiskShimIndex could not parse the freshly written index")
	}
	if e, ok := idx["node"]; !ok || e.toolName != "node" {
		t.Errorf("index entry for node = %+v (ok=%v), want toolName node", e, ok)
	}
	if _, err := os.Stat(idxPath + ".tmp"); err == nil {
		t.Errorf("AtomicWriteFile must not leave %s.tmp behind", idxPath)
	}
}
