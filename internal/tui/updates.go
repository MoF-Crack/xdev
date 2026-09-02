// updates.go — TUI 可用更新视图（嗅探结果一键更新）。
//
// 读取 sniff 缓存（discovery.SniffCache）计算可用更新（当前激活 < 最新），
// 提供 [a] 全部更新（串行逐个安装，复用既有 install 进度链）/[r] 刷新。
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/util"
)

// KeyUpdates 是进入/退出更新视图的快捷键（i=install、u=uninstall 已占用）。
const KeyUpdates = "x"

// toolUpdate 表示一条可用更新（嗅探结果）。
type toolUpdate struct {
	Tool    string
	Current string
	Latest  string
}

// computeUpdates 读取 sniff 缓存并计算可用更新（当前激活版本 < 嗅探最新版本）。
func computeUpdates(cfg *config.Config) []toolUpdate {
	var out []toolUpdate
	if cfg == nil || cfg.Core.CacheDir == "" {
		return out
	}
	data, err := os.ReadFile(filepath.Join(cfg.Core.CacheDir, "sniff-cache.json"))
	if err != nil {
		return out
	}
	var cache discovery.SniffCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return out
	}
	for toolName, result := range cache.Tools {
		if result.Unsupported || result.Error != "" || len(result.Versions) == 0 {
			continue
		}
		active, ok := util.ActiveVersion(cfg.Core.XMDevDir, toolName)
		if !ok || active == "" {
			continue
		}
		latest := ""
		for _, v := range result.Versions {
			if latest == "" || util.CompareSemver(v, latest) > 0 {
				latest = v
			}
		}
		if latest != "" && util.CompareSemver(latest, active) > 0 {
			out = append(out, toolUpdate{Tool: toolName, Current: active, Latest: latest})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}

// enterUpdatesView 进入更新视图并重算可用更新。
func (m Model) enterUpdatesView() Model {
	m.updatesView = true
	m.updates = computeUpdates(m.config)
	return m
}

// startToolUpdate 启动单个工具的更新安装（仿 startInstall：建 InstallState
// 显示进度条 + doInstall 异步执行）。
func (m Model) startToolUpdate(u toolUpdate) (Model, tea.Cmd) {
	p, ok := m.registry.Get(u.Tool)
	if !ok {
		// Spec 035 F3: registry miss aborts the batch here — this branch
		// never produces an InstallResultMsg, so the queue must be
		// cleared in place.
		m.updateQueue = nil
		m.updateIdx = 0
		m.errMsg = fmt.Sprintf("tool %s not in registry", u.Tool)
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.installState = &InstallState{
		ID:      time.Now().UnixNano(),
		Tool:    p.Name(),
		Version: u.Latest,
		Phase:   PhaseDownloading,
		Cancel:  cancel,
	}
	return m, m.doInstall(ctx, p, u.Latest)
}

// handleUpdatesKey 处理更新视图内的按键。
func (m Model) handleUpdatesKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case KeyEsc, "q":
		m.updatesView = false
		m.updateQueue = nil
		m.updateIdx = 0
		return m, nil
	case "a", "A":
		// 全部更新：队列串行逐个安装。
		m.updateQueue = append([]toolUpdate(nil), m.updates...)
		m.updateIdx = 0
		if len(m.updateQueue) == 0 {
			return m, nil
		}
		return m.startToolUpdate(m.updateQueue[0])
	case "r", "R":
		m.updates = computeUpdates(m.config)
		return m, nil
	}
	return m, nil
}

// updatesViewString 渲染更新视图。
func (m Model) updatesViewString() string {
	var sb strings.Builder
	sb.WriteString(StyleTitle.Render("Available Updates（嗅探结果）") + "\n\n")
	if len(m.updates) == 0 {
		sb.WriteString(StyleDim.Render("  暂无可用更新（先运行 xdev sniff 刷新嗅探结果）") + "\n")
	} else {
		for _, u := range m.updates {
			sb.WriteString(fmt.Sprintf("  %-20s %s → %s\n",
				lipgloss.NewStyle().Bold(true).Render(u.Tool), u.Current,
				StyleBold.Render(u.Latest)))
		}
	}
	// Spec 036 N1: render errMsg inline — the main view's errBar is never
	// reached while the updates view is displayed (View() early-returns).
	// No "(press Esc to dismiss)" hint: Esc leaves the updates view here,
	// it does not clear the error.
	if m.errMsg != "" {
		sb.WriteString("\n" + StyleError.Render(fmt.Sprintf("  Error: %s", m.errMsg)) + "\n")
	}
	sb.WriteString("\n  " + StyleDim.Render("[a] 全部更新  [r] 刷新  [Esc] 返回") + "\n")
	return sb.String()
}