package tui

import "github.com/charmbracelet/lipgloss"

// viewCategory renders the left category panel with viewport.
func (m Model) viewCategory(width, height int) string {
	style := StylePanel
	if m.focus == FocusCategory {
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

	// Viewport: only render visible lines
	total := len(m.categories)
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
	start, end := viewport(m.catIdx, total, visible)

	var lines []string
	for i := start; i < end; i++ {
		line := m.categories[i]
		if i == m.catIdx {
			line = StyleSelected.Render(" " + line + " ")
		}
		lines = append(lines, line)
	}

	// Scroll indicator on its own line
	if scrollLine > 0 {
		lines = append(lines, scrollIndicator(m.catIdx, total, visible))
	}

	if len(lines) == 0 {
		lines = append(lines, "")
	}

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return style.Width(width).Height(height - 2).Render(content)
}
