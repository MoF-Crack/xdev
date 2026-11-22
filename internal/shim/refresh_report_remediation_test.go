package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReportD26ManagedDottedAliasesAndUnrelatedEntries(t *testing.T) {
	dir := t.TempDir()
	sources := t.TempDir()
	old := filepath.Join(sources, "old")
	next := filepath.Join(sources, "next")
	for path, data := range map[string]string{old: "OLD", next: "NEW"} {
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := InstallShim(dir, "python3.11", old); err != nil {
		t.Fatal(err)
	}
	names := []string{"python3.11"}
	if runtime.GOOS == "windows" {
		names = append(names, "python3.11.exe")
	}
	// Copy fallback must be recognized by recorded source digest, not only inode.
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "python3.11")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("OLD"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := []string{"readme", "readme.txt", "foreign.exe", "python3.11.old", "python3.11.tmp", "python3.11.exe.old"}
	for _, name := range unrelated {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("KEEP"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(old, filepath.Join(dir, "foreign-link")); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate source rename replacement; historical copies can no longer use SameFile.
	if runtime.GOOS == "windows" {
		if err := os.Remove(old); err != nil {
			t.Fatal(err)
		}
	}
	r, f := RefreshDispatchers(dir, next)
	if r != len(names) || f != 0 {
		t.Errorf("refresh=%d,%d want %d,0", r, f, len(names))
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "NEW" {
			t.Errorf("%s=%q err=%v", name, data, err)
		}
	}
	for _, name := range unrelated {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "KEEP" {
			t.Errorf("unrelated %s changed: %q %v", name, data, err)
		}
	}
	if runtime.GOOS != "windows" {
		target, err := os.Readlink(filepath.Join(dir, "foreign-link"))
		if err != nil || target != old {
			t.Errorf("foreign symlink touched: %s %v", target, err)
		}
	}
	if r, f := RefreshDispatchers(dir, next); r != 0 || f != 0 {
		t.Errorf("second refresh=%d,%d", r, f)
	}
}

func TestReportD26ManagedEntryReplacementIsNotOwned(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(t.TempDir(), "old")
	next := filepath.Join(t.TempDir(), "new")
	for path, data := range map[string]string{old: "OLD", next: "NEW"} {
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := InstallShim(dir, "tool", old); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tool")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(path, []byte("USER"), 0755); err != nil {
			t.Fatal(err)
		}
	} else {
		foreign := filepath.Join(t.TempDir(), "foreign")
		if err := os.WriteFile(foreign, []byte("USER"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, path); err != nil {
			t.Fatal(err)
		}
	}
	_, f := RefreshDispatchers(dir, next)
	if f == 0 {
		t.Error("managed ownership conflict must be reported")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "USER" {
		t.Fatalf("user replacement overwritten: %q %v", data, err)
	}
}
