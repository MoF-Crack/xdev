package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewHelpOverlay renders the help popup on top of the main view.
func (m Model) viewHelpOverlay(base string) string {
	keys := []struct {
		key  string
		desc string
	}{
		{"↑/↓/j/k", "Move cursor"},
		{"←/→/h", "Switch panel focus"},
		{"Tab", "Next panel / Switch tab"},
		{"/", "Search tools"},
		{"Enter", "Activate (use) version"},
		{"i", "Install selected tool"},
		{"u", "Uninstall version"},
		{"r", "Refresh remote versions"},
		{"x", "Available updates (sniff)"},
		{"l", "Expand/collapse version list"},
		{"q/Ctrl+C", "Quit"},
		{"Esc", "Cancel / Close popup"},
		{"?", "Show this help"},
	}

	var b strings.Builder
	b.WriteString(StyleBold.Render("  XDev — Keyboard Shortcuts") + "\n\n")

	for _, k := range keys {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			StyleHelpKey.Render(fmt.Sprintf("%-14s", k.key)),
			StyleHelpDesc.Render(k.desc)))
	}

	b.WriteString("\n" + StyleDim.Render("  Press any key to close"))

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(50).
		Render(b.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup, lipgloss.WithWhitespaceChars(" "))
}
