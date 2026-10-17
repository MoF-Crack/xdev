// extract_failure_cleanup_test.go — F-23 guards.
//
// Pre-fix installByKind returned the extraction error directly and never
// cleaned installPath. Because VerifyInstallIntegrity only requires that AT
// LEAST ONE declared shim exists (aliases and platform-specific shims may
// legitimately be absent), a partially-extracted archive that happened to
// leave one binary behind made the half-written tree look "completely
// installed" to the next run — which then skipped the download/extract and
// activated garbage.
package installer

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// makePartiallyCorruptZip writes a zip whose FIRST entry is intact (the tool
// shim binary) but whose SECOND entry's stored data is corrupted, so
// extraction writes the shim and then fails on the corrupt entry. That is the
// exact shape that used to leave a half-written tree behind: the shim exists,
// so VerifyInstallIntegrity reported OK and the next run reused the tree.
func makePartiallyCorruptZip(t *testing.T, dest, toolName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}

	binName := toolName
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	// A distinctive payload so it can be located byte-for-byte below.
	corruptPayload := []byte("CORRUPT-ME-PAYLOAD-0123456789-ABCDEFGHIJ")

	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for _, e := range []struct {
		name string
		data []byte
	}{
		{toolName + "/bin/" + binName, []byte("MZ-real-shim-binary")},
		{toolName + "/bin/extra.dat", corruptPayload},
	} {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Store}
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	// Corrupt the second entry's stored bytes in place; the central directory
	// (including its CRC) stays valid, so the reader fails on the checksum
	// only when that entry is actually read — after entry 1 was written.
	idx := bytes.Index(data, corruptPayload)
	if idx < 0 {
		t.Fatal("could not locate the payload in the crafted zip")
	}
	data[idx] ^= 0xFF

	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInstall_ExtractFailureLeavesNoHalfTree asserts a failed extraction does
// not leave an install directory behind that a later run would treat as
// complete.
func TestInstall_ExtractFailureLeavesNoHalfTree(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "trunctool"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makePartiallyCorruptZip(t, cacheFile, toolName)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + "-1.0.0.zip", Strip: 1,
		},
	}

	// NOTE: installPath is deliberately NOT pre-created. An existing empty
	// dir would short-circuit into IncompleteInstallError before extraction
	// ever runs, which would not exercise the F-23 path at all.
	installPath := p.InstallDir(cfg.Core.InstallDir, "1.0.0")

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected the corrupt archive to fail extraction")
	}

	// The half-written tree must be gone, not left as a fake "installed" dir.
	if entries, rerr := os.ReadDir(installPath); rerr == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("failed extraction left %d entries in %s (%v) — a later run may treat it as complete",
			len(entries), installPath, names)
	}
}

// TestInstall_ExtractFailureTreeNotReusedAsInstalled is the end-to-end
// consequence: after a failed extraction, the leftover tree must not pass the
// integrity check that the next run uses to decide "already installed".
func TestInstall_ExtractFailureTreeNotReusedAsInstalled(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "reusetool"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makePartiallyCorruptZip(t, cacheFile, toolName)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + "-1.0.0.zip", Strip: 1,
		},
	}
	// NOTE: installPath is deliberately NOT pre-created (see the test above).
	installPath := p.InstallDir(cfg.Core.InstallDir, "1.0.0")

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected the corrupt archive to fail extraction")
	}

	// Simulate the NEXT run: it must not find a complete tree.
	if res := VerifyInstallIntegrity(p, installPath); res.OK {
		t.Errorf("leftover tree passes VerifyInstallIntegrity (%+v) — the next run would reuse a broken install", res)
	}
}

// TestInstall_IntegrityFailureStillCleansTree guards the existing behaviour
// (post-extract integrity gate cleanup) so the F-23 change does not regress
// it. A flat archive extracted with strip=1 yields a tree with no shim.
func TestInstall_IntegrityFailureStillCleansTree(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	const toolName = "flatguard"
	cacheFile := filepath.Join(cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFlatFakeZip(t, cacheFile, toolName)

	p := &fakePlugin{
		name:  toolName,
		shims: []string{toolName},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/" + toolName + "-1.0.0.zip", Strip: 1,
		},
	}

	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected the post-extract integrity gate to fail")
	}
	if !strings.Contains(err.Error(), "post-extract integrity check failed") {
		t.Fatalf("error = %v, want a post-extract integrity failure", err)
	}
	installPath := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if _, statErr := os.Stat(installPath); statErr == nil {
		t.Error("broken install dir still exists after the integrity gate failed")
	}
}
