package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/localmigrate"
	"github.com/xmdemt/xdev/internal/util"
)

const rootRedirectFile = ".root-redirect.json"

// I/O seam for deterministic commit-failure tests.
var writeRootSelection = util.AtomicWriteFile

type rootRedirect struct {
	Schema int    `json:"schema"`
	From   string `json:"from"`
	Target string `json:"target"`
	HMAC   string `json:"hmac,omitempty"`
}

func rootPathKey(dir string) string {
	dir = filepath.Clean(dir)
	// Resolve the existing ancestor too, so a not-yet-created destination below
	// a directory alias cannot evade the overlap check.
	ancestor := dir
	var suffix []string
	for {
		if actual, err := filepath.EvalSymlinks(ancestor); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				actual = filepath.Join(actual, suffix[i])
			}
			dir = actual
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	return dir
}

// resolveRootRedirect follows only a bounded, integrity-checked selection record.
// It is shared by config loading and shim home discovery, including an old
// XMDEV_HOME inherited from a shell opened before the root switch.
func resolveRootRedirect(root string) (string, error) {
	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		key := rootPathKey(root)
		if seen[key] {
			return "", fmt.Errorf("config root redirect cycle at %s", root)
		}
		seen[key] = true
		data, err := os.ReadFile(filepath.Join(root, rootRedirectFile))
		if os.IsNotExist(err) {
			return root, nil
		}
		if err != nil {
			return "", fmt.Errorf("read root selection: %w", err)
		}
		var record rootRedirect
		if err = json.Unmarshal(data, &record); err != nil {
			return "", fmt.Errorf("parse root selection: %w", err)
		}
		mac := record.HMAC
		record.HMAC = ""
		signed, err := json.Marshal(record)
		if err != nil {
			return "", err
		}
		if record.Schema != 1 || rootPathKey(record.From) != key || mac == "" || !util.VerifyHMAC(signed, mac) {
			return "", fmt.Errorf("invalid root selection at %s", root)
		}
		if !filepath.IsAbs(record.Target) || strings.HasPrefix(record.Target, `\\`) {
			return "", fmt.Errorf("root selection target must be a local absolute path")
		}
		if fi, err := os.Stat(filepath.Join(record.Target, "config.yaml")); err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("root selection target has no readable config: %s", record.Target)
		}
		root = record.Target
	}
	return "", fmt.Errorf("too many config root redirects")
}

// SwitchRoot selects a new empty configuration root and reanchors all derived
// directories. It does not migrate or delete an existing installation tree.
// A record at the old root is the commit point: subsequent processes using that
// old root load the new config. If publication fails, the old root stays usable.
func (c *Config) SwitchRoot(newRoot string) error {
	return c.switchRoot(newRoot, false)
}

// MigrateRoot copies and verifies the old tree before publishing the same root
// selection record as SwitchRoot. The old tree remains a recovery copy; external
// environment references and arbitrary embedded absolute paths are not rewritten.
func (c *Config) MigrateRoot(newRoot string) error {
	return c.switchRoot(newRoot, true)
}

func (c *Config) switchRoot(newRoot string, copyTree bool) error {
	if !filepath.IsAbs(newRoot) || strings.HasPrefix(newRoot, `\\`) || strings.HasPrefix(newRoot, "//") {
		return fmt.Errorf("new root must be a local absolute path")
	}
	newRoot = filepath.Clean(newRoot)
	c.mu.Lock()
	defer c.mu.Unlock()
	oldRoot := filepath.Clean(c.Core.XMDevDir)
	if !filepath.IsAbs(oldRoot) {
		return fmt.Errorf("current root must be absolute")
	}
	oldKey, newKey := rootPathKey(oldRoot), rootPathKey(newRoot)
	contains := func(root, path string) bool {
		rel, err := filepath.Rel(root, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	if oldKey != newKey && (contains(oldKey, newKey) || contains(newKey, oldKey)) {
		return fmt.Errorf("old and new roots must not contain one another")
	}
	// Stable per-root file locks serialize independent objects/processes as well
	// as opposing switches. Never unlink a lock while a waiter may hold it.
	roots := []string{oldKey}
	if oldKey != newKey {
		roots = append(roots, newKey)
	}
	sort.Strings(roots)
	var locks []*util.FileLock
	defer func() {
		for i := len(locks) - 1; i >= 0; i-- {
			_ = locks[i].Release()
		}
	}()
	for _, root := range roots {
		lock, err := util.AcquireFileLock(filepath.Join(root, ".root-switch.lock"), 5*time.Second)
		if err != nil {
			return err
		}
		locks = append(locks, lock)
	}
	if _, err := os.Lstat(filepath.Join(oldRoot, rootRedirectFile)); err == nil {
		return fmt.Errorf("current root has already been switched; reload configuration")
	} else if !os.IsNotExist(err) {
		return err
	}
	if oldKey == newKey {
		return nil
	}
	entries, err := os.ReadDir(newRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".root-switch.lock" {
			return fmt.Errorf("target root must be empty: %s", newRoot)
		}
	}
	if copyTree {
		if err := localmigrate.MigrateLegacyDir(oldRoot, newRoot); err != nil {
			return err
		}
	}
	next := c.cloneLocked()
	next.SetRootDir(newRoot)
	destination := filepath.Join(newRoot, "config.yaml")
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("target root already contains config.yaml; select it with --xmdevenv-dir instead: %s", newRoot)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Lstat(filepath.Join(newRoot, rootRedirectFile)); err == nil {
		return fmt.Errorf("target root already redirects to another root: %s", newRoot)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := next.saveLocked(); err != nil {
		return err
	}
	rollback := func(cause error) error {
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return errors.Join(cause, fmt.Errorf("remove uncommitted new config: %w", err))
		}
		return cause
	}
	record := rootRedirect{Schema: 1, From: oldRoot, Target: newRoot}
	signed, err := json.Marshal(record)
	if err != nil {
		return rollback(err)
	}
	record.HMAC = util.ComputeHMAC(signed)
	if record.HMAC == "" {
		return rollback(fmt.Errorf("compute root selection integrity"))
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return rollback(err)
	}
	if err := os.MkdirAll(oldRoot, 0755); err != nil {
		return rollback(err)
	}
	if err := writeRootSelection(filepath.Join(oldRoot, rootRedirectFile), data, 0600); err != nil {
		return rollback(fmt.Errorf("publish root selection: %w", err))
	}
	c.Core = next.Core
	c.Integrity = next.Integrity
	c.IntegritySchema = next.IntegritySchema
	InvalidateHomeCache()
	return nil
}
