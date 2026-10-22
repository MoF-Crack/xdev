// cache_path_safety_test.go — F-25 guards.
//
// Pre-fix the cache destination was built with
// filepath.Join(cacheDir, fmt.Sprintf("%s-%s.%s", tool, ver, format)) and
// NEITHER tool NOR ver was validated. A version string such as
// "../../../../evil" therefore escaped CacheDir entirely, so the download
// landed outside the cache (and `xdev install` would happily write there).
// plugin.Generic.InstallDir and luaBasePlugin.InstallDir both run the
// version through util.SafeName first; the cache path did not.
package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestCacheDestPath_RejectsTraversalVersion asserts a traversal version is
// refused instead of producing a path outside the cache directory.
func TestCacheDestPath_RejectsTraversalVersion(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/evil.zip",
	}

	for _, ver := range []string{
		"../../../../evil",
		`..\..\evil`,
		"..",
		"a/b",
		`a\b`,
	} {
		t.Run("ver="+ver, func(t *testing.T) {
			_, _, dest, _, _ := ins.BuildDownloadOptionsE("safetool", ver, arch)
			// Whatever the implementation does, the resolved path must stay
			// inside CacheDir — or the call must be rejected outright.
			if dest == "" {
				return // explicit rejection is acceptable
			}
			rel, err := filepath.Rel(cfg.Core.CacheDir, dest)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("cache destination %q escapes CacheDir %q (rel=%q err=%v)", dest, cfg.Core.CacheDir, rel, err)
			}
		})
	}
}

// TestCacheDestPath_RejectsTraversalTool asserts the same for the tool name.
func TestCacheDestPath_RejectsTraversalTool(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/evil.zip",
	}

	for _, tool := range []string{"../../evil", `..\..\evil`, "..", "a/b"} {
		t.Run("tool="+tool, func(t *testing.T) {
			_, _, dest, _, _ := ins.BuildDownloadOptionsE(tool, "1.0.0", arch)
			if dest == "" {
				return
			}
			rel, err := filepath.Rel(cfg.Core.CacheDir, dest)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("cache destination %q escapes CacheDir %q (rel=%q err=%v)", dest, cfg.Core.CacheDir, rel, err)
			}
		})
	}
}

// TestInstall_RejectsUnsafeVersionBeforeWritingCache asserts the whole install
// refuses an unsafe version and writes nothing outside CacheDir.
//
// It uses depPlugin because it echoes the requested spec back as the resolved
// version (fakePlugin hard-codes "1.0.0" and would never surface the unsafe
// value).
func TestInstall_RejectsUnsafeVersionBeforeWritingCache(t *testing.T) {
	ins := newDepTestInstaller(t)

	const toolName = "safeprobe"
	unsafeVer := ".."
	// Cache a real zip under the version string we will ask for, so the only
	// possible failure is the safety rejection (not a cache miss).
	cacheFile := filepath.Join(ins.cfg.Core.CacheDir, toolName+"-"+unsafeVer+".zip")
	makeFakeZip(t, cacheFile, toolName)

	p := &depPlugin{
		name: toolName,
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + ".zip", Strip: 1,
		},
	}

	_, err := ins.InstallWithProgress(t.Context(), p, unsafeVer, nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("install accepted an unsafe version string")
	}
	if !strings.Contains(err.Error(), "version") && !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("error = %v, want a version-safety rejection", err)
	}
	// Nothing may have been created outside the cache dir.
	parent := filepath.Dir(ins.cfg.Core.CacheDir)
	entries, rerr := os.ReadDir(parent)
	if rerr == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "-") || e.Name() == toolName {
				t.Errorf("unsafe cache path produced a stray entry outside CacheDir: %s", e.Name())
			}
		}
	}
}
