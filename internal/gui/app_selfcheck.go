// app_selfcheck.go — Spec 034: GUI 启动自检更新。
//
// 复用 internal/update 的策略层（CheckPolicy）与既有 ApplyUpdate 绑定：
// 启动 2s 后异步按配置自检；prompt → 前端三选对话框（事件驱动）；
// auto → 后台应用（目标为同目录 CLI 二进制）；notify → 前端提示。
// 任何失败静默降级为日志，绝不阻塞启动。
package gui

import (
	"context"
	"os"
	"time"

	"github.com/xmdemt/xdev/internal/update"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// selfUpdateEvent 是前端监听的自检事件名（单一通道，payload.action 区分场景）。
const selfUpdateEvent = "self-update"

// runStartupSelfCheck 在 GUI 启动 2s 后执行（goroutine 调用，
// 与 EnsureSelfInstalled / FixLocalVersions 同模式）。
func (a *App) runStartupSelfCheck() {
	cfg := a.getCfg()
	if cfg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.rootContext(), 30*time.Second)
	defer cancel()

	policy := update.NewPolicy()
	act, res := policy.CheckPolicy(ctx, &cfg.Core, cfg.Core.CacheDir)
	switch act {
	case update.ActionAsk:
		a.emitSelfUpdate("ask", res)
	case update.ActionNotify:
		a.emitSelfUpdate("notify", res)
	case update.ActionUpdate:
		// 更新目标是同目录 CLI 二进制（xdev.exe），不是运行中的 GUI 壳。
		target := cliBinaryNextToSelf()
		if target == "" {
			logInfo("[self-update] auto: no CLI binary next to GUI, skipping auto update")
			return
		}
		logInfo("[self-update] auto-updating CLI binary %s -> %s", target, res.LatestVersion)
		if err := update.ApplyTo(ctx, res, target); err != nil {
			logWarn("[self-update] auto update failed: %v", err)
			a.emitSelfUpdate("applied-error", res)
			return
		}
		a.emitSelfUpdate("applied", res)
	default: // ActionSkip：静默
	}
}

// cliBinaryNextToSelf 单二进制模型下返回当前可执行文件自身
// （GUI/CLI 同一二进制，更新目标即自身；Windows 上通过 rename 替换运行中文件）。
func cliBinaryNextToSelf() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// emitSelfUpdate 向前端推送自检事件（action: ask|notify|applied|applied-error）。
func (a *App) emitSelfUpdate(action string, res *update.CheckResult) {
	if a.ctx == nil || res == nil {
		return
	}
	wailsRuntime.EventsEmit(a.ctx, selfUpdateEvent, map[string]interface{}{
		"action":  action,
		"current": res.CurrentVersion,
		"latest":  res.LatestVersion,
	})
}

// IgnoreSelfUpdate 记录用户「忽略并继续」的版本（前端三选回调）。
func (a *App) IgnoreSelfUpdate(latest string) {
	cfg := a.getCfg()
	if cfg == nil {
		return
	}
	if err := update.IgnoreVersion(cfg.Core.CacheDir, latest); err != nil {
		logWarn("[self-update] record ignore %s failed: %v", latest, err)
	}
}