package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

func TestBuildDownloadOptionsPrefersConcreteManifestMirror(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{CacheDir: tmp}, Mirror: map[string]string{
		"android": "https://redirector.gvt1.com/edgedl/android/repository",
	}}
	inst := New(cfg, plugin.NewRegistry())
	archive := &manifest.Archive{
		URL:       "https://example.test/android/platform-tools_r35.0.2-win.zip",
		MirrorURL: "https://mirror.example/android/platform-tools_r35.0.2-win.zip",
		Format:    "zip",
	}
	primary, fallbacks, _ := inst.BuildDownloadOptions("android-platform-tools", "35.0.2", archive)
	// With smart mirror selection, the candidates list includes:
	//   [mirror.example, official (no rewrite triggered for example.test)]
	// Since both are unreachable (test URLs), the probe will fail and the
	// first filtered candidate (mirror.example) should be used as primary.
	if primary != archive.MirrorURL {
		t.Fatalf("primary = %q, want manifest mirror %q", primary, archive.MirrorURL)
	}
	// Fallbacks should include the remaining candidates — in this case only
	// the official URL remains.
	if len(fallbacks) != 1 || fallbacks[0] != archive.URL {
		t.Fatalf("fallbacks = %#v, want [%q]", fallbacks, archive.URL)
	}
}

// --- util.CookieFromInstallArgs ---

func TestCookieFromInstallArgs_Empty(t *testing.T) {
	got := util.CookieFromInstallArgs(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
	got = util.CookieFromInstallArgs([]string{})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestCookieFromInstallArgs_NoCookie(t *testing.T) {
	args := []string{"foo", "bar=baz", "prefix:value"}
	got := util.CookieFromInstallArgs(args)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestCookieFromInstallArgs_WithCookie(t *testing.T) {
	args := []string{"cookie:oraclelicense=accept-securebackup-cookie"}
	got := util.CookieFromInstallArgs(args)
	if got != "oraclelicense=accept-securebackup-cookie" {
		t.Errorf("expected %q, got %q", "oraclelicense=accept-securebackup-cookie", got)
	}
}

func TestCookieFromInstallArgs_MultipleArgs(t *testing.T) {
	args := []string{"other:value", "cookie:mycookie=val", "another"}
	got := util.CookieFromInstallArgs(args)
	if got != "mycookie=val" {
		t.Errorf("expected %q, got %q", "mycookie=val", got)
	}
}

func TestCookieFromInstallArgs_FirstCookieWins(t *testing.T) {
	args := []string{"cookie:first=1", "cookie:second=2"}
	got := util.CookieFromInstallArgs(args)
	if got != "first=1" {
		t.Errorf("expected first cookie %q, got %q", "first=1", got)
	}
}

// --- util.ShouldApplyMirror ---

func TestShouldApplyMirror_Adoptium(t *testing.T) {
	if util.ShouldApplyMirror("https://api.adoptium.net/v3/binary/jdk/17") {
		t.Error("Adoptium URL should bypass mirror")
	}
}

func TestShouldApplyMirror_GitHub(t *testing.T) {
	if !util.ShouldApplyMirror("https://github.com/owner/repo/releases/download/v1.0/file.tar.gz") {
		t.Error("GitHub URL should be eligible for mirror rewriting (fallback)")
	}
}

func TestShouldApplyMirror_GitHubUserContent(t *testing.T) {
	if !util.ShouldApplyMirror("https://raw.githubusercontent.com/owner/repo/main/file") {
		t.Error("githubusercontent.com URL should be eligible for mirror rewriting")
	}
}

func TestShouldApplyMirror_Upstream(t *testing.T) {
	if !util.ShouldApplyMirror("https://ftp.gnu.org/gnu/gcc/gcc-13.2.0/gcc-13.2.0.tar.xz") {
		t.Error("Upstream URL should apply mirror")
	}
}

func TestShouldApplyMirror_Oracle(t *testing.T) {
	if !util.ShouldApplyMirror("https://download.oracle.com/otn/java/jdk/17") {
		t.Error("Oracle URL should apply mirror")
	}
}

// --- util.HumanSpeedStr ---

func TestHumanSpeed_ZeroElapsed(t *testing.T) {
	got := util.HumanSpeedStr(1024, 0)
	if got != "" {
		t.Errorf("expected empty string for zero elapsed, got %q", got)
	}
}

func TestHumanSpeed_BytesPerSec(t *testing.T) {
	got := util.HumanSpeedStr(500, time.Second)
	if got != "500 B/s" {
		t.Errorf("expected %q, got %q", "500 B/s", got)
	}
}

func TestHumanSpeed_KiBPerSec(t *testing.T) {
	got := util.HumanSpeedStr(2048, time.Second)
	if got != "2.00 KiB/s" {
		t.Errorf("expected %q, got %q", "2.00 KiB/s", got)
	}
}

func TestHumanSpeed_MiBPerSec(t *testing.T) {
	got := util.HumanSpeedStr(5*1<<20, time.Second)
	if got != "5.00 MiB/s" {
		t.Errorf("expected %q, got %q", "5.00 MiB/s", got)
	}
}

func TestHumanSpeed_GiBPerSec(t *testing.T) {
	got := util.HumanSpeedStr(2*1<<30, time.Second)
	if got != "2.00 GiB/s" {
		t.Errorf("expected %q, got %q", "2.00 GiB/s", got)
	}
}

func TestHumanSpeed_FractionalDuration(t *testing.T) {
	got := util.HumanSpeedStr(10*1<<20, 500*time.Millisecond)
	if got != "20.00 MiB/s" {
		t.Errorf("expected %q, got %q", "20.00 MiB/s", got)
	}
}

// --- util.HumanETAStr ---

func TestHumanETA_ZeroDownloaded(t *testing.T) {
	got := util.HumanETAStr(0, 1000, time.Second)
	if got != "" {
		t.Errorf("expected empty string for zero downloaded, got %q", got)
	}
}

func TestHumanETA_DownloadComplete(t *testing.T) {
	got := util.HumanETAStr(1000, 1000, time.Second)
	if got != "" {
		t.Errorf("expected empty string when downloaded >= total, got %q", got)
	}
}

func TestHumanETA_DownloadExceedsTotal(t *testing.T) {
	got := util.HumanETAStr(2000, 1000, time.Second)
	if got != "" {
		t.Errorf("expected empty string when downloaded > total, got %q", got)
	}
}

func TestHumanETA_LessThanOneSecond(t *testing.T) {
	got := util.HumanETAStr(999, 1000, 999*time.Millisecond)
	if got != "0s" {
		t.Errorf("expected %q (rounds to 0s), got %q", "0s", got)
	}
}

func TestHumanETA_NormalCase(t *testing.T) {
	got := util.HumanETAStr(50, 100, time.Second)
	if got != "1s" {
		t.Errorf("expected %q, got %q", "1s", got)
	}
}

func TestHumanETA_LongerDuration(t *testing.T) {
	// 100 MiB downloaded of 400 MiB total in 10s → rate=10 MiB/s, remaining=300 MiB, eta=30s
	total := int64(400) * 1 << 20
	downloaded := int64(100) * 1 << 20
	got := util.HumanETAStr(downloaded, total, 10*time.Second)
	if got != "30s" {
		t.Errorf("expected %q, got %q", "30s", got)
	}
}

// --- plugin.HostOS / plugin.HostArch ---

func TestHostOS(t *testing.T) {
	got := plugin.HostOS()
	expected := runtime.GOOS
	switch expected {
	case "windows", "darwin", "linux":
		if got != expected {
			t.Errorf("HostOS() = %q, want %q", got, expected)
		}
	default:
		if got != expected {
			t.Errorf("HostOS() = %q, want %q for unknown OS", got, expected)
		}
	}
}

func TestHostArch(t *testing.T) {
	got := plugin.HostArch()
	// HostArch remaps amd64 → x86_64 on Linux/Darwin to match upstream
	// archive naming; keep Windows amd64 as "amd64".
	var want string
	switch runtime.GOARCH {
	case "amd64":
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			want = "x86_64"
		} else {
			want = "amd64"
		}
	case "arm64", "386":
		want = runtime.GOARCH
	default:
		want = runtime.GOARCH
	}
	if got != want {
		t.Errorf("HostArch() = %q, want %q", got, want)
	}
}

// --- util.CopyFile / util.CopyDir ---

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := []byte("hello world test content")
	srcFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcFile, content, 0o644); err != nil {
		t.Fatalf("failed to write src file: %v", err)
	}

	dstFile := filepath.Join(dstDir, "test_copy.txt")
	if err := util.CopyFile(srcFile, dstFile); err != nil {
		t.Fatalf("util.CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("failed to read dst file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("copied content mismatch: got %q, want %q", got, content)
	}
}

func TestCopyFile_SourceNotExist(t *testing.T) {
	dstDir := t.TempDir()
	dstFile := filepath.Join(dstDir, "nope.txt")
	err := util.CopyFile("/nonexistent/path/file.txt", dstFile)
	if err == nil {
		t.Error("expected error for nonexistent source file")
	}
}

func TestCopyFile_Overwrite(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcContent := []byte("new content")
	dstContent := []byte("old content")

	srcFile := filepath.Join(srcDir, "src.txt")
	dstFile := filepath.Join(dstDir, "dst.txt")

	if err := os.WriteFile(srcFile, srcContent, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.WriteFile(dstFile, dstContent, 0o644); err != nil {
		t.Fatalf("write dst: %v", err)
	}

	if err := util.CopyFile(srcFile, dstFile); err != nil {
		t.Fatalf("util.CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(srcContent) {
		t.Errorf("expected overwritten content %q, got %q", srcContent, got)
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source structure:
	//   srcDir/
	//     a.txt
	//     sub/
	//       b.txt
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("file a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("file b"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	dst := filepath.Join(dstDir, "copy")
	if err := util.CopyDir(srcDir, dst); err != nil {
		t.Fatalf("util.CopyDir failed: %v", err)
	}

	// Verify top-level file
	gotA, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(gotA) != "file a" {
		t.Errorf("a.txt: got %q, want %q", gotA, "file a")
	}

	// Verify nested file
	gotB, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(gotB) != "file b" {
		t.Errorf("b.txt: got %q, want %q", gotB, "file b")
	}
}

func TestCopyDir_SourceNotExist(t *testing.T) {
	dstDir := t.TempDir()
	err := util.CopyDir("/nonexistent/path", filepath.Join(dstDir, "dst"))
	if err == nil {
		t.Error("expected error for nonexistent source directory")
	}
}

func TestCopyDir_EmptyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	dst := filepath.Join(dstDir, "empty_copy")
	if err := util.CopyDir(srcDir, dst); err != nil {
		t.Fatalf("util.CopyDir of empty dir failed: %v", err)
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatalf("readDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty directory, got %d entries", len(entries))
	}
}

// --- InstallSelf ---

func TestInstallSelf_NewInstall(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   tmp,
			InstallDir: filepath.Join(tmp, "install"),
		},
	}

	installPath, isNew, err := InstallSelf(cfg)
	if err != nil {
		t.Fatalf("InstallSelf failed: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for new install")
	}

	expectedDir := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version)
	if installPath != expectedDir {
		t.Errorf("installPath = %q, want %q", installPath, expectedDir)
	}

	// Verify the binary was copied
	exeName := "xdev.exe"
	if runtime.GOOS != "windows" {
		exeName = "xdev"
	}
	targetExe := filepath.Join(expectedDir, exeName)
	if fi, err := os.Stat(targetExe); err != nil {
		t.Errorf("target exe not found at %s: %v", targetExe, err)
	} else if fi.IsDir() {
		t.Errorf("target exe is a directory, not a file: %s", targetExe)
	}

	// Verify the install directory was created
	if fi, err := os.Stat(expectedDir); err != nil {
		t.Errorf("install directory not found: %v", err)
	} else if !fi.IsDir() {
		t.Errorf("install path is not a directory: %s", expectedDir)
	}
}

func TestInstallSelf_AlreadyInstalled(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   tmp,
			InstallDir: filepath.Join(tmp, "install"),
		},
	}

	// First install
	installPath1, isNew1, err := InstallSelf(cfg)
	if err != nil {
		t.Fatalf("first InstallSelf failed: %v", err)
	}
	if !isNew1 {
		t.Error("first install should return isNew=true")
	}

	// Second install: target file already exists.
	// Since os.Executable() returns the test binary (not the installed one),
	// the exe != targetExe branch is taken and the binary is overwritten.
	// The isNew=false path (exe == targetExe) only triggers when the running
	// binary IS the installed binary, which cannot be easily simulated in tests.
	installPath2, isNew2, err := InstallSelf(cfg)
	if err != nil {
		t.Fatalf("second InstallSelf failed: %v", err)
	}

	expectedDir := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version)
	if installPath2 != expectedDir {
		t.Errorf("installPath = %q, want %q", installPath2, expectedDir)
	}
	if installPath1 != installPath2 {
		t.Errorf("install paths differ: first=%q second=%q", installPath1, installPath2)
	}

	// Verify the binary still exists after the second install
	exeName := "xdev.exe"
	if runtime.GOOS != "windows" {
		exeName = "xdev"
	}
	targetExe := filepath.Join(expectedDir, exeName)
	if _, err := os.Stat(targetExe); err != nil {
		t.Errorf("target exe missing after second install: %v", err)
	}

	_ = isNew2 // isNew2 is true because exe != targetExe in test context
}

func TestInstallSelf_EnsureJunction(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:   tmp,
			InstallDir: filepath.Join(tmp, "install"),
		},
	}

	_, _, err := InstallSelf(cfg)
	if err != nil {
		t.Fatalf("InstallSelf failed: %v", err)
	}

	// Verify current/xdev junction/symlink exists
	currentLink := filepath.Join(tmp, "current", "xdev")
	expectedDir := filepath.Join(cfg.Core.InstallDir, "xdev", version.Version)

	// Try Readlink first (works for symlinks and Windows junctions)
	if target, err := os.Readlink(currentLink); err == nil {
		if filepath.Clean(target) != filepath.Clean(expectedDir) {
			t.Errorf("current/xdev points to %q, want %q", target, expectedDir)
		}
		return
	}

	// Fallback: on some Windows configurations Readlink may fail for junctions;
	// verify it's at least a valid directory that resolves to the install path.
	if fi, err := os.Stat(currentLink); err != nil {
		t.Fatalf("current/xdev not found: %v", err)
	} else if !fi.IsDir() {
		t.Error("current/xdev should be a directory (junction)")
	}
}

// --- ensureSelfJunction ---

func TestEnsureSelfJunction_CreateNew(t *testing.T) {
	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "install", "xdev", "1.0.0")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir: tmp,
		},
	}

	ensureSelfJunction(cfg, installDir)

	currentLink := filepath.Join(tmp, "current", "xdev")

	// Verify via Readlink
	if target, err := os.Readlink(currentLink); err == nil {
		if filepath.Clean(target) != filepath.Clean(installDir) {
			t.Errorf("junction points to %q, want %q", target, installDir)
		}
		return
	}

	// Fallback: check it's a directory
	if fi, err := os.Stat(currentLink); err != nil {
		t.Fatalf("current/xdev not found: %v", err)
	} else if !fi.IsDir() {
		t.Error("current/xdev should be a directory (junction)")
	}
}

func TestEnsureSelfJunction_UpdateExisting(t *testing.T) {
	tmp := t.TempDir()
	oldDir := filepath.Join(tmp, "install", "xdev", "1.0.0")
	newDir := filepath.Join(tmp, "install", "xdev", "2.0.0")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir new: %v", err)
	}

	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir: tmp,
		},
	}

	// Create initial junction pointing to oldDir
	ensureSelfJunction(cfg, oldDir)

	// Update junction to point to newDir
	ensureSelfJunction(cfg, newDir)

	currentLink := filepath.Join(tmp, "current", "xdev")

	// Verify the junction now points to newDir
	if target, err := os.Readlink(currentLink); err == nil {
		if filepath.Clean(target) != filepath.Clean(newDir) {
			t.Errorf("junction points to %q, want %q", target, newDir)
		}
		return
	}

	// Fallback: verify the junction directory contains files from newDir
	// (create a marker file in newDir to distinguish from oldDir)
	marker := filepath.Join(newDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("new"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	resolvedMarker := filepath.Join(currentLink, "marker.txt")
	if data, err := os.ReadFile(resolvedMarker); err != nil {
		t.Fatalf("read marker through junction: %v", err)
	} else if string(data) != "new" {
		t.Errorf("junction still points to old directory")
	}
}
