// Package update implements self-update for the xdev binary.
//
// It queries the GitHub Releases API for the latest version,
// compares it with the running version using semver, and if a newer
// version exists, downloads the appropriate binary and replaces the
// running executable in-place.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

const (
	githubOwner = "xmdemt"
	githubRepo  = "xdev"
)

// ReleaseInfo represents a GitHub release.
type ReleaseInfo struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckResult holds the outcome of a version check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	Release        *ReleaseInfo
}

// Check queries the GitHub Releases API and compares the latest
// tag with the current version. Returns a CheckResult indicating
// whether an update is available.
func Check(ctx context.Context, currentVersion string) (*CheckResult, error) {
	log.Printf("[update] Checking for updates: current=%s", currentVersion)

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		log.Printf("[update] ERROR: failed to fetch latest release: %v", err)
		return nil, fmt.Errorf("check for updates: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")

	log.Printf("[update] Latest release: %s (current: %s)", latest, current)

	hasUpdate := semverLess(current, latest)
	if hasUpdate {
		log.Printf("[update] Update available: %s -> %s", current, latest)
	} else {
		log.Printf("[update] Already up to date")
	}

	return &CheckResult{
		CurrentVersion: current,
		LatestVersion:  latest,
		HasUpdate:      hasUpdate,
		Release:        release,
	}, nil
}

// Apply downloads the new binary from the release assets and replaces
// the currently running executable.
func Apply(ctx context.Context, result *CheckResult) error {
	if result.Release == nil {
		return fmt.Errorf("no release info available")
	}

	assetName := assetNameForPlatform()
	asset, ok := findAsset(result.Release, assetName)
	if !ok {
		return fmt.Errorf("no matching asset found for platform %s/%s (looked for %q)",
			runtime.GOOS, runtime.GOARCH, assetName)
	}

	log.Printf("[update] Downloading update: %s (%d bytes)", asset.Name, asset.Size)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	log.Printf("[update] Current executable: %s", exe)

	// Download to a temp file in the same directory as the executable
	// to avoid cross-device rename issues.
	tmpDir := filepath.Dir(exe)
	tmpFile, err := os.CreateTemp(tmpDir, ".xdev-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := downloadAsset(ctx, asset.BrowserDownloadURL, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download update: %w", err)
	}
	tmpFile.Close()

	// Verify SHA256 checksum of the downloaded binary.
	if err := verifySHA256(ctx, result.Release, asset.Name, tmpPath); err != nil {
		return fmt.Errorf("SHA256 verification failed: %w", err)
	}

	// Make the new binary executable (Unix)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			return fmt.Errorf("chmod new binary: %w", err)
		}
	}

	// Replace the running binary.
	if err := replaceBinary(exe, tmpPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	log.Printf("[update] Successfully updated to %s", result.LatestVersion)
	return nil
}

// fetchLatestRelease calls the GitHub API to get the latest release.
func fetchLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "xdev-self-update")
	// Optional: set GITHUB_TOKEN for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release JSON: %w", err)
	}
	return &release, nil
}

// assetNameForPlatform returns the expected asset filename for the
// current OS/architecture.
func assetNameForPlatform() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("xdev-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// findAsset searches for an asset matching the given name pattern.
func findAsset(release *ReleaseInfo, name string) (Asset, bool) {
	for _, a := range release.Assets {
		if a.Name == name {
			return a, true
		}
	}
	// Fallback: try partial match
	for _, a := range release.Assets {
		if strings.Contains(a.Name, runtime.GOOS) && strings.Contains(a.Name, runtime.GOARCH) {
			return a, true
		}
	}
	return Asset{}, false
}

// downloadAsset downloads a release asset to the given writer.
// Security (N-9): enforce HTTPS and block downgrade redirects to
// protect self-update from MITM.
func downloadAsset(ctx context.Context, url string, w io.Writer) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("self-update download URL must use HTTPS, got %q", url)
	}
	client := &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("insecure redirect during self-update: %s → %s", via[0].URL, req.URL)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "xdev-self-update")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

// replaceBinary replaces the current executable with the new one.
// On Windows, the running binary is locked, so we rename the old one
// first and then rename the new one into place.
func replaceBinary(currentExe, newBinary string) error {
	if runtime.GOOS == "windows" {
		return replaceBinaryWindows(currentExe, newBinary)
	}
	// Unix: atomic rename
	return os.Rename(newBinary, currentExe)
}

// replaceBinaryWindows handles the Windows case where the running
// binary is locked. We rename the old binary to a backup name,
// then rename the new one into place. The old file will be cleaned
// up on the next launch.
func replaceBinaryWindows(currentExe, newBinary string) error {
	backup := currentExe + ".old"

	// Remove any leftover backup from a previous update
	_ = os.Remove(backup)

	// Rename current binary to backup
	if err := os.Rename(currentExe, backup); err != nil {
		log.Printf("[update] WARNING: failed to rename current binary: %v", err)
		// On Windows, if rename fails, try copy-based approach
		return copyAndReplace(currentExe, newBinary)
	}

	// Rename new binary to the target location
	if err := os.Rename(newBinary, currentExe); err != nil {
		// Rollback: restore the old binary
		log.Printf("[update] ERROR: failed to rename new binary, rolling back: %v", err)
		_ = os.Rename(backup, currentExe)
		return err
	}

	// Schedule cleanup of the old binary (best effort)
	go func() {
		time.Sleep(3 * time.Second)
		_ = os.Remove(backup)
	}()

	return nil
}

// copyAndReplace copies the new binary over the current one.
// This is a fallback for Windows when rename fails.
func copyAndReplace(currentExe, newBinary string) error {
	src, err := os.Open(newBinary)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(currentExe, os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// semverLess reports whether version a is less than version b
// using a simple major.minor.patch comparison.
func semverLess(a, b string) bool {
	return util.CompareSemver(a, b) < 0
}

// verifySHA256 checks the downloaded binary against SHA256SUMS
// from the release assets.  If no checksums asset is found, it
// logs a warning and proceeds without failing.
func verifySHA256(ctx context.Context, release *ReleaseInfo, assetName, filePath string) error {
	// Look for a SHA256SUMS asset in the release.
	var sumsAsset *Asset
	for i := range release.Assets {
		name := strings.ToLower(release.Assets[i].Name)
		if name == "sha256sums.txt" || name == "sha256sums" {
			a := release.Assets[i]
			sumsAsset = &a
			break
		}
	}

	if sumsAsset == nil {
		// Security (H-11): Self-update is the most security-critical download
		// (it replaces the running binary). Refuse to update without hash
		// verification to prevent a MITM attacker from serving a malicious binary.
		return fmt.Errorf("no SHA256SUMS asset found in release — refusing to update without hash verification (security)")
	}

	log.Printf("[update] Downloading SHA256SUMS from %s", sumsAsset.BrowserDownloadURL)

	// Download the checksums file to memory.
	sumsData, err := downloadToMemory(ctx, sumsAsset.BrowserDownloadURL)
	if err != nil {
		// Security (H-11): Failed to download checksums — refuse to update
		return fmt.Errorf("failed to download SHA256SUMS: %w — refusing to update without hash verification", err)
	}

	// Parse the SHA256SUMS file.  Format: "<hash>  <filename>" per line.
	expectedHash, err := parseSHA256SUMS(string(sumsData), assetName)
	if err != nil {
		// Security (H-11): Checksums file doesn't contain our asset — refuse to update
		return fmt.Errorf("could not find %q in SHA256SUMS: %w — refusing to update without hash verification", assetName, err)
	}

	// Compute the SHA256 of the downloaded binary.
	actualHash, err := util.FileSHA256(filePath)
	if err != nil {
		return fmt.Errorf("compute SHA256 of %s: %w", filePath, err)
	}

	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s", assetName, expectedHash, actualHash)
	}

	log.Printf("[update] SHA256 verification passed for %s", assetName)
	return nil
}

// parseSHA256SUMS parses a SHA256SUMS file and returns the hash
// for the given filename.
func parseSHA256SUMS(content, filename string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<hash>  <filename>" or "<hash> <filename>"
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == filename {
			return strings.TrimSpace(parts[0]), nil
		}
		// Also try single-space separator
		parts = strings.SplitN(line, " ", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == filename {
			return strings.TrimSpace(parts[0]), nil
		}
	}
	return "", fmt.Errorf("entry for %q not found", filename)
}

// downloadToMemory downloads a URL and returns its body as bytes.
// Security (N-9): enforce HTTPS and block downgrade redirects.
func downloadToMemory(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("download URL must use HTTPS, got %q", url)
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("insecure redirect: %s → %s", via[0].URL, req.URL)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xdev-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max for checksums
}
