// app_ignore.go implements the ignored-tools system for the GUI.
//
// Users can ignore updates or local-scan results for specific tools.
// The ignore list is persisted to $XMDEV_HOME/cache/ignored_tools.json
// using the same saveJSONToCache / loadJSONFromCache pattern as favorites.
//
// Provides:
//   - GetIgnoredTools: list all ignored tools
//   - IgnoreTool: add a tool to the ignore list
//   - UnignoreTool: remove a tool from the ignore list
//   - isIgnored: internal check for filtering (used by updates & scan)
package gui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ignoredFileName is the cache file name for ignored tools.
var ignoredFileName = "ignored_tools.json"

// maxIgnoredItems is the hard cap on the number of ignored entries.
// It is a var (not const) so tests can override it.
var maxIgnoredItems = 2000

// namePattern validates tool names for the ignore list.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// IgnoredToolInfo represents a tool that the user has chosen to ignore.
type IgnoredToolInfo struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`           // "update" | "local-scan"
	Path      string `json:"path,omitempty"` // optional, only for local-scan
	IgnoredAt string `json:"ignoredAt"`      // RFC3339 timestamp
}

// ignoredStore is the on-disk storage format for ignored tools.
type ignoredStore struct {
	Items []IgnoredToolInfo `json:"items"`
}

// GetIgnoredTools returns the current list of ignored tools.
func (a *App) GetIgnoredTools() []IgnoredToolInfo {
	a.ignoredMu.RLock()
	defer a.ignoredMu.RUnlock()
	result := make([]IgnoredToolInfo, len(a.ignored))
	copy(result, a.ignored)
	return result
}

// IgnoreTool adds a tool to the ignore list.
//   - name is trimmed; validated against ^[a-zA-Z0-9._-]+$, 1-64 chars, no .. / \ NUL
//   - kind must be "update" or "local-scan"
//   - path is optional; ≤512 chars, no NUL; used only as match key
//   - hard cap of maxIgnoredItems (2000) — returns error if full
//   - idempotent: duplicate key returns nil (debug log)
func (a *App) IgnoreTool(name, kind, path string) error {
	// ── Validation ──
	name = strings.TrimSpace(name)
	if err := validateIgnoreName(name); err != nil {
		return err
	}
	if kind != "update" && kind != "local-scan" {
		return fmt.Errorf("kind must be 'update' or 'local-scan'")
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path contains null byte")
	}
	if len(path) > 512 {
		return fmt.Errorf("path exceeds 512 characters")
	}

	a.ignoredMu.Lock()
	defer a.ignoredMu.Unlock()

	// Hard cap check
	if len(a.ignored) >= maxIgnoredItems {
		logError("[ignore] add failed name=%q kind=%q: max items (%d) reached", name, kind, maxIgnoredItems)
		return fmt.Errorf("ignore list is full (max %d items)", maxIgnoredItems)
	}

	// Idempotent check
	for _, item := range a.ignored {
		if sameIgnoreKey(item, IgnoredToolInfo{Name: name, Kind: kind, Path: path}) {
			logDebug("[ignore] already ignored name=%q kind=%q", name, kind)
			return nil
		}
	}

	// Add entry
	entry := IgnoredToolInfo{
		Name:      name,
		Kind:      kind,
		Path:      path,
		IgnoredAt: time.Now().Format(time.RFC3339),
	}
	a.ignored = append(a.ignored, entry)

	if err := a.saveIgnored(); err != nil {
		// Rollback
		a.ignored = a.ignored[:len(a.ignored)-1]
		logError("[ignore] add failed name=%q kind=%q: %v", name, kind, err)
		return fmt.Errorf("failed to save ignore list: %w", err)
	}

	logInfo("[ignore] add name=%q kind=%q path=%q", name, kind, path)
	return nil
}

// UnignoreTool removes a tool from the ignore list.
//   - name/kind/path validation same as IgnoreTool
//   - if not found, logs DEBUG and returns nil (idempotent remove)
func (a *App) UnignoreTool(name, kind, path string) error {
	// ── Validation ──
	name = strings.TrimSpace(name)
	if err := validateIgnoreName(name); err != nil {
		return err
	}
	if kind != "update" && kind != "local-scan" {
		return fmt.Errorf("kind must be 'update' or 'local-scan'")
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path contains null byte")
	}
	if len(path) > 512 {
		return fmt.Errorf("path exceeds 512 characters")
	}

	a.ignoredMu.Lock()
	defer a.ignoredMu.Unlock()

	kept := a.ignored[:0]
	removed := 0
	var removedItems []IgnoredToolInfo
	for _, item := range a.ignored {
		// Exact key match, or same name+kind when the caller omitted the
		// path (dashboard unignore must drop every local-scan row for the tool).
		if sameIgnoreKey(item, IgnoredToolInfo{Name: name, Kind: kind, Path: path}) ||
			(item.Name == name && item.Kind == kind && path == "") {
			removed++
			removedItems = append(removedItems, item)
			continue
		}
		kept = append(kept, item)
	}
	if removed == 0 {
		logDebug("[ignore] remove miss name=%q kind=%q", name, kind)
		return nil
	}
	a.ignored = kept
	if err := a.saveIgnored(); err != nil {
		a.ignored = append(a.ignored, removedItems...)
		logError("[ignore] remove failed name=%q kind=%q: %v", name, kind, err)
		return fmt.Errorf("failed to save ignore list: %w", err)
	}
	logInfo("[ignore] remove name=%q kind=%q path=%q count=%d", name, kind, path, removed)
	return nil
}

// isIgnored checks whether a tool should be filtered due to being ignored.
//   - kind=update: match by name only
//   - kind=local-scan: match by name + path; if the stored entry has an empty
//     path it matches all paths for that tool name
//
// Holds a read lock on ignoredMu.
func (a *App) isIgnored(name, kind, path string) bool {
	a.ignoredMu.RLock()
	defer a.ignoredMu.RUnlock()
	for _, item := range a.ignored {
		if item.Name == name && item.Kind == kind {
			if kind == "update" {
				return true
			}
			// local-scan: empty path = ignore all instances; otherwise exact match
			if item.Path == "" || item.Path == path {
				return true
			}
		}
	}
	return false
}

// loadIgnored loads the ignored tools list from the cache file.
// If loading fails, the list is set to empty and a WARN is logged.
func (a *App) loadIgnored() {
	var store ignoredStore
	if err := a.loadJSONFromCache(ignoredFileName, &store); err != nil {
		logWarn("[ignore] load failed: %v — using empty list", err)
		a.ignoredMu.Lock()
		a.ignored = nil
		a.ignoredMu.Unlock()
		return
	}
	if store.Items == nil {
		store.Items = []IgnoredToolInfo{}
	}

	a.ignoredMu.Lock()
	a.ignored = store.Items
	a.ignoredMu.Unlock()

	cfg := a.getCfg()
	fullPath := filepath.Join(cfg.Core.CacheDir, ignoredFileName)
	logDebug("[ignore] loaded n=%d from %s", len(store.Items), fullPath)
}

// saveIgnored persists the ignored tools list to the cache file.
// Caller must hold a.ignoredMu lock (write).
func (a *App) saveIgnored() error {
	store := ignoredStore{Items: a.ignored}
	if err := a.saveJSONToCache(ignoredFileName, &store); err != nil {
		logError("[ignore] save failed: %v", err)
		return err
	}
	return nil
}

// ── Validation helpers ──

// validateIgnoreName validates the name field for ignore operations.
// Rules: trimmed length 1-64, ^[a-zA-Z0-9._-]+, no .. / \ NUL.
func validateIgnoreName(name string) error {
	if name == "" || len(name) > 64 {
		return fmt.Errorf("name must be 1-64 characters")
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("name contains null byte")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name must not contain '..'")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name must match [a-zA-Z0-9._-]+")
	}
	return nil
}

// sameIgnoreKey checks whether two entries have the same identity key.
//   - kind=update: unique by name
//   - kind=local-scan: unique by name + path (empty path = name only)
func sameIgnoreKey(a, b IgnoredToolInfo) bool {
	if a.Name != b.Name || a.Kind != b.Kind {
		return false
	}
	if a.Kind == "update" {
		return true
	}
	return a.Path == b.Path
}
