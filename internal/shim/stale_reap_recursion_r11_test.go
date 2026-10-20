package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// 第十一轮审计（**本轮自查抓到**）：`ReapStaleResidue` 曾调用
// `InvalidateShimIndex()`，形成无限递归：
//
//	buildShimIndexMap → ReapStaleResidue → InvalidateShimIndex
//	→ writeDiskShimIndex → (内存索引为 nil 时) buildShimIndexMap → …
//
// 实测症状极具迷惑性：全量 `go test ./...` 从 ~1.3 分钟变成 **>20 分钟仍未结束**，
// 而且**没有任何一条测试失败**（没有栈溢出 —— 每次递归都在做真实的目录遍历 +
// 文件写，只是永远不返回）。这类「不是崩溃、只是永远不结束」的缺陷，靠「跑一遍看
// 有没有红」是抓不到的，必须有**有界的**断言。
//
// 本守卫用两件事钉住它：
//  1. **源码级**：`ReapStaleResidue` 的函数体内不得出现 `InvalidateShimIndex`
//     （确定性、不依赖时序）。
//  2. **行为级**：在**内存索引为 nil** 的最坏状态下（`resetShimIndexState` 之后）
//     调用 `writeDiskShimIndex`，必须在**有界时间**内返回 —— 递归版本会永远跑下去。

// TestReapStaleResidue_SourceHasNoIndexInvalidation 是确定性守卫。
//
// 注意：判据必须**忽略注释**。`ReapStaleResidue` 的文档注释里为了解释这条递归
// 恰恰**写着** `InvalidateShimIndex` 这个名字（本守卫初版就因此对自己误报）。
// 这里逐行剥掉 `//` 注释与 `/* */` 块后再匹配。
func TestReapStaleResidue_SourceHasNoIndexInvalidation(t *testing.T) {
	data, err := os.ReadFile("shim.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	start := strings.Index(src, "func ReapStaleResidue(")
	if start < 0 {
		t.Fatal("ReapStaleResidue 未找到")
	}
	rest := src[start:]
	if end := strings.Index(rest[1:], "\nfunc "); end > 0 {
		rest = rest[:end+1]
	}

	// 去掉注释后只剩可执行代码。
	var code strings.Builder
	inBlock := false
	for _, line := range strings.Split(rest, "\n") {
		t := strings.TrimSpace(line)
		if inBlock {
			if idx := strings.Index(t, "*/"); idx >= 0 {
				inBlock = false
				t = strings.TrimSpace(t[idx+2:])
			} else {
				continue
			}
		}
		if strings.HasPrefix(t, "//") {
			continue
		}
		if idx := strings.Index(t, "/*"); idx >= 0 {
			if j := strings.Index(t[idx:], "*/"); j >= 0 {
				t = strings.TrimSpace(t[:idx] + t[idx+j+2:])
			} else {
				inBlock = true
				t = strings.TrimSpace(t[:idx])
			}
		}
		code.WriteString(t)
		code.WriteString("\n")
	}

	if strings.Contains(code.String(), "InvalidateShimIndex(") {
		t.Error("ReapStaleResidue 内不得调用 InvalidateShimIndex —— " +
			"会形成 buildShimIndexMap → ReapStaleResidue → InvalidateShimIndex → " +
			"writeDiskShimIndex → buildShimIndexMap 的无限递归")
	}
}

// TestWriteDiskShimIndex_RebuildTerminates 是行为守卫（有界时间）。
//
// 关键前提：先 `resetShimIndexState()` 把**内存索引置 nil** —— 这正是递归版本的
// 触发条件（`writeDiskShimIndex` 见 idx==nil 就 rebuild）。
func TestWriteDiskShimIndex_RebuildTerminates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	t.Cleanup(resetShimIndexState)

	// 造一个**过期**的 `.stale-*` 残留：递归只在「确有残留被回收」时才走
	// InvalidateShimIndex 分支 —— 没有残留就测不出该缺陷。
	staleBin := filepath.Join(home, "current", "tool.stale-recursion", "bin")
	if err := os.MkdirAll(staleBin, 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(home, "current", "tool.stale-recursion"), when, when); err != nil {
		t.Fatal(err)
	}
	// 真工具，让 rebuild 有活可干。
	realBin := filepath.Join(home, "current", "tool", "bin")
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realBin, "tool"+testExeSuffix()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	resetShimIndexState() // 内存索引 = nil → 触发 rebuild 路径

	done := make(chan struct{})
	go func() {
		defer close(done)
		writeDiskShimIndex(home)
	}()

	select {
	case <-done:
		// 正常返回 —— 递归已消除。
	case <-time.After(20 * time.Second):
		t.Fatal("待办 3 回归：writeDiskShimIndex 在 20 秒内未返回 —— " +
			"Rebuild 路径疑似再次陷入 buildShimIndexMap ↔ InvalidateShimIndex 递归")
	}
}
