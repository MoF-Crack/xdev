package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

// viewTools renders the middle tools panel with viewport.
func (m Model) viewTools(width, height int) string {
	style := StylePanel
	if m.focus == FocusTools {
		style = StylePanelFocused
	}

	// lipgloss v0.9.1: Height does NOT include border; border adds 2 lines after Height.
	// So Height(height-2) + 2 border lines = height total.
	contentHeight := height - 2
	contentWidth := width - 2 // Width includes padding but not border; subtract padding only
	if contentHeight < 1 {
		contentHeight = 1
	}
	if contentWidth < 1 {
		contentWidth = 1
	}

	total := len(m.filteredTools)
	// Reserve 1 line for scroll indicator if scrolling is needed
	scrollLine := 0
	if total > contentHeight {
		scrollLine = 1
	}
	visible := contentHeight - scrollLine
	if visible > total {
		visible = total
	}
	if visible < 1 {
		visible = 1
	}
	start, end := viewport(m.toolIdx, total, visible)

	var lines []string
	for i := start; i < end; i++ {
		p := m.filteredTools[i]
		// Determine status
		status := "○"
		version := StyleDim.Render("—")
		if m.hasInstalled(p.Name()) {
			status = "●"
			if m.activeVersion != "" {
				version = StyleActive.Render(m.activeVersion)
			}
		}

		line := fmt.Sprintf(" %s %-12s %s", status, p.Name(), version)
		if i == m.toolIdx {
			line = StyleSelected.Render(line)
		}
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		lines = append(lines, StyleDim.Render("  (no tools)"))
	}

	// Scroll indicator on its own line
	if scrollLine > 0 {
		lines = append(lines, scrollIndicator(m.toolIdx, total, visible))
	}

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return style.Width(width).Height(height - 2).Render(content)
}

// hasInstalled checks if a tool has any installed versions.
func (m Model) hasInstalled(name string) bool {
	dir := m.config.Core.InstallDir
	entries, err := readDirCompat(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// readDirCompat uses os.Stat for junction compatibility on Windows.
func readDirCompat(dir string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var infos []os.FileInfo
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if info.IsDir() {
			infos = append(infos, info)
		}
	}
	return infos, nil
}
