package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

func newPickerModel(t *testing.T, versions ...string) Model {
	t.Helper()
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   t.TempDir(),
		InstallDir: t.TempDir(),
		ShimDir:    t.TempDir(),
	}}
	var list []manifest.Version
	for _, v := range versions {
		list = append(list, manifest.Version{Version: v})
	}
	tool := &manifest.Tool{Name: "go", Category: "language", Shims: []string{"go"}, VersionList: list}
	reg := plugin.NewRegistry()
	reg.Register(plugin.NewGeneric(tool))
	return NewModel(cfg, &manifest.Catalog{}, reg)
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestNewModel_LoadsInstallableVersions: the Remote tab must be populated
// eagerly from the manifest; previously it stayed empty until 'r'.
func TestNewModel_LoadsInstallableVersions(t *testing.T) {
	m := newPickerModel(t, "1.27.3", "1.26.5", "1.25.0")
	if len(m.remoteVersions) != 3 {
		t.Fatalf("remoteVersions = %v, want 3 versions", m.remoteVersions)
	}
	m.detailTab = TabRemote
	out := m.viewDetail(80, 20)
	for _, v := range m.remoteVersions {
		if !strings.Contains(out, v) {
			t.Errorf("detail view missing installable version %q:\n%s", v, out)
		}
	}
	if !strings.Contains(out, "Remote 3") {
		t.Errorf("tab header missing remote count:\n%s", out)
	}
}

// TestInstallKey_FocusesThirdPaneList: 'i' moves focus into the third pane's
// Remote tab (no popup window), highlighting the default version.
func TestInstallKey_FocusesThirdPaneList(t *testing.T) {
	m := newPickerModel(t, "1.27.3", "1.26.5")

	updated, _ := m.Update(key("i"))
	m = updated.(Model)

	if m.focus != FocusDetail {
		t.Errorf("focus = %v, want FocusDetail (third pane)", m.focus)
	}
	if m.detailTab != TabRemote {
		t.Errorf("detailTab = %v, want TabRemote (installable list)", m.detailTab)
	}
	if m.versionIdx != 0 {
		t.Errorf("versionIdx = %d, want 0 (default version)", m.versionIdx)
	}
	if m.installState != nil {
		t.Error("pressing i must not start an install immediately")
	}
}

// TestEnterOnRemoteTab_InstallsHighlightedVersion: Enter in the Remote tab
// installs the selected row.
func TestEnterOnRemoteTab_InstallsHighlightedVersion(t *testing.T) {
	m := newPickerModel(t, "3.0.0", "2.0.0", "1.0.0")
	m.focus = FocusDetail
	m.detailTab = TabRemote
	m.versionIdx = 2

	updated, _ := m.Update(key("enter"))
	m = updated.(Model)
	if m.installState == nil {
		t.Fatal("Enter on Remote tab did not start an install")
	}
	if m.installState.Version != "1.0.0" {
		t.Errorf("installed version = %q, want 1.0.0", m.installState.Version)
	}
}

// TestViewDetail_RendersInstallProgressInThirdPane: while installing, the
// third pane shows progress instead of a full-screen overlay.
func TestViewDetail_RendersInstallProgressInThirdPane(t *testing.T) {
	m := newPickerModel(t, "1.27.3")
	m.installState = &InstallState{
		ID:      1,
		Tool:    "go",
		Version: "1.27.3",
		Phase:   PhaseDownloading,
		Percent: 42,
		Speed:   "1.2 MB/s",
	}

	out := m.viewDetail(60, 20)
	for _, want := range []string{"Installing go@1.27.3", "42%", "Press Esc to cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail panel missing %q:\n%s", want, out)
		}
	}
}

func TestInstallKey_NoInstallableVersions(t *testing.T) {
	m := newPickerModel(t)
	updated, _ := m.Update(key("i"))
	m = updated.(Model)
	if m.installState != nil {
		t.Error("install started without versions")
	}
	if !strings.Contains(m.errMsg, "no installable versions") {
		t.Errorf("errMsg = %q, want no-installable-versions error", m.errMsg)
	}
}
