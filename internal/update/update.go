// Package update implements self-update for the xdev binary.
//
// It queries the GitHub Releases API for the latest version,
// compares it with the running version using semver, and if a newer
// version exists, downloads the appropriate binary and replaces the
// running executable in-place.
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

const (
	githubOwner = "MoF-Crack"
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
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	return ApplyTo(ctx, result, exe)
}

// ApplyTo downloads the new binary from the release assets and replaces
// the binary at exePath. 用于 GUI 自检更新：GUI 进程（xdev-gui）更新的是
// 同目录的 CLI 二进制（xdev.exe），而不是运行中的 GUI 壳自身。
func ApplyTo(ctx context.Context, result *CheckResult, exePath string) error {
	if result.Release == nil {
		return fmt.Errorf("no release info available")
	}

	assetName := assetNameForPlatform(result.LatestVersion)
	asset, ok := findAsset(result.Release, assetName)
	if !ok {
		return fmt.Errorf("no matching asset found for platform %s/%s (looked for %q)",
			runtime.GOOS, runtime.GOARCH, assetName)
	}

	log.Printf("[update] Downloading update: %s (%d bytes)", asset.Name, asset.Size)

	exe, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	log.Printf("[update] Target executable: %s", exe)

	// Download to a temp file in the same directory as the executable
	// to avoid cross-device rename issues.
	tmpDir := filepath.Dir(exe)
	tmpFile, err := os.CreateTemp(tmpDir, ".xdev-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	archivePath := tmpFile.Name()
	defer os.Remove(archivePath)

	if err := downloadAsset(ctx, asset.BrowserDownloadURL, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download update: %w", err)
	}
	tmpFile.Close()

	if err := verifySHA256(ctx, result.Release, asset.Name, archivePath); err != nil {
		return fmt.Errorf("SHA256 verification failed: %w", err)
	}

	binPath, err := extractUpdateBinary(archivePath, asset.Name, tmpDir)
	if err != nil {
		return fmt.Errorf("extract update binary: %w", err)
	}
	defer os.Remove(binPath)

	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0o755); err != nil {
			return fmt.Errorf("chmod new binary: %w", err)
		}
	}

	if err := replaceBinary(exe, binPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	// P2-A（第六轮审计 shim 链）：publish 后立即刷新 shims/ 下现存的分发器副本。
	// replaceBinaryWindows 走 rename 舞步发布新 inode——工具 Activate 时刻硬链接
	// 的旧分发器仍持有旧 inode（缺 PATH 透传/别名修复，遮蔽系统真实工具）。
	// 此前刷新只挂在 GUI domReady 的 EnsureSelfInstalled 上：`xdev self update`、
	// TUI 自检更新、GUI 手动更新全部不刷新，CLI-only 主机永久滞后；GUI 主机
	// 也要等下一次启动才收敛。exe 此刻指向新二进制（rename 已完成），作为刷新
	// 源是完整镜像。刷新失败只告警——更新本体已成功，刷新由下次启动兜底。
	refreshShimDispatchers(exe)

	log.Printf("[update] Successfully updated to %s", result.LatestVersion)
	return nil
}

// refreshShimDispatchers 把 home 下 shims/ 的现存分发器副本重指到 selfPath。
//
// 第七轮审计 F-47：不能硬拼 `<home>/shims` —— `core.shim_dir` 是**受支持的可配置项**
// （internal/cli/config.go 的 `config set core.shim_dir` 可写），GUI 侧的同一动作走
// cfg.Core.ShimDir。旧实现硬拼默认路径，于是改过 shim_dir 的机器上：刷新落在不存在
// 的目录 → shim.RefreshDispatchers 返回 (0,0) **静默成功** → 真实 shim 目录里的旧
// inode 分发器永不更新，正是第六轮 P2-A 要修的「分发器版本永久滞后、遮蔽真实工具」。
//
// 现在优先读配置里的 shim_dir，读不到再回落 `<home>/shims`；两者都拿不到才跳过。
// 刷新失败/目录不存在时给出可诊断的告警，而不是静默。
func refreshShimDispatchers(selfPath string) {
	shimDir := resolveShimDir()
	if shimDir == "" {
		log.Printf("[update] WARNING: cannot resolve shim dir (no config and no home) — shim dispatchers not refreshed; run `xdev doctor` to repair")
		return
	}
	if fi, err := os.Stat(shimDir); err != nil || !fi.IsDir() {
		log.Printf("[update] WARNING: shim dir %s does not exist or is not a directory — shim dispatchers not refreshed", shimDir)
		return
	}
	refreshed, failed := shim.RefreshDispatchers(shimDir, selfPath)
	if refreshed > 0 || failed > 0 {
		log.Printf("[update] shim dispatchers refreshed=%d failed=%d (dir=%s)", refreshed, failed, shimDir)
	}
	if failed > 0 {
		log.Printf("[update] WARNING: %d shim dispatcher(s) could not be refreshed in %s", failed, shimDir)
	}
}

// resolveShimDir 返回本次更新应当刷新的 shim 目录：优先配置里的 core.shim_dir，
// 回落 `<home>/shims`（F-47）。
func resolveShimDir() string {
	if cfg, err := config.Load(""); err == nil && cfg != nil {
		if d := cfg.Core.ShimDir; d != "" {
			return d
		}
	}
	home := config.ResolveHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "shims")
}

// fetchLatestRelease calls the GitHub API to get the latest release.
func fetchLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)

	// Security (M-2 fix): the release JSON gates the self-update binary
	// replacement — this metadata client must use the same shared redirect
	// policy as downloadAsset/downloadToMemory (blocks HTTPS→HTTP downgrades
	// and redirects to private hosts).
	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: util.SecureRedirectCheck(10),
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "xdev-self-update")
	// GitHub token 复用 remote 的统一解析（显式配置/env/注册表/GH_TOKEN），
	// 不再自行弱化读取（Spec 034 审计消除重复）。
	if token := remote.EffectiveGitHubToken(); token != "" {
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

// assetNameForPlatform is the CI archive name for this OS/arch and version.
// Windows: xdev-windows-amd64-v0.3.2.zip
// Unix:    xdev-linux-amd64-v0.3.2.tar.gz
func assetNameForPlatform(version string) string {
	ver := strings.TrimPrefix(version, "v")
	base := fmt.Sprintf("xdev-%s-%s-v%s", runtime.GOOS, runtime.GOARCH, ver)
	if runtime.GOOS == "windows" {
		return base + ".zip"
	}
	return base + ".tar.gz"
}

func binaryNameInArchive() string {
	if runtime.GOOS == "windows" {
		return "xdev.exe"
	}
	return "xdev"
}

// extractUpdateBinary unpacks the CLI binary from a zip or tar.gz.
// Only a root-level xdev / xdev.exe is accepted (zip-slip: no "..", no dirs).
func extractUpdateBinary(archivePath, archiveName, destDir string) (string, error) {
	want := binaryNameInArchive()
	// 第六轮审计 P2-B（shim 链）：唯名暂存文件（票 31 / P1-24 的既定教训：
	// 固定 <name> 会被并发写者互相截断）。GUI 自动更新（每次启动 ~2s 触发）与
	// CLI `xdev self update` 可能同时跑到这里——固定名 .xdev-update-bin 下，
	// 后写者的 O_TRUNC 截断前者的在途提取，两个 io.Copy 写同一文件后谁先
	// rename 谁把混合损坏字节发布成 home 主二进制；而损坏的 home 副本无法
	// 自愈（stale.go probe 失败 → 永久跳过，只有「完全缺失」分支才重建）。
	// 唯名后各进程写各自的暂存文件，publish 点（rename）保持原子。
	tmp, err := os.CreateTemp(destDir, ".xdev-update-bin-*")
	if err != nil {
		return "", fmt.Errorf("create update staging file: %w", err)
	}
	out := tmp.Name()
	// 提取器以 O_CREATE|O_TRUNC 打开同一路径，这里关闭即可（留空文件无碍）。
	if err := tmp.Close(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("create update staging file: %w", err)
	}
	lower := strings.ToLower(archiveName)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		if err := extractNamedZipMember(archivePath, want, out); err != nil {
			os.Remove(out)
			return "", err
		}
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		if err := extractNamedTarGzMember(archivePath, want, out); err != nil {
			os.Remove(out)
			return "", err
		}
	default:
		os.Remove(out)
		return "", fmt.Errorf("unsupported update archive %q", archiveName)
	}
	return out, nil
}

func extractNamedZipMember(zipPath, wantName, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if !safeArchiveMember(name, wantName) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeUpdateFile(dest, rc)
		rc.Close()
		return err
	}
	return fmt.Errorf("archive missing %q", wantName)
}

func extractNamedTarGzMember(tarPath, wantName, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		if !safeArchiveMember(name, wantName) {
			continue
		}
		return writeUpdateFile(dest, tr)
	}
	return fmt.Errorf("archive missing %q", wantName)
}

func safeArchiveMember(name, want string) bool {
	name = strings.TrimPrefix(name, "./")
	if name != want {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	return true
}

func writeUpdateFile(dest string, r io.Reader) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(r, 200<<20)) // 200 MiB cap
	return err
}

// findAsset searches for an asset matching the given name exactly.
// Spec 035 F6: the partial-match (GOOS/GOARCH substring) fallback was
// removed — it could select an unintended archive (e.g. the GUI bundle
// "xdev-gui-windows-amd64-v1.0.zip") when the exact name is missing.
// CI asset names match assetNameForPlatform verbatim on every platform.
func findAsset(release *ReleaseInfo, name string) (Asset, bool) {
	for _, a := range release.Assets {
		if a.Name == name {
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
		Timeout:       10 * time.Minute,
		CheckRedirect: util.SecureRedirectCheck(10),
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

	// 票 32（P1-25 / E-06）崩溃自愈：上一次更新若在「currentExe → .old」
	// 之后、「新二进制 → currentExe」之前中断（断电/杀进程/蓝屏），
	// currentExe 会永久缺失而 .old 完好。旧实现紧接着就 os.Remove(backup)
	// ——把唯一的可恢复副本删掉，再走 copyAndReplace 新建文件，崩溃被
	// 固化成「二进制丢失」。现在先把 .old 恢复回 currentExe 再继续。
	if healed, err := restoreFromBackup(currentExe); err != nil {
		log.Printf("[update] WARNING: recover %s from %s: %v", currentExe, backup, err)
	} else if healed {
		log.Printf("[update] recovered %s from an interrupted previous update", currentExe)
	}

	// 票 32（E-06）：清理上一次中断留下的 .new 半成品（copyAndReplace 的
	// 中间文件），避免把陈旧内容当成新二进制发布。
	_ = os.Remove(currentExe + ".new")

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

// restoreFromBackup renames currentExe+".old" back to currentExe when the live
// binary is missing (票 32 / P1-25 / E-06).  Reports whether a restore
// happened.  A missing live binary together with a present backup is the
// signature of an interrupted update; without this step the only recoverable
// copy gets deleted by the caller.
func restoreFromBackup(currentExe string) (bool, error) {
	if _, err := os.Stat(currentExe); err == nil {
		return false, nil // 活二进制在位：无需自愈
	} else if !os.IsNotExist(err) {
		return false, err
	}
	backup := currentExe + ".old"
	fi, err := os.Stat(backup)
	if err != nil {
		return false, nil // 没有可恢复的副本
	}
	if fi.IsDir() {
		return false, nil
	}
	if err := os.Rename(backup, currentExe); err != nil {
		return false, err
	}
	return true, nil
}

// copyAndReplace copies the new binary over the current one.
// This is a fallback for Windows when rename fails.
//
// 票 32（E-06）：旧实现直接以 O_TRUNC 打开 currentExe 覆盖写——中途失败
// （磁盘满/被中断）会留下半截损坏的二进制，而且运行中的 exe 更是写不通。
// 现在改为「写 <currentExe>.new → rename 覆盖」：rename 是原子发布点，
// 失败时 currentExe 保持原样（或缺失由 restoreFromBackup 自愈），
// 只留下一个可被启动清理的 .new 中间文件。
func copyAndReplace(currentExe, newBinary string) error {
	staging := currentExe + ".new"
	_ = os.Remove(staging)

	src, err := os.Open(newBinary)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(staging, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(staging)
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		os.Remove(staging)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(staging)
		return err
	}
	if err := os.Rename(staging, currentExe); err != nil {
		os.Remove(staging)
		return err
	}
	return nil
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
		// Format: "<hash>  <filename>" — the standard sha256sum output
		// uses exactly two spaces (CI generates it via 'sha256sum *').
		// Spec 035 F7: the single-space fallback was removed; lenient
		// parsing is unnecessary on this security-sensitive path.
		parts := strings.SplitN(line, "  ", 2)
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
		Timeout:       30 * time.Second,
		CheckRedirect: util.SecureRedirectCheck(10),
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
