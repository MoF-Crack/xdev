// selfcheck.go — Spec 034: CLI 交互入口启动自检。
//
// 只在三个交互入口（无参 TUI / xdev tui / xdev gui）调用：
// 一次性子命令与 shim 形态不做网络检查（避免拖慢脚本）。
// 非 TTY（管道/CI）或 --no-path-fix 时，prompt 降级为一行提示，绝不阻塞。
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/update"
)

// errQuitSelfUpdate 表示用户选择「退出」；由调用方 os.Exit(0)。
var errQuitSelfUpdate = errors.New("quit self-update")

// stdinIsTTY 判断 stdin 是否为交互终端。
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// runSelfCheck 执行启动自检（更新检查策略由 internal/update 决策）。
func runSelfCheck() error {
	cfg := Cfg()
	if !cfg.Core.SelfUpdate {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	policy := update.NewPolicy()
	act, res := policy.CheckPolicy(ctx, &cfg.Core, cfg.Core.CacheDir)
	switch act {
	case update.ActionAsk:
		// 非 TTY / --no-path-fix：降级为一行提示，不读 stdin。
		if !stdinIsTTY() || NoPathFix() {
			fmt.Printf("xdev: 有可用更新 v%s -> v%s（运行 'xdev self update' 更新）\n", res.CurrentVersion, res.LatestVersion)
			return nil
		}
		err := promptSelfUpdate(res, os.Stdin, cfg.Core.CacheDir, runSelfUpdate)
		if errors.Is(err, errQuitSelfUpdate) {
			os.Exit(0)
		}
		return err
	case update.ActionNotify:
		fmt.Printf("xdev: 有可用更新 v%s -> v%s（运行 'xdev self update' 更新）\n", res.CurrentVersion, res.LatestVersion)
	case update.ActionUpdate:
		fmt.Printf("xdev: 自动更新 v%s -> v%s ...\n", res.CurrentVersion, res.LatestVersion)
		if err := runSelfUpdate(); err != nil {
			fmt.Printf("xdev: 自动更新失败: %v\n", err)
		}
	default:
		// ActionSkip：静默
	}
	return nil
}

// promptSelfUpdate 三选交互：更新 / 忽略并继续 / 退出。
// reader/apply/quit 均可注入（测试用）。
//
//	apply 为「更新」动作执行器（生产：runSelfUpdate）
func promptSelfUpdate(res *update.CheckResult, r io.Reader, cacheDir string, apply func() error) error {
	reader := bufio.NewReader(r)
	for {
		fmt.Printf("xdev 有可用更新：v%s → v%s。选择 [U]pdate 更新 / [I]gnore 忽略并继续 / [Q]uit 退出: ",
			res.CurrentVersion, res.LatestVersion)
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF（Ctrl+D）：不写忽略记录，直接继续。
			return nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "u", "update":
			return apply()
		case "i", "ignore":
			_ = update.IgnoreVersion(cacheDir, res.LatestVersion)
			fmt.Printf("已忽略 v%s，出现新版本时会再次提示\n", res.LatestVersion)
			return nil
		case "q", "quit", "exit":
			return errQuitSelfUpdate
		}
	}
}
