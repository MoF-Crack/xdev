package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TC-S1-1 默认值：self_update=true / mode=prompt / stale=true
func TestSelfUpdateDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	if !cfg.Core.SelfUpdate {
		t.Error("SelfUpdate default = false, want true")
	}
	if cfg.Core.SelfUpdateMode != "prompt" {
		t.Errorf("SelfUpdateMode default = %q, want prompt", cfg.Core.SelfUpdateMode)
	}
	if !cfg.Core.SelfUpdateStale {
		t.Error("SelfUpdateStale default = false, want true")
	}
}

// TC-S1-2 roundtrip：mode/stale 持久化回读一致
func TestSelfUpdateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Core.SelfUpdateMode = "auto"
	cfg.Core.SelfUpdateStale = false
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Core.SelfUpdateMode != "auto" {
		t.Errorf("SelfUpdateMode after roundtrip = %q, want auto", loaded.Core.SelfUpdateMode)
	}
	if loaded.Core.SelfUpdateStale {
		t.Error("SelfUpdateStale after roundtrip = true, want false")
	}
}

// TC-S1-4 旧 config.yaml 缺新字段 → Load 回填默认
func TestSelfUpdateBackfillFromLegacyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XMDEV_FORCE_HOME", "1")

	if err := os.MkdirAll(filepath.Join(tmp), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "core:\n  self_update: true\n  log_level: info\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Core.SelfUpdateMode != "prompt" {
		t.Errorf("SelfUpdateMode backfill = %q, want prompt", cfg.Core.SelfUpdateMode)
	}
	if !cfg.Core.SelfUpdateStale {
		t.Error("SelfUpdateStale backfill = false, want true")
	}
	if !cfg.Core.SelfUpdate {
		t.Error("SelfUpdate lost from legacy file")
	}
}