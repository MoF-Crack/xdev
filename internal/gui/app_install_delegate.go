package gui

import (
	"context"
	"fmt"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// guiDownloadFunc 把 downloader.Manager 包装成 installer.DownloadFunc。
//
// # 为什么需要这一层（2026-10-25 架构收口）
//
// installer 默认用 downloader.Download —— 一次性阻塞调用，没有
// pause/resume/cancel，也没有任务查询。GUI 的进度对话框需要这些能力，
// 于是修复前它**整条安装编排自己重写了一遍**（gui/app_install.go 原
// 400 行），导致「先下载校验成功、再删旧安装」这条 A-07 安全语义在
// GUI 侧漏过一次（原文件 186-194 行的注释自己承认）。
//
// 现在 installer.InstallOptions 提供 DownloadFunc 钩子：GUI 只替换
// 「把文件取回来」这一步，其余全部（已装判定 / 完整性校验 / Force
// 删除时机 / TOFU / 解压 / PostInstall / Activate / 回滚）由 installer
// 单一拥有。
//
// 下载语义完全一致：同一个 downloader.Options（含 ExpectedSHA、
// URLShaMap、ManifestSignature、FallbackURLs、ProxyURL），因此 SHA-256
// 校验与 TOFU 行为两条路径不会再分叉。
func (a *App) guiDownloadFunc(taskID, toolName, displayVersion string) installer.DownloadFunc {
	return func(ctx context.Context, opts downloader.Options) error {
		// 保留 GUI 自己的进度/状态事件（比 installer 的默认日志丰富）：
		// OnStatus 会把重试、镜像回退、代理回退上报成阶段文案，
		// 并按「是否重新开始传输」决定进度条归零还是保持。
		opts.OnProgress = func(downloaded, total int64, elapsed time.Duration) {
			pct := -1.0
			if total > 0 {
				pct = float64(downloaded) / float64(total) * 100
			}
			a.emitInstallProgress(taskID, toolName, displayVersion, "Downloading...", pct,
				util.HumanSpeedStr(downloaded, elapsed), util.HumanETAStr(downloaded, total, elapsed))
		}
		opts.OnStatus = func(message string) {
			pct := -1.0
			if isTransferRestart(message) {
				pct = 0
			}
			a.emitInstallProgress(taskID, toolName, displayVersion, message, pct, "", "")
		}

		if err := a.dlMgr.StartDownload(taskID, toolName, displayVersion, opts); err != nil {
			return err
		}

		// 轮询等待下载结束（F-20 / D18：绑定应用根上下文，应用退出时立即停）。
		const maxPolls = 7200 // 500ms * 7200 = 1 小时上限
		rootDone := a.rootContext().Done()
		for i := 0; i < maxPolls; i++ {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-rootDone:
				a.dlMgr.RemoveTask(taskID)
				return fmt.Errorf("installation cancelled by app shutdown")
			}
			task, err := a.dlMgr.GetTask(taskID)
			if err != nil {
				return err
			}
			switch task.GetStatus() {
			case downloader.StatusCompleted:
				// 下载完成（downloadStatus 在置 Completed 前已校验 ExpectedSHA）。
				// 后续解压/激活由 installer 继续，这里只把任务从管理器摘掉。
				a.dlMgr.RemoveTask(taskID)
				return nil
			case downloader.StatusFailed:
				msg := task.ErrorMessage
				if msg == "" {
					msg = "下载失败"
				}
				a.dlMgr.RemoveTask(taskID)
				return fmt.Errorf("%s", msg)
			case downloader.StatusCancelled:
				a.dlMgr.RemoveTask(taskID)
				return fmt.Errorf("下载已取消")
			}
		}
		a.dlMgr.Cancel(taskID)
		a.dlMgr.RemoveTask(taskID)
		return fmt.Errorf("安装等待超时（1 小时）")
	}
}

// isTransferRestart 判断状态消息是否代表「从头开始传输」。
//
// 这三种情况进度条应归零：首次尝试、切换到回退 URL、代理不可达后改直连。
// 而同一 URL 内的重试（Attempt 2/3…）会从 .part 续传，字节进度仍有效，
// 因此保持当前位置（用 -1 哨兵表示「不改百分比」）。
func isTransferRestart(message string) bool {
	return hasAnyPrefix(message, "Attempt 1/", "Trying fallback", "Proxy unreachable")
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

// emitInstallProgress 上报一次安装进度事件。
//
// a.ctx 为 nil 时跳过（测试环境没有 Wails runtime；Wails 的 EventsEmit
// 对 nil context 会 log.Fatalf，历史上直接把测试进程杀掉、连 FAIL 行
// 都不打印）。
func (a *App) emitInstallProgress(taskID, tool, version, phase string, percent float64, speed, eta string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "installProgress", InstallProgress{
		TaskID:  taskID,
		Tool:    tool,
		Version: version,
		Phase:   phase,
		Percent: percent,
		Speed:   speed,
		ETA:     eta,
	})
}

// emitInstallError 上报一次安装错误事件（同样受 a.ctx nil 保护）。
func (a *App) emitInstallError(taskID, tool, version, msg string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
		"taskId":  taskID,
		"tool":    tool,
		"version": version,
		"error":   msg,
	})
}

// emitActivationWarning 上报一次「版本已装好但未激活成功」的软警告。
//
// 同受 a.ctx nil 保护。修复前三处各自手写这整段 EventsEmit，
// 其中两处漏了保护（Wails 对 nil ctx 会 log.Fatalf 杀进程）。
func (a *App) emitActivationWarning(tool, version, msg string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "xdev:activation-warning", map[string]string{
		"tool":    tool,
		"version": version,
		"message": msg,
	})
}

// emitTOFUWarning 上报一次 TOFU（占位 SHA-256 首次信任）提示。
func (a *App) emitTOFUWarning(tool, version, msg string) {
	if a.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, "xdev:tofu-warning", map[string]string{
		"tool":    tool,
		"version": version,
		"message": msg,
	})
}

// 编译期确认：guiDownloadFunc 的返回类型与 installer.DownloadFunc 一致。
var _ installer.DownloadFunc = (&App{}).guiDownloadFunc("", "", "")

// 保证未使用的 import 不会因后续改动而报错（这些类型在委托路径里出现）。
var (
	_ = manifest.InstallKindArchive
	_ plugin.Plugin
)
