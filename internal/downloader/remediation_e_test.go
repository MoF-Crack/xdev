package downloader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

func TestRemediationE_PublicCleanupProtectsBusyGeneration(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "all"}[all], func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "archive.zip")
			p := partPathFor(dest, "https://127.0.0.1/a")
			for _, path := range []string{p, p + ".meta"} {
				if err := os.WriteFile(path, []byte("live"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lock, err := util.AcquireFileLock(p+".lock", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()
			if all {
				err = CleanupAllPartialsInDir(dir)
			} else {
				err = CleanupPartialFile(dest)
			}
			if err == nil {
				t.Error("busy generation must return an identifiable busy result")
			}
			for _, path := range []string{p, p + ".meta", p + ".lock"} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("busy generation changed: %s: %v", path, err)
				}
			}
		})
	}
}

func TestRemediationE_CleanupLiteralPathsAndErrors(t *testing.T) {
	for _, name := range []string{"cache[valid]", "cache["} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			p := partPathFor(filepath.Join(dir, "archive.zip"), "https://127.0.0.1/a")
			if err := os.WriteFile(p, []byte("residue"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CleanupAllPartialsInDir(dir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("partial remains after cleanup: %v", err)
			}
		})
	}
	t.Run("remove-failure", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "archive.zip.part")
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "keep"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := CleanupAllPartialsInDir(dir); err == nil {
			t.Error("delete failure was swallowed")
		}
	})
	t.Run("read-failure", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "not-directory")
		if err := os.WriteFile(dir, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := CleanupAllPartialsInDir(dir); err == nil {
			t.Error("ReadDir failure was swallowed")
		}
		if err := CleanupPartialFile(filepath.Join(dir, "archive.zip")); err == nil {
			t.Error("single cleanup ReadDir failure was swallowed")
		}
	})
}

func TestRemediationE_StableLockPathSurvivesCleanup(t *testing.T) {
	dir := t.TempDir()
	p := partPathFor(filepath.Join(dir, "archive.zip"), "https://127.0.0.1/a")
	if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := util.AcquireFileLock(p+".lock", 0)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAllPartialsInDir(dir); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(p + ".lock")
	if err != nil {
		t.Fatalf("stable lock path was removed: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("cleanup replaced the lock inode")
	}
}

func TestRemediationE_FailureHasOneFinalAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	SetAuditLogPath(path)
	defer SetAuditLogPath("")
	err := Download(context.Background(), Options{ToolName: "demo", Version: "1.0", IsThirdParty: true, Quiet: true})
	if err == nil {
		t.Fatal("empty URL should fail")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing failure audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("audit entries=%d, want one", len(lines))
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"tool": "demo", "ver": "1.0", "event": "final", "status": "failed", "errorClass": "invalid_request"} {
		if record[k] != want {
			t.Errorf("%s=%v, want %s", k, record[k], want)
		}
	}
}
