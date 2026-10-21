package shim

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// 第十一轮审计（Round 10 报告待办 3）：`.stale-*` 残留**只过滤、不回收**。
//
// 缺陷现场：`removeLinkEntry` 在 `os.RemoveAll(stale)` 失败时只记 WARNING
//（那是刻意的 —— 失败不该让安装流程中断），但全仓**没有任何**后续清扫，
// 于是 `current/<tool>.stale-<rand>` 只增不减，永久占着 `current/` 下的目录。
// Round 10 只做了过滤（`IsStaleResidueName`），把「不被当成工具」解决了，
// 但「残留本身还在」没解决。
//
// 修复：新增 `ReapStaleResidue` —— 按**年龄**（默认 1 小时）回收 `.stale-*` /
// `.tmp-*`，并在 `buildShimIndexMap`（枚举 current/ 的唯一热点）里顺带调用。
//
// 为什么必须按年龄而不是无条件删：`removeLinkEntry` 是「先 rename 再 RemoveAll」，
// 中间那一瞬 `.stale-*` 是**活着的临时名**；GUI 与 CLI 并发时无条件删会把另一个
// 进程正在删除的目录抽走。这与 Round 10 的 `.tmp-*` 清扫（`util.sweepStaleTempFiles`）
// 是同一条设计原则。

// makeStaleEntry 在 current/ 下造一个带 bin/ 的残留目录，并把 mtime 拨到指定年龄。
func makeStaleEntry(t *testing.T, home, name string, age time.Duration) string {
	t.Helper()
	bin := filepath.Join(home, "current", name, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// 放一个可执行文件，模拟「残留目录里真有东西会被索引」的最坏形态。
	if err := os.WriteFile(filepath.Join(bin, "ghost"+testExeSuffix()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	// 目录本身的 mtime 才是判据（e.Info() 返回的就是它）。
	if err := os.Chtimes(filepath.Join(home, "current", name), when, when); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, "current", name)
}

// TestReapStaleResidue_RemovesAgedEntries 是核心守卫：过期残留必须被删。
func TestReapStaleResidue_RemovesAgedEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	staleDir := makeStaleEntry(t, home, "tool.stale-old", 2*time.Hour)
	tmpDir := makeStaleEntry(t, home, "tool.tmp-old", 2*time.Hour)

	removed := ReapStaleResidue(home)
	if removed != 2 {
		t.Errorf("ReapStaleResidue 回收 %d 个，want 2", removed)
	}
	for _, p := range []string{staleDir, tmpDir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("待办 3 复现：过期残留未被回收: %s", p)
		}
	}
}

// **核心安全断言**：新近的残留（模拟在途的 rename→RemoveAll 中间态）绝不能被删。
func TestReapStaleResidue_KeepsFreshEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	fresh := makeStaleEntry(t, home, "tool.stale-fresh", 0)

	removed := ReapStaleResidue(home)
	if removed != 0 {
		t.Errorf("ReapStaleResidue 回收 %d 个，want 0（在途残留不得被删）", removed)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("待办 3 复现：在途的 .stale-* 被误删（会与并发进程的 RemoveAll 竞争）: %v", err)
	}
}

// 真正的工具目录绝不能被回收（哪怕它很旧）。
func TestReapStaleResidue_NeverTouchesRealTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	realTool := makeStaleEntry(t, home, "faketool", 48*time.Hour)

	removed := ReapStaleResidue(home)
	if removed != 0 {
		t.Errorf("ReapStaleResidue 回收 %d 个，want 0（真实工具目录不得被删）", removed)
	}
	if _, err := os.Stat(realTool); err != nil {
		t.Errorf("待办 3 复现：真实工具目录被回收了！%v", err)
	}
}

// 端到端：buildShimIndexMap（扫描热点）必须顺带回收过期残留，
// 且索引里既不含残留、也不含被回收的条目。
func TestBuildShimIndexMap_ReapsAgedResidue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	t.Cleanup(resetShimIndexState)

	// 真工具。
	realBin := filepath.Join(home, "current", "tool", "bin")
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realBin, "tool"+testExeSuffix()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	staleDir := makeStaleEntry(t, home, "tool.stale-abc", 3*time.Hour)

	idx := buildShimIndexMap(home)

	// 真工具在索引里。
	if e, ok := idx[normalizeShimAlias("tool")]; !ok || e.toolName != "tool" {
		t.Errorf("真工具别名未进索引: %+v", e)
	}
	// 残留里的 ghost **永不**进索引（这一条与回收时机无关，是永久的过滤契约）。
	if e, ok := idx[normalizeShimAlias("ghost")]; ok {
		t.Errorf("残留里的 ghost 进了索引: %+v", e)
	}

	// 第十二轮收口：**`buildShimIndexMap` 不再回收**（扫描必须纯读 ——
	// 扫描里的副作用正是 Round 11 无限递归的根因）。因此这里**不再**断言
	// 「过期残留已被物理删除」，而是断言它**仍然存在**：这才是本轮契约。
	//
	// 回收改由 `ReapStaleResidue` / `StartStaleResidueSweeper` 负责，
	// 其行为由 `residue_r13_test.go` 的用例覆盖。
	if _, err := os.Stat(staleDir); err != nil {
		t.Errorf("本轮契约：buildShimIndexMap 是纯读，不应删除残留；"+
			"但它不见了（%s）—— 说明扫描里又混进了副作用: %v", staleDir, err)
	}
}

// 时间门槛是可注入的变量（便于运维收紧/放宽），不能硬编码。
func TestReapStaleResidue_HonoursConfigurableAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	p := makeStaleEntry(t, home, "tool.stale-x", 10*time.Second)

	// 默认 1 小时门槛下不该被删。
	if n := ReapStaleResidue(home); n != 0 {
		t.Errorf("默认门槛下回收 %d 个，want 0", n)
	}
	// 收紧到 1 秒后应被回收。
	prev := staleResidueMaxAge
	staleResidueMaxAge = time.Second
	defer func() { staleResidueMaxAge = prev }()

	if n := ReapStaleResidue(home); n != 1 {
		t.Errorf("收紧门槛后回收 %d 个，want 1", n)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("收紧门槛后残留未被回收: %s", p)
	}
}

// current/ 不存在时不得 panic（全新 home）。
func TestReapStaleResidue_MissingCurrentDirIsSafe(t *testing.T) {
	home := t.TempDir()
	if n := ReapStaleResidue(home); n != 0 {
		t.Errorf("current/ 不存在时回收 %d 个，want 0", n)
	}
}
