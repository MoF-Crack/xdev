// app_ignore_test.go tests the ignored-tools backend API.
package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Test: Idempotent ignore ──

func TestIgnore_Idempotent(t *testing.T) {
	app := newTestApp()

	// First ignore should succeed
	if err := app.IgnoreTool("go", "update", ""); err != nil {
		t.Fatalf("first ignore failed: %v", err)
	}

	// Idempotent second ignore should return nil (already exists)
	if err := app.IgnoreTool("go", "update", ""); err != nil {
		t.Fatalf("idempotent ignore failed: %v", err)
	}

	items := app.GetIgnoredTools()
	if len(items) != 1 {
		t.Fatalf("expected 1 ignored tool, got %d", len(items))
	}
	if items[0].Name != "go" || items[0].Kind != "update" {
		t.Errorf("unexpected item: Name=%q Kind=%q", items[0].Name, items[0].Kind)
	}
	if items[0].IgnoredAt == "" {
		t.Error("IgnoredAt should not be empty")
	}
}

// ── Test: Unignore ──

func TestIgnore_Unignore(t *testing.T) {
	app := newTestApp()

	if err := app.IgnoreTool("java", "update", ""); err != nil {
		t.Fatalf("ignore failed: %v", err)
	}
	if len(app.GetIgnoredTools()) != 1 {
		t.Fatalf("expected 1 item after ignore")
	}

	if err := app.UnignoreTool("java", "update", ""); err != nil {
		t.Fatalf("unignore failed: %v", err)
	}

	items := app.GetIgnoredTools()
	if len(items) != 0 {
		t.Errorf("expected 0 items after unignore, got %d", len(items))
	}
}

// ── Test: Unignore miss (not found) ──

func TestIgnore_Unignore_Miss(t *testing.T) {
	app := newTestApp()

	// Unignoring a non-existent entry should succeed silently
	if err := app.UnignoreTool("nonexistent", "update", ""); err != nil {
		t.Fatalf("unignore miss should succeed, got: %v", err)
	}
}

// ── Test: Reject bad names ──

func TestIgnore_RejectBadName(t *testing.T) {
	app := newTestApp()
	tests := []struct {
		name string
		kind string
	}{
		{"", "update"},                    // empty
		{"../etc", "update"},              // contains ..
		{"path/slash", "update"},          // contains /
		{"back\\slash", "update"},         // contains \
		{strings.Repeat("a", 65), "update"}, // too long
		{"bad name", "update"},            // contains space
		{"tool@name", "update"},           // contains @
		{"tool$name", "update"},           // contains $
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.IgnoreTool(tt.name, tt.kind, "")
			if err == nil {
				t.Errorf("expected error for name=%q", tt.name)
			}
		})
	}
}

// ── Test: Reject bad kind ──

func TestIgnore_RejectBadKind(t *testing.T) {
	app := newTestApp()
	if err := app.IgnoreTool("go", "invalid-kind", ""); err == nil {
		t.Error("expected error for invalid kind")
	}
}

// ── Test: Reject NUL byte ──

func TestIgnore_RejectNUL(t *testing.T) {
	app := newTestApp()

	// NUL in name
	if err := app.IgnoreTool("go\x00lang", "update", ""); err == nil {
		t.Error("expected error for NUL in name")
	}

	// NUL in path
	if err := app.IgnoreTool("go", "local-scan", "valid\x00path"); err == nil {
		t.Error("expected error for NUL in path")
	}
}

// ── Test: Reject overlong path ──

func TestIgnore_RejectLongPath(t *testing.T) {
	app := newTestApp()
	longPath := strings.Repeat("x", 513)
	if err := app.IgnoreTool("go", "local-scan", longPath); err == nil {
		t.Error("expected error for path >512 chars")
	}
}

// ── Test: Corrupt JSON → empty list, no panic ──

func TestIgnore_CorruptJSON(t *testing.T) {
	app := newTestApp()

	// Write corrupt JSON to the ignored file
	cfg := app.getCfg()
	corruptPath := filepath.Join(cfg.Core.CacheDir, ignoredFileName)
	if err := os.MkdirAll(cfg.Core.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{corrupt}"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	// loadIgnored must not panic and produce empty list
	app.loadIgnored()

	items := app.GetIgnoredTools()
	if items == nil || len(items) != 0 {
		t.Errorf("expected empty list after corrupt load, got %v", items)
	}
}

// ── Test: Hard cap ──

func TestIgnore_HardCap(t *testing.T) {
	// Override max to a small value for testing
	oldMax := maxIgnoredItems
	maxIgnoredItems = 3
	t.Cleanup(func() { maxIgnoredItems = oldMax })

	app := newTestApp()

	// Fill to cap
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("tool-%d", i)
		if err := app.IgnoreTool(name, "update", ""); err != nil {
			t.Fatalf("unexpected error on item %d: %v", i, err)
		}
	}

	// Should hit cap
	if err := app.IgnoreTool("overflow", "update", ""); err == nil {
		t.Error("expected error when exceeding cap")
	}

	// Verify still only 3 items
	items := app.GetIgnoredTools()
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

// ── Test: Local-scan unique key (name+path) ──

func TestIgnore_LocalScanUniqueKey(t *testing.T) {
	app := newTestApp()

	// Same name, different paths → both stored
	if err := app.IgnoreTool("go", "local-scan", "/path/a"); err != nil {
		t.Fatalf("ignore path a failed: %v", err)
	}
	if err := app.IgnoreTool("go", "local-scan", "/path/b"); err != nil {
		t.Fatalf("ignore path b failed: %v", err)
	}

	items := app.GetIgnoredTools()
	if len(items) != 2 {
		t.Fatalf("expected 2 items for different paths, got %d", len(items))
	}

	// Same name + same path → idempotent (count stays 2)
	if err := app.IgnoreTool("go", "local-scan", "/path/a"); err != nil {
		t.Fatalf("idempotent ignore failed: %v", err)
	}
	items = app.GetIgnoredTools()
	if len(items) != 2 {
		t.Errorf("expected still 2 after idempotent add, got %d", len(items))
	}

	// Unignore one path
	if err := app.UnignoreTool("go", "local-scan", "/path/a"); err != nil {
		t.Fatalf("unignore path a failed: %v", err)
	}
	items = app.GetIgnoredTools()
	if len(items) != 1 {
		t.Errorf("expected 1 item after unignore, got %d", len(items))
	}
	if items[0].Path != "/path/b" {
		t.Errorf("expected remaining item to have path /path/b, got %q", items[0].Path)
	}
}

// ── Test: Update and local-scan are independent ──

func TestIgnore_UpdateLocalScanIndependent(t *testing.T) {
	app := newTestApp()

	if err := app.IgnoreTool("go", "update", ""); err != nil {
		t.Fatalf("ignore update failed: %v", err)
	}
	if err := app.IgnoreTool("go", "local-scan", ""); err != nil {
		t.Fatalf("ignore local-scan failed: %v", err)
	}

	items := app.GetIgnoredTools()
	if len(items) != 2 {
		t.Fatalf("expected 2 items (update + local-scan), got %d", len(items))
	}

	// Unignore only update
	if err := app.UnignoreTool("go", "update", ""); err != nil {
		t.Fatalf("unignore update failed: %v", err)
	}

	items = app.GetIgnoredTools()
	if len(items) != 1 {
		t.Fatalf("expected 1 item after unignoring update, got %d", len(items))
	}
	if items[0].Kind != "local-scan" {
		t.Errorf("expected remaining item kind=local-scan, got %q", items[0].Kind)
	}
}

// ── Test: isIgnored helper ──

func TestIgnore_IsIgnored(t *testing.T) {
	app := newTestApp()

	if err := app.IgnoreTool("go", "update", ""); err != nil {
		t.Fatalf("ignore go update: %v", err)
	}
	if err := app.IgnoreTool("python", "local-scan", "/usr/bin/python3"); err != nil {
		t.Fatalf("ignore python local-scan: %v", err)
	}

	// update: match by name
	if !app.isIgnored("go", "update", "") {
		t.Error("go/update should be ignored")
	}
	if app.isIgnored("java", "update", "") {
		t.Error("java/update should NOT be ignored")
	}

	// local-scan: match by name + path
	if !app.isIgnored("python", "local-scan", "/usr/bin/python3") {
		t.Error("python/local-scan with exact path should be ignored")
	}
	if app.isIgnored("python", "local-scan", "/other/path") {
		t.Error("python/local-scan with different path should NOT be ignored")
	}
}

// ── Test: isIgnored with empty path (match all) ──

func TestIgnore_IsIgnored_EmptyPath(t *testing.T) {
	app := newTestApp()

	// Ignore all local-scan for "node" (empty path)
	if err := app.IgnoreTool("node", "local-scan", ""); err != nil {
		t.Fatalf("ignore node local-scan empty path: %v", err)
	}

	// Any path should match
	if !app.isIgnored("node", "local-scan", "/any/path") {
		t.Error("node/local-scan with any path should be ignored when stored path is empty")
	}
	if !app.isIgnored("node", "local-scan", "") {
		t.Error("node/local-scan with empty path should be ignored")
	}
}

// ── Test: GetIgnoredTools returns a copy ──

func TestIgnore_GetIgnoredTools_Copy(t *testing.T) {
	app := newTestApp()

	if err := app.IgnoreTool("go", "update", ""); err != nil {
		t.Fatalf("ignore failed: %v", err)
	}

	// Get copy and modify it
	items := app.GetIgnoredTools()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	items[0].Name = "hacked"

	// Getting again should return original
	items2 := app.GetIgnoredTools()
	if items2[0].Name != "go" {
		t.Errorf("GetIgnoredTools should return a copy, got %q", items2[0].Name)
	}
}

// ── Test: ValidateIgnoreName ──

func TestValidateIgnoreName(t *testing.T) {
	tests := []struct {
		name    string
		valid   bool
	}{
		{"go", true},
		{"node.js", true},
		{"typescript-5.0", true},
		{"_underscore", true},
		{"", false},
		{strings.Repeat("a", 65), false},
		{"../etc", false},
		{"path/sep", false},
		{"back\\slash", false},
		{"has space", false},
		{"special@char", false},
		{"dollar$ign", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIgnoreName(tt.name)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
