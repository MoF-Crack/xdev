package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
)

// Linux/WSL regression guards for the `xdev activate` cd-hook block. Windows
// writes its PATH through the registry and its tests never exercised a
// hand-edited, newline-less RC file, so none of this was observable there.

func TestRemoveBlock_MultipleHookBlocksConverge(t *testing.T) {
	block := hookMarker + "\nold\n" + hookEndMarker + "\n"
	in := "# head\n" + block + "# middle\n" + block + "# tail\n"
	out := removeBlock(in, hookMarker, hookEndMarker)
	if strings.Contains(out, hookMarker) {
		t.Fatalf("hook blocks left after removeBlock: %q", out)
	}
	if out != "# head\n# middle\n# tail\n" {
		t.Fatalf("surrounding lines not preserved: %q", out)
	}
}

// A hand-edited RC without a trailing newline must not get the marker glued
// onto its last line (`alias ll=ls# >>> xdev hook >>>`, which bash rejects).
func TestInjectHookRC_NoTrailingNewline_NotGlued(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(rc, []byte("alias ll=ls"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := env.ShellInfo{Shell: "bash", RCFile: rc}
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: "/h/.xdev"}}

	if err := injectHookRC(info, cfg); err != nil {
		t.Fatalf("injectHookRC: %v", err)
	}
	got, _ := os.ReadFile(rc)
	text := string(got)
	if strings.Contains(text, "alias ll=ls"+hookMarker) {
		t.Fatalf("hook marker glued onto previous line: %q", text)
	}
	if !strings.Contains(text, "alias ll=ls\n"+hookMarker) {
		t.Fatalf("missing separating newline before hook marker: %q", text)
	}
}

// Repeated activates must be byte-idempotent and a final deactivate must
// restore the file exactly (the old code grew the RC by one newline per run and
// left a stray newline behind).
func TestInjectHookRC_ByteIdempotentAndRestores(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	original := "# user content\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	info := env.ShellInfo{Shell: "bash", RCFile: rc}
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: "/h/.xdev"}}

	if err := injectHookRC(info, cfg); err != nil {
		t.Fatalf("inject 1: %v", err)
	}
	first, _ := os.ReadFile(rc)
	if got := strings.Count(string(first), hookMarker); got != 1 {
		t.Fatalf("hook marker count = %d, want 1", got)
	}
	for i := 0; i < 3; i++ {
		if err := injectHookRC(info, cfg); err != nil {
			t.Fatalf("inject %d: %v", i+2, err)
		}
	}
	again, _ := os.ReadFile(rc)
	if string(first) != string(again) {
		t.Fatalf("repeated activate is not idempotent:\n%q\nvs\n%q", first, again)
	}

	if err := removeHookRC(info); err != nil {
		t.Fatalf("removeHookRC: %v", err)
	}
	restored, _ := os.ReadFile(rc)
	if string(restored) != original {
		t.Fatalf("remove did not restore original bytes: got %q want %q", restored, original)
	}
}
