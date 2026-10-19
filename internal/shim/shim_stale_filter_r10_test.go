package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// 第十轮审计 P3-8（P3）守卫（shim 侧）。
//
// 缺陷现场：`current/` 下的 `.stale-<随机>` / `.tmp-<随机>` 残留
// （removeLinkEntry 的旁路重命名产物、Activate 的临时链接名）会被
// buildShimIndexMap 的 `ReadDir(current)` + `Stat` + `IsDir` 当成**一个工具**：
// 审计实测扫描得到 `[tool tool.stale-demo]`，其内的可执行文件会进反向索引，
// 于是 `xdev shim <alias>` 可能分发到一条本不该存在的路径。
//
// 修复：扫描侧过滤 `.stale-` / `.tmp-` 前缀（这是可确定性验证的部分）。
// 回收策略见 installer 侧（按年龄）。

// TestBuildShimIndexMap_FiltersStaleAndTmpEntries 是缺陷的端到端复现：
// current/ 下同时存在真工具目录与两种残留，反向索引必须只认真工具。
func TestBuildShimIndexMap_FiltersStaleAndTmpEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	// 真工具：current/tool/bin/tool[.exe]
	realBin := filepath.Join(home, "current", "tool", "bin")
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	realExe := filepath.Join(realBin, "tool"+testExeSuffix())
	if err := os.WriteFile(realExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 残留一：.stale- 旁路目录（removeLinkEntry 的 rename 产物）。
	staleDir := filepath.Join(home, "current", "tool.stale-demo", "bin")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleExe := filepath.Join(staleDir, "ghost"+testExeSuffix())
	if err := os.WriteFile(staleExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 残留二：.tmp- 临时链接名（Activate 的 os.CreateTemp 产物）。
	tmpDir := filepath.Join(home, "current", "tool.tmp-1234")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tempalias"+testExeSuffix()), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx := buildShimIndexMap(home)

	// 真工具的别名必须在索引里。
	if e, ok := idx[normalizeShimAlias("tool")]; !ok || e.toolName != "tool" {
		t.Errorf("真工具别名 tool 未进索引：%+v (ok=%v)", e, ok)
	}
	// 残留目录内的可执行文件绝不能被当成工具（P3-8 的核心断言）。
	if e, ok := idx[normalizeShimAlias("ghost")]; ok {
		t.Errorf("P3-8 复现：.stale- 残留目录里的 %q 被当成工具 %q 注册进反向索引（路径 %s）",
			"ghost", e.toolName, e.path)
	}
	if e, ok := idx[normalizeShimAlias("tempalias")]; ok {
		t.Errorf("P3-8 复现：.tmp- 残留目录里的 %q 被当成工具 %q 注册进反向索引（路径 %s）",
			"tempalias", e.toolName, e.path)
	}
	// 残留目录本身也不得作为工具名出现。
	for _, bad := range []string{"tool.stale-demo", "tool.tmp-1234"} {
		if e, ok := idx[normalizeShimAlias(bad)]; ok {
			t.Errorf("残留条目 %q 被当成工具注册：%+v", bad, e)
		}
	}
}

// TestLookup_DoesNotDispatchIntoStaleResidue 是缺陷的**后果**断言：
// 残留目录里的可执行文件不得被 Lookup 解析出来（否则 shim 会把命令
// 分发到一条本不该存在的路径）。
func TestLookup_DoesNotDispatchIntoStaleResidue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	t.Cleanup(resetShimIndexState)

	staleBin := filepath.Join(home, "current", "tool.stale-demo", "bin")
	if err := os.MkdirAll(staleBin, 0o755); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(staleBin, "ghost"+testExeSuffix())
	if err := os.WriteFile(residue, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	target, _, _, err := Lookup("ghost", nil)
	if err == nil {
		t.Fatalf("P3-8 复现：Lookup 把残留目录里的可执行文件解析成了目标 %q", target)
	}
	if target != "" {
		t.Errorf("Lookup 返回了残留路径 %q，want 空（绝不返回残留路径）", target)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("错误信息 = %q，want 'not installed' 提示", err.Error())
	}
}
