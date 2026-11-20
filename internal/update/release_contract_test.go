package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// The release jobs run this against their actual staged updater payloads, not
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
	version := strings.TrimPrefix(os.Getenv("XDEV_RELEASE_VERSION"), "v")
	count := 0
	windowsZIP := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".zip")) {
			continue
		}
		count++
		isWindowsZIP := runtime.GOOS == "windows" && strings.HasSuffix(name, ".zip")
		windowsZIP = windowsZIP || isWindowsZIP
		t.Run(name, func(t *testing.T) {
			out, err := extractUpdateBinary(filepath.Join(dir, name), name, t.TempDir())
			if err != nil {
				t.Fatalf("release archive rejected by updater: %v", err)
			}
			info, err := os.Stat(out)
			if err != nil || info.Size() == 0 {
				t.Fatalf("empty/missing update binary: %v", err)
			}
			if version != "" && !strings.Contains(name, "-v"+version+".") {
				t.Fatalf("asset version mismatch: %s", name)
			}
			if isWindowsZIP {
				archiveVersion := version
				if archiveVersion == "" {
					archiveVersion = strings.TrimSuffix(strings.TrimPrefix(name, "xdev-windows-"+runtime.GOARCH+"-v"), ".zip")
				}
				if !validUpdateVersion(archiveVersion) || name != assetNameForPlatform(archiveVersion) {
					t.Fatalf("noncanonical Windows update archive: %s", name)
				}
				assertReleaseWindowsExecutable(t, dir, rawExeNameForTarget(archiveVersion, runtime.GOARCH), out)
			}
		})
	}
	if count == 0 {
		t.Fatal("no updater archive was staged; cannot verify release")
	}
	if runtime.GOOS == "windows" && !windowsZIP {
		t.Fatal("no legacy Windows update ZIP was staged; cannot verify release")
	}
}

func assertReleaseWindowsExecutable(t *testing.T, dir, name, extractedExe string) {
	t.Helper()
	raw := filepath.Join(dir, name)
	info, err := os.Lstat(raw)
	if err != nil {
		t.Fatalf("required release executable %s: %v", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("required release executable %s must be a nonempty regular file", name)
	}
	want, err := util.FileSHA256(extractedExe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := util.FileSHA256(raw)
	if err != nil || got != want {
		t.Fatalf("release executable %s differs from the ZIP root binary: want SHA256 %s, got %s (%v)", name, want, got, err)
	}

	// Platform jobs check the pair before the release job merges and hashes
	// all platforms. When validating a final staged release, check the manifest too.
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	digest, err := parseSHA256SUMS(string(sums), name)
	if err != nil || digest != got {
		t.Fatalf("SHA256SUMS does not authenticate %s: want %s, got %s (%v)", name, got, digest, err)
	}
}
