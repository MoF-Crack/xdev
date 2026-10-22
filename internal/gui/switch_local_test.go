package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// Ticket 06 (audit-fixes / P0-08) guard tests: SwitchToLocalVersion must
// validate the tool name and plugin registration BEFORE touching the
// filesystem, and must propagate link-removal / junction-creation
// failures instead of logging them and returning nil.
//
// The historical bug: current/<toolName> was built from an unvalidated
// name and the link removed/re-created before the plugin lookup — a
// traversal name attacked the real current/node link, and a failed
// junction left the tool's link dangling while the caller saw success.

// switchTestApp builds an App with a temp home, a "node" plugin and a
// pre-created current/ directory. Returns the app and the current dir.
func switchTestApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "node", shims: []string{"node"}})
	app := integrityTestApp(t, reg, base)
	currentDir := filepath.Join(app.getCfg().Core.XMDevDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}
	return app, currentDir
}

// L1: a traversal tool name must be rejected with zero filesystem side
// effects — the real current/node link (which the cleaned path would
// alias) must be untouched.
func TestSwitchToLocalVersion_InvalidName_RejectsBeforeFS(t *testing.T) {
	app, currentDir := switchTestApp(t)

	// A real current/node link exists and must survive the attack.
	realTarget := t.TempDir()
	currentNode := filepath.Join(currentDir, "node")
	if err := util.CreateJunction(currentNode, realTarget); err != nil {
		t.Fatalf("setup junction: %v", err)
	}
	local := t.TempDir() // a perfectly valid explicit local path

	payload := `..\current\node`
	if runtime.GOOS != "windows" {
		payload = "../current/node"
	}
	err := app.SwitchToLocalVersion(payload, local)
	if err == nil {
		t.Fatal("expected rejection for traversal tool name")
	}

	got, rerr := os.Readlink(currentNode)
	if rerr != nil {
		t.Fatalf("current/node link was destroyed by a rejected call: %v", rerr)
	}
	if got != realTarget {
		t.Errorf("current/node target changed: got %q, want %q", got, realTarget)
	}
	// The local dir must not have been adopted anywhere.
	if link, _ := os.Readlink(currentNode); strings.EqualFold(link, local) {
		t.Error("traversal call must not create any link to the local path")
	}
}

// L2: an unregistered (but syntactically valid) tool name must be
// rejected before any filesystem mutation — the current/ directory
// listing is unchanged.
func TestSwitchToLocalVersion_Unregistered_RejectsBeforeFS(t *testing.T) {
	app, currentDir := switchTestApp(t)
	// Pre-populate current/ so a mutation would be observable.
	if err := util.CreateJunction(filepath.Join(currentDir, "node"), t.TempDir()); err != nil {
		t.Fatalf("setup junction: %v", err)
	}
	before := listDirNames(t, currentDir)

	err := app.SwitchToLocalVersion("not-a-registered-tool", t.TempDir())
	if err == nil {
		t.Fatal("expected rejection for unregistered tool name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention the unknown tool, got: %v", err)
	}

	after := listDirNames(t, currentDir)
	if strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("current/ mutated by rejected call: before=%v after=%v", before, after)
	}
}

// L3: a junction-creation failure must surface as an error — previously
// it was logged ("falling back" — there was no fallback) and nil was
// returned, leaving the tool's link deleted and dangling.
func TestSwitchToLocalVersion_JunctionFails_ReturnsError(t *testing.T) {
	base := t.TempDir()
	reg := plugin.NewRegistry()
	reg.Register(&guiFakePlugin{name: "node", shims: []string{"node"}})
	app := integrityTestApp(t, reg, base)
	// current/ is intentionally NOT created: CreateJunction cannot create
	// a link whose parent directory is missing → deterministic failure.

	err := app.SwitchToLocalVersion("node", t.TempDir())
	if err == nil {
		t.Fatal("expected error when junction creation fails (was swallowed before the fix)")
	}
	if !strings.Contains(err.Error(), "junction") && !strings.Contains(err.Error(), "link") {
		t.Errorf("error should mention the junction failure, got: %v", err)
	}
}

// L4: the happy path is unchanged — a valid switch re-points the link and
// returns nil. Note: on Windows, os.Readlink is the canonical way to
// inspect a junction target (filepath.EvalSymlinks does not resolve
// directory junctions — verified empirically on Go 1.26).
func TestSwitchToLocalVersion_Valid_Unchanged(t *testing.T) {
	app, currentDir := switchTestApp(t)

	local := t.TempDir()
	if err := app.SwitchToLocalVersion("node", local); err != nil {
		t.Fatalf("valid switch failed: %v", err)
	}

	link := filepath.Join(currentDir, "node")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("current/node link missing after valid switch: %v", err)
	}
	if got != local && !strings.EqualFold(got, local) {
		t.Errorf("current/node target = %q, want %q", got, local)
	}
}

// listDirNames returns the sorted names of a directory's entries.
func listDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
