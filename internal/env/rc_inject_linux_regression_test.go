package env

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These are the Linux/WSL regression guards for RC injection. They exist
// because the Windows-only test matrix could not see any of them: Windows
// writes its PATH to the registry, so the POSIX `text += block` shape and the
// missing-HOME fallback never bit.

// A hand-edited RC frequently has no trailing newline. Injecting must not glue
// the marker onto the user's last line (that produced `alias ll=ls# >>> xdev
// init >>>`, which `bash -n` rejects).
func TestInjectShellRC_NoTrailingNewline_NotGlued(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(rc, []byte("alias ll=ls"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := ShellInfo{Shell: "bash", RCFile: rc, XMDevTag: marker}
	if err := InjectShellRC(info, "/home/u/.xdev", "/home/u/.xdev/shims"); err != nil {
		t.Fatalf("InjectShellRC: %v", err)
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "alias ll=ls"+marker) {
		t.Fatalf("marker glued onto the previous line: %q", text)
	}
	if !strings.Contains(text, "alias ll=ls\n"+marker) {
		t.Fatalf("expected a separating newline before the marker, got %q", text)
	}
}

// Repeated injects and a final remove must be byte-exact: the old code grew the
// RC file by one newline per `activate` and left a stray newline after
// `deactivate`.
func TestInjectShellRC_ByteIdempotentAndRestores(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	original := []byte("# user content\n")
	if err := os.WriteFile(rc, original, 0o644); err != nil {
		t.Fatal(err)
	}
	info := ShellInfo{Shell: "bash", RCFile: rc, XMDevTag: marker}

	if err := InjectShellRC(info, "/h/.xdev", "/h/.xdev/shims"); err != nil {
		t.Fatalf("inject 1: %v", err)
	}
	first, _ := os.ReadFile(rc)
	if err := InjectShellRC(info, "/h/.xdev", "/h/.xdev/shims"); err != nil {
		t.Fatalf("inject 2: %v", err)
	}
	second, _ := os.ReadFile(rc)
	if string(first) != string(second) {
		t.Fatalf("second inject changed the file:\n--- after 1 ---\n%q\n--- after 2 ---\n%q", first, second)
	}
	for i := 0; i < 3; i++ {
		if err := InjectShellRC(info, "/h/.xdev", "/h/.xdev/shims"); err != nil {
			t.Fatalf("inject %d: %v", i+3, err)
		}
	}
	third, _ := os.ReadFile(rc)
	if string(first) != string(third) {
		t.Fatalf("repeated injects are not idempotent:\n%q\nvs\n%q", first, third)
	}
	if got := strings.Count(string(third), marker); got != 1 {
		t.Fatalf("marker count = %d, want 1", got)
	}

	if err := RemoveShellRC(info); err != nil {
		t.Fatalf("RemoveShellRC: %v", err)
	}
	restored, _ := os.ReadFile(rc)
	if string(restored) != string(original) {
		t.Fatalf("remove did not restore the original bytes: got %q want %q", restored, original)
	}
}

// Two pre-existing blocks (hand-copied, or left by an older version) must
// converge to exactly one on inject and zero on remove. The old removeBlock
// only deleted the first occurrence.
func TestRemoveBlock_MultipleBlocksConverge(t *testing.T) {
	block := marker + "\nold\n" + endMarker + "\n"
	in := "# head\n" + block + "# middle\n" + block + "# tail\n"
	out := removeBlock(in, marker, endMarker)
	if strings.Contains(out, marker) {
		t.Fatalf("blocks left after removeBlock: %q", out)
	}
	if out != "# head\n# middle\n# tail\n" {
		t.Fatalf("surrounding lines not preserved: %q", out)
	}
}

// Without HOME we cannot build an absolute RC path. Fail closed instead of
// writing a relative ".bashrc" into the caller's working directory.
func TestDetectUnixShell_MissingHomeFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only shape")
	}
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("HOME", "")
	info := detectUnixShell()
	if info.RCFile != "" {
		t.Fatalf("expected empty RCFile when HOME is unset, got %q", info.RCFile)
	}
}
