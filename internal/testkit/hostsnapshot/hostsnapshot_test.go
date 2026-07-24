package hostsnapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
)

func TestDiff_DetectsEnvChange(t *testing.T) {
	before := Snapshot{Env: map[string]string{"GOROOT": "A"}}
	after := Snapshot{Env: map[string]string{"GOROOT": "B"}}
	changes := Diff(before, after)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %#v", len(changes), changes)
	}
	if changes[0].Kind != "env" || changes[0].Key != "GOROOT" {
		t.Fatalf("unexpected change: %#v", changes[0])
	}
	if changes[0].Before != "A" || changes[0].After != "B" {
		t.Fatalf("unexpected values: %#v", changes[0])
	}
}

func TestDiff_NoChange(t *testing.T) {
	s := Snapshot{Env: map[string]string{"GOROOT": "X"}, Files: map[string]FileMeta{}}
	if got := Diff(s, s); len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}

func TestDiff_DetectsFileChange(t *testing.T) {
	before := Snapshot{Files: map[string]FileMeta{"/tmp/x": {Exists: false}}}
	after := Snapshot{Files: map[string]FileMeta{"/tmp/x": {Exists: true, Size: 1}}}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Fatalf("got %#v", changes)
	}
}

func TestDefaultOptions_IncludesKnownKeys(t *testing.T) {
	opts := DefaultOptions()
	found := false
	for _, k := range opts.EnvKeys {
		if k == "GOROOT" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("DefaultOptions missing GOROOT")
	}
	if len(opts.ExcludePathPrefixes) == 0 {
		t.Fatal("expected temp exclude prefixes")
	}
}

func TestCapture_RoundTripEnvKeys(t *testing.T) {
	opts := Options{EnvKeys: []string{"GOROOT", "GOPATH", "PATH"}}
	s, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Env["GOROOT"]; !ok {
		t.Fatal("missing GOROOT key in snapshot")
	}
	// Second capture should match for unchanged host.
	s2, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(s, s2); len(changes) != 0 {
		t.Fatalf("stable host should not change: %#v", changes)
	}
}

func TestCapture_FilePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "marker.txt")
	opts := Options{Paths: []string{p}}
	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if before.Files[filepath.Clean(p)].Exists {
		t.Fatal("expected missing before create")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Fatalf("want file change, got %#v", changes)
	}
}

func TestCapture_ExcludesTempPrefix(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ignored.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Paths:               []string{p},
		ExcludePathPrefixes: []string{dir},
	}
	s, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 0 {
		t.Fatalf("excluded path should not be captured: %#v", s.Files)
	}
}

// TestCapture_DetectsUserEnvWrite proves SC-001 on platforms where
// SetUserEnvVar persists user environment (Windows registry).
func TestCapture_DetectsUserEnvWrite(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("persistent user env write probe is Windows-only")
	}
	const key = "GOROOT"
	orig, _ := env.GetUserEnvVar(key)
	opts := Options{EnvKeys: []string{key}}

	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}

	probe := orig + "__xdev_host_snapshot_probe__"
	if err := env.SetUserEnvVar(key, probe); err != nil {
		t.Fatalf("SetUserEnvVar: %v", err)
	}
	t.Cleanup(func() {
		if orig == "" {
			_ = env.DeleteUserEnvVar(key)
			return
		}
		_ = env.SetUserEnvVar(key, orig)
	})

	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) == 0 {
		t.Fatal("expected env change after SetUserEnvVar")
	}
	found := false
	for _, c := range changes {
		if c.Kind == "env" && c.Key == key && c.After == probe {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing GOROOT probe change: %#v", changes)
	}
}
