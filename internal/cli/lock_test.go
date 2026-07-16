package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/lockfile"
	"github.com/xmdemt/xdev/internal/project"
)

// newLockCheckTestDir creates a temp project dir with .xdev.toml containing
// the given tools and returns the directory path.
func newLockCheckTestDir(t *testing.T, tools map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	content := "[tools]\n"
	for k, v := range tools {
		content += k + " = \"" + v + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(tmp, ".xdev.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .xdev.toml: %v", err)
	}
	return tmp
}

func TestLockCheck_InSync(t *testing.T) {
	dir := newLockCheckTestDir(t, map[string]string{"go": "1.22"})
	lf := lockfile.New()
	lf.AddTool("go", lockfile.LockedTool{Version: "1.22", Resolved: "1.22.5"})
	if err := lockfile.Save(dir, lf); err != nil {
		t.Fatalf("save lockfile: %v", err)
	}

	_, projCfg := project.FindConfig(dir)
	if projCfg == nil {
		t.Fatal("project config not found")
	}

	err := lockCheck(dir, projCfg, true)
	if err != nil {
		t.Errorf("lockCheck returned error for in-sync lockfile: %v", err)
	}
}

func TestLockCheck_SpecDrift(t *testing.T) {
	dir := newLockCheckTestDir(t, map[string]string{"go": "1.22"})
	lf := lockfile.New()
	// Lockfile has different version than toml
	lf.AddTool("go", lockfile.LockedTool{Version: "1.21", Resolved: "1.21.0"})
	if err := lockfile.Save(dir, lf); err != nil {
		t.Fatalf("save lockfile: %v", err)
	}

	_, projCfg := project.FindConfig(dir)
	if projCfg == nil {
		t.Fatal("project config not found")
	}

	err := lockCheck(dir, projCfg, false)
	if err == nil {
		t.Error("expected error for spec drift, got nil")
	}
}

func TestLockCheck_StaleEntry(t *testing.T) {
	dir := newLockCheckTestDir(t, map[string]string{"go": "1.22"})
	lf := lockfile.New()
	lf.AddTool("go", lockfile.LockedTool{Version: "1.22", Resolved: "1.22.5"})
	// Stale entry: in lockfile but not in toml
	lf.AddTool("stale-tool", lockfile.LockedTool{Version: "1.0", Resolved: "1.0.0"})
	if err := lockfile.Save(dir, lf); err != nil {
		t.Fatalf("save lockfile: %v", err)
	}

	_, projCfg := project.FindConfig(dir)
	if projCfg == nil {
		t.Fatal("project config not found")
	}

	err := lockCheck(dir, projCfg, false)
	if err == nil {
		t.Error("expected error for stale entry, got nil")
	}
}

func TestLockCheck_MissingLockfile(t *testing.T) {
	dir := newLockCheckTestDir(t, map[string]string{"go": "1.22"})
	_, projCfg := project.FindConfig(dir)
	if projCfg == nil {
		t.Fatal("project config not found")
	}

	err := lockCheck(dir, projCfg, false)
	if err == nil {
		t.Error("expected error for missing lockfile, got nil")
	}
}

func TestLockGenerate_SkipsUnsupported(t *testing.T) {
	dir := newLockCheckTestDir(t, map[string]string{"nosh": "1"})
	cfg := &config.Config{}
	cfg.Core.XMDevDir = dir
	cfg.Core.InstallDir = filepath.Join(dir, "install")
	cfg.Core.CacheDir = filepath.Join(dir, "cache")

	// Use empty args to lock all tools in toml
	err := lockGenerate(context.Background(), cfg, dir, &project.ProjectConfig{Tools: map[string]string{"nosh": "1"}}, nil, true)
	if err != nil {
		// unsupported tool is skipped, not errored
		t.Errorf("lockGenerate should skip unsupported tools without error: %v", err)
	}
	// Verify lockfile was created but empty (no supported tools)
	lf, loadErr := lockfile.Load(dir)
	if loadErr != nil {
		t.Fatalf("lockfile.Load: %v", loadErr)
	}
	if len(lf.Tools) != 0 {
		t.Errorf("expected 0 tools in lockfile (unsupported skipped), got %d", len(lf.Tools))
	}
}
