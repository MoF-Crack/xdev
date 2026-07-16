package lockfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		tools map[string]LockedTool
	}{
		{
			name:  "empty tools",
			tools: map[string]LockedTool{},
		},
		{
			name: "single tool",
			tools: map[string]LockedTool{
				"golangci-lint": {
					Version:     "1.59.0",
					Resolved:    "v1.59.0",
					SHA256:      "abc123def456",
					URL:         "https://example.com/golangci-lint-1.59.0.tar.gz",
					InstallPath: ".xdev/tools/golangci-lint",
				},
			},
		},
		{
			name: "multiple tools",
			tools: map[string]LockedTool{
				"golangci-lint": {
					Version:     "1.59.0",
					Resolved:    "v1.59.0",
					SHA256:      "abc123def456",
					URL:         "https://example.com/golangci-lint-1.59.0.tar.gz",
					InstallPath: ".xdev/tools/golangci-lint",
				},
				"staticcheck": {
					Version:     "0.4.7",
					Resolved:    "v0.4.7",
					SHA256:      "789xyz000111",
					URL:         "https://example.com/staticcheck-0.4.7.tar.gz",
					InstallPath: ".xdev/tools/staticcheck",
				},
				"gosec": {
					Version:     "2.20.0",
					Resolved:    "v2.20.0",
					SHA256:      "deadbeefcafe",
					URL:         "https://example.com/gosec-2.20.0.tar.gz",
					InstallPath: ".xdev/tools/gosec",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			lf := &Lockfile{
				Version:   1,
				CreatedAt: ts,
				Tools:     tt.tools,
			}

			if err := Save(dir, lf); err != nil {
				t.Fatalf("Save failed: %v", err)
			}

			got, err := Load(dir)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}

			if got.Version != lf.Version {
				t.Errorf("Version = %d, want %d", got.Version, lf.Version)
			}

			// TOML serializes times with sub-second precision; compare truncated
			if !got.CreatedAt.Truncate(time.Second).Equal(lf.CreatedAt.Truncate(time.Second)) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, lf.CreatedAt)
			}

			if len(got.Tools) != len(lf.Tools) {
				t.Fatalf("len(Tools) = %d, want %d", len(got.Tools), len(lf.Tools))
			}

			for name, want := range lf.Tools {
				gotTool, ok := got.Tools[name]
				if !ok {
					t.Errorf("tool %q missing from loaded lockfile", name)
					continue
				}
				if gotTool != want {
					t.Errorf("tool %q:\n  got  %+v\n  want %+v", name, gotTool, want)
				}
			}
		})
	}
}

func TestAddRemoveTool(t *testing.T) {
	tests := []struct {
		name         string
		add          map[string]LockedTool
		remove       []string
		wantNames    []string
		wantTools    map[string]LockedTool
		overwrite    string
		overwriteTool LockedTool
	}{
		{
			name: "add one remove it",
			add: map[string]LockedTool{
				"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"},
			},
			remove:    []string{"tool-a"},
			wantNames: []string{},
		},
		{
			name: "add three remove middle",
			add: map[string]LockedTool{
				"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"},
				"tool-b": {Version: "2.0", Resolved: "v2.0", SHA256: "bbb", URL: "http://b", InstallPath: "/b"},
				"tool-c": {Version: "3.0", Resolved: "v3.0", SHA256: "ccc", URL: "http://c", InstallPath: "/c"},
			},
			remove:    []string{"tool-b"},
			wantNames: []string{"tool-a", "tool-c"},
			wantTools: map[string]LockedTool{
				"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"},
				"tool-c": {Version: "3.0", Resolved: "v3.0", SHA256: "ccc", URL: "http://c", InstallPath: "/c"},
			},
		},
		{
			name: "remove nonexistent is no-op",
			add: map[string]LockedTool{
				"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"},
			},
			remove:    []string{"tool-z"},
			wantNames: []string{"tool-a"},
			wantTools: map[string]LockedTool{
				"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"},
			},
		},
		{
			name:   "add overwrites existing",
			add:    map[string]LockedTool{"tool-a": {Version: "1.0", Resolved: "v1.0", SHA256: "aaa", URL: "http://a", InstallPath: "/a"}},
			remove: []string{},
			wantNames: []string{"tool-a"},
			wantTools: map[string]LockedTool{
				"tool-a": {Version: "2.0", Resolved: "v2.0", SHA256: "bbb", URL: "http://a2", InstallPath: "/a2"},
			},
			overwrite: "tool-a",
			overwriteTool: LockedTool{Version: "2.0", Resolved: "v2.0", SHA256: "bbb", URL: "http://a2", InstallPath: "/a2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := New()

			for name, tool := range tt.add {
				lf.AddTool(name, tool)
			}
			if tt.overwrite != "" {
				lf.AddTool(tt.overwrite, tt.overwriteTool)
			}
			for _, name := range tt.remove {
				lf.RemoveTool(name)
			}

			if len(lf.Tools) != len(tt.wantNames) {
				t.Errorf("len(Tools) = %d, want %d", len(lf.Tools), len(tt.wantNames))
			}

			for name, want := range tt.wantTools {
				got, ok := lf.Tools[name]
				if !ok {
					t.Errorf("tool %q missing", name)
					continue
				}
				if got != want {
					t.Errorf("tool %q:\n  got  %+v\n  want %+v", name, got, want)
				}
			}
		})
	}
}

func TestFindConfigDir(t *testing.T) {
	t.Run("finds .xdev.toml in parent", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".xdev.toml"), []byte(""), 0o644); err != nil {
			t.Fatalf("create config: %v", err)
		}

		sub := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got := FindConfigDir(sub)
		if got != root {
			t.Errorf("FindConfigDir(%q) = %q, want %q", sub, got, root)
		}
	})

	t.Run("finds .xdev.lock in parent", func(t *testing.T) {
		root := t.TempDir()
		lf := New()
		if err := Save(root, lf); err != nil {
			t.Fatalf("Save: %v", err)
		}

		sub := filepath.Join(root, "x", "y")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got := FindConfigDir(sub)
		if got != root {
			t.Errorf("FindConfigDir(%q) = %q, want %q", sub, got, root)
		}
	})

	t.Run("returns empty when no config", func(t *testing.T) {
		dir := t.TempDir()
		got := FindConfigDir(dir)
		if got != "" {
			t.Errorf("FindConfigDir(%q) = %q, want empty string", dir, got)
		}
	})

	t.Run("finds config in same directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".xdev.toml"), []byte(""), 0o644); err != nil {
			t.Fatalf("create config: %v", err)
		}

		got := FindConfigDir(dir)
		if got != dir {
			t.Errorf("FindConfigDir(%q) = %q, want %q", dir, got, dir)
		}
	})
}
