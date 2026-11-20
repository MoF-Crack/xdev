package update

import (
	"fmt"
	"runtime"
	"strings"
)

// TargetPlatform is the platform of this executable, including its architecture.
// It is authoritative for both in-app installation and an optional manual link;
// browsers and frontend user-agent strings never choose update packages.
func TargetPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// SelectedAsset resolves the one canonical archive this executable can install.
// GUI previews and Apply use this same selector so a manual link cannot point to
// another platform while the primary action installs the current platform.
func SelectedAsset(result *CheckResult) (Asset, error) {
	return selectedAssetForTarget(result, runtime.GOOS, runtime.GOARCH)
}

func selectedAssetForTarget(result *CheckResult, goos, goarch string) (Asset, error) {
	if result == nil || result.Release == nil {
		return Asset{}, fmt.Errorf("no release info available")
	}
	if !validUpdateVersion(strings.TrimPrefix(result.LatestVersion, "v")) {
		return Asset{}, fmt.Errorf("invalid update version %q", result.LatestVersion)
	}
	name := assetNameForTarget(result.LatestVersion, goos, goarch)
	asset, found := findAsset(result.Release, name)
	if !found {
		return Asset{}, fmt.Errorf("no unique matching update archive for %s/%s (looked for %q)", goos, goarch, name)
	}
	if !usableAsset(asset) {
		return Asset{}, fmt.Errorf("update archive %q is unavailable or has invalid download metadata", name)
	}
	return asset, nil
}
