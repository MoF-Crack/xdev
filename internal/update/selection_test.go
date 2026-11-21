package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func allPlatformSelectionFixture() *CheckResult {
	release := &ReleaseInfo{TagName: "v1.0.0-rc.1"}
	for _, target := range [][2]string{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		name := assetNameForTarget("1.0.0-rc.1", target[0], target[1])
		release.Assets = append(release.Assets, Asset{Name: name, BrowserDownloadURL: "https://example.test/" + name, Size: 1024, State: "uploaded"})
	}
	release.Assets = append(release.Assets, Asset{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.test/SHA256SUMS", Size: 512, State: "uploaded"}, Asset{Name: "xdev-darwin-arm64-v1.0.0-rc.1.dmg", BrowserDownloadURL: "https://example.test/app.dmg", Size: 1024}, Asset{Name: "xdev-1.0.0-rc.1-x86_64.AppImage", BrowserDownloadURL: "https://example.test/app.AppImage", Size: 1024})
	return &CheckResult{CurrentVersion: "1.0.0-beta.3", LatestVersion: "1.0.0-rc.1", HasUpdate: true, Release: release}
}

func TestSelectedAssetUsesOneExactPlatform(t *testing.T) {
	for _, tc := range []struct{ os, arch, want string }{
		{"windows", "amd64", "xdev-windows-amd64-v1.0.0-rc.1.zip"},
		{"windows", "arm64", "xdev-windows-arm64-v1.0.0-rc.1.zip"},
		{"linux", "amd64", "xdev-linux-amd64-v1.0.0-rc.1.tar.gz"},
		{"linux", "arm64", "xdev-linux-arm64-v1.0.0-rc.1.tar.gz"},
		{"darwin", "amd64", "xdev-darwin-amd64-v1.0.0-rc.1.tar.gz"},
		{"darwin", "arm64", "xdev-darwin-arm64-v1.0.0-rc.1.tar.gz"},
	} {
		t.Run(tc.os+"/"+tc.arch, func(t *testing.T) {
			asset, err := selectedAssetForTarget(allPlatformSelectionFixture(), tc.os, tc.arch)
			if err != nil || asset.Name != tc.want {
				t.Fatalf("selected=%+v error=%v want=%s", asset, err, tc.want)
			}
		})
	}
	result := allPlatformSelectionFixture()
	asset, err := SelectedAsset(result)
	if err != nil || TargetPlatform() != runtime.GOOS+"/"+runtime.GOARCH || asset.Name != assetNameForPlatform(result.LatestVersion) {
		t.Fatalf("runtime selection diverges: asset=%+v error=%v platform=%s", asset, err, TargetPlatform())
	}
}

func TestSelectedAssetNeverFallsBackToOtherPlatformOrAmbiguousArchive(t *testing.T) {
	for _, tc := range []string{"unsupported", "missing", "duplicate", "http-url", "empty-size", "pending"} {
		t.Run(tc, func(t *testing.T) {
			r := allPlatformSelectionFixture()
			goos, arch := "windows", "amd64"
			switch tc {
			case "unsupported":
				goos, arch = "plan9", "amd64"
			case "missing":
				r.Release.Assets = r.Release.Assets[1:]
			case "duplicate":
				r.Release.Assets = append(r.Release.Assets, r.Release.Assets[0])
			case "http-url":
				r.Release.Assets[0].BrowserDownloadURL = "http://example.test/win.zip"
			case "empty-size":
				r.Release.Assets[0].Size = 0
			case "pending":
				r.Release.Assets[0].State = "new"
			}
			asset, err := selectedAssetForTarget(r, goos, arch)
			if err == nil || asset.Name != "" {
				t.Fatalf("bad platform selection fell back: asset=%+v err=%v", asset, err)
			}
		})
	}
}

func TestApplyInvalidPlatformArchiveStopsBeforeFilesystemWrites(t *testing.T) {
	result := allPlatformSelectionFixture()
	// Keep valid archives for every other platform, but remove this executable's.
	var others []Asset
	for _, a := range result.Release.Assets {
		if a.Name != assetNameForPlatform(result.LatestVersion) {
			others = append(others, a)
		}
	}
	result.Release.Assets = others
	dir := t.TempDir()
	target := filepath.Join(dir, "xdev.exe")
	if err := os.WriteFile(target, []byte("original binary"), 0600); err != nil {
		t.Fatal(err)
	}
	err := ApplyToWithProgress(context.Background(), result, target, func(Progress) { t.Error("invalid selection emitted download progress") })
	if err == nil || !strings.Contains(err.Error(), TargetPlatform()) {
		t.Fatalf("missing platform did not fail clearly: %v", err)
	}
	bytes, readErr := os.ReadFile(target)
	if readErr != nil || string(bytes) != "original binary" {
		t.Fatalf("invalid asset altered target: %q %v", bytes, readErr)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("invalid platform left download artifacts: %+v", entries)
	}
}

func TestSelectedAssetPrefersExactWindowsRawExecutable(t *testing.T) {
	raw := Asset{Name: "xdev-windows-amd64-v1.0.0-rc.1.exe", BrowserDownloadURL: "https://example.test/opaque-asset", Size: 1024, State: "uploaded"}
	for _, tc := range []struct {
		name, goos, arch, want string
		edit                   func([]Asset) []Asset
	}{
		{name: "preferred over ZIP", goos: "windows", arch: "amd64", want: raw.Name},
		{"raw only", "windows", "amd64", raw.Name, func(assets []Asset) []Asset { return assets[len(assets)-1:] }},
		{name: "other architecture", goos: "windows", arch: "arm64", want: "xdev-windows-arm64-v1.0.0-rc.1.zip"},
		{name: "Linux tarball", goos: "linux", arch: "amd64", want: "xdev-linux-amd64-v1.0.0-rc.1.tar.gz"},
		{name: "macOS tarball", goos: "darwin", arch: "arm64", want: "xdev-darwin-arm64-v1.0.0-rc.1.tar.gz"},
		{"raw absent despite similar names", "windows", "amd64", "xdev-windows-amd64-v1.0.0-rc.1.zip", func(assets []Asset) []Asset {
			assets[len(assets)-1].Name = "xdev-windows-amd64-v1.0.0-rc.2.exe"
			return append(assets, Asset{Name: "xdev-gui-windows-amd64-v1.0.0-rc.1.exe", BrowserDownloadURL: raw.BrowserDownloadURL, Size: 1})
		}},
		{"duplicate raw", "windows", "amd64", "", func(assets []Asset) []Asset { return append(assets, raw) }},
		{"invalid raw URL", "windows", "amd64", "", func(assets []Asset) []Asset {
			assets[len(assets)-1].BrowserDownloadURL = "http://example.test/raw"
			return assets
		}},
		{"empty raw", "windows", "amd64", "", func(assets []Asset) []Asset {
			assets[len(assets)-1].Size = 0
			return assets
		}},
		{"pending raw", "windows", "amd64", "", func(assets []Asset) []Asset {
			assets[len(assets)-1].State = "starter"
			return assets
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := allPlatformSelectionFixture()
			result.LatestVersion = "v" + result.LatestVersion
			result.Release.Assets = append(result.Release.Assets, raw)
			if tc.edit != nil {
				result.Release.Assets = tc.edit(result.Release.Assets)
			}
			asset, err := selectedAssetForTarget(result, tc.goos, tc.arch)
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), raw.Name) || asset != (Asset{}) {
					t.Fatalf("bad raw asset was hidden by a fallback: selected=%+v error=%v", asset, err)
				}
				return
			}
			if err != nil || asset.Name != tc.want {
				t.Fatalf("selected=%+v error=%v want=%s", asset, err, tc.want)
			}
			if tc.want == raw.Name && asset != raw {
				t.Fatalf("selected raw metadata was modified: %+v", asset)
			}
		})
	}
}

func TestCheckWindowsRawExecutableRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Check selects the executing platform")
	}
	raw := Asset{Name: "xdev-windows-" + runtime.GOARCH + "-v2.0.0.exe", BrowserDownloadURL: "https://example.test/opaque-windows-payload", Size: 4096, State: "uploaded"}
	zip := Asset{Name: assetNameForPlatform("2.0.0"), BrowserDownloadURL: "https://example.test/opaque-zip", Size: 1024}
	sums := Asset{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.test/opaque-sums", Size: 512}
	badRaw, badSums := raw, sums
	badRaw.BrowserDownloadURL = ""
	badSums.Size = 0
	for _, tc := range []struct {
		name   string
		assets []Asset
		want   string
	}{
		{"raw only", []Asset{raw, sums}, "2.0.0"},
		{"raw and ZIP", []Asset{zip, raw, sums}, "2.0.0"},
		{"invalid raw and valid ZIP", []Asset{badRaw, zip, sums}, "1.1.0"},
		{"duplicate raw and valid ZIP", []Asset{raw, zip, sums, raw}, "1.1.0"},
		{"missing checksums", []Asset{raw}, "1.1.0"},
		{"invalid checksum metadata", []Asset{raw, badSums}, "1.1.0"},
		{"only release page link", []Asset{sums}, "1.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signature := Asset{Name: checksumsSignatureAssetName, BrowserDownloadURL: "https://example.test/SHA256SUMS.sig", Size: 250}
			assets := append(append([]Asset(nil), tc.assets...), signature)
			releases := []map[string]any{
				{"tag_name": "v2.0.0", "html_url": "https://example.test/releases/tag/v2.0.0", "assets": assets},
				releaseFixture("v1.1.0", false),
			}
			useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/MoF-Crack/xdev/releases" {
					t.Errorf("unexpected discovery request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(releases)
			})
			ctx := restrictRawUpdateTestTransport(t, func(r *http.Request) bool { return r.URL.Host == "api.github.com" })
			got, err := Check(ctx, "1.0.0")
			if err != nil || !got.HasUpdate || got.LatestVersion != tc.want {
				t.Fatalf("Check=%+v error=%v want=%s", got, err, tc.want)
			}
			if tc.want == "2.0.0" {
				selected, err := SelectedAsset(got)
				if err != nil || selected != raw {
					t.Fatalf("Check and Apply disagree on the selected asset: %+v, %v", selected, err)
				}
			}
		})
	}
}

func TestApplyWindowsRawExecutableVerifiesBeforeReplacing(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Apply selects the executing platform")
	}
	const version = "1.0.0-rc.2"
	rawName := "xdev-windows-" + runtime.GOARCH + "-v" + version + ".exe"
	zipName := assetNameForPlatform(version)
	payload := []byte("MZ raw executable fixture, not a ZIP")
	for _, tc := range []struct{ name, wantErr string }{
		{"verified", ""},
		{"hash mismatch", "hash mismatch"},
		{"no exact raw checksum", "entry for"},
		{"truncated raw download", "download update:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe := updateTargetFixture(t)
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/opaque/12345":
					if tc.name == "truncated raw download" {
						w.Header().Set("Content-Length", strconv.Itoa(len(payload)+100))
					}
					_, _ = w.Write(payload)
				case "/opaque/checksums":
					hash := sha256.Sum256(payload)
					_, _ = fmt.Fprintf(w, "%x  %s\n", hash, zipName)
					if tc.name == "hash mismatch" {
						hash[0] ^= 0xff
					}
					if tc.name != "no exact raw checksum" {
						_, _ = fmt.Fprintf(w, "%x  %s\n", hash, rawName)
					}
				default:
					t.Errorf("unexpected download (ZIP fallback or guessed URL): %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			ctx := restrictRawUpdateTestTransport(t, func(r *http.Request) bool { return strings.HasPrefix(r.URL.String(), server.URL+"/") })
			result := &CheckResult{LatestVersion: version, Release: &ReleaseInfo{TagName: "v" + version, Assets: []Asset{
				{Name: zipName, BrowserDownloadURL: server.URL + "/fallback-zip", Size: 1},
				{Name: rawName, BrowserDownloadURL: server.URL + "/opaque/12345", Size: int64(len(payload)), State: "uploaded"},
				{Name: "SHA256SUMS", BrowserDownloadURL: server.URL + "/opaque/checksums", Size: 1},
			}}}
			var events []Progress
			err := ApplyToWithProgress(ctx, result, exe, func(p Progress) {
				events = append(events, p)
				if p.Phase == "extracting" {
					t.Error("raw executable was treated as an archive")
				}
				if p.Phase == "installing" {
					got, readErr := os.ReadFile(exe)
					if readErr != nil || string(got) != "old binary" {
						t.Errorf("raw executable published before installation: %q, %v", got, readErr)
					}
				}
			})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Apply error=%v want %q", err, tc.wantErr)
				}
				assertNoUpdateCompletion(t, events)
				assertOldUpdateTarget(t, exe)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var phases []string
			for _, p := range events {
				phases = append(phases, p.Phase)
			}
			if phases = slices.Compact(phases); !slices.Equal(phases, []string{"downloading", "verifying", "installing", "completed"}) {
				t.Fatalf("raw install phases=%v", phases)
			}
			last := events[len(events)-1]
			if last.Percent != 100 || last.DownloadedBytes != int64(len(payload)) || last.TotalBytes != int64(len(payload)) {
				t.Fatalf("raw install lost byte counts: %+v", last)
			}
			got, err := os.ReadFile(exe)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("installed bytes do not match the verified executable: %q, %v", got, err)
			}
		})
	}
}

// Even if code starts constructing URLs, these fixtures never use the network
// outside the trusted local TLS server or perform real GitHub requests.
func restrictRawUpdateTestTransport(t *testing.T, allow func(*http.Request) bool) context.Context {
	t.Helper()
	fixture := http.DefaultTransport
	return context.WithValue(context.Background(), networkContextKey{}, updateNetwork{transport: updateRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !allow(r) {
			t.Errorf("unexpected outbound update request: %s", r.URL)
			return nil, fmt.Errorf("unexpected outbound update request")
		}
		return fixture.RoundTrip(r)
	})})
}
