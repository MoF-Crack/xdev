package gui

import (
	"os"
	"strings"
	"testing"
)

// stripLineComments 去掉整行注释与行尾注释，使源码级断言只匹配**真实代码**，
// 不被解释性注释里的同名文本误触发。
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

// 第七轮审计 F-05 守卫 —— 2026-10-25 架构收口后**加强**。
//
// # 原守卫（已废弃）
//
// 修复前 GUI 有一套自己的安装编排（约 330 行），F-05 的缺陷是
// `os.RemoveAll(installPath)` 排在下载**之前**：一旦下载失败（离线 / 404 /
// 镜像全挂 / 哈希不符 / 用户取消），原本完好可用的安装就被摧毁。
// 当时的守卫用源码级顺序断言：「RemoveAll 必须出现在 StatusCompleted
// 分支内」。
//
// # 现在
//
// GUI 不再自己编排 —— 整段委托给 installer.InstallWithProgress，而
// installer 里的 force 删除时机早已是 A-07 修复后的正确顺序
// （installer.go 的 forceReinstall 分支，由
// TestForceInstall_DownloadFailKeepsOld 守卫）。
//
// 因此本守卫改为断言**更强的性质**：GUI 侧根本不存在第二份删除实现。
// 从「顺序对不对」升级为「只有一份实现」—— 顺序问题从此不可能在 GUI
// 侧复现，因为这里没有任何顺序可写错。
func TestF05_ForceRemovalHappensAfterDownloadNotBefore(t *testing.T) {
	files := []string{"app_install.go", "app_install_delegate.go"}
	var combined strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		combined.WriteString(stripLineComments(string(raw)))
	}
	s := combined.String()

	// 1. GUI 不得自行删除安装目录 —— 那是 installer 的职责。
	//    若有人重新引入，F-05 的顺序约束就会再次变成「GUI 侧的第二个真相」。
	if strings.Contains(s, "os.RemoveAll(installPath)") {
		t.Error("GUI 不得自行删除安装目录：force 重装的删除时机必须由 " +
			"installer.InstallWithProgress 单一拥有（A-07 / F-05）")
	}

	// 2. 安装必须走 InstallWithProgress —— 这是「只有一份编排」的前提。
	if !strings.Contains(s, "InstallWithProgress(") {
		t.Error("GUI 安装必须委托 installer.InstallWithProgress；" +
			"自建编排会让 A-07/F-05 类语义再次分叉")
	}

	// 3. 下载仍必须经 downloader.Manager（GUI 需要 pause/resume/cancel），
	//    且通过 DownloadFunc 钩子交给 installer —— 不能绕开 installer 自己下载。
	if !strings.Contains(s, "a.dlMgr.StartDownload(") {
		t.Error("GUI 下载必须经 downloader.Manager（进度对话框需要 pause/resume/cancel）")
	}
	if !strings.Contains(s, "DownloadFunc:") {
		t.Error("GUI 必须通过 installer.InstallOptions.DownloadFunc 注入下载器，" +
			"而不是绕开 installer 自行编排")
	}
}
