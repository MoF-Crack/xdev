package tui

import (
	"fmt"
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
		{PhaseInstalling, "Installing"},
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

// newTUITestModel builds a minimal Model for direct-message tests.
func newTUITestModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   t.TempDir(),
			InstallDir: t.TempDir(),
			ShimDir:    t.TempDir(),
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()
	return NewModel(cfg, cat, reg)
}

// TestInstallProgress_NegativePercentKeepsLastValue (Spec 035 F5):
// PhaseInstalling emits percent=-1 (indeterminate). The percent must
// stay at the last known value while the phase text still updates.
func TestInstallProgress_NegativePercentKeepsLastValue(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Phase: PhaseDownloading, Percent: 50}

	updated, _ := m.Update(InstallProgressMsg{ID: 1, Phase: PhaseInstalling, Percent: -1})
	m2 := updated.(Model)
	if m2.installState == nil {
		t.Fatal("installState unexpectedly cleared")
	}
	if m2.installState.Percent != 50 {
		t.Errorf("Percent = %v, want 50 (kept from previous update)", m2.installState.Percent)
	}
	if m2.installState.Phase != PhaseInstalling {
		t.Errorf("Phase = %v, want PhaseInstalling (phase must still update)", m2.installState.Phase)
	}
}

// TestInstallProgress_NormalPercentStillUpdates (Spec 035 F5):
// the negative-percent guard must not swallow legitimate values.
func TestInstallProgress_NormalPercentStillUpdates(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Phase: PhaseDownloading, Percent: 10}

	updated, _ := m.Update(InstallProgressMsg{ID: 1, Phase: PhaseActivating, Percent: 75})
	m2 := updated.(Model)
	if m2.installState.Percent != 75 {
		t.Errorf("Percent = %v, want 75", m2.installState.Percent)
	}
	if m2.installState.Phase != PhaseActivating {
		t.Errorf("Phase = %v, want PhaseActivating", m2.installState.Phase)
	}
}

// TestViewInstallOverlay_ClampsNegativePercent (Spec 035 F5): even if
// installState.Percent is somehow negative, the overlay must not render
// "-1%" or an empty progress bar (defense-in-depth clamp).
func TestViewInstallOverlay_ClampsNegativePercent(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Phase: PhaseInstalling, Percent: -1}

	// Width/height are set by NewModel (80x24) — enough for the overlay.
	out := m.viewInstallOverlay("")
	if strings.Contains(out, "-1%") {
		t.Errorf("overlay renders -1%%, want clamped 0%%:\n%s", out)
	}
	if !strings.Contains(out, "Installing go@1.26.5") {
		t.Errorf("overlay missing tool/version line:\n%s", out)
	}
}

// TestInstallResultError_ClearsUpdateQueue (Spec 035 F3): a failed
// install clears the batch update queue so a later plain install
// cannot accidentally resume "update all".
//
// 票 40（P2-19 / E-15）：结果消息现在的归属判定是
// `msg.ID != m.currentOpID`，因此用例必须模拟真实起点——startInstall
// 会同时置 installState.ID 与 currentOpID。此前本用例只手工构造了
// installState，不设 currentOpID → 修复后消息被判为陈旧消息丢弃（必红）。
func TestInstallResultError_ClearsUpdateQueue(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5"}
	m.currentOpID = 1
	m.updateQueue = []toolUpdate{{Tool: "go", Current: "1.0", Latest: "2.0"}}
	m.updateIdx = 0

	updated, _ := m.Update(InstallResultMsg{ID: 1, Err: fmt.Errorf("boom")})
	m2 := updated.(Model)
	if m2.updateQueue != nil {
		t.Errorf("updateQueue = %v, want nil after failed install", m2.updateQueue)
	}
	if m2.updateIdx != 0 {
		t.Errorf("updateIdx = %d, want 0", m2.updateIdx)
	}
	if m2.installState != nil {
		t.Error("installState should be cleared")
	}
	if m2.currentOpID != 0 {
		t.Errorf("currentOpID = %d, want 0（结果已消费，操作槽位必须复位）", m2.currentOpID)
	}
}

// TestInstallCancelKey_ClearsUpdateQueue (Spec 035 F3): pressing Esc
// while a batch update install runs cancels it AND clears the queue.
func TestInstallCancelKey_ClearsUpdateQueue(t *testing.T) {
	m := newTUITestModel(t)
	cancelled := false
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Cancel: func() { cancelled = true }}
	m.updateQueue = []toolUpdate{{Tool: "go", Current: "1.0", Latest: "2.0"}}
	m.updateIdx = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)
	if !cancelled {
		t.Error("install cancel func was not invoked")
	}
	if m2.installState != nil {
		t.Error("installState should be cleared")
	}
	if m2.updateQueue != nil {
		t.Errorf("updateQueue = %v, want nil after cancel", m2.updateQueue)
	}
}

// TestStartToolUpdate_RegistryMissClearsQueue (Spec 035 F3): when the
// tool is not in the registry the batch aborts in place (no
// InstallResultMsg will follow).
func TestStartToolUpdate_RegistryMissClearsQueue(t *testing.T) {
	m := newTUITestModel(t)
	m.updateQueue = []toolUpdate{{Tool: "ghost", Current: "1.0", Latest: "2.0"}}
	m.updateIdx = 0

	m2, _ := m.startToolUpdate(m.updateQueue[0])
	if m2.updateQueue != nil {
		t.Errorf("updateQueue = %v, want nil after registry miss", m2.updateQueue)
	}
	if m2.errMsg == "" {
		t.Error("errMsg should explain the registry miss")
	}
}

// TestUpdatesView_InstallTakesKeyPrecedence (Spec 035 F4): while both
// the updates view and an install overlay are active, Esc must cancel
// the install (not leave the view); the updates view stays open.
func TestUpdatesView_InstallTakesKeyPrecedence(t *testing.T) {
	m := newTUITestModel(t)
	m.updatesView = true
	cancelled := false
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Cancel: func() { cancelled = true }}
	m.updateQueue = []toolUpdate{{Tool: "go", Current: "1.0", Latest: "2.0"}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)
	if !cancelled {
		t.Error("Esc should cancel the install first")
	}
	if m2.installState != nil {
		t.Error("installState should be cleared by Esc")
	}
	if !m2.updatesView {
		t.Error("updates view should remain open after cancelling the install")
	}
	if m2.updateQueue != nil {
		t.Error("updateQueue should be cleared along with the cancel")
	}
}

// TestView_UpdatesViewWithInstallShowsBatch (Spec 035 F4): when a batch
// update runs inside the updates view, the install overlay replaces the
// view and carries the batch context line.
func TestView_UpdatesViewWithInstallShowsBatch(t *testing.T) {
	m := newTUITestModel(t)
	m.updatesView = true
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Phase: PhaseDownloading, Percent: 40}
	m.updateQueue = make([]toolUpdate, 5)
	m.updateQueue[1] = toolUpdate{Tool: "go", Latest: "1.26.5"}
	m.updateIdx = 1

	out := m.View()
	if !strings.Contains(out, "Installing go@1.26.5") {
		t.Errorf("view should render the install overlay inside updates view:\n%s", out)
	}
	if !strings.Contains(out, "Batch 2/5") {
		t.Errorf("view should render the batch context line:\n%s", out)
	}
}

// TestView_PlainInstallOverlayHasNoBatchLine (Spec 035 F4): a plain
// single install (no update queue) renders exactly as before — no
// batch line.
func TestView_PlainInstallOverlayHasNoBatchLine(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 1, Tool: "go", Version: "1.26.5", Phase: PhaseDownloading, Percent: 40}

	out := m.viewInstallOverlay("")
	if strings.Contains(out, "Batch") {
		t.Errorf("plain install overlay must not contain a Batch line:\n%s", out)
	}
}

// TestHelpOverlay_ListsUpdatesKey (Spec 035 F9): the help key table
// includes the [x] updates-view shortcut.
func TestHelpOverlay_ListsUpdatesKey(t *testing.T) {
	m := newTUITestModel(t)
	out := m.viewHelpOverlay("")
	if !strings.Contains(out, "Available updates") {
		t.Errorf("help overlay should list the x shortcut:\n%s", out)
	}
}

// TestUpdatesViewRendersErrMsg (Spec 036 N1): errors must be visible
// inside the updates view — View() early-returns for updatesView and
// never reaches the main view's errBar.
func TestUpdatesViewRendersErrMsg(t *testing.T) {
	m := newTUITestModel(t)
	m.updatesView = true
	m.errMsg = "boom"
	out := m.View()
	if !strings.Contains(out, "Error: boom") {
		t.Errorf("updates view should render errMsg inline:\n%s", out)
	}
}

// TestUpdatesViewNoErrMsgWhenEmpty (Spec 036 N1): no error line when
// errMsg is empty.
func TestUpdatesViewNoErrMsgWhenEmpty(t *testing.T) {
	m := newTUITestModel(t)
	m.updatesView = true
	out := m.View()
	if strings.Contains(out, "Error:") {
		t.Errorf("updates view should not render an error line when errMsg is empty:\n%s", out)
	}
}

// TestUpdatesViewEmptyListWithErrMsg (Spec 036 N1): the empty-list hint
// and the error line coexist.
func TestUpdatesViewEmptyListWithErrMsg(t *testing.T) {
	m := newTUITestModel(t)
	m.updatesView = true
	m.errMsg = "boom"
	out := m.View()
	if !strings.Contains(out, "暂无可用更新") || !strings.Contains(out, "Error: boom") {
		t.Errorf("both the empty-list hint and the error line should render:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// 票 40（P2-19 / E-15）：陈旧结果消息判定改用 currentOpID，且必须覆盖
// 成功与错误两条分支
// ---------------------------------------------------------------------------
//
// 旧实现只在 installState != nil 时比对 ID：取消（installState=nil）或
// 卸载（借用 installState.ID，为 0）之后到达的迟到消息会被当作「当前
// 操作的结果」——错误消息弹误导性横幅，成功消息甚至会让刚被清空的
// 「全部更新」队列静默续跑（N-14）。

// TestStartInstall_SetsCurrentOpID 起点：startInstall 同时置位
// installState.ID 与 currentOpID（doInstall 只构造 Cmd，不执行安装）。
func TestStartInstall_SetsCurrentOpID(t *testing.T) {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   t.TempDir(),
			InstallDir: t.TempDir(),
			ShimDir:    t.TempDir(),
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()
	tool := &manifest.Tool{Name: "go", Category: "language", Shims: []string{"go"},
		VersionList: []manifest.Version{{Version: "1.26.5"}}}
	reg.Register(plugin.NewGeneric(tool))

	m := NewModel(cfg, cat, reg)
	updated, _ := m.startInstall()
	m2 := updated.(Model)

	if m2.installState == nil {
		t.Fatal("startInstall 未建立 installState")
	}
	if m2.currentOpID == 0 {
		t.Error("currentOpID = 0, want 非零（结果消息将无法与操作对上）")
	}
	if m2.installState.ID != m2.currentOpID {
		t.Errorf("installState.ID = %d, currentOpID = %d, want 相同",
			m2.installState.ID, m2.currentOpID)
	}
}

// TestInstallResultSuccess_LateMessageDoesNotResumeQueue（N-14）：取消后
// 迟到的**成功**消息不得推动更新队列（旧实现会走成功分支续跑/清队列）。
func TestInstallResultSuccess_LateMessageDoesNotResumeQueue(t *testing.T) {
	m := newTUITestModel(t)
	// 取消后的状态：currentOpID 已作废，队列刻意保留以暴露「静默续跑」。
	m.currentOpID = 0
	m.updateQueue = []toolUpdate{
		{Tool: "go", Current: "1.0", Latest: "2.0"},
		{Tool: "node", Current: "18.0", Latest: "20.0"},
	}
	m.updateIdx = 0

	updated, cmd := m.Update(InstallResultMsg{ID: 1, Tool: "go", Version: "2.0"})
	m2 := updated.(Model)
	if cmd != nil {
		t.Error("迟到的成功消息触发了命令（可能是续跑批量更新）")
	}
	if len(m2.updateQueue) != 2 || m2.updateIdx != 0 {
		t.Errorf("迟到消息改动了更新队列: len=%d idx=%d, want 2/0",
			len(m2.updateQueue), m2.updateIdx)
	}
	if m2.installState != nil {
		t.Error("迟到的成功消息不应建立 installState")
	}
}

// TestInstallResultError_LateMessageDoesNotSetErrMsg：取消后迟到的**错误**
// 消息不得弹出误导性错误横幅。
func TestInstallResultError_LateMessageDoesNotSetErrMsg(t *testing.T) {
	m := newTUITestModel(t)
	m.currentOpID = 0

	updated, cmd := m.Update(InstallResultMsg{ID: 99, Err: fmt.Errorf("stale boom")})
	m2 := updated.(Model)
	if m2.errMsg != "" {
		t.Errorf("errMsg = %q, want 空（陈旧错误消息被污染当前状态）", m2.errMsg)
	}
	if cmd != nil {
		t.Error("陈旧错误消息不应触发命令")
	}
}

// TestInstallCancel_InvalidatesOpID：取消即作废 currentOpID——取消后到达
// 的结果消息走「陈旧」分支被整体丢弃（E-15 的原始场景）。
func TestInstallCancel_InvalidatesOpID(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 42, Tool: "go", Version: "1.26.5", Cancel: func() {}}
	m.currentOpID = 42

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)
	if m2.currentOpID != 0 {
		t.Fatalf("取消后 currentOpID = %d, want 0", m2.currentOpID)
	}

	updated, cmd := m2.Update(InstallResultMsg{ID: 42, Err: fmt.Errorf("cancelled install failed late")})
	m3 := updated.(Model)
	if m3.errMsg != "" {
		t.Errorf("取消后迟到的错误消息污染了 errMsg: %q", m3.errMsg)
	}
	if cmd != nil {
		t.Error("取消后迟到的结果消息不应触发命令")
	}
}

// TestUninstallResultMsg_OwnTypeAndStaleGuard：卸载结果使用**独立消息类型**
// （防与安装结果跨空间撞号），陈旧判定口径与安装一致。
func TestUninstallResultMsg_OwnTypeAndStaleGuard(t *testing.T) {
	m := newTUITestModel(t)
	m.currentOpID = 7

	// 1) ID 不匹配 → 整体丢弃，连 currentOpID 都不动。
	updated, cmd := m.Update(UninstallResultMsg{ID: 6, Tool: "go", Version: "1.0", Err: fmt.Errorf("stale")})
	m2 := updated.(Model)
	if m2.errMsg != "" || cmd != nil {
		t.Errorf("陈旧卸载结果被接受: errMsg=%q cmd=%v", m2.errMsg, cmd)
	}
	if m2.currentOpID != 7 {
		t.Errorf("陈旧卸载结果改动了 currentOpID = %d, want 7", m2.currentOpID)
	}

	// 2) ID 匹配的成功 → 复位 currentOpID 并刷新数据。
	updated, cmd = m2.Update(UninstallResultMsg{ID: 7, Tool: "go", Version: "1.0"})
	m3 := updated.(Model)
	if cmd == nil {
		t.Error("当前卸载操作的成功结果应触发刷新命令")
	}
	if m3.currentOpID != 0 {
		t.Errorf("currentOpID = %d, want 0（结果已消费）", m3.currentOpID)
	}

	// 3) ID 匹配的失败 → 错误可见。
	updated, _ = m2.Update(UninstallResultMsg{ID: 7, Tool: "go", Version: "1.0", Err: fmt.Errorf("uninstall boom")})
	m4 := updated.(Model)
	if m4.errMsg != "uninstall boom" {
		t.Errorf("errMsg = %q, want %q", m4.errMsg, "uninstall boom")
	}
	if m4.currentOpID != 0 {
		t.Errorf("currentOpID = %d, want 0", m4.currentOpID)
	}
}

// TestInstallResultMsg_StaleForNewerOp：新操作进行中时，上一个操作的迟到
// 消息不得干扰新操作（installState 与 currentOpID 都保持）。
func TestInstallResultMsg_StaleForNewerOp(t *testing.T) {
	m := newTUITestModel(t)
	m.installState = &InstallState{ID: 2, Tool: "node", Version: "20.0"}
	m.currentOpID = 2
	m.updateQueue = []toolUpdate{{Tool: "go", Current: "1.0", Latest: "2.0"}}

	updated, cmd := m.Update(InstallResultMsg{ID: 1, Err: fmt.Errorf("stale boom")})
	m2 := updated.(Model)
	if m2.installState == nil || m2.installState.ID != 2 {
		t.Errorf("当前安装状态被陈旧消息破坏: %+v", m2.installState)
	}
	if m2.currentOpID != 2 {
		t.Errorf("currentOpID = %d, want 2", m2.currentOpID)
	}
	if m2.errMsg != "" || cmd != nil {
		t.Errorf("陈旧消息产生副作用: errMsg=%q cmd=%v", m2.errMsg, cmd)
	}
	if len(m2.updateQueue) != 1 {
		t.Errorf("updateQueue 被陈旧消息改动: %v", m2.updateQueue)
	}
}
