package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/pathmode"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

// These I/O boundaries permit deterministic failure injection without touching
// a real user PATH or relying on administrator-dependent permission failures.
var pathModeSave = func(c *config.Config, mode string) error { return c.SetPathMode(mode) }
var pathModeTransform = env.TransformUserPath
var pathModeRename = os.Rename

func pathModePath(current, home, shimDir, mode string, bins []string) string {
	key := func(p string) string {
		p = filepath.Clean(strings.TrimSpace(p))
		if os.PathSeparator == 92 {
			p = strings.ToLower(p)
		}
		return p
	}
	currentRoot := key(filepath.Join(home, "current"))
	xdevRoot := key(filepath.Join(home, "current", "xdev"))
	var kept []string
	for _, entry := range filepath.SplitList(current) {
		k := key(entry)
		if k == key(shimDir) {
			continue
		}
		if pathmode.UsesShims(mode) && strings.HasPrefix(k, currentRoot+string(os.PathSeparator)) && k != xdevRoot && !strings.HasPrefix(k, xdevRoot+string(os.PathSeparator)) {
			continue
		}
		kept = append(kept, entry)
	}
	additions := bins
	if pathmode.UsesShims(mode) {
		additions = []string{shimDir}
	}
	seen := map[string]bool{}
	var result []string
	for _, entry := range append(additions, kept...) {
		if entry == "" {
			continue
		}
		k := key(entry)
		if !seen[k] {
			seen[k] = true
			result = append(result, entry)
		}
	}
	return strings.Join(result, string(os.PathListSeparator))
}

// prepareModeShims only creates missing active aliases. Existing entries remain
// untouched, including dormant shims when switching to direct. No full directory
// backup or deletion is necessary. Hold the existing dispatcher-management lock
// until commit/rollback so another xdev dispatcher writer cannot race compensation.
func (a *App) prepareModeShims(cfg *config.Config, aliases []string) (rollback func() error, finish func(), err error) {
	noop := func() error { return nil }
	done := func() {}
	if len(aliases) == 0 {
		return noop, done, nil
	}
	lock, err := util.AcquireToolLock(cfg.Core.ShimDir, "dispatcher-management", 30*time.Second)
	if err != nil {
		return nil, nil, err
	}
	release := func() { _ = lock.Release() }
	stage, err := os.MkdirTemp(cfg.Core.ShimDir, ".path-mode-")
	if err != nil {
		release()
		return nil, nil, err
	}
	keepStage := false
	finish = func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
		release()
	}
	type change struct {
		target, backup string
		published      bool
	}
	var changes []*change
	rollback = func() error {
		var errs []error
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if c.published {
				if e := os.Remove(c.target); e != nil && !os.IsNotExist(e) {
					errs = append(errs, e)
					continue
				}
			}
			if c.backup != "" {
				if e := os.Rename(c.backup, c.target); e != nil {
					errs = append(errs, e)
				}
			}
		}
		if len(errs) > 0 {
			keepStage = true
			return fmt.Errorf("shim rollback incomplete; recovery files retained at %s: %w", stage, errors.Join(errs...))
		}
		return nil
	}
	fail := func(cause error) (func() error, func(), error) {
		re := rollback()
		finish()
		return nil, nil, errors.Join(cause, re)
	}
	generated := filepath.Join(stage, "generated")
	missing := map[string]bool{}
	aliasKey := func(name string) string {
		if os.PathSeparator == 92 {
			return strings.ToLower(name)
		}
		return name
	}
	for _, alias := range aliases {
		if e := util.ValidateShimName(alias); e != nil {
			return fail(e)
		}
		names := []string{alias}
		if os.PathSeparator == 92 && !strings.HasSuffix(strings.ToLower(alias), ".exe") {
			names = append(names, alias+".exe")
		}
		needs := false
		for _, name := range names {
			_, e := os.Lstat(filepath.Join(cfg.Core.ShimDir, name))
			if os.IsNotExist(e) {
				missing[aliasKey(name)] = true
				needs = true
			} else if e != nil {
				return fail(e)
			}
		}
		if needs {
			if e := shim.InstallShim(generated, alias, a.newInstaller(cfg).ResolveShimExe()); e != nil {
				return fail(e)
			}
		}
	}
	if len(missing) == 0 {
		return rollback, finish, nil
	}
	var files []string
	err = filepath.WalkDir(generated, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(generated, path)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".locks" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Dir(rel) == "." {
			if !missing[aliasKey(rel)] {
				return nil
			}
		} else {
			// InstallShim emits twin ownership records on Windows. Publish only
			// the record for an entry we actually create, never claim an existing
			// user-owned twin merely because its other spelling was absent.
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var record struct {
				Name string `json:"name"`
			}
			if e := json.Unmarshal(data, &record); e != nil {
				return e
			}
			if !missing[aliasKey(record.Name)] {
				return nil
			}
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return fail(err)
	}
	sort.Strings(files)
	for index, rel := range files {
		target := filepath.Join(cfg.Core.ShimDir, rel)
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return fail(e)
		}
		c := &change{target: target}
		changes = append(changes, c)
		if info, e := os.Lstat(target); e == nil {
			if !info.Mode().IsRegular() {
				return fail(fmt.Errorf("refuse replacing non-file shim metadata %s", target))
			}
			c.backup = filepath.Join(stage, fmt.Sprintf("backup-%d", index))
			if e := pathModeRename(target, c.backup); e != nil {
				c.backup = ""
				return fail(e)
			}
		} else if !os.IsNotExist(e) {
			return fail(e)
		}
		if e := pathModeRename(filepath.Join(generated, rel), target); e != nil {
			return fail(e)
		}
		c.published = true
	}
	return rollback, finish, nil
}
