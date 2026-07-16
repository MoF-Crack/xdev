// Package tui implements the interactive terminal user interface for xdev,
// built on the Bubble Tea framework (charmbracelet/bubbletea).
//
// # Architecture
//
// The TUI follows the Elm Architecture pattern:
//   - Model: holds all application state (Model struct)
//   - Update: handles messages and updates state (Update method)
//   - View: renders the UI from state (View method)
//
// # Layout
//
// The TUI displays a three-panel layout:
//   - Left: Category list (languages, build tools, Android, etc.)
//   - Center: Tool list for the selected category
//   - Right: Detail panel with installed/remote versions, install button
//
// Plus a search bar at the top and a help overlay toggled by '?'.
//
// # Key Bindings
//
//   - Tab/Shift+Tab: cycle focus between panels
//   - ↑/↓ or j/k: navigate within a panel
//   - Enter: install selected version / activate version
//   - u: uninstall selected version
//   - /: start search
//   - Esc: cancel search / close help
//   - ?: toggle help overlay
//   - q: quit
//
// # Install State Machine
//
// Installation is performed asynchronously. The InstallState struct tracks
// the current install with a unique ID (time.Now().UnixNano()). Progress
// and result messages carry this ID so stale messages from a previous
// install can be discarded. This prevents race conditions when the user
// rapidly switches between tools while an install is in progress.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/version"
)

// FocusArea represents which panel currently has keyboard focus.
type FocusArea int

const (
	FocusCategory FocusArea = iota
	FocusTools
	FocusDetail
	FocusSearch
)

// DetailTab represents which tab is active in the detail panel.
type DetailTab int

const (
	TabInstalled DetailTab = iota
	TabRemote
)

// Model is the top-level Bubble Tea model for the TUI.
type Model struct {
	// Window dimensions
	width  int
	height int

	// Focus
	focus FocusArea

	// Data
	catalog  *manifest.Catalog
	registry *plugin.Registry
	config   *config.Config

	// Category panel
	categories []string
	catIdx     int

	// Tools panel
	allTools     []plugin.Plugin
	filteredTools []plugin.Plugin
	toolIdx      int

	// Detail panel
	installedVersions []string
	remoteVersions    []string
	versionIdx        int
	detailTab         DetailTab
	activeVersion     string

	// Search
	searchInput textinput.Model
	searching   bool

	// Install state
	installState *InstallState

	// Help overlay
	showHelp bool

	// Expand version list
	expandVersions bool

	// Uninstall confirmation
	uninstallConfirm bool

	// Error message to display
	errMsg string
}

// InstallState tracks an in-progress installation.
type InstallState struct {
	ID      int64 // unique install ID to discard stale progress messages
	Tool    string
	Version string
	Phase   InstallPhase
	Percent float64
	Speed   string
	ETA     string
	Cancel  func()
}

// NewModel creates a new TUI model.
func NewModel(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry) Model {
	// Collect unique categories
	catSet := map[string]bool{}
	allTools := reg.All()
	for _, t := range allTools {
		catSet[t.Category()] = true
	}
	categories := []string{"All"}
	for c := range catSet {
		categories = append(categories, c)
	}
	sort.Strings(categories[1:])

	// Search input
	si := textinput.New()
	si.Placeholder = "Search tools..."
	si.CharLimit = 50

	return Model{
		width:         80,
		height:        24,
		focus:         FocusTools,
		catalog:       cat,
		registry:      reg,
		config:        cfg,
		categories:    categories,
		allTools:      allTools,
		filteredTools: allTools,
		toolIdx:       0,
		searchInput:   si,
		detailTab:     TabInstalled,
	}
}

// NewProgram creates a Bubble Tea program ready to run.
func NewProgram(cfg *config.Config, cat *manifest.Catalog, reg *plugin.Registry) *tea.Program {
	m := NewModel(cfg, cat, reg)
	return tea.NewProgram(m, tea.WithAltScreen())
}

func (m Model) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureCursorsVisible()
		return m, nil

	case tea.KeyMsg:
		// Global: Ctrl+C always quits
		if msg.String() == KeyCtrlC {
			return m, tea.Quit
		}

		// Help overlay
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// Uninstall confirmation dialog
		if m.uninstallConfirm {
			return m.handleUninstallConfirmKey(msg)
		}

		// Install progress overlay
		if m.installState != nil {
			return m.handleInstallOverlayKey(msg)
		}

		// Search mode
		if m.searching {
			return m.handleSearchKey(msg)
		}

		// Normal mode
		return m.handleMainKey(msg)

	case InstallProgressMsg:
			if m.installState != nil && m.installState.ID == msg.ID {
				m.installState.Phase = msg.Phase
				m.installState.Percent = msg.Percent
				m.installState.Speed = msg.Speed
				m.installState.ETA = msg.ETA
				// Continue ticking until install completes
				return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
					if m.installState == nil {
						return nil
					}
					return InstallProgressMsg{
						ID:      m.installState.ID,
						Tool:    msg.Tool,
						Version: msg.Version,
						Phase:   m.installState.Phase,
						Percent: m.installState.Percent,
						Speed:   m.installState.Speed,
						ETA:     m.installState.ETA,
					}
				})
			}
			// Stale progress message (ID mismatch) — discard
			return m, nil

		case InstallResultMsg:
			if m.installState != nil && m.installState.ID != msg.ID {
				// Stale result — discard
				return m, nil
			}
			m.installState = nil
		if msg.Err != nil {
			m.errMsg = msg.Err.Error()
			return m, nil
		}
		// Refresh data
		return m, m.refreshData()

	case RemoteVersionsMsg:
		m.remoteVersions = msg.Versions
		return m, nil

	case RefreshMsg:
		m.allTools = m.registry.All()
		m.applyCategoryFilter()
		m.loadInstalledVersions()
		m.errMsg = ""
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if m.width < MinWidth || m.height < MinHeight {
		return fmt.Sprintf("\n  Terminal too small (%dx%d). Minimum: %dx%d\n  Press Ctrl+C to quit.", m.width, m.height, MinWidth, MinHeight)
	}

	// Title bar — exactly 1 line, Width may wrap so Height(1) truncates
	title := StyleTitle.Render(fmt.Sprintf("XDev v%s", version.Version))

	// Search indicator
	searchHint := ""
	if m.searching {
		searchHint = StyleBold.Render("  Search: ") + m.searchInput.View()
	} else {
		searchHint = StyleDim.Render("  Press / to search")
	}

	topBar := lipgloss.NewStyle().
		Width(m.width).
		Height(1).
		Render(title + searchHint)

	// Three panels — each panel's Width does NOT include border; border adds 2 cols.
	// Total rendered width = catW+2 + toolsW+2 + detailW+2 = availableWidth + 6.
	// So availableWidth must be m.width - 6 to fit exactly.
	borderCols := 6 // 2 per panel (left + right border)
	availableWidth := m.width - borderCols
	catW := max(availableWidth/8, 12)
	toolsW := max(availableWidth*3/8, 20)
	detailW := availableWidth - catW - toolsW
	if detailW < 20 {
		toolsW = availableWidth - catW - 20
		detailW = 20
	}

	// Account for error message line in height budget
	errMsgLines := 0
	if m.errMsg != "" {
		errMsgLines = 1
	}

	// Available height for the three panels:
	// total height - title bar (1) - status bar (2) - error message (0 or 1)
	panelHeight := m.height - TitleBarHeight - StatusBarHeight - errMsgLines
	if panelHeight < 5 {
		panelHeight = 5
	}

	catView := m.viewCategory(catW, panelHeight)
	toolsView := m.viewTools(toolsW, panelHeight)
	detailView := m.viewDetail(detailW, panelHeight)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, catView, toolsView, detailView)

	// Status bar — exactly 2 lines
	statusBar := m.viewStatusBar()

	// Compose — total should be exactly m.height lines
	output := lipgloss.JoinVertical(lipgloss.Top, topBar, panels, statusBar)

	// Error message — accounted for in panelHeight, so total stays m.height
	if m.errMsg != "" {
		errBar := StyleError.Render(fmt.Sprintf(" Error: %s (press Esc to dismiss) ", m.errMsg))
		output = lipgloss.JoinVertical(lipgloss.Top, output, errBar)
	}

	// Overlays (replace the entire view)
	if m.installState != nil {
		output = m.viewInstallOverlay(output)
	}
	if m.uninstallConfirm {
		output = m.viewUninstallOverlay(output)
	}
	if m.showHelp {
		output = m.viewHelpOverlay(output)
	}

	// Safety net: truncate to terminal height to prevent overflow
	output = truncateToHeight(output, m.height)

	return output
}

// --- Key handlers ---

func (m Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case KeyQuit:
		return m, tea.Quit

	case KeyEsc:
		m.errMsg = ""
		return m, nil

	case KeyUp, "k":
		return m.moveCursor(-1)

	case KeyDown, "j":
		return m.moveCursor(1)

	case KeyLeft, "h":
		m.focus = (m.focus - 1 + 3) % 3 // cycle: detail→tools→category

	case KeyRight:
		m.focus = (m.focus + 1) % 3 // cycle: category→tools→detail

	case KeyTab:
		// When focus is on Detail panel, Tab switches between Installed/Remote tabs
		if m.focus == FocusDetail {
			if m.detailTab == TabInstalled {
				m.detailTab = TabRemote
			} else {
				m.detailTab = TabInstalled
			}
			m.versionIdx = 0
		} else {
			m.focus = (m.focus + 1) % 3
		}

	case KeyExpand:
		// Toggle expanded version list in detail panel
		m.expandVersions = !m.expandVersions
		return m, nil

	case KeySearch:
		m.searching = true
		m.searchInput.Focus()
		return m, nil

	case KeyHelp:
		m.showHelp = true
		return m, nil

	case KeyInstall:
		return m.startInstall()

	case KeyUninstall:
		return m.startUninstall()

	case KeyEnter:
		return m.activateVersion()

	case KeyRefresh:
		return m, m.fetchRemoteVersions()
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case KeyEsc:
		m.searching = false
		m.searchInput.SetValue("")
		m.filteredTools = m.applyCategoryFilterPure()
		m.toolIdx = 0
		return m, nil

	case KeyEnter:
		m.searching = false
		query := m.searchInput.Value()
		m.searchInput.SetValue("")
		if query != "" {
			m.filteredTools = fuzzyFilter(m.applyCategoryFilterPure(), query)
			m.toolIdx = 0
			m.focus = FocusTools
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		// Live filter using fuzzy matching
		query := m.searchInput.Value()
		if query == "" {
			m.filteredTools = m.applyCategoryFilterPure()
		} else {
			m.filteredTools = fuzzyFilter(m.applyCategoryFilterPure(), query)
		}
		m.toolIdx = 0
		return m, cmd
	}
}

func (m Model) handleInstallOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == KeyEsc || msg.String() == "c" {
		if m.installState != nil && m.installState.Cancel != nil {
			m.installState.Cancel()
		}
		m.installState = nil
		return m, nil
	}
	return m, nil
}

// --- Cursor movement ---

func (m Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case FocusCategory:
		m.catIdx = clamp(m.catIdx+delta, 0, len(m.categories)-1)
		m.applyCategoryFilter()
		m.toolIdx = 0

	case FocusTools:
		m.toolIdx = clamp(m.toolIdx+delta, 0, len(m.filteredTools)-1)
		m.loadInstalledVersions()
		m.versionIdx = 0

	case FocusDetail:
		maxIdx := len(m.detailVersions()) - 1
		m.versionIdx = clamp(m.versionIdx+delta, 0, maxIdx)
	}

	return m, nil
}

// --- Data helpers ---

func (m *Model) applyCategoryFilter() {
	m.filteredTools = m.applyCategoryFilterPure()
}

func (m *Model) applyCategoryFilterPure() []plugin.Plugin {
	if m.catIdx == 0 { // "All"
		return m.allTools
	}
	cat := m.categories[m.catIdx]
	var filtered []plugin.Plugin
	for _, t := range m.allTools {
		if t.Category() == cat {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (m *Model) loadInstalledVersions() {
	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		m.installedVersions = nil
		m.activeVersion = ""
		return
	}
	p := m.filteredTools[m.toolIdx]
	installDir := filepath.Join(m.config.Core.InstallDir, p.Name())
	entries, err := os.ReadDir(installDir)
	if err != nil {
		m.installedVersions = nil
		m.activeVersion = ""
		return
	}
	// Use os.Stat for junction compatibility on Windows
	var versions []string
	for _, e := range entries {
		full := filepath.Join(installDir, e.Name())
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		versions = append(versions, e.Name())
	}
	sort.Strings(versions)
	m.installedVersions = versions

	// Determine active version
	currentLink := filepath.Join(m.config.Core.XMDevDir, "current", p.Name())
	if target, err := os.Readlink(currentLink); err == nil {
		m.activeVersion = filepath.Base(target)
	} else {
		m.activeVersion = ""
	}
}

func (m *Model) detailVersions() []string {
	if m.detailTab == TabInstalled {
		return m.installedVersions
	}
	return m.remoteVersions
}

func (m Model) refreshData() tea.Cmd {
	return func() tea.Msg {
		return RefreshMsg{}
	}
}

func (m Model) fetchRemoteVersions() tea.Cmd {
	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		return nil
	}
	p := m.filteredTools[m.toolIdx]
	return func() tea.Msg {
		versions := p.Manifest().Versions()
		return RemoteVersionsMsg{Tool: p.Name(), Versions: versions}
	}
}

// --- Action handlers ---

func (m Model) startInstall() (tea.Model, tea.Cmd) {
	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		return m, nil
	}
	p := m.filteredTools[m.toolIdx]
	ver := p.DefaultVersion()
	if ver == "" {
		m.errMsg = fmt.Sprintf("no default version for %s", p.Name())
		return m, nil
	}
	// Set installState so the progress overlay is displayed
	ctx, cancel := context.WithCancel(context.Background())
		m.installState = &InstallState{
			ID:      time.Now().UnixNano(),
			Tool:    p.Name(),
			Version: ver,
			Phase:   PhaseDownloading,
			Cancel:  cancel,
		}
	return m, m.doInstall(ctx, p, ver)
}

func (m Model) activateVersion() (tea.Model, tea.Cmd) {
	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		return m, nil
	}
	p := m.filteredTools[m.toolIdx]
	versions := m.detailVersions()
	if m.detailTab != TabInstalled || len(versions) == 0 || m.versionIdx >= len(versions) {
		return m, nil
	}
	ver := versions[m.versionIdx]
	installPath := p.InstallDir(m.config.Core.InstallDir, ver)
	if _, err := os.Stat(installPath); err != nil {
		m.errMsg = fmt.Sprintf("%s@%s not installed", p.Name(), ver)
		return m, nil
	}
	// Activate
	ins := installer.New(m.config, m.registry)
	if err := ins.Activate(p, installPath, ver); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.activeVersion = ver
	return m, nil
}

func (m Model) startUninstall() (tea.Model, tea.Cmd) {
	if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) {
		return m, nil
	}
	versions := m.detailVersions()
	if m.detailTab != TabInstalled || len(versions) == 0 || m.versionIdx >= len(versions) {
		m.errMsg = "select an installed version to uninstall"
		return m, nil
	}
	m.uninstallConfirm = true
	return m, nil
}

func (m Model) handleUninstallConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.uninstallConfirm = false
		versions := m.detailVersions()
		if len(m.filteredTools) == 0 || m.toolIdx >= len(m.filteredTools) || m.versionIdx >= len(versions) {
			return m, nil
		}
		p := m.filteredTools[m.toolIdx]
		ver := versions[m.versionIdx]
		return m, m.doUninstall(p, ver)
	case KeyEsc, "n", "N":
		m.uninstallConfirm = false
		return m, nil
	default:
		return m, nil
	}
}

// --- View methods are implemented in their respective files:
// category.go, tools.go, detail.go, statusbar.go, install.go, help.go ---

// --- Utility ---

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// viewport returns the [start, end) indices for a visible window of size
// `visible` that keeps `cursor` in view. Items outside [start, end) should
// not be rendered.
func viewport(cursor, total, visible int) (start, end int) {
	if total <= visible {
		return 0, total
	}
	// Keep cursor centered when possible
	half := visible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + visible
	if end > total {
		end = total
		start = end - visible
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// scrollIndicator returns a small visual hint showing the scroll position.
func scrollIndicator(cursor, total, visible int) string {
	if total <= visible {
		return ""
	}
	// Simple percentage indicator
	pct := cursor * 100 / (total - 1)
	return StyleDim.Render(fmt.Sprintf("  [%d%%]", pct))
}

// contentVisibleHeight returns the number of visible content lines for a panel
// of the given height. Since lipgloss Height does not include border (border
// adds 2 lines after Height), the content area = panelHeight - 2.
func contentVisibleHeight(panelHeight int) int {
	h := panelHeight - 2
	if h < 1 {
		h = 1
	}
	return h
}

// ensureCursorsVisible adjusts all cursor indices so they remain within the
// visible viewport after a terminal resize. This prevents the cursor from
// landing outside the rendered area when the window shrinks.
func (m *Model) ensureCursorsVisible() {
	// Clamp category cursor
	if len(m.categories) > 0 {
		m.catIdx = clamp(m.catIdx, 0, len(m.categories)-1)
	}

	// Clamp tool cursor
	if len(m.filteredTools) > 0 {
		m.toolIdx = clamp(m.toolIdx, 0, len(m.filteredTools)-1)
	}

	// Clamp version cursor
	versions := m.detailVersions()
	if len(versions) > 0 {
		m.versionIdx = clamp(m.versionIdx, 0, len(versions)-1)
	}
}

// truncateToHeight ensures the output does not exceed maxLines lines.
// This is a safety net to prevent the TUI from overflowing the terminal.
func truncateToHeight(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}
