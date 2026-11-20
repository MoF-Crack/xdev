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

// SelectedAsset resolves the canonical update payload for this executable.
// Windows prefers the direct executable; only releases without it use the ZIP.
// Check, GUI previews and Apply all select from the same release metadata.
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
	if goos == "windows" {
		name := rawExeNameForTarget(result.LatestVersion, goarch)
		var raw Asset
		for _, asset := range result.Release.Assets {
			if asset.Name != name {
				continue
			}
			if raw.Name != "" {
				return Asset{}, fmt.Errorf("duplicate update executable %q", name)
			}
			raw = asset
		}
		// A bad current-format asset cannot be hidden by the legacy archive.
		if raw.Name != "" {
			if !usableAsset(raw) {
				return Asset{}, fmt.Errorf("update executable %q is unavailable or has invalid download metadata", name)
			}
			return raw, nil
		}
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

func rawExeNameForTarget(version, goarch string) string {
	return strings.TrimSuffix(assetNameForTarget(version, "windows", goarch), ".zip") + ".exe"
}
