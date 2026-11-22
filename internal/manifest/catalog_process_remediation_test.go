package manifest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Independent process reads avoid carrying a prior in-memory catalog across
// invocations and exercise the persistent cache used by CLI and shim startup.
func TestD21_CrossProcessCache(t *testing.T) {
	const modeKey = "XDEV_TEST_D21_CHILD_MODE"
	if mode := os.Getenv(modeKey); mode != "" {
		remediationEmbedded(t, mode)
		c, err := LoadCatalogCached(os.Getenv("XDEV_TEST_D21_DIR"), os.Getenv("XDEV_TEST_D21_CACHE"))
		if err != nil {
			t.Fatal(err)
		}
		actual := ""
		if tool, ok := c.Tool("cross"); ok {
			actual = tool.DisplayName
		}
		if want := os.Getenv("XDEV_TEST_D21_WANT"); actual != want {
			t.Fatalf("read %q, want %q", actual, want)
		}
		return
	}
	for _, mode := range []string{"none", "nested"} {
		t.Run(mode, func(t *testing.T) {
			dir, cache := t.TempDir(), t.TempDir()
			remediationWrite(t, filepath.Join(dir, "anchor.yaml"), "name: anchor\n")
			run := func(want string) {
				t.Helper()
				cmd := exec.Command(os.Args[0], "-test.run=^TestD21_CrossProcessCache$")
				cmd.Env = append(os.Environ(), modeKey+"="+mode, "XDEV_TEST_D21_DIR="+dir, "XDEV_TEST_D21_CACHE="+cache, "XDEV_TEST_D21_WANT="+want)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v: %s", err, output)
				}
			}
			run("")
			p := filepath.Join(dir, "third-party", "cross.yaml")
			remediationWrite(t, p, "name: cross\ndisplay_name: One\n")
			run("One")
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			remediationWrite(t, p, "name: cross\ndisplay_name: Two\n")
			if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			run("Two")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			run("")
		})
	}
}

func TestD22_FileAtManifestRootIsError(t *testing.T) {
	for _, mode := range []string{"none", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			root := filepath.Join(t.TempDir(), "file")
			remediationWrite(t, root, "not a directory")
			if _, err := LoadCatalogCached(root, t.TempDir()); err == nil {
				t.Fatal("regular file at manifest root produced a catalog")
			}
		})
	}
}

// Exercise a real permission failure when the host enforces POSIX mode bits.
func TestD22_DeniedThirdPartyNeverReturnsWarmCache(t *testing.T) {
	for _, mode := range []string{"none", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			dir, cache := t.TempDir(), t.TempDir()
			tp := filepath.Join(dir, thirdPartySubdir)
			remediationWrite(t, filepath.Join(tp, "tool.yaml"), "name: old\n")
			if _, err := LoadCatalogCached(dir, cache); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(tp, 0); err != nil {
				t.Skipf("mode bits unsupported: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(tp, 0755); err != nil {
					t.Error(err)
				}
			})
			if _, err := os.ReadDir(tp); err == nil {
				t.Skip("host can still read mode-000 directory")
			}
			if _, err := LoadCatalogCached(dir, cache); err == nil {
				t.Fatal("denied source returned a catalog")
			}
		})
	}
}

func TestD21_FingerprintIncludesSourceAndRelativePath(t *testing.T) {
	f := manifestFile{source: "disk", relative: "x.yaml", data: []byte("name: x")}
	other := f
	other.relative = "third-party/x.yaml"
	embedded := f
	embedded.source = "embedded"
	key := func(files ...manifestFile) string {
		fp, ok := catalogFingerprint(nil, files)
		if !ok {
			t.Fatal("unexpected read failure")
		}
		return fp
	}
	if key(f) == key(other) || key(embedded) == key(f) {
		t.Fatal("key lost relative path or source identity")
	}
	if key(f, other) != key(other, f) {
		t.Fatal("fingerprint depends on file discovery order")
	}
}
