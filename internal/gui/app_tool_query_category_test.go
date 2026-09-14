// app_tool_query_category_test.go tests the Spec 024 归并层
// (GetCategories / computeToolsByCategory) that merges ide/agent/harness
// into the single flat development_tools category.
package gui

import (
	"context"
	"sort"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// catFakePlugin is a guiFakePlugin variant that allows setting Category,
// needed because guiFakePlugin.Category() is hardcoded to "language".
type catFakePlugin struct {
	name     string
	category string
	manifest *manifest.Tool
}

func (c *catFakePlugin) Name() string                          { return c.name }
func (c *catFakePlugin) DisplayName() string                   { return c.name }
func (c *catFakePlugin) Description() string                   { return "" }
func (c *catFakePlugin) Homepage() string                      { return "" }
func (c *catFakePlugin) Category() string                      { return c.category }
func (c *catFakePlugin) Manifest() *manifest.Tool              { return c.manifest }
func (c *catFakePlugin) ResolveVersion(string) (string, error) { return "1.0.0", nil }
func (c *catFakePlugin) DefaultVersion() string                { return "1.0.0" }
func (c *catFakePlugin) ArchiveFor(string) (*manifest.Archive, error) {
	return nil, nil
}
func (c *catFakePlugin) InstallDir(root, v string) string { return root + "/" + c.name + "/" + v }
func (c *catFakePlugin) Shims() []string                  { return nil }
func (c *catFakePlugin) EnvVars(string) map[string]string { return nil }
func (c *catFakePlugin) PostInstall(_ context.Context, _ string) error {
	return nil
}
func (c *catFakePlugin) Deactivate(string) error  { return nil }
func (c *catFakePlugin) RequiredPlugin() []string { return nil }

var _ plugin.Plugin = (*catFakePlugin)(nil)

// catTestApp builds an App with the given plugins registered.
func catTestApp(t *testing.T, plugins ...*catFakePlugin) *App {
	t.Helper()
	reg := plugin.NewRegistry()
	for _, p := range plugins {
		reg.Register(p)
	}
	return integrityTestApp(t, reg, t.TempDir())
}

// findCategory looks up a CategoryInfo by name in the list.
func findCategory(cats []CategoryInfo, name string) (CategoryInfo, bool) {
	for _, c := range cats {
		if c.Name == name {
			return c, true
		}
	}
	return CategoryInfo{}, false
}

// TestDevToolsCategoryMerge verifies GetCategories merges ide/agent/harness
// counts into development_tools and no longer outputs the old entries.
func TestDevToolsCategoryMerge(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "vscode", category: "ide"},
		&catFakePlugin{name: "cursor", category: "ide"},
		&catFakePlugin{name: "claude-code", category: "agent"},
		&catFakePlugin{name: "copilot-cli", category: "harness"},
		&catFakePlugin{name: "cmake", category: "build"},
		&catFakePlugin{name: "node", category: "language"},
	)

	cats := app.GetCategories()

	// development_tools should be present with count = 4 (2 ide + 1 agent + 1 harness)
	dev, ok := findCategory(cats, "development_tools")
	if !ok {
		t.Fatalf("development_tools category not found; got %v", cats)
	}
	if dev.Count != 4 {
		t.Errorf("development_tools count = %d, want 4", dev.Count)
	}

	// old categories must NOT appear
	for _, old := range []string{"ide", "agent", "harness"} {
		if _, found := findCategory(cats, old); found {
			t.Errorf("old category %q still present; got %v", old, cats)
		}
	}

	// language and build remain
	if _, ok := findCategory(cats, "language"); !ok {
		t.Errorf("language category missing; got %v", cats)
	}
	if _, ok := findCategory(cats, "build"); !ok {
		t.Errorf("build category missing; got %v", cats)
	}

	// development_tools should appear between build and mobile in order
	devIdx, buildIdx, mobileIdx := -1, -1, -1
	for i, c := range cats {
		switch c.Name {
		case "development_tools":
			devIdx = i
		case "build":
			buildIdx = i
		case "mobile":
			mobileIdx = i
		}
	}
	if devIdx >= 0 && buildIdx >= 0 && devIdx < buildIdx {
		t.Errorf("development_tools (idx %d) before build (idx %d); want after build", devIdx, buildIdx)
	}
	if devIdx >= 0 && mobileIdx >= 0 && devIdx > mobileIdx {
		t.Errorf("development_tools (idx %d) after mobile (idx %d); want before mobile", devIdx, mobileIdx)
	}
}

// TestDevToolsCategoryAlwaysPresent verifies development_tools appears even
// when no tools have that category (mirrors language always-present behavior).
func TestDevToolsCategoryAlwaysPresent(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "node", category: "language"},
		&catFakePlugin{name: "cmake", category: "build"},
	)
	cats := app.GetCategories()
	if _, ok := findCategory(cats, "development_tools"); !ok {
		t.Errorf("development_tools should always be present (even with count 0); got %v", cats)
	}
}

// TestGetToolsByCategoryDevTools verifies GetToolsByCategory("development_tools")
// returns all tools whose category is in the merge set.
func TestGetToolsByCategoryDevTools(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "vscode", category: "ide"},
		&catFakePlugin{name: "cursor", category: "ide"},
		&catFakePlugin{name: "claude-code", category: "agent"},
		&catFakePlugin{name: "copilot-cli", category: "harness"},
		&catFakePlugin{name: "zed", category: "development_tools"},
		&catFakePlugin{name: "cmake", category: "build"},
		&catFakePlugin{name: "node", category: "language"},
	)

	// Clear cache to ensure fresh compute (App constructor may pre-warm).
	app.InvalidateToolsCache()
	tools := app.GetToolsByCategory("development_tools")

	got := make(map[string]bool, len(tools))
	for _, ti := range tools {
		got[ti.Name] = true
	}
	want := []string{"vscode", "cursor", "claude-code", "copilot-cli", "zed"}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q missing from development_tools; got %v", name, got)
		}
	}
	if len(tools) != len(want) {
		t.Errorf("development_tools count = %d, want %d (got=%v)", len(tools), len(want), got)
	}

	// build tools must NOT leak into development_tools
	if got["cmake"] {
		t.Errorf("cmake (build) leaked into development_tools")
	}
	if got["node"] {
		t.Errorf("node (language) leaked into development_tools")
	}
}

// TestCatalogInstallButtonOnlyForUninstalledPackageManagers verifies
// language/build/IDE tools never set CanInstall. Catalog Install is
// gated on canInstall===true, so a local Go/VS Code/Gradle copy must
// not show an Install button the way yarn/pnpm/pipx did.
func TestCatalogInstallButtonOnlyForUninstalledPackageManagers(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "go", category: "language"},
		&catFakePlugin{name: "gradle", category: "build"},
		&catFakePlugin{name: "vscode", category: "ide"},
	)
	app.InvalidateToolsCache()
	for _, category := range []string{"language", "build", "development_tools"} {
		for _, ti := range app.GetToolsByCategory(category) {
			if ti.CanInstall {
				t.Errorf("%s/%s CanInstall=true; catalog would show Install for a non-PM tool", category, ti.Name)
			}
			if ti.OfferSubToolInstall {
				t.Errorf("%s/%s OfferSubToolInstall=true; that flag is yarn/pnpm-only", category, ti.Name)
			}
		}
	}
}

// TestDevToolsCategoryResidualFallback verifies the merge layer tolerates
// disk manifests that still carry old category fields (ide/agent/harness).
// This is the core safety guarantee of the Spec 024 migration.
func TestDevToolsCategoryResidualFallback(t *testing.T) {
	// Simulate a disk manifest that was NOT migrated: category still "ide".
	app := catTestApp(t,
		&catFakePlugin{name: "vscode", category: "ide"},
	)
	cats := app.GetCategories()
	dev, ok := findCategory(cats, "development_tools")
	if !ok {
		t.Fatalf("development_tools not found despite residual ide; got %v", cats)
	}
	if dev.Count != 1 {
		t.Errorf("development_tools count = %d, want 1 (residual ide merged)", dev.Count)
	}
	if _, found := findCategory(cats, "ide"); found {
		t.Errorf("residual ide should be hidden behind development_tools; got %v", cats)
	}

	app.InvalidateToolsCache()
	tools := app.GetToolsByCategory("development_tools")
	if len(tools) != 1 || tools[0].Name != "vscode" {
		t.Errorf("residual ide tool not surfaced under development_tools; got %v", tools)
	}
}

// TestOldCategoryQueryStillWorks verifies backward compatibility:
// querying ide/agent/harness directly still returns those tools
// (regular filtering path, for tests/external callers).
func TestOldCategoryQueryStillWorks(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "vscode", category: "ide"},
		&catFakePlugin{name: "claude-code", category: "agent"},
	)
	// Direct query for "ide" goes through the merge branch (isDevToolsCategory)
	// and returns all dev-tools — this is acceptable: the merge layer is
	// symmetric. We verify it returns the tool rather than erroring.
	app.InvalidateToolsCache()
	tools := app.GetToolsByCategory("ide")
	found := false
	for _, ti := range tools {
		if ti.Name == "vscode" {
			found = true
		}
	}
	if !found {
		t.Errorf("querying legacy 'ide' did not surface vscode; got %v", tools)
	}
}

// TestDevToolsCategorySortedNames is a sanity check that the merge set
// constant covers exactly the Spec 024 expected names.
func TestDevToolsCategorySortedNames(t *testing.T) {
	want := map[string]bool{
		"ide": true, "agent": true, "harness": true, "development_tools": true,
	}
	if len(devToolsCategoryNames) != len(want) {
		t.Errorf("devToolsCategoryNames size = %d, want %d", len(devToolsCategoryNames), len(want))
	}
	for k := range want {
		if !devToolsCategoryNames[k] {
			t.Errorf("devToolsCategoryNames missing %q", k)
		}
	}
	// Ensure "build" is NOT in the merge set (regression guard)
	if isDevToolsCategory("build") {
		t.Errorf("build must not be a dev-tools category")
	}
	if isDevToolsCategory("language") {
		t.Errorf("language must not be a dev-tools category")
	}
}

// TestDevToolsCategoryOrderInResult guards the fixed display order.
func TestDevToolsCategoryOrderInResult(t *testing.T) {
	app := catTestApp(t,
		&catFakePlugin{name: "node", category: "language"},
		&catFakePlugin{name: "cmake", category: "build"},
		&catFakePlugin{name: "vscode", category: "development_tools"},
		&catFakePlugin{name: "android", category: "mobile"},
	)
	cats := app.GetCategories()
	names := make([]string, 0, len(cats))
	for _, c := range cats {
		names = append(names, c.Name)
	}
	// Expected fixed order (local/package-manager may or may not appear
	// depending on state; only assert the relative order of the fixed ones).
	expected := []string{"language", "build", "development_tools", "mobile"}
	var idxs []int
	for _, e := range expected {
		for i, n := range names {
			if n == e {
				idxs = append(idxs, i)
				break
			}
		}
	}
	if !sort.IntsAreSorted(idxs) {
		t.Errorf("fixed order broken; names=%v idxs=%v", names, idxs)
	}
}
