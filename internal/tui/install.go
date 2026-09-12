// install.go implements the asynchronous install/uninstall operations
// for the TUI, with progress reporting via shared state.
//
// # Concurrency Model
//
// Install operations run in a goroutine (via tea.Batch). Progress is
// shared through a progressState struct protected by a sync.Mutex.
// The TUI polls this state via a tick command (progressTickCmd) that
// fires every 100ms during an active install.
//
// # Stale Message Prevention
//
// Each install is assigned a unique ID (from InstallState.ID). The
// doInstall function captures this ID and includes it in all result
// and progress messages. The Update handler checks the ID and discards
// messages that don't match the current install state.
//
// # doUninstall
//
// Uninstall runs synchronously in a tea.Cmd and returns an
// UninstallResultMsg carrying the same ID scheme (Model.currentOpID) so the
// Update handler can apply the identical stale-message guard — see ticket 40
// (P2-19 / E-15).
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/plugin"
)

// progressState is shared between the install goroutine and the TUI update loop.
type progressState struct {
	mu      sync.Mutex
	phase   int
	percent float64
	speed   string
	eta     string
}

func (ps *progressState) store(phase int, percent float64, speed, eta string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.phase = phase
	if percent < 0 {
		// Spec 035 F5: indeterminate phases (e.g. PhaseInstalling emits -1
		// while a rolling installer runs) keep the last known percent —
		// matches the GUI, which also only swaps the phase text.
		// Common sequence: download ends at 100, Installing shows 100%.
		return
	}
	ps.percent = percent
	ps.speed = speed
	ps.eta = eta
}

func (ps *progressState) load() (phase int, percent float64, speed, eta string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.phase, ps.percent, ps.speed, ps.eta
}

// doInstall starts an asynchronous install operation with progress reporting.
func (m Model) doInstall(ctx context.Context, p plugin.Plugin, ver string) tea.Cmd {
	toolName := p.Name()
	// 票 40（P2-19 / E-15）：ID 取 currentOpID（beginOp 分配，同时写入
	// installState.ID）——单一来源，避免结果 ID 与操作槽位来源分叉。
	installID := m.currentOpID
	ps := &progressState{}

	return tea.Batch(
		// The actual install operation
		func() tea.Msg {
			ins := installer.New(m.config, m.registry)
			res, err := ins.InstallWithProgress(ctx, p, ver, func(phase installer.ProgressPhase, percent float64, speed string, eta string) {
				ps.store(int(phase), percent, speed, eta)
			}, installer.InstallOptions{})
			if err != nil {
				return InstallResultMsg{ID: installID, Err: err}
			}
			return InstallResultMsg{
				ID:      installID,
				Tool:    res.Tool,
				Version: res.Version,
				Path:    res.InstallPath,
			}
		},
		// Progress ticker: polls the shared state and sends updates
		m.progressTickCmd(ps, toolName, ver, installID),
	)
}

// progressTickCmd returns a Cmd that sends periodic progress updates.
func (m Model) progressTickCmd(ps *progressState, tool, ver string, installID int64) tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		if m.installState == nil {
			return nil
		}
		phase, percent, speed, eta := ps.load()
		return InstallProgressMsg{
			ID:      installID,
			Tool:    tool,
			Version: ver,
			Phase:   InstallPhase(phase),
			Percent: percent,
			Speed:   speed,
			ETA:     eta,
		}
	})
}

// doUninstall starts an asynchronous uninstall operation.
//
// 票 40（P2-19 / E-15）：结果改用独立的 UninstallResultMsg，并携带
// beginOp 分配的 currentOpID（旧实现复用 InstallResultMsg 且
// installState==nil 时 ID 恒为 0——既依赖安装侧判定的漏洞被接受，又与安装
// 结果共用一个消息空间）。
func (m Model) doUninstall(p plugin.Plugin, ver string) tea.Cmd {
	opID := m.currentOpID
	return func() tea.Msg {
		ins := installer.New(m.config, m.registry)
		if err := ins.Uninstall(p, ver); err != nil {
			return UninstallResultMsg{ID: opID, Tool: p.Name(), Version: ver, Err: err}
		}
		return UninstallResultMsg{
			ID:      opID,
			Tool:    p.Name(),
			Version: ver,
		}
	}
}

// viewInstallOverlay renders the install progress overlay on top of the main view.
func (m Model) viewInstallOverlay(base string) string {
	if m.installState == nil {
		return base
	}
	s := m.installState

	// Spec 035 F5: defensive clamp — a negative percent (indeterminate
	// phase) must never render as "-1%" or an empty progress bar.
	percent := s.Percent
	if percent < 0 {
		percent = 0
	}

	// Progress bar
	barWidth := 30
	filled := int(percent / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var b strings.Builder
	b.WriteString(StyleBold.Render(fmt.Sprintf("  Installing %s@%s", s.Tool, s.Version)) + "\n\n")
	// Spec 035 F4: batch update context (only when a batch queue is
	// active; plain single installs render exactly as before).
	if m.updateQueue != nil {
		b.WriteString(StyleDim.Render(fmt.Sprintf("  Batch %d/%d", m.updateIdx+1, len(m.updateQueue))) + "\n")
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n", s.Phase.String(), bar))
	b.WriteString(fmt.Sprintf("  %.0f%%", percent))
	if s.Speed != "" {
		b.WriteString(fmt.Sprintf("   Speed: %s", s.Speed))
	}
	if s.ETA != "" {
		b.WriteString(fmt.Sprintf("   ETA: %s", s.ETA))
	}
	b.WriteString("\n\n")
	b.WriteString(StyleDim.Render("  Press Esc to cancel"))

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(50).
		Render(b.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup, lipgloss.WithWhitespaceChars(" "))
}

// viewUninstallOverlay renders the uninstall confirmation dialog.
func (m Model) viewUninstallOverlay(base string) string {
	toolName := ""
	version := ""
	if len(m.filteredTools) > 0 && m.toolIdx < len(m.filteredTools) {
		toolName = m.filteredTools[m.toolIdx].Name()
		versions := m.detailVersions()
		if m.versionIdx < len(versions) {
			version = versions[m.versionIdx]
		}
	}

	var b strings.Builder
	b.WriteString(StyleBold.Render("  Confirm Uninstall") + "\n\n")
	b.WriteString(fmt.Sprintf("  Remove %s@%s?", toolName, version) + "\n\n")
	b.WriteString(StyleDim.Render("  y = Yes, n/Esc = No"))

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(1, 2).
		Width(40).
		Render(b.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup, lipgloss.WithWhitespaceChars(" "))
}
