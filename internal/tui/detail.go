package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewDetail renders the right detail panel with viewport.
func (m Model) viewDetail(width, height int) string {
	style := StylePanel
	if m.focus == FocusDetail {
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

	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		content := lipgloss.NewStyle().Width(contentWidth).Height(contentHeight).Render(StyleDim.Render("  Select a tool"))
		return style.Width(width).Height(height - 2).Render(content)
	}

	p := m.filteredTools[m.toolIdx]
	var b strings.Builder

	// Header (always visible, not scrollable)
	b.WriteString(StyleBold.Render(p.DisplayName()) + "\n")
	b.WriteString(StyleDim.Render(p.Description()) + "\n")

	// Tab indicator
	tabLine := ""
	if m.detailTab == TabInstalled {
		tabLine = StyleActive.Render("[Installed]") + "  " + StyleDim.Render("Remote")
	} else {
		tabLine = StyleDim.Render("Installed") + "  " + StyleActive.Render("[Remote]")
	}
	b.WriteString(tabLine + "\n")

	// Version list
	versions := m.detailVersions()
	headerLines := 3 // header + desc + tab
	availHeight := contentHeight - headerLines
	if availHeight < 1 {
		availHeight = 1
	}

	if len(versions) == 0 {
		if m.detailTab == TabInstalled {
			b.WriteString(StyleDim.Render("  (not installed)\n"))
		} else {
			b.WriteString(StyleDim.Render("  (press r to refresh)\n"))
		}
	} else if m.expandVersions {
		// Expanded mode: show all versions without viewport limit
		for i, v := range versions {
			b.WriteString(m.renderVersionLine(i, v, versions))
		}
	} else {
		// Normal viewport mode
		total := len(versions)
		scrollLine := 0
		if total > availHeight {
			scrollLine = 1
		}
		visible := availHeight - scrollLine
		if visible > total {
			visible = total
		}
		if visible < 1 {
			visible = 1
		}
		start, end := viewport(m.versionIdx, total, visible)

		for i := start; i < end; i++ {
			b.WriteString(m.renderVersionLine(i, versions[i], versions))
		}

		if scrollLine > 0 {
			b.WriteString(scrollIndicator(m.versionIdx, total, visible) + "\n")
		}
	}

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(contentHeight).
		Render(b.String())
	return style.Width(width).Height(height - 2).Render(content)
}

// renderVersionLine renders a single version line in the detail panel.
func (m Model) renderVersionLine(idx int, version string, allVersions []string) string {
	prefix := "  "
	vstyle := StyleVersionRemote
	if m.detailTab == TabInstalled {
		vstyle = StyleVersionInstalled
		if version == m.activeVersion {
			prefix = " *"
			vstyle = StyleVersionActive
		}
	}
	line := prefix + " " + version
	if idx == m.versionIdx && m.focus == FocusDetail {
		line = StyleSelected.Render(" " + version + " ")
	} else {
		line = vstyle.Render(line)
	}
	return line + "\n"
}
