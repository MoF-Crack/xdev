// cache_sidecar_shortpin_test.go — F-21 guards: verifyCacheSidecar must not
// slice a short sidecar pin. Pre-fix the guard only rejected an EMPTY pin
// (len(parts) < 1) and all-zero placeholders, then sliced parts[0][:16]
// unconditionally. A truncated pin such as "abc" panicked with
// "slice bounds out of range [:16]" BEFORE the caller's os.Remove, so the
// corrupt pin was never cleared and every subsequent install crashed the
// same way.
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

// TestVerifyCacheSidecar_ShortPinDoesNotPanic covers the helper directly
// with the malformed pins that used to panic.
func TestVerifyCacheSidecar_ShortPinDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, pin := range []string{"abc", "1", "short", "123456789012345"} {
		t.Run("pin="+pin, func(t *testing.T) {
			if err := os.WriteFile(file+".sha256", []byte(pin+"  archive.zip\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("verifyCacheSidecar panicked on a %d-char pin: %v", len(pin), r)
				}
			}()

			ok, reason := verifyCacheSidecar(file)
			if ok {
				t.Errorf("ok = true for a short pin %q, want false (pin cannot match a real hash)", pin)
			}
			if reason == "" {
				t.Error("reason is empty for a short pin, want a mismatch explanation")
			}
		})
	}
}

// TestInstall_ShortPinDoesNotPanicAndClearsPin drives the real cache-hit
// path: the corrupt pin must be reported as a mismatch (not a panic) and
// both the archive and the pin must be removed so the next run can recover.
func TestInstall_ShortPinDoesNotPanicAndClearsPin(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "shortpin"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	// A truncated pin (3 chars) — the exact shape that used to panic.
	if err := os.WriteFile(cacheFile+".sha256", []byte("abc  "+filepath.Base(cacheFile)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + "-1.0.0.zip", SHA256: "", Strip: 1,
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Install panicked on a short sidecar pin: %v", r)
		}
	}()

	// Offline: the rejected cache triggers a re-download that must fail.
	_, err := ins.Install(context.Background(), p, "1.0.0")
	if err == nil {
		t.Fatal("expected install to fail (cache rejected, re-download offline)")
	}

	// The panic used to happen before these removals, so the broken pin
	// survived and every later install crashed again.
	if _, statErr := os.Stat(cacheFile); statErr == nil {
		t.Error("cached archive should have been removed on pin mismatch")
	}
	if _, statErr := os.Stat(cacheFile + ".sha256"); statErr == nil {
		t.Error("short sidecar pin should have been removed so the next install can recover")
	}
}

// TestVerifyCacheSidecar_MismatchPreviewTruncatesSafely pins down the
// diagnostic formatting: a mismatching pin of any length must produce a
// bounded preview rather than a panic or an unbounded dump.
func TestVerifyCacheSidecar_MismatchPreviewTruncatesSafely(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Valid-length but wrong pin → mismatch branch with a full 64-char pin.
	if err := os.WriteFile(file+".sha256", []byte(strings.Repeat("a", 64)+"  archive.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := verifyCacheSidecar(file)
	if ok || reason == "" {
		t.Fatalf("mismatching 64-char pin: ok=%v reason=%q, want reject with reason", ok, reason)
	}
	if !strings.Contains(reason, "...") {
		t.Errorf("reason = %q, want a truncated (\"...\") preview", reason)
	}
}
