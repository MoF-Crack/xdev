package gui

import (
	"os"
	"strings"
	"testing"
)

// stripLineComments 去掉整行注释与行尾注释，使源码级断言只匹配**真实代码**，
// 不被解释性注释里的同名文本误触发（本文件注释里就写了 os.RemoveAll(installPath)
// 作为说明）。
func stripLineComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			b.WriteString("\n")
			continue
		}
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// 第七轮审计 F-05 守卫（源码级顺序断言）：force/repair 重装**不得在下载之前**
// 删除已有安装目录。
//
// 为什么用源码级断言而不是行为测试：GUI 的安装编排需要真实下载管理器 +
// Wails 事件管线才能跑到「下载失败」分支，行为测试要拉起整套 mock；而这条
// 缺陷的本质是**语句顺序**（RemoveAll 相对 StartDownload 的位置），顺序错误
// 本身就是可静态判定的事实。仓库对同类「结构性约束」已有先例
// （如 install_progress_test.go 用源码扫描断言事件载荷带 taskID）。
//
// 语义基准：internal/installer 的 A-07（票 26）——
// `TestForceInstall_DownloadFailKeepsOld` 断言下载失败必须保留旧安装。
// GUI 是第二套独立编排，必须与它同语义。
func TestF05_ForceRemovalHappensAfterDownloadNotBefore(t *testing.T) {
	raw, err := os.ReadFile("app_install.go")
	if err != nil {
		t.Fatal(err)
	}
	s := stripLineComments(string(raw))

	// 1. 删除旧安装目录的语句必须存在（修复不是把删除删掉，而是移位）。
	const removal = "os.RemoveAll(installPath)"
	if !strings.Contains(s, removal) {
		t.Fatalf("expected %q to still exist (force reinstall must still be able to remove)", removal)
	}

	// 2. 下载启动点。
	startIdx := strings.Index(s, "a.dlMgr.StartDownload(")
	if startIdx < 0 {
		t.Fatal("a.dlMgr.StartDownload( not found — the install orchestration changed shape")
	}

	// 3. 下载成功分支（StatusCompleted）里的删除才是合法的。
	completedIdx := strings.Index(s, "if status == downloader.StatusCompleted {")
	if completedIdx < 0 {
		t.Fatal("StatusCompleted branch not found")
	}
	completedEnd := strings.Index(s[completedIdx:], "if status == downloader.StatusFailed")
	if completedEnd < 0 {
		t.Fatal("could not delimit the StatusCompleted branch")
	}
	completedBranch := s[completedIdx : completedIdx+completedEnd]
	if !strings.Contains(completedBranch, removal) {
		t.Error("F-05 复现：force 重装的 os.RemoveAll(installPath) 不在下载成功分支内 —— " +
			"下载失败时旧安装会被摧毁（与 installer A-07 语义相反）")
	}

	// 4. 下载启动之前的区域**不得**出现该删除。
	beforeDownload := s[:startIdx]
	if strings.Contains(beforeDownload, removal) {
		t.Error("F-05 复现：os.RemoveAll(installPath) 出现在 StartDownload 之前 —— " +
			"「先删后下」会在离线/404/哈希不符/用户取消时摧毁完好的安装")
	}

	// 5. force 意图必须以布尔量记录并在下载成功后消费（防止只删了删除语句
	//    而 force 语义整体失效）。
	if !strings.Contains(s, "forceReinstall = true") {
		t.Error("expected forceReinstall intent flag to be recorded")
	}
	if !strings.Contains(s, "if forceReinstall {") {
		t.Error("expected forceReinstall to gate the deferred removal")
	}
}
