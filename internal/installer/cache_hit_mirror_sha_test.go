// cache_hit_mirror_sha_test.go — F-24 guards.
//
// Pre-fix the cache-hit branch of downloadWithProgress compared the cached
// file ONLY against a.SHA256. When an archive is fetched from a mirror that
// serves a byte-different (but manifest-declared) rebuild under
// mirror_sha256, the cached file matched the MIRROR hash — yet the cache-hit
// check rejected it, deleted the archive and re-downloaded it on every single
// install. runInstaller (:1158) and the URLShaMap (:980) already accept
// "official OR mirror"; the cache-hit branch did not.
package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestCacheHit_MirrorSHA256Accepted asserts a cached archive whose hash
// matches the manifest's mirror_sha256 is accepted (not deleted).
func TestCacheHit_MirrorSHA256Accepted(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "mirrorcache"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	realSHA := fileSHA(t, cacheFile)

	// The manifest declares a DIFFERENT official hash (as if the official
	// build were a separate rebuild) and the cached file's real hash as the
	// mirror hash.
	officialSHA := strings.Repeat("a", 64)
	if realSHA == officialSHA {
		t.Fatal("test fixture produced an all-'a' hash; choose another constant")
	}

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL:          "https://example.test/" + toolName + "-1.0.0.zip",
			SHA256:       officialSHA,
			MirrorSHA256: realSHA,
			Strip:        1,
		},
	}

	// Offline: the ONLY way this can succeed is by accepting the cache.
	res, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("install failed although the cache matches mirror_sha256: %v", err)
	}
	if res.InstallPath == "" {
		t.Error("InstallPath is empty")
	}
	// The cached archive must NOT have been deleted/re-downloaded.
	if _, statErr := os.Stat(cacheFile); statErr != nil {
		t.Errorf("cached archive was deleted despite matching mirror_sha256: %v", statErr)
	}
}

// TestCacheHit_MirrorSHA256MismatchStillRejects asserts the widened check is
// not a blanket acceptance: a cache matching NEITHER hash is still rejected
// and removed.
func TestCacheHit_MirrorSHA256MismatchStillRejects(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "mirrorcachebad"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + "-1.0.0.zip",
			// Both declared hashes are wrong for the cached bytes.
			SHA256:       strings.Repeat("b", 64),
			MirrorSHA256: strings.Repeat("c", 64),
			Strip:        1,
		},
	}

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected install to fail (cache matches neither hash, re-download offline)")
	}
	if _, statErr := os.Stat(cacheFile); statErr == nil {
		t.Error("mismatching cached archive should have been removed")
	}
}

// TestCacheHit_AllZeroMirrorSHAIgnored asserts a placeholder mirror hash does
// not accidentally accept a mismatching cache.
func TestCacheHit_AllZeroMirrorSHAIgnored(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "mirrorzerocache"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL:    "https://example.test/" + toolName + "-1.0.0.zip",
			SHA256: strings.Repeat("d", 64),
			// 64 zeros = canonical placeholder; must be skipped, not matched.
			MirrorSHA256: strings.Repeat("0", 64),
			Strip:        1,
		},
	}

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected install to fail (official hash mismatches, mirror hash is a placeholder)")
	}
}
