package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".xdev.toml")

	cfg := &ProjectConfig{
		Tools: map[string]string{
			"node":   "20",
			"python": "^3.12",
			"go":     "1.22",
		},
		Env: map[string]string{
			"DATABASE_URL": "postgres://localhost/app",
			"DEBUG":        "true",
		},
		Tasks: map[string]Task{
			"build": {
				Run:     "go build ./...",
				Desc:    "Build the project",
				Dir:     "cmd/app",
				Sources: []string{"**/*.go"},
			},
			"test": {
				Run:  "go test ./...",
				Desc: "Run tests",
			},
		},
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify tools
	if len(loaded.Tools) != len(cfg.Tools) {
		t.Errorf("Tools count: got %d, want %d", len(loaded.Tools), len(cfg.Tools))
	}
	for k, v := range cfg.Tools {
		if got := loaded.Tools[k]; got != v {
			t.Errorf("Tools[%q]: got %q, want %q", k, got, v)
		}
	}

	// Verify env
	if len(loaded.Env) != len(cfg.Env) {
		t.Errorf("Env count: got %d, want %d", len(loaded.Env), len(cfg.Env))
	}
	for k, v := range cfg.Env {
		if got := loaded.Env[k]; got != v {
			t.Errorf("Env[%q]: got %q, want %q", k, got, v)
		}
	}

	// Verify tasks
	if len(loaded.Tasks) != len(cfg.Tasks) {
		t.Errorf("Tasks count: got %d, want %d", len(loaded.Tasks), len(cfg.Tasks))
	}
	for name, want := range cfg.Tasks {
		got, ok := loaded.Tasks[name]
		if !ok {
			t.Errorf("Tasks[%q]: missing", name)
			continue
		}
		if got.Run != want.Run {
			t.Errorf("Tasks[%q].Run: got %q, want %q", name, got.Run, want.Run)
		}
		if got.Desc != want.Desc {
			t.Errorf("Tasks[%q].Desc: got %q, want %q", name, got.Desc, want.Desc)
		}
		if got.Dir != want.Dir {
			t.Errorf("Tasks[%q].Dir: got %q, want %q", name, got.Dir, want.Dir)
		}
		if len(got.Sources) != len(want.Sources) {
			t.Errorf("Tasks[%q].Sources count: got %d, want %d", name, len(got.Sources), len(want.Sources))
		}
	}
}

func TestFindConfig(t *testing.T) {
	t.Run("finds config in parent directory", func(t *testing.T) {
		root := t.TempDir()
		cfgPath := filepath.Join(root, ".xdev.toml")
		cfg := &ProjectConfig{
			Tools: map[string]string{"node": "20"},
			Env:   map[string]string{},
			Tasks: map[string]Task{},
		}
		if err := SaveConfig(cfgPath, cfg); err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}

		// Create nested subdirectory
		sub := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}

		foundDir, foundCfg := FindConfig(sub)
		if foundDir != root {
			t.Errorf("FindConfig dir: got %q, want %q", foundDir, root)
		}
		if foundCfg == nil {
			t.Fatal("FindConfig returned nil config")
		}
		if foundCfg.Tools["node"] != "20" {
			t.Errorf("FindConfig config: got node=%q, want %q", foundCfg.Tools["node"], "20")
		}
	})

	t.Run("returns empty when not found", func(t *testing.T) {
		// Use a temp dir with no .xdev.toml anywhere
		dir := t.TempDir()
		foundDir, foundCfg := FindConfig(dir)
		if foundDir != "" {
			t.Errorf("FindConfig dir: got %q, want empty", foundDir)
		}
		if foundCfg != nil {
			t.Errorf("FindConfig config: got %v, want nil", foundCfg)
		}
	})
}

func TestSetTool(t *testing.T) {
	t.Run("creates new config file", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetTool(dir, "node", "20"); err != nil {
			t.Fatalf("SetTool failed: %v", err)
		}

		cfgPath := filepath.Join(dir, ".xdev.toml")
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			t.Fatal("config file was not created")
		}

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}
		if cfg.Tools["node"] != "20" {
			t.Errorf("tool version: got %q, want %q", cfg.Tools["node"], "20")
		}
	})

	t.Run("updates existing tool version", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetTool(dir, "node", "20"); err != nil {
			t.Fatalf("first SetTool failed: %v", err)
		}
		if err := SetTool(dir, "node", "22"); err != nil {
			t.Fatalf("second SetTool failed: %v", err)
		}

		cfgPath := filepath.Join(dir, ".xdev.toml")
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}
		if cfg.Tools["node"] != "22" {
			t.Errorf("tool version: got %q, want %q", cfg.Tools["node"], "22")
		}
	})
}

func TestUnsetTool(t *testing.T) {
	t.Run("removes existing tool", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetTool(dir, "node", "20"); err != nil {
			t.Fatalf("SetTool failed: %v", err)
		}
		if err := SetTool(dir, "python", "3.12"); err != nil {
			t.Fatalf("SetTool failed: %v", err)
		}

		if err := UnsetTool(dir, "node"); err != nil {
			t.Fatalf("UnsetTool failed: %v", err)
		}

		cfgPath := filepath.Join(dir, ".xdev.toml")
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}
		if _, ok := cfg.Tools["node"]; ok {
			t.Error("node should have been removed")
		}
		if cfg.Tools["python"] != "3.12" {
			t.Errorf("python version: got %q, want %q", cfg.Tools["python"], "3.12")
		}
	})

	t.Run("returns error for nonexistent tool", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetTool(dir, "node", "20"); err != nil {
			t.Fatalf("SetTool failed: %v", err)
		}

		err := UnsetTool(dir, "python")
		if err == nil {
			t.Fatal("expected error for nonexistent tool, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error message: got %q, want it to contain 'not found'", err.Error())
		}
	})
}

func TestFormatTools(t *testing.T) {
	t.Run("with tools", func(t *testing.T) {
		cfg := &ProjectConfig{
			Tools: map[string]string{
				"node":   "20",
				"python": "3.12",
			},
		}
		out := FormatTools(cfg)
		if !strings.Contains(out, "node") || !strings.Contains(out, "20") {
			t.Errorf("output missing tool info: %q", out)
		}
		if !strings.Contains(out, "python") || !strings.Contains(out, "3.12") {
			t.Errorf("output missing tool info: %q", out)
		}
	})

	t.Run("with empty tools", func(t *testing.T) {
		cfg := &ProjectConfig{
			Tools: map[string]string{},
		}
		out := FormatTools(cfg)
		if out != "(no tools configured)" {
			t.Errorf("got %q, want %q", out, "(no tools configured)")
		}
	})
}
