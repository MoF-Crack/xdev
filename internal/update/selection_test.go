package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
