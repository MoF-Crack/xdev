//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// CI can put t.TempDir below C:\Users\RUNNER~1 while Windows process APIs
// report C:\Users\runneradmin. Production preparation resolves the path first;
// assertions and direct low-level handle fixtures must use the same identity.
func TestRestartCanonicalPathPreservesWindowsShortPathIdentity(t *testing.T) {
	dir, err := canonicalRestartPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "an executable with a long name.exe")
	if err := os.WriteFile(target, []byte("owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ptr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	size, err := windows.GetShortPathName(ptr, &buffer[0], uint32(len(buffer)))
	if err != nil || size == 0 || size >= uint32(len(buffer)) {
		t.Fatalf("resolve Windows short path: size=%d error=%v", size, err)
	}
	alias := windows.UTF16ToString(buffer[:size])
	resolved, err := canonicalRestartPath(alias)
	if err != nil || !sameRestartPath(resolved, target) {
		t.Fatalf("short alias lost file identity: alias=%q resolved=%q target=%q error=%v", alias, resolved, target, err)
	}
	longInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	shortInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(longInfo, shortInfo) {
		t.Fatalf("short and canonical names do not refer to the same file: %v", err)
	}
}
