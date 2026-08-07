package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveBlock(t *testing.T) {
	in := "a\n" + marker + "\nstuff\n" + endMarker + "\nb\n"
	out := removeBlock(in, marker, endMarker)
	if strings.Contains(out, marker) {
		t.Errorf("expected marker to be removed, got %q", out)
	}
	if !strings.HasPrefix(out, "a\n") || !strings.HasSuffix(out, "b\n") {
		t.Errorf("expected surrounding lines preserved, got %q", out)
	}
}

func TestRemoveBlockMissing(t *testing.T) {
	in := "a\nb\nc\n"
	out := removeBlock(in, marker, endMarker)
	if out != in {
		t.Errorf("expected unchanged, got %q", out)
	}
}

func TestInjectShellRCUnix(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".zshrc")
	info := ShellInfo{Shell: "zsh", RCFile: rc, XMDevTag: marker}
	if err := InjectShellRC(info, "/home/user/.xdev", "/home/user/.xdev/shims"); err != nil {
		t.Fatalf("InjectShellRC: %v", err)
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read rc: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, marker) {
		t.Errorf("expected marker in rc, got %q", text)
	}
	if !strings.Contains(text, "XMDEV_HOME") {
		t.Errorf("expected XMDEV_HOME in rc, got %q", text)
	}
	if !strings.Contains(text, "/home/user/.xdev/shims") {
		t.Errorf("expected shim dir in rc, got %q", text)
	}

	// Injecting again must not duplicate the block.
	if err := InjectShellRC(info, "/home/user/.xdev", "/home/user/.xdev/shims"); err != nil {
		t.Fatalf("InjectShellRC idempotent: %v", err)
	}
	data2, _ := os.ReadFile(rc)
	if got := strings.Count(string(data2), marker); got != 1 {
		t.Errorf("expected exactly 1 marker, got %d", got)
	}

	// RemoveShellRC should strip it.
	if err := RemoveShellRC(info); err != nil {
		t.Fatalf("RemoveShellRC: %v", err)
	}
	data3, _ := os.ReadFile(rc)
	if strings.Contains(string(data3), marker) {
		t.Errorf("expected marker removed, got %q", string(data3))
	}
}

func TestRenderShellBlockFish(t *testing.T) {
	out := renderShellBlock("fish", "/x", "/x/shims")
	if !strings.Contains(out, "set -gx") {
		t.Errorf("expected fish syntax, got %q", out)
	}
}

func TestRenderShellBlockPowerShell(t *testing.T) {
	out := renderShellBlock("powershell", "/x", "/x/shims")
	if !strings.Contains(out, "$env:") {
		t.Errorf("expected powershell syntax, got %q", out)
	}
}

// ---- ToolBinPaths tests ----

func TestToolBinPaths_Go_Nested(t *testing.T) {
	// strip:0 layout: installPath/go/bin/
	installPath := t.TempDir()
	nestedBin := filepath.Join(installPath, "go", "bin")
	if err := os.MkdirAll(nestedBin, 0o755); err != nil {
		t.Fatal(err)
	}
	got := ToolBinPaths("go", installPath)
	want := []string{nestedBin}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(go nested) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(go nested)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Cangjie(t *testing.T) {
	// strip:1 layout: installPath/bin/
	installPath := t.TempDir()
	binDir := filepath.Join(installPath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := ToolBinPaths("cangjie", installPath)
	want := []string{binDir}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(cangjie) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(cangjie)[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToolBinPaths_Cangjie_Nested(t *testing.T) {
	// strip:0 layout: installPath/cangjie/bin/
	installPath := t.TempDir()
	nestedBin := filepath.Join(installPath, "cangjie", "bin")
	if err := os.MkdirAll(nestedBin, 0o755); err != nil {
		t.Fatal(err)
	}
	got := ToolBinPaths("cangjie", installPath)
	want := []string{nestedBin, installPath}
	if len(got) != len(want) {
		t.Fatalf("ToolBinPaths(cangjie nested) = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("ToolBinPaths(cangjie nested)[%d] = %q, want %q", i, p, want[i])
		}
	}
}
