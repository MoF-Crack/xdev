package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The release jobs run this against their actual staged updater archives, not
// against a hand-written approximation of the packaging layout.
func TestReleaseArchiveContract(t *testing.T) {
	dir := os.Getenv("XDEV_RELEASE_ARCHIVE_DIR")
	if dir == "" {
		t.Skip("set XDEV_RELEASE_ARCHIVE_DIR to validate staged release archives")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".zip")) {
			continue
		}
		count++
		t.Run(name, func(t *testing.T) {
			out, err := extractUpdateBinary(filepath.Join(dir, name), name, t.TempDir())
			if err != nil {
				t.Fatalf("release archive rejected by updater: %v", err)
			}
			info, err := os.Stat(out)
			if err != nil || info.Size() == 0 {
				t.Fatalf("empty/missing update binary: %v", err)
			}
			version := strings.TrimPrefix(os.Getenv("XDEV_RELEASE_VERSION"), "v")
			if version != "" && !strings.Contains(name, "-v"+version+".") {
				t.Fatalf("asset version mismatch: %s", name)
			}
		})
	}
	if count == 0 {
		t.Fatal("no updater archive was staged; cannot verify release")
	}
}
