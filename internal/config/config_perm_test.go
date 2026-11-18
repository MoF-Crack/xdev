package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSave_FileMode0600 covers L11: config.yaml may carry proxy credentials;
// it must not be world-readable on POSIX.
func TestSave_FileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs, mode bits are not meaningful")
	}
	dir := t.TempDir()
	cfg := defaults(dir)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("config.yaml mode = %o, want 600", got)
	}
}
