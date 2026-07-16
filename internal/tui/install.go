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
// InstallResultMsg with the same ID scheme for consistency.
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
	installID := m.installState.ID
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
func (m Model) doUninstall(p plugin.Plugin, ver string) tea.Cmd {
	installID := int64(0)
	if m.installState != nil {
		installID = m.installState.ID
	}
	return func() tea.Msg {
		ins := installer.New(m.config, m.registry)
		if err := ins.Uninstall(p, ver); err != nil {
			return InstallResultMsg{ID: installID, Err: err}
		}
		return InstallResultMsg{
			ID:      installID,
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

	// Progress bar
	barWidth := 30
	filled := int(s.Percent / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var b strings.Builder
	b.WriteString(StyleBold.Render(fmt.Sprintf("  Installing %s@%s", s.Tool, s.Version)) + "\n\n")
	b.WriteString(fmt.Sprintf("  %s  %s\n", s.Phase.String(), bar))
	b.WriteString(fmt.Sprintf("  %.0f%%", s.Percent))
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
