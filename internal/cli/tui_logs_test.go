package cli

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTuiLogPath(t *testing.T) {
	got := tuiLogPath(filepath.Join("/home", "u", ".xdev"))
	want := filepath.Join("/home", "u", ".xdev", "logs", "tui.log")
	if got != want {
		t.Fatalf("tuiLogPath = %q, want %q", got, want)
	}
}

func TestRedirectLogsTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "tui.log")

	prev := log.Writer()
	defer func() {
		log.SetOutput(prev)
		if tuiLogFile != nil {
			_ = tuiLogFile.Close()
			tuiLogFile = nil
		}
	}()

	if err := redirectLogsTo(path); err != nil {
		t.Fatalf("redirectLogsTo: %v", err)
	}
	log.Print("probe-tui-log-123")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "probe-tui-log-123") {
		t.Fatalf("log file missing probe line:\n%s", data)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("log mode = %o, want 600", perm)
		}
	}
}

func TestShouldPromptSelfUpdate(t *testing.T) {
	cases := []struct {
		allowPrompt, stdinTTY, noPathFix bool
		want                             bool
	}{
		{true, true, false, true},
		{false, true, false, false}, // TUI/GUI must never block on stdin
		{true, false, false, false}, // CI / pipe
		{true, true, true, false},   // --no-path-fix
		{false, false, true, false},
	}
	for _, c := range cases {
		if got := shouldPromptSelfUpdate(c.allowPrompt, c.stdinTTY, c.noPathFix); got != c.want {
			t.Errorf("shouldPromptSelfUpdate(%v,%v,%v) = %v, want %v",
				c.allowPrompt, c.stdinTTY, c.noPathFix, got, c.want)
		}
	}
}
