// cache_sidecar_test.go tests cache-hit sidecar pin verification (F-04 /
// D15): a cached archive whose .sha256 pin does not match must be rejected
// and re-downloaded, never silently accepted.
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

// TestCacheHit_SidecarPinMatches verifies a cache hit with a matching
// .sha256 pin is accepted and installed offline.
func TestCacheHit_SidecarPinMatches(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "fakego"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	realSHA := fileSHA(t, cacheFile)
	// 预写匹配的 sidecar pin（manifest SHA 为空时由 pin 提供校验）。
	if err := os.WriteFile(cacheFile+".sha256", []byte(realSHA+"  "+filepath.Base(cacheFile)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: "", Strip: 1,
		},
	}

	res, err := ins.Install(context.Background(), p, "1.0.0")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if !res.Activated {
		t.Error("Activated = false, want true (cache accepted via matching pin)")
	}
	// 归档与 pin 都应保留（未被误删）。
	if _, err := os.Stat(cacheFile); err != nil {
		t.Errorf("cached archive should be kept: %v", err)
	}
	if _, err := os.Stat(cacheFile + ".sha256"); err != nil {
		t.Errorf("sidecar pin should be kept: %v", err)
	}
}

// TestCacheHit_SidecarPinMismatchRemovesCache verifies a cache hit whose
// .sha256 pin does not match the cached file is rejected: the archive and
// pin are removed and a re-download is attempted (which fails offline —
// the assertion is on the removal + error path).
func TestCacheHit_SidecarPinMismatchRemovesCache(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "fakego"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	// 预写一个与真实哈希不一致的 pin（全 1 的合法格式）。
	wrongSHA := strings.Repeat("1", 64)
	if err := os.WriteFile(cacheFile+".sha256", []byte(wrongSHA+"  "+filepath.Base(cacheFile)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: "", Strip: 1,
		},
	}

	_, err := ins.Install(context.Background(), p, "1.0.0")
	if err == nil {
		t.Fatal("expected install to fail (cache rejected, re-download offline)")
	}
	// 缓存归档与 pin 必须已被删除。
	if _, statErr := os.Stat(cacheFile); statErr == nil {
		t.Error("cached archive should have been removed on pin mismatch")
	}
	if _, statErr := os.Stat(cacheFile + ".sha256"); statErr == nil {
		t.Error("sidecar pin should have been removed on pin mismatch")
	}
}

// TestVerifyCacheSidecar covers the helper directly.
func TestVerifyCacheSidecar(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := fileSHA(t, file)

	// 无 pin → 接受（TOFU 建立）。
	if ok, reason := verifyCacheSidecar(file); !ok || reason != "" {
		t.Errorf("no pin: ok=%v reason=%q, want (true, \"\")", ok, reason)
	}
	// 匹配 pin → 接受。
	if err := os.WriteFile(file+".sha256", []byte(hash+"  archive.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, reason := verifyCacheSidecar(file); !ok {
		t.Errorf("matching pin rejected: %s", reason)
	}
	// 不匹配 pin → 拒绝 + 原因。
	if err := os.WriteFile(file+".sha256", []byte(strings.Repeat("1", 64)+"  archive.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, reason := verifyCacheSidecar(file); ok || reason == "" {
		t.Errorf("mismatching pin: ok=%v reason=%q, want reject with reason", ok, reason)
	}
}
