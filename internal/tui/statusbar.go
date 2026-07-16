package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewStatusBar renders the bottom status bar.
func (m Model) viewStatusBar() string {
	shortcutHints := []string{
		StyleHelpKey.Render("i") + ":Install",
		StyleHelpKey.Render("u") + ":Uninstall",
		StyleHelpKey.Render("Enter") + ":Use",
		StyleHelpKey.Render("/") + ":Search",
		StyleHelpKey.Render("q") + ":Quit",
		StyleHelpKey.Render("?") + ":Help",
	}
	hints := strings.Join(shortcutHints, "  ")

	// Build path info line, appending search query indicator when active
	pathInfo := StyleDim.Render(fmt.Sprintf("PATH: %s", m.config.Core.ShimDir))
	if m.searching {
		query := m.searchInput.Value()
		if query != "" {
			pathInfo += StyleBold.Render(fmt.Sprintf("  |  Search: %q", query))
		} else {
			pathInfo += StyleDim.Render("  |  Search: (typing...)")
		}
	}

	// Height(1) prevents wrapping — if content is too wide it truncates instead
	line1 := StyleStatusBar.Width(m.width).Height(1).Render(hints)
	line2 := StyleStatusBar.Width(m.width).Height(1).Render(pathInfo)

	return lipgloss.JoinVertical(lipgloss.Left, line1, line2)
}
