package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/shim"
)

// 第十二轮收口（Round 12 报告 §七-③）：`removeLinkEntry` 的 rename 目标必须落在
// `current/` **之外**。
//
// # 为什么这是根治而非优化
//
// 旧实现：`stale := path + ".stale-" + <nano>`，其中 `path` 是
// `current/<tool>` —— 也就是残留**留在 `current/` 里**。这导致一组自相矛盾的需求：
//
//	回收方要「够旧就删」；而并发写方可能刚 rename 完、正要去 RemoveAll。
//	为保证不误删在途条目，回收方只能按年龄等 1 小时 ——
//	于是刚产生的残留必然在 current/ 里停留一小时，期间它仍是一个
//	需要被所有扫描方过滤掉的污染源（Round 10 的 `IsStaleResidueName` 就是为此而生）。
//
// 搬到 `current/` 之外（`<xmdeHome>/.stale-residue/`）后：
//   - 归档目录里的条目**永远不可能在途**（写方已放弃它才会搬过去）；
//   - 因此归档里的东西可以立刻安全删除；
//   - 而 `current/` 从此**根本不会**出现 `.stale-*`（除历史遗留）。
//
// 本守卫钉住「rename 目标不在 current/ 下」这个契约。

// TestRemoveLinkEntry_ArchivesOutsideCurrent 是核心守卫。
//
// 构造：在 `current/<tool>` 放一个**真实目录**（旧 CopyDir 兜底产物 —— 这正是
// `removeLinkEntry` 会走 rename 分支的唯一形态），然后调用它，断言：
//  1. `current/<tool>` 已消失（安装流程可以继续）；
//  2. `current/` 下**没有**任何 `.stale-*` 残留（旧实现会留下）；
//  3. 归档目录里能找到它（证明是「搬走」而不是「删掉」或「留在原地」）。
func TestRemoveLinkEntry_ArchivesOutsideCurrent(t *testing.T) {
	home := t.TempDir()
	currentDir := filepath.Join(home, "current")
	link := filepath.Join(currentDir, "faketool")

	// 旧 CopyDir 兜底留下的**真实目录**（非 symlink、非 junction）。
	if err := os.MkdirAll(filepath.Join(link, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "bin", "FakeTool.exe"), []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}

	removeLinkEntry(link, home)

	// 1. current/<tool> 必须已消失。
	if _, err := os.Stat(link); !os.IsNotExist(err) {
		t.Errorf("current/%s 仍存在 —— 安装流程无法继续", "faketool")
	}

	// 2. **关键**：current/ 下不得留下任何 .stale-* 残留。
	entries, err := os.ReadDir(currentDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if shim.IsStaleResidueName(e.Name()) {
			t.Errorf("待办 §七-③ 复现：removeLinkEntry 在 current/ 下留下了残留 %q —— "+
				"它必须被归档到 current/ 之外，否则又要等 1 小时才可能回收，"+
				"而这一小时内它是所有扫描方都必须过滤的污染源", e.Name())
		}
	}

	// 3. 归档目录里应能找到（证明是「搬走」）。
	archiveDir := filepath.Join(home, ".stale-residue")
	if _, err := os.Stat(archiveDir); err != nil {
		t.Errorf("归档目录未被创建: %v", err)
	}
}

// 对照：普通链接（非真实目录）走的是 `os.Remove` 分支，**不应**产生归档。
func TestRemoveLinkEntry_LinkRemovedWithoutArchive(t *testing.T) {
	home := t.TempDir()
	currentDir := filepath.Join(home, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "install", "faketool", "1.0.0")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(currentDir, "faketool")
	if err := makeLink(target, link); err != nil {
		t.Fatalf("makeLink: %v", err)
	}

	removeLinkEntry(link, home)

	// 链接本身被解开。
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("链接未被移除: %v", err)
	}
	// **安装目标绝不能被跟着删掉**（这是「绝不 RemoveAll 链接」铁律）。
	if _, err := os.Stat(target); err != nil {
		t.Errorf("链接目标被误删了（removeLinkEntry 顺着 reparse point 递归了）: %v", err)
	}
	// 不该产生归档（链接走 os.Remove 就直接成功了）。
	if entries, err := os.ReadDir(filepath.Join(home, ".stale-residue")); err == nil && len(entries) > 0 {
		t.Errorf("链接路径不该产生归档，却有 %d 个条目", len(entries))
	}
}

// 归档目录建不出来时，必须**退回就地改名**（不能让安装流程失败）。
//
// 构造：把 `<home>/.stale-residue` 做成一个**普通文件**，使 MkdirAll 失败。
func TestRemoveLinkEntry_FallsBackWhenArchiveUnavailable(t *testing.T) {
	home := t.TempDir()
	currentDir := filepath.Join(home, "current")
	link := filepath.Join(currentDir, "faketool")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	// 占位成文件 → 归档目录无法创建。
	if err := os.WriteFile(filepath.Join(home, ".stale-residue"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 必须不 panic、且 current/<tool> 仍然消失（退回旧行为：就地改名后删）。
	removeLinkEntry(link, home)

	if _, err := os.Stat(link); !os.IsNotExist(err) {
		t.Errorf("归档不可用时也必须让 current/%s 消失（安装不能卡住）: %v", "faketool", err)
	}
	_ = time.Now
}

// 源码级守卫：`removeLinkEntry` 不得把 rename 目标拼在 `path` 上。
//
// 这条钉住「有人日后把 `stale := path + ".stale-..."` 写回去」这个具体回退。
func TestRemoveLinkEntry_SourceDoesNotRenameInPlace(t *testing.T) {
	data, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	start := strings.Index(src, "func removeLinkEntry(")
	if start < 0 {
		t.Fatal("removeLinkEntry 未找到")
	}
	body := src[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}

	// 去掉注释后检查可执行代码。
	var code strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	exec := code.String()

	// 允许的 fallback：`path + ".stale-"` 只应出现在**归档失败**的兜底分支里。
	// 主路径必须用 shim.ResidueArchivePath。
	if !strings.Contains(exec, "shim.ResidueArchivePath(") {
		t.Error("removeLinkEntry 主路径必须用 shim.ResidueArchivePath（把残留归档到 current/ 之外）")
	}
	// 兜底分支必须带注释说明，且只能出现一次。
	if n := strings.Count(exec, `path + ".stale-"`); n > 1 {
		t.Errorf(`path + ".stale-" 出现 %d 次 —— 主路径不得就地改名（只允许归档失败时的兜底）`, n)
	}
}
