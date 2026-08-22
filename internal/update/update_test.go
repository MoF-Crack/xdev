package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// ---------------------------------------------------------------------------
// parseSemver
// ---------------------------------------------------------------------------

func TestParseSemver_Basic(t *testing.T) {
	got := util.ParseSemverParts("1.2.3")
	want := [3]int{1, 2, 3}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "1.2.3", got, want)
	}
}

func TestParseSemver_ZeroVersion(t *testing.T) {
	got := util.ParseSemverParts("0.1.0")
	want := [3]int{0, 1, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "0.1.0", got, want)
	}
}

func TestParseSemver_WithVPrefix(t *testing.T) {
	got := util.ParseSemverParts("v1.0.0")
	want := [3]int{1, 0, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "v1.0.0", got, want)
	}
}

func TestParseSemver_WithPreRelease(t *testing.T) {
	got := util.ParseSemverParts("2.3.4-beta.1")
	want := [3]int{2, 3, 4}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "2.3.4-beta.1", got, want)
	}
}

func TestParseSemver_VPrefixWithPreRelease(t *testing.T) {
	got := util.ParseSemverParts("v3.1.0-rc.2")
	want := [3]int{3, 1, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "v3.1.0-rc.2", got, want)
	}
}

func TestParseSemver_LargeNumbers(t *testing.T) {
	got := util.ParseSemverParts("10.20.30")
	want := [3]int{10, 20, 30}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "10.20.30", got, want)
	}
}

func TestParseSemver_IncompleteVersion(t *testing.T) {
	// Only major.minor — patch defaults to 0
	got := util.ParseSemverParts("1.2")
	want := [3]int{1, 2, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "1.2", got, want)
	}
}

func TestParseSemver_OnlyMajor(t *testing.T) {
	got := util.ParseSemverParts("5")
	want := [3]int{5, 0, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "5", got, want)
	}
}

func TestParseSemver_EmptyString(t *testing.T) {
	got := util.ParseSemverParts("")
	want := [3]int{0, 0, 0}
	if got != want {
		t.Errorf("util.ParseSemverParts(%q) = %v, want %v", "", got, want)
	}
}

// ---------------------------------------------------------------------------
// semverLess
// ---------------------------------------------------------------------------

func TestSemverLess_Equal(t *testing.T) {
	if semverLess("1.2.3", "1.2.3") {
		t.Errorf("semverLess(%q, %q) = true, want false", "1.2.3", "1.2.3")
	}
}

func TestSemverLess_LessByPatch(t *testing.T) {
	if !semverLess("1.2.3", "1.2.4") {
		t.Errorf("semverLess(%q, %q) = false, want true", "1.2.3", "1.2.4")
	}
}

func TestSemverLess_LessByMinor(t *testing.T) {
	if !semverLess("1.2.9", "1.3.0") {
		t.Errorf("semverLess(%q, %q) = false, want true", "1.2.9", "1.3.0")
	}
}

func TestSemverLess_LessByMajor(t *testing.T) {
	if !semverLess("1.99.99", "2.0.0") {
		t.Errorf("semverLess(%q, %q) = false, want true", "1.99.99", "2.0.0")
	}
}

func TestSemverLess_GreaterByPatch(t *testing.T) {
	if semverLess("1.2.5", "1.2.4") {
		t.Errorf("semverLess(%q, %q) = true, want false", "1.2.5", "1.2.4")
	}
}

func TestSemverLess_GreaterByMinor(t *testing.T) {
	if semverLess("2.0.0", "1.9.9") {
		t.Errorf("semverLess(%q, %q) = true, want false", "2.0.0", "1.9.9")
	}
}

func TestSemverLess_WithVPrefix(t *testing.T) {
	if !semverLess("v1.0.0", "v1.0.1") {
		t.Errorf("semverLess(%q, %q) = false, want true", "v1.0.0", "v1.0.1")
	}
}

func TestSemverLess_ZeroVersions(t *testing.T) {
	if semverLess("0.0.0", "0.0.0") {
		t.Errorf("semverLess(%q, %q) = true, want false", "0.0.0", "0.0.0")
	}
}

// ---------------------------------------------------------------------------
// findAsset
// ---------------------------------------------------------------------------

func TestFindAsset_ExactMatch(t *testing.T) {
	release := &ReleaseInfo{
		Assets: []Asset{
			{Name: "xdev-linux-amd64", BrowserDownloadURL: "http://example.com/linux"},
			{Name: "xdev-windows-amd64.exe", BrowserDownloadURL: "http://example.com/windows"},
			{Name: "SHA256SUMS", BrowserDownloadURL: "http://example.com/sums"},
		},
	}
	asset, ok := findAsset(release, "xdev-linux-amd64")
	if !ok {
		t.Fatal("findAsset: expected to find exact match")
	}
	if asset.Name != "xdev-linux-amd64" {
		t.Errorf("findAsset name = %q, want %q", asset.Name, "xdev-linux-amd64")
	}
}

func TestFindAsset_NoMatch(t *testing.T) {
	// Asset must not contain runtime.GOOS+GOARCH substrings, otherwise the
	// partial-match fallback in findAsset would accept it on Linux/Darwin CI.
	release := &ReleaseInfo{
		Assets: []Asset{
			{Name: "other-tool-solaris-sparc", BrowserDownloadURL: "http://example.com/other"},
		},
	}
	_, ok := findAsset(release, "xdev-linux-amd64")
	if ok {
		t.Error("findAsset: expected no match, got one")
	}
}

func TestFindAsset_EmptyAssets(t *testing.T) {
	release := &ReleaseInfo{}
	_, ok := findAsset(release, "xdev-linux-amd64")
	if ok {
		t.Error("findAsset: expected no match for empty assets")
	}
}

// ---------------------------------------------------------------------------
// parseSHA256SUMS
// ---------------------------------------------------------------------------

func TestParseSHA256SUMS_DoubleSpace(t *testing.T) {
	content := "abc123def456  xdev-linux-amd64\n789xyz  other-file\n"
	hash, err := parseSHA256SUMS(content, "xdev-linux-amd64")
	if err != nil {
		t.Fatalf("parseSHA256SUMS: unexpected error: %v", err)
	}
	if hash != "abc123def456" {
		t.Errorf("parseSHA256SUMS hash = %q, want %q", hash, "abc123def456")
	}
}

func TestParseSHA256SUMS_SingleSpace(t *testing.T) {
	content := "abc123def456 xdev-linux-amd64\n"
	hash, err := parseSHA256SUMS(content, "xdev-linux-amd64")
	if err != nil {
		t.Fatalf("parseSHA256SUMS: unexpected error: %v", err)
	}
	if hash != "abc123def456" {
		t.Errorf("parseSHA256SUMS hash = %q, want %q", hash, "abc123def456")
	}
}

func TestParseSHA256SUMS_NotFound(t *testing.T) {
	content := "abc123  some-other-file\n"
	_, err := parseSHA256SUMS(content, "xdev-linux-amd64")
	if err == nil {
		t.Error("parseSHA256SUMS: expected error for missing filename")
	}
}

func TestParseSHA256SUMS_EmptyContent(t *testing.T) {
	_, err := parseSHA256SUMS("", "xdev-linux-amd64")
	if err == nil {
		t.Error("parseSHA256SUMS: expected error for empty content")
	}
}

func TestAssetNameForPlatform_VersionedArchive(t *testing.T) {
	got := assetNameForPlatform("0.3.2")
	wantPrefix := "xdev-" + runtime.GOOS + "-" + runtime.GOARCH + "-v0.3.2"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("assetNameForPlatform = %q, want prefix %q", got, wantPrefix)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(got, ".zip") {
			t.Errorf("windows asset should be .zip, got %q", got)
		}
	} else if !strings.HasSuffix(got, ".tar.gz") {
		t.Errorf("unix asset should be .tar.gz, got %q", got)
	}
	if got2 := assetNameForPlatform("v0.3.2"); got2 != got {
		t.Errorf("v-prefix should be stripped: %q vs %q", got2, got)
	}
}

func TestGithubOwnerIsReleaseRepo(t *testing.T) {
	if githubOwner != "MoF-Crack" {
		t.Errorf("githubOwner = %q, want MoF-Crack", githubOwner)
	}
}

func TestSafeArchiveMember(t *testing.T) {
	if !safeArchiveMember("xdev.exe", "xdev.exe") {
		t.Error("root xdev.exe should be accepted")
	}
	if safeArchiveMember("../xdev.exe", "xdev.exe") {
		t.Error("zip-slip path must be rejected")
	}
	if safeArchiveMember("bin/xdev.exe", "xdev.exe") {
		t.Error("nested member must be rejected")
	}
	if safeArchiveMember("xdev", "xdev.exe") {
		t.Error("wrong basename must be rejected")
	}
}

func TestExtractNamedZipMember_RootBinary(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "upd.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("xdev.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	dest := filepath.Join(dir, "out.bin")
	if err := extractNamedZipMember(zipPath, "xdev.exe", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Errorf("extracted %q", got)
	}
}

func TestExtractNamedZipMember_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bad.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("../xdev.exe")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("nope"))
	zw.Close()
	zf.Close()

	dest := filepath.Join(dir, "out.bin")
	if err := extractNamedZipMember(zipPath, "xdev.exe", dest); err == nil {
		t.Fatal("expected error for zip-slip member")
	}
}

func TestParseSHA256SUMS_MultipleEntries(t *testing.T) {
	content := `aaa111  file-a
bbb222  file-b
ccc333  file-c
`
	hash, err := parseSHA256SUMS(content, "file-b")
	if err != nil {
		t.Fatalf("parseSHA256SUMS: unexpected error: %v", err)
	}
	if hash != "bbb222" {
		t.Errorf("parseSHA256SUMS hash = %q, want %q", hash, "bbb222")
	}
}
