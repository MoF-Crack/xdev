package env

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestHookOutput_SkipsDirsAlreadyInPATH: `xdev hook` runs on every cd, so a
// directory already on PATH must not be prepended again (L6: PATH growth).
func TestHookOutput_SkipsDirsAlreadyInPATH(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin")
	out := HookOutput("bash", []string{dir, other}, nil)
	if strings.Contains(out, dir) {
		t.Fatalf("already-present dir was re-added:\n%s", out)
	}
	if !strings.Contains(out, other) {
		t.Fatalf("missing dir was not added:\n%s", out)
	}
}

func TestHookOutput_AllDirsPresent_NoPathLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	out := HookOutput("bash", []string{dir}, nil)
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestHookOutput_FishEnvVarUsesFishQuoting(t *testing.T) {
	out := HookOutput("fish", nil, map[string]string{"XDEV_TEST": "a'b"})
	if !strings.Contains(out, `set -gx XDEV_TEST 'a\'b'`) {
		t.Fatalf("fish env var quoting wrong:\n%s", out)
	}
	if strings.Contains(out, `'a'\''b'`) {
		t.Fatalf("POSIX quoting leaked into fish output:\n%s", out)
	}
}

func TestHookOutput_DuplicateInputDirsCollapse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path separator shape")
	}
	dir := t.TempDir()
	t.Setenv("PATH", "/usr/bin")
	out := HookOutput("bash", []string{dir, dir}, nil)
	if got := strings.Count(out, dir); got != 1 {
		t.Fatalf("dir appears %d times in output:\n%s", got, out)
	}
}
