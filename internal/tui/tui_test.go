package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

func TestNewModelCreatesValidState(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    "/tmp/xmde-test",
			InstallDir:  "/tmp/xmde-test/install",
			ShimDir:     "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	m := NewModel(cfg, cat, reg)

	if m.focus != FocusTools {
		t.Errorf("default focus = %d, want FocusTools", m.focus)
	}
	if len(m.categories) < 1 || m.categories[0] != "All" {
		t.Errorf("categories = %v, want [All ...]", m.categories)
	}
	if m.searching {
		t.Error("searching should be false initially")
	}
	if m.showHelp {
		t.Error("showHelp should be false initially")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 0, 0},
	}
	for _, tt := range tests {
		if got := clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

func TestMax(t *testing.T) {
	if got := max(3, 5); got != 5 {
		t.Errorf("max(3,5) = %d, want 5", got)
	}
	if got := max(7, 2); got != 7 {
		t.Errorf("max(7,2) = %d, want 7", got)
	}
}

func TestInstallPhaseString(t *testing.T) {
	tests := []struct {
		phase InstallPhase
		want  string
	}{
		{PhaseDownloading, "Downloading"},
		{PhaseExtracting, "Extracting"},
		{PhaseActivating, "Activating"},
		{InstallPhase(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("InstallPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestCategoryFilterAll(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   "/tmp/xmde-test",
			InstallDir: "/tmp/xmde-test/install",
			ShimDir:    "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	// Register two tools with different categories
	t1 := &manifest.Tool{Name: "a", Category: "language", Shims: []string{"a"}, VersionList: []manifest.Version{{Version: "1"}}}
	t2 := &manifest.Tool{Name: "b", Category: "build", Shims: []string{"b"}, VersionList: []manifest.Version{{Version: "1"}}}
	reg.Register(plugin.NewGeneric(t1))
	reg.Register(plugin.NewGeneric(t2))

	m := NewModel(cfg, cat, reg)
	m.catIdx = 0 // "All"
	m.applyCategoryFilter()
	if len(m.filteredTools) != 2 {
		t.Errorf("All category: got %d tools, want 2", len(m.filteredTools))
	}
}

func TestCategoryFilterSpecific(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   "/tmp/xmde-test",
			InstallDir: "/tmp/xmde-test/install",
			ShimDir:    "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	t1 := &manifest.Tool{Name: "a", Category: "language", Shims: []string{"a"}, VersionList: []manifest.Version{{Version: "1"}}}
	t2 := &manifest.Tool{Name: "b", Category: "build", Shims: []string{"b"}, VersionList: []manifest.Version{{Version: "1"}}}
	reg.Register(plugin.NewGeneric(t1))
	reg.Register(plugin.NewGeneric(t2))

	m := NewModel(cfg, cat, reg)
	// Find the "build" category index
	for i, c := range m.categories {
		if c == "build" {
			m.catIdx = i
			break
		}
	}
	m.applyCategoryFilter()
	if len(m.filteredTools) != 1 {
		t.Errorf("Build category: got %d tools, want 1", len(m.filteredTools))
	}
	if len(m.filteredTools) > 0 && m.filteredTools[0].Name() != "b" {
		t.Errorf("Build category: got %q, want b", m.filteredTools[0].Name())
	}
}

func TestViewport(t *testing.T) {
	tests := []struct {
		cursor, total, visible int
		wantStart, wantEnd     int
	}{
		// Total fits in viewport
		{0, 5, 10, 0, 5},
		{3, 5, 10, 0, 5},
		// Need scrolling — cursor at top
		{0, 20, 5, 0, 5},
		// Cursor in middle — centered
		{10, 20, 5, 8, 13},
		// Cursor near bottom
		{19, 20, 5, 15, 20},
		// Cursor at bottom
		{18, 20, 5, 15, 20},
		// Single item visible
		{5, 20, 1, 5, 6},
	}
	for _, tt := range tests {
		start, end := viewport(tt.cursor, tt.total, tt.visible)
		if start != tt.wantStart || end != tt.wantEnd {
			t.Errorf("viewport(%d, %d, %d) = [%d, %d), want [%d, %d)",
				tt.cursor, tt.total, tt.visible, start, end, tt.wantStart, tt.wantEnd)
		}
		// Verify cursor is within [start, end)
		if tt.cursor < start || tt.cursor >= end {
			t.Errorf("viewport(%d, %d, %d): cursor %d not in [%d, %d)",
				tt.cursor, tt.total, tt.visible, tt.cursor, start, end)
		}
	}
}

func TestScrollIndicator(t *testing.T) {
	// No indicator when everything fits
	if got := scrollIndicator(0, 5, 10); got != "" {
		t.Errorf("scrollIndicator(0,5,10) = %q, want empty", got)
	}
	// Indicator shown when scrolling needed
	if got := scrollIndicator(0, 20, 5); got == "" {
		t.Error("scrollIndicator(0,20,5) should not be empty")
	}
}

func TestContentVisibleHeight(t *testing.T) {
	tests := []struct {
		panelHeight int
		want        int
	}{
		{10, 8},
		{5, 3},
		{2, 1},
		{1, 1},
	}
	for _, tt := range tests {
		if got := contentVisibleHeight(tt.panelHeight); got != tt.want {
			t.Errorf("contentVisibleHeight(%d) = %d, want %d", tt.panelHeight, got, tt.want)
		}
	}
}

func TestEnsureCursorsVisible(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   "/tmp/xmde-test",
			InstallDir: "/tmp/xmde-test/install",
			ShimDir:    "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	// Register 5 tools
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		t := &manifest.Tool{Name: name, Category: "language", Shims: []string{name}, VersionList: []manifest.Version{{Version: "1"}}}
		reg.Register(plugin.NewGeneric(t))
	}

	m := NewModel(cfg, cat, reg)

	// Set cursor out of bounds
	m.catIdx = 100
	m.toolIdx = 100
	m.versionIdx = 100

	m.ensureCursorsVisible()

	// catIdx should be clamped to last category
	if m.catIdx >= len(m.categories) {
		t.Errorf("catIdx = %d, want < %d", m.catIdx, len(m.categories))
	}
	// toolIdx should be clamped to last tool
	if m.toolIdx >= len(m.filteredTools) {
		t.Errorf("toolIdx = %d, want < %d", m.toolIdx, len(m.filteredTools))
	}
}

func TestWindowSizeMsgAdaptsLayout(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   "/tmp/xmde-test",
			InstallDir: "/tmp/xmde-test/install",
			ShimDir:    "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	// Register tools so filteredTools is not empty
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		t := &manifest.Tool{Name: name, Category: "language", Shims: []string{name}, VersionList: []manifest.Version{{Version: "1"}}}
		reg.Register(plugin.NewGeneric(t))
	}

	m := NewModel(cfg, cat, reg)

	// Simulate resize
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(Model)
	if m2.width != 120 || m2.height != 40 {
		t.Errorf("after resize: width=%d height=%d, want 120x40", m2.width, m2.height)
	}

	// Simulate shrink — cursor should be clamped
	m.catIdx = 100
	m.toolIdx = 100
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m2 = updated.(Model)
	if m2.catIdx >= len(m2.categories) {
		t.Errorf("after shrink: catIdx=%d should be < %d", m2.catIdx, len(m2.categories))
	}
	if m2.toolIdx >= len(m2.filteredTools) {
		t.Errorf("after shrink: toolIdx=%d should be < %d", m2.toolIdx, len(m2.filteredTools))
	}
}

func TestTruncateToHeight(t *testing.T) {
	// Fewer lines than max — no truncation
	if got := truncateToHeight("a\nb", 5); got != "a\nb" {
		t.Errorf("truncateToHeight(2-line, 5) = %q, want %q", got, "a\nb")
	}
	// More lines than max — truncation
	if got := truncateToHeight("a\nb\nc\nd", 2); got != "a\nb" {
		t.Errorf("truncateToHeight(4-line, 2) = %q, want %q", got, "a\nb")
	}
	// Exact match — no truncation
	if got := truncateToHeight("a\nb", 2); got != "a\nb" {
		t.Errorf("truncateToHeight(2-line, 2) = %q, want %q", got, "a\nb")
	}
	// Single line
	if got := truncateToHeight("hello", 1); got != "hello" {
		t.Errorf("truncateToHeight(1-line, 1) = %q, want %q", got, "hello")
	}
}

func TestViewOutputLineCount(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   "/tmp/xmde-test",
			InstallDir: "/tmp/xmde-test/install",
			ShimDir:    "/tmp/xmde-test/shims",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()

	// Register some tools
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		t := &manifest.Tool{Name: name, Category: "language", Shims: []string{name}, VersionList: []manifest.Version{{Version: "1"}}}
		reg.Register(plugin.NewGeneric(t))
	}

	m := NewModel(cfg, cat, reg)

	// Test various terminal sizes
	sizes := []struct {
		w, h int
	}{
		{80, 24},
		{120, 40},
		{100, 30},
		{80, 25},
		{80, 50},
	}

	for _, sz := range sizes {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m2 := updated.(Model)
		output := m2.View()
		lineCount := len(strings.Split(output, "\n"))
		if lineCount != sz.h {
			t.Errorf("View() output %d lines for %dx%d terminal, want %d lines",
				lineCount, sz.w, sz.h, sz.h)
		}
	}
}
