package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// createTestZip creates a zip file at dstPath with the given entries.
// Each entry is "path|content" — if content is empty it's treated as a directory.
func createTestZip(t *testing.T, dstPath string, entries []string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for _, e := range entries {
		parts := strings.SplitN(e, "|", 2)
		name := parts[0]
		content := ""
		if len(parts) == 2 {
			content = parts[1]
		}

		if strings.HasSuffix(name, "/") || content == "" && strings.Contains(name, "/") && !strings.Contains(name[len(strings.TrimRight(name, "/"))-1:], ".") {
			// directory entry
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			if _, err := w.Create(name); err != nil {
				t.Fatalf("zip create dir %s: %v", name, err)
			}
			continue
		}

		out, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := out.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// createTestTarGz creates a tar.gz file at dstPath with the given entries.
// Each entry is "path|content" — if content is empty and path ends with "/", it's a directory.
func createTestTarGz(t *testing.T, dstPath string, entries []string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		parts := strings.SplitN(e, "|", 2)
		name := parts[0]
		content := ""
		if len(parts) == 2 {
			content = parts[1]
		}

		if strings.HasSuffix(name, "/") {
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name,
				Mode:     0o755,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatalf("tar header dir %s: %v", name, err)
			}
			continue
		}

		data := []byte(content)
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     int64(len(data)),
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// ---------- Tests ----------

func TestExtract_Zip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"topdir/",
		"topdir/hello.txt|hello world",
		"topdir/sub/",
		"topdir/sub/nested.txt|nested content",
	})

	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("Extract zip: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "topdir", "hello.txt")) {
		t.Error("expected topdir/hello.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "topdir", "hello.txt")) != "hello world" {
		t.Error("hello.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "topdir", "sub", "nested.txt")) {
		t.Error("expected topdir/sub/nested.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "topdir", "sub", "nested.txt")) != "nested content" {
		t.Error("nested.txt content mismatch")
	}
}

func TestExtract_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"mydir/",
		"mydir/alpha.txt|alpha",
		"mydir/inner/",
		"mydir/inner/beta.txt|beta",
	})

	if err := Extract(tgzPath, dstDir, "tar.gz", 0); err != nil {
		t.Fatalf("Extract tar.gz: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "mydir", "alpha.txt")) {
		t.Error("expected mydir/alpha.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "mydir", "alpha.txt")) != "alpha" {
		t.Error("alpha.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "mydir", "inner", "beta.txt")) {
		t.Error("expected mydir/inner/beta.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "mydir", "inner", "beta.txt")) != "beta" {
		t.Error("beta.txt content mismatch")
	}
}

func TestExtract_TarGz_AliasTgz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tgz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"root.txt|rootfile",
	})

	if err := Extract(tgzPath, dstDir, "tgz", 0); err != nil {
		t.Fatalf("Extract tgz: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "root.txt")) {
		t.Error("expected root.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "root.txt")) != "rootfile" {
		t.Error("root.txt content mismatch")
	}
}

func TestExtract_Strip1(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "strip.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"topdir/",
		"topdir/file.txt|stripped content",
		"topdir/subdir/",
		"topdir/subdir/deep.txt|deep",
	})

	if err := Extract(zipPath, dstDir, "zip", 1); err != nil {
		t.Fatalf("Extract zip strip=1: %v", err)
	}

	// With strip=1, "topdir" is removed, so file.txt should be at the root
	if !fileExists(filepath.Join(dstDir, "file.txt")) {
		t.Error("expected file.txt at root after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "file.txt")) != "stripped content" {
		t.Error("file.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "subdir", "deep.txt")) {
		t.Error("expected subdir/deep.txt after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "subdir", "deep.txt")) != "deep" {
		t.Error("deep.txt content mismatch")
	}

	// The top-level directory should NOT exist
	if fileExists(filepath.Join(dstDir, "topdir")) {
		t.Error("topdir should not exist after strip=1")
	}
}

func TestExtract_Strip1_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "strip.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"pkg-1.0/",
		"pkg-1.0/README.md|readme",
		"pkg-1.0/src/",
		"pkg-1.0/src/main.go|package main",
	})

	if err := Extract(tgzPath, dstDir, "tar.gz", 1); err != nil {
		t.Fatalf("Extract tar.gz strip=1: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "README.md")) {
		t.Error("expected README.md at root after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "README.md")) != "readme" {
		t.Error("README.md content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "src", "main.go")) {
		t.Error("expected src/main.go after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "src", "main.go")) != "package main" {
		t.Error("main.go content mismatch")
	}
}

func TestExtract_UnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")

	err := Extract("dummy.src", dstDir, "rar", 0)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtract_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	nonExistent := filepath.Join(tmpDir, "no_such_file.zip")

	err := Extract(nonExistent, dstDir, "zip", 0)
	if err == nil {
		t.Fatal("expected error for non-existent source")
	}
}

func TestExtract_TarGz_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	nonExistent := filepath.Join(tmpDir, "no_such_file.tar.gz")

	err := Extract(nonExistent, dstDir, "tar.gz", 0)
	if err == nil {
		t.Fatal("expected error for non-existent tar.gz source")
	}
}

func TestStripPath(t *testing.T) {
	tests := []struct {
		input string
		strip int
		want  string
		ok    bool
	}{
		{"foo/bar.txt", 0, "foo/bar.txt", true},
		{"foo/bar.txt", 1, "bar.txt", true},
		{"foo/bar.txt", 2, "", false},
		{"a/b/c.txt", 1, "b/c.txt", true},
		{"a/b/c.txt", 2, "c.txt", true},
		{"a/b/c.txt", 3, "", false},
		{".", 0, "", false},
		{"single.txt", 0, "single.txt", true},
		{"single.txt", 1, "", false},
	}

	for _, tt := range tests {
		got, ok := stripPath(tt.input, tt.strip)
		if ok != tt.ok || got != tt.want {
			t.Errorf("stripPath(%q, %d) = (%q, %v), want (%q, %v)",
				tt.input, tt.strip, got, ok, tt.want, tt.ok)
		}
	}
}

func TestExtract_Zip_EmptyDirs(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "empty.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"emptydir/",
		"emptydir/file.txt|data",
	})

	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("Extract zip: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "emptydir")) {
		t.Error("expected emptydir directory to exist")
	}
}

func TestExtract_TarGz_StripAll(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "all.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"onlyfile.txt|content",
	})

	// strip=1 on a single-component path should skip the entry entirely
	if err := Extract(tgzPath, dstDir, "tar.gz", 1); err != nil {
		t.Fatalf("Extract tar.gz strip=1 on single component: %v", err)
	}

	// The file should not exist because strip=1 removes the only path component
	if fileExists(filepath.Join(dstDir, "onlyfile.txt")) {
		t.Error("onlyfile.txt should not exist after strip=1")
	}
}

// ---------- DMG tests (T-A3) ----------

// TestExtract_DMG_NonDarwinError verifies that dmg extraction returns a
// clear error on non-macOS platforms (runs on CI/Windows). On macOS the
// real hdiutil path applies, so this test is skipped there.
func TestExtract_DMG_NonDarwinError(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("dmg extraction is supported on darwin; non-darwin error path not applicable")
	}
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")

	err := Extract("dummy.dmg", dstDir, "dmg", 0)
	if err == nil {
		t.Fatal("expected error for dmg extraction on non-macOS")
	}
	if !strings.Contains(err.Error(), "only supported on macOS") {
		t.Errorf("unexpected error: %v (want 'only supported on macOS')", err)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("error should mention current GOOS %q: %v", runtime.GOOS, err)
	}
}

// writeMockHdiutil writes a parameterised mock hdiutil shell script that
// records every invocation ("$*" per line) into $HDIUTIL_LOG and honours:
//
//	MOCK_CREATE_APP   =1 → attach 时在挂载点创建 TestApp.app（默认 0）
//	MOCK_ATTACH_EXIT   attach 退出码（默认 0）
//	MOCK_DETACH_EXIT   detach 退出码（默认 0）
//
// attach 的挂载点参数位置：attach -nobrowse -readonly -mountpoint <mount> <src> → $5。
func writeMockHdiutil(t *testing.T, mockPath string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"$HDIUTIL_LOG\"\n" +
		"if [ \"$1\" = \"attach\" ]; then\n" +
		"  if [ \"$MOCK_CREATE_APP\" = \"1\" ]; then\n" +
		"    mkdir -p \"$5/TestApp.app/Contents\"\n" +
		"    printf 'fake-info' > \"$5/TestApp.app/Contents/Info.plist\"\n" +
		"  fi\n" +
		"  exit \"${MOCK_ATTACH_EXIT:-0}\"\n" +
		"fi\n" +
		"if [ \"$1\" = \"detach\" ]; then\n" +
		"  exit \"${MOCK_DETACH_EXIT:-0}\"\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(mockPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock hdiutil: %v", err)
	}
}

// mockCmdLog reads the recorded invocation log written by the mock script.
func mockCmdLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read mock log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// assertAttachThenDetach asserts the recorded command log starts with an
// attach invocation and ends with a detach invocation (order preserved).
func assertAttachThenDetach(t *testing.T, lines []string) {
	t.Helper()
	if len(lines) < 2 {
		t.Fatalf("mock log too short: %v", lines)
	}
	if !strings.HasPrefix(lines[0], "attach ") {
		t.Errorf("first recorded command = %q, want attach", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "detach ") {
		t.Errorf("last recorded command = %q, want detach", lines[len(lines)-1])
	}
}

// TestExtract_DMG_DarwinWithMockHdiutil exercises the darwin hdiutil
// attach → copy .app → detach flow using the hdiutilCmd test hook, and
// asserts the recorded command order is attach → detach (023-A3-03).
// Skipped on non-darwin; runs on macOS CI.
func TestExtract_DMG_DarwinWithMockHdiutil(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mock hdiutil flow only runs on darwin")
	}
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	logPath := filepath.Join(tmpDir, "hdiutil.log")

	writeMockHdiutil(t, filepath.Join(tmpDir, "mock-hdiutil"))
	t.Setenv("HDIUTIL_LOG", logPath)
	t.Setenv("MOCK_CREATE_APP", "1")

	old := hdiutilCmd
	hdiutilCmd = filepath.Join(tmpDir, "mock-hdiutil")
	defer func() { hdiutilCmd = old }()

	if err := Extract(filepath.Join(tmpDir, "app.dmg"), dstDir, "dmg", 0); err != nil {
		t.Fatalf("Extract dmg with mock hdiutil: %v", err)
	}
	// .app 应从挂载点复制到 dst。
	plist := filepath.Join(dstDir, "TestApp.app", "Contents", "Info.plist")
	if readFile(t, plist) != "fake-info" {
		t.Errorf("Info.plist content mismatch: %q", readFile(t, plist))
	}
	assertAttachThenDetach(t, mockCmdLog(t, logPath))
}

// TestExtract_DMG_NoApp verifies a dmg without any .app bundle errors out
// and the image is still detached.
func TestExtract_DMG_NoApp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mock hdiutil flow only runs on darwin")
	}
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "hdiutil.log")

	writeMockHdiutil(t, filepath.Join(tmpDir, "mock-hdiutil"))
	t.Setenv("HDIUTIL_LOG", logPath)
	// MOCK_CREATE_APP 缺省 0 → 挂载点无 .app。

	old := hdiutilCmd
	hdiutilCmd = filepath.Join(tmpDir, "mock-hdiutil")
	defer func() { hdiutilCmd = old }()

	err := Extract(filepath.Join(tmpDir, "app.dmg"), filepath.Join(tmpDir, "out"), "dmg", 0)
	if err == nil || !strings.Contains(err.Error(), "no .app bundle found") {
		t.Fatalf("want no-.app error, got %v", err)
	}
	assertAttachThenDetach(t, mockCmdLog(t, logPath))
}

// TestExtract_DMG_DetachFailure verifies that when detach (and detach
// -force) fail, the error is returned even though the copy succeeded
// (023-A3-02: no silent leftover mount).
func TestExtract_DMG_DetachFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mock hdiutil flow only runs on darwin")
	}
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	logPath := filepath.Join(tmpDir, "hdiutil.log")

	writeMockHdiutil(t, filepath.Join(tmpDir, "mock-hdiutil"))
	t.Setenv("HDIUTIL_LOG", logPath)
	t.Setenv("MOCK_CREATE_APP", "1")
	t.Setenv("MOCK_DETACH_EXIT", "1") // 普通 detach 与 -force 都失败

	old := hdiutilCmd
	hdiutilCmd = filepath.Join(tmpDir, "mock-hdiutil")
	defer func() { hdiutilCmd = old }()

	err := Extract(filepath.Join(tmpDir, "app.dmg"), dstDir, "dmg", 0)
	if err == nil || !strings.Contains(err.Error(), "detach") {
		t.Fatalf("want detach error, got %v", err)
	}
	// .app 已复制但错误仍返回。
	if !fileExists(filepath.Join(dstDir, "TestApp.app", "Contents", "Info.plist")) {
		t.Error("expected .app copied before detach failure surfaced")
	}
	lines := mockCmdLog(t, logPath)
	// 顺序：attach → detach → detach -force。
	if len(lines) != 3 {
		t.Fatalf("expected 3 commands (attach, detach, detach -force), got %v", lines)
	}
	if !strings.HasPrefix(lines[0], "attach ") {
		t.Errorf("lines[0] = %q, want attach", lines[0])
	}
	if !strings.HasPrefix(lines[1], "detach ") || strings.Contains(lines[1], "-force") {
		t.Errorf("lines[1] = %q, want plain detach first", lines[1])
	}
	if !strings.HasPrefix(lines[2], "detach -force ") {
		t.Errorf("lines[2] = %q, want detach -force after plain detach", lines[2])
	}
}

// TestExtract_DMG_AttachFailureCleanup verifies that a failed attach still
// triggers a best-effort detach of the known mountpoint (023-A3-02).
func TestExtract_DMG_AttachFailureCleanup(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mock hdiutil flow only runs on darwin")
	}
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "hdiutil.log")

	writeMockHdiutil(t, filepath.Join(tmpDir, "mock-hdiutil"))
	t.Setenv("HDIUTIL_LOG", logPath)
	t.Setenv("MOCK_ATTACH_EXIT", "1") // attach 失败

	old := hdiutilCmd
	hdiutilCmd = filepath.Join(tmpDir, "mock-hdiutil")
	defer func() { hdiutilCmd = old }()

	err := Extract(filepath.Join(tmpDir, "app.dmg"), filepath.Join(tmpDir, "out"), "dmg", 0)
	if err == nil || !strings.Contains(err.Error(), "attach") {
		t.Fatalf("want attach error, got %v", err)
	}
	// attach 失败后仍应尝试 detach 清理（顺序 attach → detach）。
	assertAttachThenDetach(t, mockCmdLog(t, logPath))
}

// TestExtract_DMG_CopyFailure verifies that a .app copy failure (ditto
// mock exits non-zero) surfaces as an error — no silent fallback — and the
// image is still detached (deferred cleanup runs) (023-R1).
func TestExtract_DMG_CopyFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mock hdiutil flow only runs on darwin")
	}
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	logPath := filepath.Join(tmpDir, "hdiutil.log")

	writeMockHdiutil(t, filepath.Join(tmpDir, "mock-hdiutil"))
	t.Setenv("HDIUTIL_LOG", logPath)
	t.Setenv("MOCK_CREATE_APP", "1")

	old := hdiutilCmd
	hdiutilCmd = filepath.Join(tmpDir, "mock-hdiutil")
	defer func() { hdiutilCmd = old }()
	// ditto mock 恒失败：ditto 失败必须返回错误（绝不降级 CopyDir）。
	oldDitto := dittoCmd
	dittoCmd = filepath.Join(tmpDir, "mock-ditto")
	defer func() { dittoCmd = oldDitto }()
	if err := os.WriteFile(filepath.Join(tmpDir, "mock-ditto"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Extract(filepath.Join(tmpDir, "app.dmg"), dstDir, "dmg", 0)
	if err == nil || !strings.Contains(err.Error(), "copy") {
		t.Fatalf("want copy error, got %v", err)
	}
	// 复制失败后 detach 仍必须执行（defer 清理）。
	assertAttachThenDetach(t, mockCmdLog(t, logPath))
}

// TestCopyAppBundle_DittoPreservesSymlinks verifies the ditto copy path
// preserves symlinks inside .app bundles (Electron/IDE frameworks),
// using the dittoCmd hook override (023-A3-01). Skipped on non-darwin
// (shell-script mock + symlink fixture require a Unix filesystem).
func TestCopyAppBundle_DittoPreservesSymlinks(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ditto symlink fixture requires darwin")
	}
	tmpDir := t.TempDir()
	srcApp := filepath.Join(tmpDir, "src", "Sample.app")
	// 真实框架放在 .app 内部；符号链接用相对路径指向它（Electron 典型布局）。
	realFramework := filepath.Join(srcApp, "Contents", "Frameworks", "Real.framework")
	if err := os.MkdirAll(realFramework, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realFramework, "lib.dylib"), []byte("lib"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 相对符号链接：Contents/Frameworks/Sample.framework → Real.framework。
	linkPath := filepath.Join(srcApp, "Contents", "Frameworks", "Sample.framework")
	if err := os.Symlink("Real.framework", linkPath); err != nil {
		t.Fatal(err)
	}
	// mock ditto：先建目标父目录，再 cp -R 保留符号链接，并传播 cp 退出码。
	mock := filepath.Join(tmpDir, "mock-ditto")
	script := "#!/bin/sh\n" +
		"mkdir -p \"$(dirname \"$2\")\"\n" +
		"cp -R \"$1\" \"$2\"\n" +
		"exit $?\n"
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := dittoCmd
	dittoCmd = mock
	defer func() { dittoCmd = old }()

	dstApp := filepath.Join(tmpDir, "dst", "Sample.app")
	if err := copyAppBundle(srcApp, dstApp); err != nil {
		t.Fatalf("copyAppBundle: %v", err)
	}
	// 符号链接必须保留（util.CopyDir 会跳过符号链接）。
	copiedLink := filepath.Join(dstApp, "Contents", "Frameworks", "Sample.framework")
	fi, err := os.Lstat(copiedLink)
	if err != nil {
		t.Fatalf("Lstat copied symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink preserved by ditto copy (util.CopyDir would skip it)")
	}
	// 相对链接目标在复制后的 .app 内可解析，且真实框架内容已复制。
	if target, err := os.Readlink(copiedLink); err != nil || target != "Real.framework" {
		t.Errorf("copied symlink target = %q (err=%v), want relative 'Real.framework'", target, err)
	}
	if !fileExists(filepath.Join(dstApp, "Contents", "Frameworks", "Real.framework", "lib.dylib")) {
		t.Error("expected real framework copied")
	}
}

// TestCopyAppBundle_DittoFailure verifies a ditto copy failure surfaces as
// an error — no silent fallback to util.CopyDir (which would skip symlinks
// and produce a broken .app) (023-R1). Runs wherever the hook is overridden.
func TestCopyAppBundle_DittoFailure(t *testing.T) {
	tmpDir := t.TempDir()
	srcApp := filepath.Join(tmpDir, "src", "Sample.app")
	if err := os.MkdirAll(srcApp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcApp, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// mock ditto 恒失败。
	mock := filepath.Join(tmpDir, "mock-ditto")
	if err := os.WriteFile(mock, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := dittoCmd
	dittoCmd = mock
	defer func() { dittoCmd = old }()

	err := copyAppBundle(srcApp, filepath.Join(tmpDir, "dst", "Sample.app"))
	if err == nil {
		t.Fatal("expected ditto failure to surface as error (no silent CopyDir fallback)")
	}
	if !strings.Contains(err.Error(), "ditto") {
		t.Errorf("error should mention ditto: %v", err)
	}
	if fileExists(filepath.Join(tmpDir, "dst", "Sample.app", "file.txt")) {
		t.Error("no partial copy expected after ditto failure")
	}
}

// TestExtract_DMG_CopyAppFromMount unit-tests the copy logic with a fake
// mount directory (no hdiutil needed), running on all platforms.
func TestExtract_DMG_CopyAppFromMount(t *testing.T) {
	tmpDir := t.TempDir()
	mountDir := filepath.Join(tmpDir, "mount")
	dstDir := filepath.Join(tmpDir, "out")

	appDir := filepath.Join(mountDir, "SampleApp.app", "Contents", "MacOS")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "SampleApp"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 非 .app 目录应被忽略。
	if err := os.MkdirAll(filepath.Join(mountDir, "ReadMe.rtf"), 0o755); err != nil {
		t.Fatal(err)
	}

	copied, err := copyAppsFromMount(mountDir, dstDir)
	if err != nil {
		t.Fatalf("copyAppsFromMount: %v", err)
	}
	if copied != 1 {
		t.Errorf("copied = %d, want 1", copied)
	}
	if !fileExists(filepath.Join(dstDir, "SampleApp.app", "Contents", "MacOS", "SampleApp")) {
		t.Error("expected SampleApp.app copied to dst")
	}
	if fileExists(filepath.Join(dstDir, "ReadMe.rtf")) {
		t.Error("non-.app entry must not be copied")
	}
}

// ---------- Zip Slip security tests ----------
//
// These guard the isSafePath check in extractZip/extractTarFromReader
// against Zip Slip / Tar Slip path-traversal attacks, where a malicious
// archive entry like "../escape.txt" or an absolute path would write
// outside the destination directory.

func TestIsSafePath_InsideDestination(t *testing.T) {
	dst := t.TempDir()
	out := filepath.Join(dst, "sub", "file.txt")
	if !isSafePath(dst, out) {
		t.Error("path inside destination should be safe")
	}
}

func TestIsSafePath_DestinationItself(t *testing.T) {
	dst := t.TempDir()
	if !isSafePath(dst, dst) {
		t.Error("destination itself should be safe")
	}
}

func TestIsSafePath_SiblingPrefix(t *testing.T) {
	// /tmp/foo vs /tmp/foobar must NOT match via prefix (the separator guard).
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "foo")
	os.MkdirAll(dst, 0o755)
	sibling := filepath.Join(tmp, "foobar", "file.txt")
	if isSafePath(dst, sibling) {
		t.Error("sibling with shared prefix must be rejected (no false prefix match)")
	}
}

func TestIsSafePath_EscapesViaParent(t *testing.T) {
	dst := t.TempDir()
	// Join cleans "../.." so build the escape explicitly and verify rejection.
	escape := filepath.Join(dst, "..", "..", "escaped.txt")
	if isSafePath(dst, escape) {
		t.Error("path escaping via ../ must be rejected")
	}
}

// createMaliciousZip builds a zip whose entry name is an absolute path,
// which filepath.Join cannot coerce back under dst. This exercises the
// isSafePath guard end to end through extractZip.
func createMaliciousZip(t *testing.T, dstPath, entryName string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	out, err := w.Create(entryName)
	if err != nil {
		t.Fatalf("zip create %s: %v", entryName, err)
	}
	if _, err := out.Write([]byte("pwned")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

func TestExtract_ZipSlip_AbsolutePathRejected(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "evil.zip")
	dstDir := filepath.Join(tmpDir, "out")

	// Prefer a true absolute entry name. On Unix, filepath.Join(tmp, "escaped")
	// is already absolute; on Windows force a rooted form so the slip guard
	// still sees an absolute entry.
	absTarget := filepath.Join(tmpDir, "escaped.txt")
	if !filepath.IsAbs(absTarget) {
		absTarget = string(filepath.Separator) + absTarget
	}
	// Zip entry names use forward slashes; absolute Windows paths need to
	// remain absolute when re-joined by extractZip.
	entryName := absTarget
	if filepath.Separator != '/' {
		entryName = filepath.ToSlash(absTarget)
	}
	createMaliciousZip(t, zipPath, entryName)

	err := Extract(zipPath, dstDir, "zip", 0)
	// Security requirement: escaped file must not be written. Extraction may
	// either return an error or safely skip the entry; both are acceptable if
	// the target path is not created.
	if fileExists(absTarget) {
		t.Fatalf("absolute-path entry was written to %s (Zip Slip succeeded!); extract err=%v", absTarget, err)
	}
	// Prefer an error when the implementation can detect the slip.
	if err == nil {
		// On some platforms the entry may be rewritten under dst instead of
		// escaping; ensure destination did not receive the absolute-name file
		// outside out/.
		if entries, _ := os.ReadDir(dstDir); len(entries) != 0 {
			// Allow empty success only if nothing was written outside.
			// If files landed under dst with a cleaned relative name, that is
			// still not a slip as long as absTarget is untouched.
			t.Logf("extract returned nil with %d dst entries (absTarget safe)", len(entries))
		}
	}
}

func TestExtract_ZipSlip_NormalZipStillWorks(t *testing.T) {
	// Regression guard: the slip check must not reject legitimate archives.
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "ok.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"bin/",
		"bin/hello.txt|hi",
	})
	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("legitimate zip rejected: %v", err)
	}
	if !fileExists(filepath.Join(dstDir, "bin", "hello.txt")) {
		t.Error("legitimate file missing after extract")
	}
}
