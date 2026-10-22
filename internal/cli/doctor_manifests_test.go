package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
)

// 20261021 审计 P3-16：损坏 manifest 必须可枚举、可重播种（先备份）。
func TestScanManifestIssues_FindsUnparseableFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte("name: good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 重复键 + 未闭合结构，确保 YAML 解析失败。
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("name: broken\narch: x\narch: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	issues := scanManifestIssues(dir)
	if len(issues) != 1 {
		t.Fatalf("scanManifestIssues = %d issues, want 1: %+v", len(issues), issues)
	}
	if issues[0].File != "broken.yaml" || issues[0].Tool != "broken" {
		t.Errorf("issue = %+v, want broken.yaml/broken", issues[0])
	}
	if !strings.Contains(issues[0].Err.Error(), "already defined") {
		t.Errorf("error should mention duplicate key, got %q", issues[0].Err)
	}
	if squashed := squashError(issues[0].Err.Error()); strings.Contains(squashed, "\n") ||
		!strings.Contains(squashed, "already defined") {
		t.Errorf("squashError should keep the actionable detail on one line, got %q", squashed)
	}
}

func TestScanManifestIssues_MissingDirIsQuiet(t *testing.T) {
	if issues := scanManifestIssues(filepath.Join(t.TempDir(), "missing")); issues != nil {
		t.Errorf("missing dir must return nil, got %+v", issues)
	}
}

func TestReseedManifest_BacksUpOldFileAndWritesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor.yaml")
	if err := os.WriteFile(path, []byte("broken: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	backup, err := reseedManifest(path, []byte("name: cursor\n"))
	if err != nil {
		t.Fatalf("reseedManifest: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup file missing: %v", err)
	}
	if err := manifest.ParseFile(path); err != nil {
		t.Errorf("reseeded file does not parse: %v", err)
	}
	old, err := os.ReadFile(backup)
	if err != nil || string(old) != "broken: [\n" {
		t.Errorf("backup content = %q (err=%v), want the old bytes", old, err)
	}
}

func TestFindJunkVersionDirs_OnlyKnownArtifacts(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   home,
		InstallDir: filepath.Join(home, "install"),
	}}
	toolDir := filepath.Join(cfg.Core.InstallDir, "gradle")
	for _, name := range []string{
		"unsafe-version-blocked",
		"lld_is_a_generic_driver._",
		"gopath",           // 用户数据（Go modules）——绝不能动
		"export_templates", // 用户数据（Godot）——绝不能动
		"9.7.1",            // 正常版本
	} {
		if err := os.MkdirAll(filepath.Join(toolDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	junk := findJunkVersionDirs(cfg)
	if len(junk) != 2 {
		t.Fatalf("findJunkVersionDirs = %d, want 2: %+v", len(junk), junk)
	}
	got := junk[0].Name + "," + junk[1].Name
	if got != "lld_is_a_generic_driver._,unsafe-version-blocked" {
		t.Errorf("junk = %q, want the two xdev artifacts only", got)
	}
}

func TestQuarantineJunkVersionDirs_MovesNotDeletes(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{
		XMDevDir:   home,
		InstallDir: filepath.Join(home, "install"),
	}}
	toolDir := filepath.Join(cfg.Core.InstallDir, "gradle")
	junkPath := filepath.Join(toolDir, "unsafe-version-blocked")
	if err := os.MkdirAll(junkPath, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(junkPath, "keep.txt")
	if err := os.WriteFile(payload, []byte("evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	userData := filepath.Join(toolDir, "gopath")
	if err := os.MkdirAll(userData, 0o755); err != nil {
		t.Fatal(err)
	}

	moved, failed := quarantineJunkVersionDirs(cfg, findJunkVersionDirs(cfg))
	if len(failed) != 0 || len(moved) != 1 {
		t.Fatalf("moved=%v failed=%v, want 1 moved / 0 failed", moved, failed)
	}
	if _, err := os.Stat(junkPath); !os.IsNotExist(err) {
		t.Errorf("junk dir still at original path: %v", err)
	}
	if _, err := os.Stat(userData); err != nil {
		t.Errorf("user-data dir gopath was moved/deleted: %v", err)
	}

	// 证据必须仍在 .trash 里（可恢复），而不是被删掉。
	var found string
	_ = filepath.Walk(filepath.Join(home, ".trash"), func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && info.Name() == "keep.txt" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Error("quarantined payload not found under <home>/.trash — it was deleted, not moved")
	}
}
