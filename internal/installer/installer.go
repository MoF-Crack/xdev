// Package installer is the high-level "install a tool" workflow engine.
//
// The Installer type orchestrates the full lifecycle of a tool version:
//
//  1. Resolve the requested version spec (e.g. "^3.12" → "3.12.4")
//  2. Pick the right archive for the current OS/arch via the Plugin interface
//  3. Apply mirror rewriting for faster downloads (China-optimized by default)
//  4. Download to <cacheDir>/<tool>-<ver>.<fmt> with SHA-256 verification
//  5. Extract to <installDir>/<tool>/<ver>/ (supports zip, tar.gz, 7z, zstd)
//  6. Run the plugin's PostInstall hook
//  7. Activate the version:
//     a. Create/update the current/<tool> junction or symlink
//     b. Install shim binaries in shims/ (shim mode) or add to PATH (direct mode)
//     c. Write user-level environment variables (e.g. GOROOT, JAVA_HOME)
//     d. Broadcast WM_SETTINGCHANGE on Windows
//  8. Update the project lockfile (.xdev.lock)
//
// # Environment Write Control
//
// The noEnvWrite flag (set via SetNoEnvWrite, connected to --no-path-fix)
// controls whether Activate writes to the Windows registry. When true,
// setUserEnvVars, AddCurrentBinPaths, and BroadcastEnvironmentChange are
// all skipped. This is essential for CI/CD and integration tests that must
// not pollute the host environment.
//
// # Atomic Operations
//
// File writes in dependent packages (config, lockfile) use the tmp+rename
// pattern to prevent corruption. The Installer itself does not write files
// directly except through these packages.
package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/extractor"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/mirror"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/signer"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// TOFUWarningCallback is called when an archive with an empty or placeholder
// SHA-256 is downloaded and the actual hash is computed. The callback can be
// used by the GUI to emit a Wails event for the frontend warning dialog.
type TOFUWarningCallback func(tool, version, osName, arch, sha256 string)

// Installer is a high-level workflow runner that coordinates tool installation,
// activation, and uninstallation. It holds references to the global config,
// mirror table, and plugin registry, and delegates OS-specific and format-
// specific work to the extractor, downloader, shim, and env packages.
type Installer struct {
	cfg        *config.Config   // Global xdev configuration
	mir        *mirror.Table    // Mirror URL rewriting and speed testing
	reg        *plugin.Registry // Plugin registry for tool lookup
	noEnvWrite bool             // When true, skip registry env-var writes (set by --no-path-fix)
	runner     execRunner       // Runs installer binaries/scripts (mockable in tests)

	mirrorCacheOnce sync.Once          // Ensures mirror health cache is loaded once
	mirrorCache     *MirrorHealthCache // Lazy-loaded mirror health cache

	tofuCB TOFUWarningCallback // Optional callback for TOFU warnings (set by GUI)
}

// New constructs an Installer with the given config and plugin registry.
// It also initializes the download audit log path under <XMDevDir>/logs/.
func New(cfg *config.Config, reg *plugin.Registry) *Installer {
	// Initialize audit log path
	if cfg.Core.XMDevDir != "" {
		downloader.SetAuditLogPath(filepath.Join(cfg.Core.XMDevDir, "logs", "download_audit.jsonl"))
	}
	return &Installer{
		cfg:    cfg,
		mir:    mirror.FromConfig(cfg),
		reg:    reg,
		runner: osExecRunner{},
	}
}

// SetNoEnvWrite controls whether Activate writes user-level environment
// variables (e.g. JAVA_HOME, GOROOT) to the Windows registry.  When true,
// the registry and WM_SETTINGCHANGE broadcast are skipped — used by
// --no-path-fix to leave the host environment untouched during tests.
func (i *Installer) SetNoEnvWrite(skip bool) { i.noEnvWrite = skip }

// execRunner abstracts running an external command (installer binary or
// script) so that the routing logic can be tested without actually
// executing anything.
type execRunner interface {
	Run(ctx context.Context, name string, args []string, env []string, dir string) error
}

// osExecRunner is the production execRunner backed by os/exec.
type osExecRunner struct{}

func (osExecRunner) Run(ctx context.Context, name string, args []string, env []string, dir string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// 隐藏子进程的控制台窗口（Windows），避免 rustup 等安装器弹出
	// 独立命令行窗口打断用户操作（与 getVersion 等其他调用一致）。
	util.HideWindow(cmd)
	return cmd.Run()
}

// envHasKey 报告 env 列表（"KEY=value" 形式）中是否已存在指定键。
// 用于判断子进程环境是否已显式设置代理等变量，避免覆盖用户配置。
func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// SetExecRunner injects a custom execRunner (used by tests to verify
// routing without executing real commands).
func (i *Installer) SetExecRunner(r execRunner) { i.runner = r }

// SetTOFUWarningCallback registers a callback that is invoked when an archive
// with an empty or placeholder SHA-256 is downloaded and the actual hash is
// computed and written to the on-disk manifest. The GUI uses this to emit a
// Wails event for the frontend warning dialog.
func (i *Installer) SetTOFUWarningCallback(cb TOFUWarningCallback) { i.tofuCB = cb }

// ProgressPhase represents the current phase of installation for progress reporting.
// Used by ProgressCallback to inform the UI which stage is active.
type ProgressPhase int

const (
	PhaseDownloading ProgressPhase = iota // Downloading the archive
	PhaseExtracting                       // Extracting the archive to disk
	PhaseActivating                       // Creating symlinks, shims, and env vars
)

// ProgressCallback is called periodically during installation to report progress.
// phase indicates the current stage, percent is 0-100, speed and eta are
// human-readable strings (e.g. "5.2 MB/s", "2m30s") and may be empty
// during non-download phases.
type ProgressCallback func(phase ProgressPhase, percent float64, speed string, eta string)

// InstallResult is the outcome of a single install.
// InstallResult is the outcome of a single install operation.
// It captures both the resolved version and the original user spec so
// that callers (CLI lockfile writer) can persist reproducible pins.
type InstallResult struct {
	Tool        string // Tool name (e.g. "go")
	Version     string // Resolved concrete version (e.g. "3.12.0")
	Spec        string // Original user spec (e.g. "^3.12", "latest"); empty if unknown
	URL         string // Archive upstream URL (pre-mirror), for lockfile pinning
	SHA256      string // Archive SHA-256 (may be empty when manifest omits it)
	InstallPath string // Local installation directory
	Activated   bool   // Whether the version was set as active

	// TOFUWarning is set when the archive had an empty or placeholder SHA-256
	// and the actual hash was computed and written to the on-disk manifest.
	// The frontend can check this field to show a warning dialog.
	TOFUWarning string `json:"tofuWarning,omitempty"` // computed SHA-256, non-empty when TOFU occurred

	// ActivationError is set when the version was installed successfully
	// but activation (shim/PATH update) failed — typically because the
	// currently active version's binary is locked by a running process.
	// The version is fully installed and can be activated later via
	// `xdev use <tool>@<version>` once the conflicting process exits.
	ActivationError string `json:"activationError,omitempty"`
}

// InstallOptions controls optional behaviour of the install workflow.
type InstallOptions struct {
	// NoActivate, when true, skips the Activate() step so that the
	// tool is downloaded and extracted but not wired into the shim
	// farm or the "current" symlink. Useful for pre-fetching versions.
	NoActivate bool

	// BatchIndex is the 1-based index of this tool in a batch install.
	// When > 0, the progress display will show "[N/M]" prefix.
	BatchIndex int

	// BatchTotal is the total number of tools in a batch install.
	BatchTotal int

	// Force, when true, removes any existing install directory (complete
	// or incomplete) and reinstalls from scratch. Without Force, a
	// complete install is only re-activated, and an incomplete install
	// returns ErrIncompleteInstall instead of being silently activated.
	Force bool
}

// ErrIncompleteInstall is the sentinel error wrapped by
// IncompleteInstallError. It is returned when an install directory exists
// but fails VerifyInstallIntegrity and no Force reinstall was requested.
var ErrIncompleteInstall = errors.New("incomplete install")

// IncompleteInstallError describes an existing-but-incomplete installation.
// Callers can use errors.Is(err, ErrIncompleteInstall) to detect it and,
// for example, prompt the user to repair the installation.
type IncompleteInstallError struct {
	Tool    string
	Version string
	Path    string
	Reason  string
	Missing []string
}

func (e *IncompleteInstallError) Error() string {
	msg := fmt.Sprintf("%s@%s is incompletely installed at %s: %s", e.Tool, e.Version, e.Path, e.Reason)
	if len(e.Missing) > 0 {
		msg += " (missing: " + strings.Join(e.Missing, ", ") + ")"
	}
	return msg
}

// Is reports whether target is ErrIncompleteInstall.
func (e *IncompleteInstallError) Is(target error) bool {
	return target == ErrIncompleteInstall
}

// CheckProxyConfigured reports whether a proxy is configured for downloads,
// either via xdev config (GetProxyURL) or the standard environment variables
// (HTTP_PROXY/HTTPS_PROXY/http_proxy/https_proxy). Used to warn users when an
// archive requires a proxy but none is available (Spec 021 D7 — shared by
// CLI downloadWithProgress and the GUI install path).
func CheckProxyConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if cfg.GetProxyURL("https") != "" {
		return true
	}
	return os.Getenv("HTTP_PROXY") != "" ||
		os.Getenv("HTTPS_PROXY") != "" ||
		os.Getenv("http_proxy") != "" ||
		os.Getenv("https_proxy") != ""
}

// Install performs the full install workflow for p at the given
// version spec (e.g. "latest" or "3.12.0") without progress reporting.
// For progress support, use InstallWithProgress.
func (i *Installer) Install(ctx context.Context, p plugin.Plugin, spec string) (*InstallResult, error) {
	return i.InstallWithProgress(ctx, p, spec, nil, InstallOptions{})
}

// BatchInstallItem represents a single tool to install in a batch operation.
// Version may be empty to use the tool's default version.
type BatchInstallItem struct {
	ToolName string // e.g. "python"
	Version  string // resolved version or spec; "" = use DefaultVersion
}

// BatchInstallResult records the outcome of a single batch install attempt.
type BatchInstallResult struct {
	Tool        string
	Version     string
	InstallPath string // local installation directory (filled for "installed" status)
	Status      string // "installed" | "already_installed" | "unsupported" | "failed"
	Error       error  // non-nil when Status is "failed"
}

// InstallMany installs a batch of tools. For each item it:
//  1. Looks up the plugin in the registry (Status="unsupported" if not found)
//  2. Checks if the version is already installed (Status="already_installed" if so)
//  3. Calls InstallWithProgress (Status="installed" | "failed")
//
// Returns one BatchInstallResult per item, in the same order as items.
// The caller is responsible for any post-install steps (activation, cleanup)
// and for formatting output.
func (i *Installer) InstallMany(ctx context.Context, items []BatchInstallItem) []BatchInstallResult {
	results := make([]BatchInstallResult, len(items))
	for idx, item := range items {
		p, ok := i.reg.Get(item.ToolName)
		if !ok {
			results[idx] = BatchInstallResult{
				Tool:   item.ToolName,
				Status: "unsupported",
				Error:  fmt.Errorf("unsupported tool %q", item.ToolName),
			}
			continue
		}

		ver := item.Version
		if ver == "" {
			ver = p.DefaultVersion()
		}

		// Check if already installed. Unlike the old plain directory check,
		// an existing-but-incomplete install is NOT treated as installed —
		// same decision as InstallWithProgress (Spec 021 D8).
		installPath := p.InstallDir(i.cfg.Core.InstallDir, ver)
		if fi, err := os.Stat(installPath); err == nil && fi.IsDir() {
			integrity := VerifyInstallIntegrity(p, installPath)
			if integrity.OK {
				results[idx] = BatchInstallResult{
					Tool:        item.ToolName,
					Version:     ver,
					InstallPath: installPath,
					Status:      "already_installed",
				}
				continue
			}
			// 不完整安装：与 InstallWithProgress 一致，返回 ErrIncompleteInstall。
			log.Printf("[install] WARNING: %s@%s is incompletely installed at %s: %s",
				item.ToolName, ver, installPath, integrity.Reason)
			results[idx] = BatchInstallResult{
				Tool:        item.ToolName,
				Version:     ver,
				InstallPath: installPath,
				Status:      "failed",
				Error: &IncompleteInstallError{
					Tool:    item.ToolName,
					Version: ver,
					Path:    installPath,
					Reason:  integrity.Reason,
					Missing: integrity.Missing,
				},
			}
			continue
		}

		// Install
		res, err := i.InstallWithProgress(ctx, p, ver, nil, InstallOptions{
			BatchIndex: idx + 1,
			BatchTotal: len(items),
		})
		if err != nil {
			results[idx] = BatchInstallResult{
				Tool:    item.ToolName,
				Version: ver,
				Status:  "failed",
				Error:   err,
			}
			continue
		}

		results[idx] = BatchInstallResult{
			Tool:        item.ToolName,
			Version:     res.Version,
			InstallPath: res.InstallPath,
			Status:      "installed",
		}
	}
	return results
}

// InstallWithProgress performs the full install workflow with optional progress reporting.
// The workflow is:
//  1. Resolve version spec via plugin
//  2. Select archive for current OS/arch
//  3. Check if already installed: if intact and no Force, just activate;
//     if incomplete and no Force, return ErrIncompleteInstall; if Force,
//     remove the existing directory and reinstall
//  4. Download with mirror support and SHA-256 verification
//  5. Extract archive to install directory
//  6. Run PostInstall hook
//  7. Activate (create junction, shims, env vars)
//
// If onProgress is nil, no progress callbacks are made.
func (i *Installer) InstallWithProgress(ctx context.Context, p plugin.Plugin, spec string, onProgress ProgressCallback, opts InstallOptions) (*InstallResult, error) {
	log.Printf("[install] Starting install: tool=%s spec=%s", p.Name(), spec)

	// Check required plugin before resolving version
	if requires := p.RequiredPlugin(); len(requires) > 0 {
		for _, depName := range requires {
			depPlugin, ok := i.reg.Get(depName)
			if !ok {
				return nil, fmt.Errorf("required plugin %q is not supported; cannot install %q", depName, p.Name())
			}
			depPath := depPlugin.InstallDir(i.cfg.Core.InstallDir, depPlugin.DefaultVersion())
			if fi, err := os.Stat(depPath); err != nil || !fi.IsDir() {
				return nil, fmt.Errorf("required plugin %q is not installed; run 'xdev install %s' first before installing %q", depName, depName, p.Name())
			}
		}
	}

	ver, err := p.ResolveVersion(spec)
	if err != nil {
		log.Printf("[install] ERROR: resolve version failed for %s@%s: %v", p.Name(), spec, err)
		return nil, err
	}
	log.Printf("[install] Resolved version: %s -> %s", spec, ver)

	arch, err := p.ArchiveFor(ver)
	if err != nil {
		log.Printf("[install] ERROR: archive resolution failed for %s@%s: %v", p.Name(), ver, err)
		return nil, err
	}
	log.Printf("[install] Archive: url=%s format=%s os=%s arch=%s", arch.URL, arch.Format, arch.OS, arch.Arch)

	installPath := p.InstallDir(i.cfg.Core.InstallDir, ver)
	log.Printf("[install] Install path: %s", installPath)

	// Capture archive metadata once for lockfile pinning. We record the
	// upstream URL (pre-mirror) and manifest SHA-256 so downstream callers
	// (the CLI lockfile writer) can persist reproducible version pins.
	resSpec := spec
	resURL := arch.URL
	resSHA := arch.SHA256

	if _, err := os.Stat(installPath); err == nil {
		// Already installed. Verify integrity: an incomplete install must
		// not be silently activated — return ErrIncompleteInstall so the
		// caller can prompt the user for a same-version repair.
		integrity := VerifyInstallIntegrity(p, installPath)
		if !integrity.OK && !opts.Force {
			log.Printf("[install] WARNING: %s@%s is incompletely installed at %s: %s",
				p.Name(), ver, installPath, integrity.Reason)
			return nil, &IncompleteInstallError{
				Tool:    p.Name(),
				Version: ver,
				Path:    installPath,
				Reason:  integrity.Reason,
				Missing: integrity.Missing,
			}
		}
		if opts.Force {
			// Force reinstall: remove the existing directory (complete or
			// incomplete) and fall through to a full download + install.
			log.Printf("[install] Force reinstall requested for %s@%s — removing existing install dir", p.Name(), ver)
			if err := os.RemoveAll(installPath); err != nil {
				return nil, fmt.Errorf("force reinstall: remove existing install dir: %w", err)
			}
		} else {
			// Intact install — just activate (unless NoActivate).
			log.Printf("[install] Already installed at %s", installPath)
			if opts.NoActivate {
				log.Printf("[install] NoActivate set, skipping activation")
				return &InstallResult{Tool: p.Name(), Version: ver, Spec: resSpec, URL: resURL, SHA256: resSHA, InstallPath: installPath, Activated: false}, nil
			}
			if onProgress != nil {
				onProgress(PhaseActivating, 100, "", "")
			}
			if err := i.Activate(p, installPath, ver); err != nil {
				log.Printf("[install] ERROR: activation failed: %v", err)
				return nil, err
			}
			log.Printf("[install] Activation complete: %s@%s", p.Name(), ver)
			return &InstallResult{Tool: p.Name(), Version: ver, Spec: resSpec, URL: resURL, SHA256: resSHA, InstallPath: installPath, Activated: true}, nil
		}
	}

	log.Printf("[install] Starting download phase")
	if err := i.downloadWithProgress(ctx, p.Name(), ver, arch, onProgress, opts.BatchIndex, opts.BatchTotal); err != nil {
		log.Printf("[install] ERROR: download failed for %s@%s: %v", p.Name(), ver, err)
		return nil, err
	}
	log.Printf("[install] Download complete, starting install-by-kind")
	if onProgress != nil {
		onProgress(PhaseExtracting, 0, "", "")
	}
	if err := i.installByKind(ctx, p, arch, ver, installPath); err != nil {
		log.Printf("[install] ERROR: install-by-kind failed for %s@%s: %v", p.Name(), ver, err)
		return nil, err
	}
	// Hard gate for archive extracts only. Executable/script installers
	// may populate the install tree asynchronously or outside installPath
	// (e.g. rustup), so binary-presence checks would false-fail them.
	kind := resolveKind(arch)
	if kind == manifest.InstallKindArchive {
		if err := ensureInstallIntegrity(p, installPath, arch.Strip); err != nil {
			log.Printf("[install] ERROR: %v", err)
			// Best-effort cleanup so the broken dir is not left as "installed".
			_ = os.RemoveAll(installPath)
			return nil, err
		}
		log.Printf("[install] Install-by-kind complete (integrity OK)")
	} else {
		log.Printf("[install] Install-by-kind complete (kind=%s, skip extract integrity)", kind)
	}

	// Post-install hook
	if err := p.PostInstall(ctx, installPath); err != nil {
		log.Printf("[install] WARNING: post-install hook failed for %s@%s: %v", p.Name(), ver, err)
	}

	if opts.NoActivate {
		log.Printf("[install] NoActivate set, skipping activation")
		return &InstallResult{Tool: p.Name(), Version: ver, Spec: resSpec, URL: resURL, SHA256: resSHA, InstallPath: installPath, Activated: false}, nil
	}

	log.Printf("[install] Starting activation")
	if onProgress != nil {
		onProgress(PhaseActivating, 50, "", "")
	}
	if err := i.Activate(p, installPath, ver); err != nil {
		// Activation (shim/PATH update) may fail when the currently
		// active version's binary is locked by a running process.
		// The new version IS fully installed and can be activated
		// later via `xdev use` once the conflicting process exits.
		// We must NOT treat this as a failure — return a successful
		// result with Activated=false and a hint.
		log.Printf("[install] WARNING: activation failed for %s@%s (version installed, not activated): %v", p.Name(), ver, err)
		if onProgress != nil {
			onProgress(PhaseActivating, 100, "", "")
		}
		return &InstallResult{
			Tool:            p.Name(),
			Version:         ver,
			Spec:            resSpec,
			URL:             resURL,
			SHA256:          resSHA,
			InstallPath:     installPath,
			Activated:       false,
			ActivationError: err.Error(),
		}, nil
	}
	if onProgress != nil {
		onProgress(PhaseActivating, 100, "", "")
	}
	log.Printf("[install] Install complete: %s@%s -> %s", p.Name(), ver, installPath)
	return &InstallResult{Tool: p.Name(), Version: ver, Spec: resSpec, URL: resURL, SHA256: resSHA, InstallPath: installPath, Activated: true}, nil
}

// downloadWithProgress downloads the archive for a tool version, applying
// mirror rewriting and SHA-256 verification. It supports:
//   - Mirror URL rewriting via the mirror table
//   - Cache hits: if the file already exists in the download cache with a
//     matching SHA-256, the download is skipped
//   - Manifest signature verification (Ed25519) when .yaml.sig files are present
//   - Fallback URLs: the original upstream URL and mirror_url are tried if the
//     primary (mirrored) URL fails
//   - Progress reporting via the onProgress callback
func (i *Installer) downloadWithProgress(ctx context.Context, tool, ver string, a *manifest.Archive, onProgress ProgressCallback, batchIndex, batchTotal int) error {
	url, fallbackURLs, dest, urlShaMap := i.BuildDownloadOptions(tool, ver, a)
	log.Printf("[install] Cache destination: %s", dest)

	progressDesc := fmt.Sprintf("%s@%s", tool, ver)
	if batchTotal > 0 {
		progressDesc = fmt.Sprintf("[%d/%d] %s", batchIndex, batchTotal, progressDesc)
	}
	if _, err := os.Stat(dest); err == nil {
		// Cache hit — verify SHA-256 if available
		if a.SHA256 != "" && !util.IsAllZeroHash(a.SHA256) {
			if cacheHash, err := util.FileSHA256(dest); err == nil {
				if !strings.EqualFold(cacheHash, a.SHA256) {
					expPreview := a.SHA256
					if len(expPreview) > 16 {
						expPreview = expPreview[:16] + "..."
					}
					gotPreview := cacheHash
					if len(gotPreview) > 16 {
						gotPreview = gotPreview[:16] + "..."
					}
					log.Printf("[install] Cache SHA-256 mismatch! expected=%s got=%s — re-downloading", expPreview, gotPreview)
					os.Remove(dest)
					// Fall through to download
				} else {
					log.Printf("[install] Cache hit (verified): %s", dest)
					if onProgress != nil {
						onProgress(PhaseDownloading, 100, "", "")
					}
					downloader.LogAudit(downloader.AuditEntry{
						Tool:     tool,
						Version:  ver,
						URL:      a.URL,
						SHA256:   cacheHash,
						Source:   "cache",
						Verified: true,
					})
					return nil
				}
			}
		} else {
			// 缓存命中但 manifest 无可靠 SHA-256（空或占位符）。
			// Security (F-04 / D15): 若存在 .sha256 sidecar pin 则先校验；
			// 不匹配时删除缓存（含 pin）并重新下载，绝不静默接受被篡改的缓存。
			// 无 pin 时接受缓存并走 TOFU 建立 pin。
			ok, reason := verifyCacheSidecar(dest)
			if !ok {
				log.Printf("[install] WARNING: %s — removing cached archive and re-downloading", reason)
				os.Remove(dest)
				os.Remove(dest + ".sha256")
			} else {
				log.Printf("[install] Cache hit: %s already exists, skipping download", dest)
				if onProgress != nil {
					onProgress(PhaseDownloading, 100, "", "")
				}
				downloader.LogAudit(downloader.AuditEntry{
					Tool:    tool,
					Version: ver,
					URL:     a.URL,
					Source:  "cache",
				})
				// TOFU: If the archive had an empty or placeholder SHA-256, compute
				// the actual hash from the cached file and write to on-disk manifest.
				if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
					if err := i.TOFUArchiveIfNeeded(tool, ver, a, dest); err != nil {
						log.Printf("[install] TOFU warning: failed to update manifest for %s@%s: %v", tool, ver, err)
					}
				}
				return nil
			}
		}
	}
	log.Printf("[install] Cache miss, starting download from %s", url)

	// Load manifest signature if available.
	manifestSig, isThirdParty := i.LoadManifestSignature(tool)

	// Proxy warning: if the archive explicitly requires a proxy but none
	// is configured (either via env vars or xdev config), warn the user.
	if a.RequiresProxy && !CheckProxyConfigured(i.cfg) {
		log.Printf("[install] WARNING: %s@%s requires a proxy but none is configured", tool, ver)
		fmt.Fprintf(os.Stderr,
			"⚠ WARNING: This download requires a proxy to access (GitHub releases may be unreachable in mainland China).\n"+
				"  Set HTTP_PROXY/HTTPS_PROXY environment variables or configure a proxy in xdev config.\n")
	}

	var err error
	err = downloader.Download(ctx, downloader.Options{
		URL:                  url,
		Dest:                 dest,
		ExpectedSHA:          a.SHA256,
		URLShaMap:            urlShaMap,
		ProgressDesc:         progressDesc,
		Cookie:               util.CookieFromInstallArgs(a.InstallArgs),
		ManifestSignature:    manifestSig,
		IsThirdParty:         isThirdParty,
		AllowPlaceholderTOFU: a.SHA256 == "" || util.IsAllZeroHash(a.SHA256),
		ProxyURL:             i.cfg.GetProxyURL("https"),
		FallbackURLs:         fallbackURLs,
		// Surface non-byte status updates (retries, fallback switches,
		// range restarts) through the audit log. The GUI wires its own
		// OnStatus for live progress events.
		OnStatus: func(message string) {
			log.Printf("[download] status: %s", message)
		},
		OnProgress: func(downloaded, total int64, elapsed time.Duration) {
			if onProgress != nil && total > 0 {
				pct := float64(downloaded) / float64(total) * 100
				speed := util.HumanSpeedStr(downloaded, elapsed)
				eta := util.HumanETAStr(downloaded, total, elapsed)
				onProgress(PhaseDownloading, pct, speed, eta)
			}
		},
	})
	if err != nil {
		return err
	}

	// TOFU: If the archive had an empty or placeholder SHA-256, compute the
	// actual hash from the downloaded file and write it to the on-disk manifest.
	if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
		if err := i.TOFUArchiveIfNeeded(tool, ver, a, dest); err != nil {
			// TOFU is best-effort — log the error but don't fail the install.
			log.Printf("[install] TOFU warning: failed to update manifest for %s@%s: %v", tool, ver, err)
		}
	}

	return nil
}

// LoadManifestSignature loads the Ed25519 manifest signature for a tool
// (if available) and reports whether the tool is a third-party manifest.
//
// Resolution order (shared by CLI and GUI install paths, Spec 021 D6):
//  1. <ManifestDir>/<tool>.yaml.sig (built-in manifest signature)
//  2. <ManifestDir>/third-party/<tool>.yaml.sig (third-party signature)
//  3. Embedded manifest signature (signer.ReadEmbeddedSignature)
//  4. Third-party manifest existence (no signature → TOFU fallback)
func (i *Installer) LoadManifestSignature(tool string) ([]byte, bool) {
	var manifestSig []byte
	isThirdParty := false
	thirdPartyDir := filepath.Join(i.cfg.Core.ManifestDir, "third-party")
	sigPath := filepath.Join(i.cfg.Core.ManifestDir, tool+".yaml.sig")
	if sigData, err := os.ReadFile(sigPath); err == nil {
		manifestSig = sigData
		log.Printf("[install] Found manifest signature for %s", tool)
	} else if sigData, err := os.ReadFile(filepath.Join(thirdPartyDir, tool+".yaml.sig")); err == nil {
		manifestSig = sigData
		isThirdParty = true
		log.Printf("[install] Found third-party manifest signature for %s", tool)
	} else {
		// Also check embedded manifests
		if embeddedSig, err := signer.ReadEmbeddedSignature(tool); err == nil && embeddedSig != nil {
			if sigJSON, err := json.Marshal(embeddedSig); err == nil {
				manifestSig = sigJSON
				log.Printf("[install] Found embedded manifest signature for %s", tool)
			}
		}
		// Determine if this is a third-party manifest (no embedded/built-in sig)
		if _, err := os.Stat(filepath.Join(thirdPartyDir, tool+".yaml")); err == nil {
			isThirdParty = true
			log.Printf("[install] Third-party manifest detected for %s (TOFU fallback if no signature)", tool)
		}
	}
	return manifestSig, isThirdParty
}

// TOFUArchiveIfNeeded computes the SHA-256 of the downloaded archive and
// writes it to the on-disk manifest when the manifest carried an empty or
// placeholder hash. It also invokes the TOFUWarningCallback if set.
// This is best-effort: errors are logged but not returned to the caller.
// Exported for the GUI install path (Spec 021 D7).
func (i *Installer) TOFUArchiveIfNeeded(tool, ver string, a *manifest.Archive, dest string) error {
	actualSHA, err := util.FileSHA256(dest)
	if err != nil {
		return fmt.Errorf("compute SHA-256: %w", err)
	}
	if actualSHA == "" {
		return fmt.Errorf("computed SHA-256 is empty")
	}

	// Determine OS and arch from the archive entry.
	osName := a.OS
	arch := a.Arch
	if osName == "" || osName == "any" {
		osName = plugin.HostOS()
	}
	if arch == "" || arch == "any" {
		arch = plugin.HostArch()
	}

	// Write to on-disk manifest.
	manifestDir := filepath.Join(i.cfg.Core.XMDevDir, "manifests")
	if err := manifest.UpdateArchiveSHA256(manifestDir, tool, ver, osName, arch, actualSHA); err != nil {
		return fmt.Errorf("UpdateArchiveSHA256: %w", err)
	}

	log.Printf("[install] TOFU: computed SHA256 for %s@%s (%s/%s): %s", tool, ver, osName, arch, actualSHA)

	// Invoke callback (GUI mode).
	if i.tofuCB != nil {
		i.tofuCB(tool, ver, osName, arch, actualSHA)
	}

	return nil
}

// verifyCacheSidecar 校验缓存归档的 .sha256 sidecar pin（F-04 / D15）。
// 返回 (ok, reason)：
//   - ok=true 表示可以接受缓存（无 pin、pin 无效、或 pin 与文件哈希一致）
//   - ok=false 且 reason 非空时，调用方应删除缓存并重新下载
func verifyCacheSidecar(dest string) (bool, string) {
	sidecarData, err := os.ReadFile(dest + ".sha256")
	if err != nil {
		return true, "" // 无 pin：接受，由 TOFU 建立新 pin
	}
	parts := strings.Fields(strings.TrimSpace(string(sidecarData)))
	if len(parts) < 1 || util.IsAllZeroHash(parts[0]) {
		return true, "" // pin 内容无效：接受，由 TOFU 覆写
	}
	cacheHash, err := util.FileSHA256(dest)
	if err != nil {
		return false, fmt.Sprintf("cannot compute SHA-256 of cached archive %s: %v", dest, err)
	}
	if !strings.EqualFold(cacheHash, parts[0]) {
		return false, fmt.Sprintf("cache sidecar pin mismatch for %s (cached=%s got=%s)",
			dest, parts[0][:16]+"...", cacheHash[:16]+"...")
	}
	return true, ""
}

// verifyInstallerSidecarPin 校验安装器二进制与其 .sha256 sidecar pin 是否一致
// （F-04/D15）。返回 (matched, err)：
//   - matched=true：pin 存在且与文件哈希一致，允许运行
//   - matched=false：pin 缺失或内容无效（拒绝运行——安装器必须可验证）
//   - err：读取/计算哈希失败
func verifyInstallerSidecarPin(src string) (bool, error) {
	data, err := os.ReadFile(src + ".sha256")
	if err != nil {
		return false, nil // 无 pin：拒绝（需要真实 manifest SHA）
	}
	parts := strings.Fields(strings.TrimSpace(string(data)))
	if len(parts) < 1 || util.IsAllZeroHash(parts[0]) {
		return false, nil // pin 内容无效：拒绝
	}
	got, err := util.FileSHA256(src)
	if err != nil {
		return false, fmt.Errorf("compute SHA-256 of %s: %w", src, err)
	}
	return strings.EqualFold(got, parts[0]), nil
}

// getMirrorHealthCache returns the mirror health cache, loading it from
// disk on first call. Subsequent calls return the cached instance.
func (i *Installer) getMirrorHealthCache() *MirrorHealthCache {
	i.mirrorCacheOnce.Do(func() {
		i.mirrorCache = LoadMirrorHealthCache(i.cfg.Core.CacheDir)
	})
	return i.mirrorCache
}

// buildDownloadOptions constructs the download URL, fallback URLs, and cache
// destination path for a tool archive. It applies mirror rewriting and
// smart mirror selection (concurrent HEAD probe, health cache, user preference).
//
// Returns (primaryURL, fallbackURLs, cacheDestPath, urlShaMap).
func (i *Installer) BuildDownloadOptions(tool, ver string, a *manifest.Archive) (string, []string, string, map[string]string) {
	// 1. Build candidate list from multiple sources.
	candidates := i.buildMirrorCandidates(tool, a)

	// 2. Filter out known-unavailable mirrors via health cache.
	cache := i.getMirrorHealthCache()
	filtered := make([]string, 0, len(candidates))
	skipped := 0
	for _, u := range candidates {
		if cache.IsAvailable(u) {
			filtered = append(filtered, u)
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		log.Printf("[install] Skipped %d known-unavailable mirror(s) (health cache)", skipped)
	}

	// 3. Select best mirror via concurrent probe, or use the only candidate.
	var primary string
	if len(filtered) == 0 {
		// All candidates filtered out — fall back to official URL.
		primary = a.URL
		log.Printf("[install] WARNING: all mirrors unavailable for %s@%s, falling back to official URL", tool, ver)
	} else if len(filtered) == 1 {
		primary = filtered[0]
	} else {
		// Multiple candidates — probe concurrently and pick the best.
		best, err := SelectBestMirror(filtered, cache, defaultProbeTimeout)
		if err != nil {
			// All probes failed — use first filtered candidate as fallback.
			primary = filtered[0]
		} else {
			primary = best
		}
	}

	// 4. Persist cache (best-effort).
	cache.Save()

	// 5. Build fallback list: remaining candidates (excluding selected primary).
	fallbackURLs := make([]string, 0, len(filtered))
	for _, u := range filtered {
		if u != primary {
			fallbackURLs = append(fallbackURLs, u)
		}
	}
	// Always ensure official URL is the last fallback (if not already in list).
	officialInFallback := false
	for _, u := range fallbackURLs {
		if u == a.URL {
			officialInFallback = true
			break
		}
	}
	if !officialInFallback && a.URL != "" && a.URL != primary {
		fallbackURLs = append(fallbackURLs, a.URL)
	}

	dest := filepath.Join(i.cfg.Core.CacheDir, fmt.Sprintf("%s-%s.%s", tool, ver, a.Format))

	// 6. Build URL→SHA256 map so the downloader can verify each URL
	// against the correct hash (official URL uses a.SHA256, mirror URLs
	// use a.MirrorSHA256 when set, falling back to a.SHA256).
	shaMap := map[string]string{
		a.URL: a.SHA256,
	}
	// Mirror rewrite URL (same file, same SHA as official)
	if util.ShouldApplyMirror(a.URL) {
		if alt := i.mir.Apply(tool, a.URL); alt != a.URL {
			shaMap[alt] = a.SHA256
		}
	}
	// Manifest-declared mirror URLs
	for _, mu := range a.EffectiveMirrorURLs() {
		if a.MirrorSHA256 != "" {
			shaMap[mu] = a.MirrorSHA256
		} else {
			shaMap[mu] = a.SHA256
		}
	}
	// User-configured mirror preference URLs (same file, same SHA)
	for _, u := range i.cfg.Core.MirrorPreference {
		shaMap[u] = a.SHA256
	}

	return primary, fallbackURLs, dest, shaMap
}

// buildMirrorCandidates assembles the ordered list of candidate mirror URLs
// for the given archive. The order reflects priority:
//  1. User-configured mirror_preference (from config.yaml)
//  2. Manifest mirror_urls / mirror_url (via EffectiveMirrorURLs)
//  3. Mirror table rewrite (for GitHub/upstream URLs)
//  4. Official URL (always last)
func (i *Installer) buildMirrorCandidates(tool string, a *manifest.Archive) []string {
	seen := make(map[string]bool)
	candidates := make([]string, 0, 8)

	addUnique := func(url string) {
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		candidates = append(candidates, url)
	}

	// 1. User mirror_preference (highest priority)
	for _, u := range i.cfg.Core.MirrorPreference {
		addUnique(u)
	}

	// 2. Manifest mirror URLs
	for _, u := range a.EffectiveMirrorURLs() {
		addUnique(u)
	}

	// 3. Mirror table rewrite (for GitHub/upstream URLs eligible for mirroring)
	if util.ShouldApplyMirror(a.URL) {
		if alt := i.mir.Apply(tool, a.URL); alt != a.URL {
			addUnique(alt)
		}
	}

	// 4. Official URL (always last resort)
	addUnique(a.URL)

	return candidates
}

// extract unpacks a cached archive to the destination directory.
// It delegates to extractor.Extract which supports zip, tar.gz, tar.xz,
// 7z, and zstd formats. The strip parameter controls how many leading
// path components to remove (e.g. strip=1 removes the top-level directory
// common in Go and Python archives).
func (i *Installer) extract(tool string, a *manifest.Archive, ver, dest string) error {
	// The cache filename uses the tool name (matching downloadWithProgress)
	src := filepath.Join(i.cfg.Core.CacheDir, fmt.Sprintf("%s-%s.%s", tool, ver, a.Format))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		// Fallback: try matching by glob in case of naming mismatch
		matches, _ := filepath.Glob(filepath.Join(i.cfg.Core.CacheDir, fmt.Sprintf("*-%s.%s", ver, a.Format)))
		if len(matches) > 0 {
			src = matches[0]
			log.Printf("[install] Extract: matched cache file by glob: %s", src)
		} else {
			log.Printf("[install] ERROR: cache miss for extraction, tried: %s", src)
			return fmt.Errorf("cache miss: %s", src)
		}
	}
	log.Printf("[install] Extracting: src=%s dest=%s format=%s strip=%d", src, dest, a.Format, a.Strip)
	err := extractor.Extract(src, dest, a.Format, a.Strip)
	if err != nil {
		log.Printf("[install] ERROR: extraction failed: %v", err)
	} else {
		log.Printf("[install] Extraction complete: %s", dest)
	}
	return err
}

// resolveKind returns the effective install kind for an archive, applying
// a conservative URL-based heuristic when the manifest doesn't declare
// install_kind explicitly. This lets older manifests (e.g. rust.yaml
// before the kind field was added) benefit from executable/script
// routing without re-signing — the signature payload only covers
// tool+sha256map+timestamp, not kind.
func resolveKind(a *manifest.Archive) string {
	kind := a.EffectiveKind()
	if kind == manifest.InstallKindArchive && a.InstallKind == "" {
		u := strings.ToLower(a.URL)
		switch {
		case strings.Contains(u, "sh.rustup.rs"):
			kind = manifest.InstallKindScript
		case strings.Contains(u, "rustup-init.exe"):
			kind = manifest.InstallKindExecutable
		}
	}
	return kind
}

// installByKind dispatches the post-download step based on the archive's
// resolved install kind:
//   - archive (default): extract the cached file into the install dir
//   - executable: verify SHA-256, then run the cached binary
//   - script: verify SHA-256, then execute the cached script via "sh"
func (i *Installer) installByKind(ctx context.Context, p plugin.Plugin, a *manifest.Archive, ver, installPath string) error {
	kind := resolveKind(a)
	switch kind {
	case manifest.InstallKindExecutable, manifest.InstallKindScript:
		return i.runInstaller(ctx, p, a, ver, installPath)
	default:
		return i.extract(p.Name(), a, ver, installPath)
	}
}

// installerRunMu 串行化“以原始文件名复制安装器 → 运行 → 清理”临界区。
// 同一 URL 基名（如 rustup-init.exe）在并发安装两个版本时会被竞争
// 复制/删除，互斥锁保证安全。
var installerRunMu sync.Mutex

// runInstaller executes a cached installer binary or script after
// verifying its SHA-256. Unlike the extract path, this runs code — so
// the SHA check is mandatory: an empty or all-zero (placeholder) SHA
// is refused, and a mismatch rejects the run.
//
// F-04/D15 例外：当 manifest SHA 为空/占位符时，若缓存文件旁存在匹配的
// .sha256 sidecar pin（TOFU 首次下载时建立），允许运行——这修复了
// rust 1.97.1（rustup-init.exe，sha256 为空）等工具的安装。
func (i *Installer) runInstaller(ctx context.Context, p plugin.Plugin, a *manifest.Archive, ver, installPath string) error {
	src := filepath.Join(i.cfg.Core.CacheDir, fmt.Sprintf("%s-%s.%s", p.Name(), ver, a.Format))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("installer binary not in cache: %s", src)
	}

	// Security: executable/script must have a real SHA-256 (no placeholders),
	// unless a matching sidecar pin exists.
	if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
		matched, err := verifyInstallerSidecarPin(src)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("refusing to run %s: SHA-256 is missing or placeholder and no matching sidecar pin exists", p.Name())
		}
		log.Printf("[install] %s SHA-256 verified via sidecar pin", p.Name())
	} else {
		got, err := util.FileSHA256(src)
		if err != nil {
			return fmt.Errorf("compute SHA-256 of %s: %w", src, err)
		}
		if !strings.EqualFold(got, a.SHA256) {
			return fmt.Errorf("SHA-256 mismatch for %s: expected %s, got %s", p.Name(), a.SHA256, got)
		}
	}

	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}

	// 部分安装器（rustup 等）按 argv[0] 文件名分派角色（rustc/cargo/
	// rustdoc/rustup-init…），而 xdev 缓存名 <tool>-<ver>.<fmt> 会破坏
	// 该机制（rustup 会报 "unknown proxy name"）。若 URL 原始文件名与
	// 缓存名不同，将文件复制为原始文件名再运行，运行后清理。
	runPath := src
	if base := filepath.Base(a.URL); base != "" && base != filepath.Base(src) {
		alt := filepath.Join(i.cfg.Core.CacheDir, base)
		installerRunMu.Lock()
		defer installerRunMu.Unlock()
		if err := util.CopyFile(src, alt); err != nil {
			return fmt.Errorf("prepare installer under original name %s: %w", alt, err)
		}
		runPath = alt
		// LIFO：先 os.Remove(alt) 再 Unlock，清理也在锁内。
		defer os.Remove(alt)
	}

	// Build the command. Scripts are run via "sh" on all platforms;
	// executables are run directly.
	//
	// 支持 {{version}} / {{tool}} / {{install}} 模板替换：滚动安装器
	// （如 rustup-init）需要把目标工具链版本传给 --default-toolchain；
	// 静默安装器（如 JetBrains /D=<dir>）需要目标安装目录。替换在运行时
	// 进行，无模板时行为不变。
	//
	// strings.NewReplacer 为单趟替换：替换值不会再被扫描，三个占位符
	// 互不依赖（顺序无关），不会级联展开。
	replacer := strings.NewReplacer("{{version}}", ver, "{{tool}}", p.Name(), "{{install}}", installPath)
	args := make([]string, len(a.InstallArgs))
	for i, arg := range a.InstallArgs {
		args[i] = replacer.Replace(arg)
	}

	kind := resolveKind(a)
	var name string
	if kind == manifest.InstallKindScript {
		name = "sh"
		args = append([]string{src}, args...)
	} else {
		name = runPath
	}

	// Inherit current environment and overlay plugin env vars so the
	// installer (e.g. rustup) sees CARGO_HOME / RUSTUP_HOME.
	env := os.Environ()
	for k, v := range p.EnvVars(installPath) {
		env = append(env, k+"="+v)
	}
	// 把 xdev 配置的代理注入子进程环境变量（HTTP_PROXY/HTTPS_PROXY），
	// 否则 rustup 等安装器下载工具链时不走代理，导致极慢或失败。
	// 仅当子进程环境尚未设置代理时才注入（尊重用户显式设置）。
	if proxyURL := i.cfg.GetProxyURL("https"); proxyURL != "" {
		if !envHasKey(env, "HTTPS_PROXY") && !envHasKey(env, "https_proxy") {
			env = append(env, "HTTPS_PROXY="+proxyURL)
		}
	}
	if proxyURL := i.cfg.GetProxyURL("http"); proxyURL != "" {
		if !envHasKey(env, "HTTP_PROXY") && !envHasKey(env, "http_proxy") {
			env = append(env, "HTTP_PROXY="+proxyURL)
		}
	}

	log.Printf("[install] Running installer: %s %v (dir=%s)", name, args, installPath)
	return i.runner.Run(ctx, name, args, env, installPath)
}

// Activate wires a freshly installed version into the active toolchain:
//
//   - $XMDEV_HOME/current/<tool> → $XMDEV_HOME/install/<tool>/<ver> (junction/symlink)
//   - $XMDEV_HOME/shims/<name> → xdev binary copy (one per shim name, shim mode only)
//   - User-level env vars written to Windows registry (e.g. GOROOT, JAVA_HOME)
//   - WM_SETTINGCHANGE broadcast so new terminals pick up changes
//
// On Windows, junction creation is preferred over symbolic links because
// junctions don't require Developer Mode or elevated privileges. The
// fallback chain is: junction → symlink → directory copy.
//
// When noEnvWrite is true (set by --no-path-fix), registry writes and
// WM_SETTINGCHANGE broadcast are skipped for test isolation.
//
// Security (H-9): Activate now supports rollback. If any step fails after
// the junction has been created, the previous junction is restored and
// environment variables are rolled back to their original values.
func (i *Installer) Activate(p plugin.Plugin, installPath, ver string) error {
	lock, err := util.AcquireToolLock(i.cfg.Core.XMDevDir, p.Name(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("activate lock %s: %w", p.Name(), err)
	}
	defer lock.Release()

	currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		log.Printf("[install] ERROR: failed to create current dir %s: %v", currentDir, err)
		return err
	}
	currentLink := filepath.Join(currentDir, p.Name())
	log.Printf("[install] Activating: %s -> %s", currentLink, installPath)

	// --- H-9 Rollback: Save previous state ---
	var oldJunctionTarget string
	var junctionExisted bool
	if _, err := os.Lstat(currentLink); err == nil {
		junctionExisted = true
		if target, err := os.Readlink(currentLink); err == nil {
			oldJunctionTarget = target
			if fi2, err2 := os.Stat(target); err2 == nil && fi2.IsDir() {
				_ = p.Deactivate(target)
			}
		}
	}

	_ = os.Remove(currentLink)

	// --- Step 1: Create junction/symlink ---
	junctionCreated := false
	// Windows 上优先使用 Junction（不需要管理员权限，比 SymbolicLink 更可靠）
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(currentLink, installPath); err != nil {
			log.Printf("[install] Junction failed, trying symlink: %v", err)
			if err := os.Symlink(installPath, currentLink); err != nil {
				log.Printf("[install] Symlink also failed, trying other fallbacks: %v", err)
				if err := fallbackLink(installPath, currentLink); err != nil {
					log.Printf("[install] ERROR: all link methods failed for %s -> %s: %v", currentLink, installPath, err)
					return fmt.Errorf("symlink %s -> %s (and fallbacks also failed: %v)", currentLink, installPath, err)
				}
			}
		}
		junctionCreated = true
	} else {
		if err := os.Symlink(installPath, currentLink); err != nil {
			log.Printf("[install] Symlink failed, trying fallbacks: %v", err)
			if err := fallbackLink(installPath, currentLink); err != nil {
				log.Printf("[install] ERROR: all link methods failed for %s -> %s: %v", currentLink, installPath, err)
				return fmt.Errorf("symlink %s -> %s (and fallbacks also failed: %v)", currentLink, installPath, err)
			}
			log.Printf("[install] Fallback link succeeded for %s", currentLink)
		}
		junctionCreated = true
	}

	// --- Step 2: PATH mode dispatch ---
	pathsAdded := false
	if i.cfg.GetPathMode() == "direct" {
		currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
		binPaths := env.ToolBinPaths(p.Name(), currentDir)
		if len(binPaths) > 0 {
			if i.noEnvWrite {
				log.Printf("[install] Skipping AddCurrentBinPaths (noEnvWrite=true)")
			} else if err := env.AddCurrentBinPaths(binPaths); err != nil {
				log.Printf("[install] WARNING: failed to add current bin paths to PATH: %v", err)
			} else {
				pathsAdded = true
				for _, bp := range binPaths {
					log.Printf("[install] Added to PATH: %s", bp)
				}
			}
		}
		log.Printf("[install] Direct mode: skipping shim creation, using PATH entries instead")
	} else {
		shimExe := i.ResolveShimExe()
		log.Printf("[install] Creating shims in %s (exe=%s)", i.cfg.Core.ShimDir, shimExe)
		for _, name := range p.Shims() {
			log.Printf("[install] Installing shim: %s", name)
			if err := shim.InstallShim(i.cfg.Core.ShimDir, name, shimExe); err != nil {
				log.Printf("[install] ERROR: shim install failed for %s: %v", name, err)
				// H-9 Rollback: restore previous junction
				i.rollbackActivation(currentLink, junctionExisted, oldJunctionTarget, p)
				return fmt.Errorf("shim install failed for %s: %w (activation rolled back)", name, err)
			}
		}
	}

	// --- Step 3: Write user env vars (H-10: transactional) ---
	vars := p.EnvVars(installPath)
	envVarsWritten := false
	if len(vars) > 0 {
		if i.noEnvWrite {
			log.Printf("[install] Skipping user env var writes (noEnvWrite=true)")
		} else {
			if err := i.setUserEnvVars(vars); err != nil {
				log.Printf("[install] ERROR: setUserEnvVars failed, rolling back activation: %v", err)
				// H-9 Rollback
				i.rollbackActivation(currentLink, junctionExisted, oldJunctionTarget, p)
				if pathsAdded {
					currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
					binPaths := env.ToolBinPaths(p.Name(), currentDir)
					env.RemoveCurrentBinPaths(binPaths)
				}
				return fmt.Errorf("failed to set env vars (activation rolled back): %w", err)
			}
			envVarsWritten = true
		}
	}

	log.Printf("[install] Activation complete: %s@%s (%d shims)", p.Name(), ver, len(p.Shims()))
	shim.InvalidateShimIndex()

	// 广播 WM_SETTINGCHANGE 让新终端感知 PATH 变化
	if !i.noEnvWrite {
		env.BroadcastEnvironmentChange()
	}

	_ = junctionCreated
	_ = envVarsWritten

	return nil
}

// rollbackActivation restores the previous junction/symlink after a failed
// activation step. This is part of the H-9 fix for partial-activation failures.
func (i *Installer) rollbackActivation(currentLink string, junctionExisted bool, oldTarget string, p plugin.Plugin) {
	log.Printf("[install] Rolling back activation for %s", currentLink)
	_ = os.Remove(currentLink)
	if junctionExisted && oldTarget != "" {
		if runtime.GOOS == "windows" {
			if err := util.CreateJunction(currentLink, oldTarget); err != nil {
				log.Printf("[install] WARNING: failed to restore junction during rollback: %v", err)
			}
		} else {
			if err := os.Symlink(oldTarget, currentLink); err != nil {
				log.Printf("[install] WARNING: failed to restore symlink during rollback: %v", err)
			}
		}
		log.Printf("[install] Restored previous junction: %s -> %s", currentLink, oldTarget)
	}
}

// ResolveShimExe 确定用于创建 shim 的 xdev 二进制路径
// 使用 current/xdev/xdev.exe（通过 junction 指向 install/xdev/<version>/xdev.exe）
// 这样 xdev 自身也通过 junction+PATH 机制管理，无需复制二进制到根目录
func (i *Installer) ResolveShimExe() string {
	// 优先使用 current/xdev/xdev.exe（junction 方式）
	currentXdevExe := filepath.Join(i.cfg.Core.XMDevDir, "current", "xdev", "xdev.exe")
	if runtime.GOOS != "windows" {
		currentXdevExe = filepath.Join(i.cfg.Core.XMDevDir, "current", "xdev", "xdev")
	}
	if fi, err := os.Stat(currentXdevExe); err == nil && !fi.IsDir() {
		return currentXdevExe
	}

	// 回退：尝试旧路径 XMDevDir/xdev.exe（兼容旧版本安装）
	homeExe := filepath.Join(i.cfg.Core.XMDevDir, "xdev.exe")
	if runtime.GOOS != "windows" {
		homeExe = filepath.Join(i.cfg.Core.XMDevDir, "xdev")
	}
	if fi, err := os.Stat(homeExe); err == nil && !fi.IsDir() {
		return homeExe
	}

	// 最后回退：使用当前运行的二进制路径
	exe, err := os.Executable()
	if err != nil {
		return "xdev"
	}
	return exe
}

// setUserEnvVars 将工具的环境变量写入用户级环境变量
// Security (H-10): Returns an error if any write fails, allowing the caller
// to trigger rollback. Writes are tracked so they can be undone on failure.
func (i *Installer) setUserEnvVars(vars map[string]string) error {
	// Non-Windows: persistent env vars not supported, skip silently
	// (the env package's Unix stubs return an error, which would trigger
	// rollback — we avoid that by skipping at the caller level).
	if runtime.GOOS != "windows" {
		log.Printf("[install] Skipping persistent env var writes (non-Windows: not supported)")
		return nil
	}
	// Save original values for potential rollback
	originals := make(map[string]string)
	var firstErr error
	var written []string

	for name, value := range vars {
		if name == "PATH" {
			continue // PATH 由 shim 管理，不写入
		}
		// Save current value for rollback
		if cur, err := env.GetUserEnvVarTrusted(name); err == nil {
			originals[name] = cur
		}
		if err := env.SetUserEnvVarTrusted(name, value); err != nil {
			log.Printf("[install] ERROR: failed to set user env var %s: %v", name, err)
			firstErr = err
			break
		} else {
			log.Printf("[install] Set user env var: %s=%s", name, value)
			written = append(written, name)
		}
	}

	if firstErr != nil {
		// Rollback: restore original values for all vars we successfully wrote
		log.Printf("[install] Rolling back %d env var writes due to failure", len(written))
		for _, name := range written {
			if orig, ok := originals[name]; ok {
				if err := env.SetUserEnvVarTrusted(name, orig); err != nil {
					log.Printf("[install] WARNING: failed to rollback env var %s: %v", name, err)
				}
			} else {
				// Wasn't set before — try to delete
				if err := env.DeleteUserEnvVarTrusted(name); err != nil {
					log.Printf("[install] WARNING: failed to delete env var %s during rollback: %v", name, err)
				}
			}
		}
		return fmt.Errorf("failed to set env vars (rolled back %d): %w", len(written), firstErr)
	}
	return nil
}

// ExtractAndActivate 解压（或运行安装器）已下载的缓存文件并激活工具。
// 与 InstallWithProgress 的 installByKind 一致：executable/script 安装器
// （如 rustup-init.exe）通过 runInstaller 运行，而非解压——修复 GUI 安装
// 路径误把 exe 当归档解压（需系统 7z）的问题。
// 解压/运行成功即视为安装完成。激活（shim/PATH 切换）失败不会使
// 安装失败——新版本已完整安装在 installPath，用户可以稍后通过
// `xdev use` 手动切换。返回 (nil, activationErr) 形式：
//   - err == nil：解压、激活都成功
//   - err != nil 且解压已成功：err 是激活错误（版本已安装）
//   - err != nil 且解压失败：err 是解压错误（版本未安装）
func (i *Installer) ExtractAndActivate(p plugin.Plugin, a *manifest.Archive, ver, installPath string) error {
	kind := resolveKind(a)
	switch kind {
	case manifest.InstallKindExecutable, manifest.InstallKindScript:
		// 可执行/脚本安装器：运行安装器（含 SHA/sidecar pin 校验），
		// 跳过解压与完整性硬门（安装器可能异步/在 installPath 外写文件）。
		if err := i.runInstaller(context.Background(), p, a, ver, installPath); err != nil {
			return fmt.Errorf("installer run failed: %w", err)
		}
	default:
		if err := i.extract(p.Name(), a, ver, installPath); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
		// Hard gate after extract (same as InstallWithProgress): broken trees
		// must not be treated as successful installs.
		if err := ensureInstallIntegrity(p, installPath, a.Strip); err != nil {
			log.Printf("[install] ERROR: %v", err)
			_ = os.RemoveAll(installPath)
			return err
		}
	}
	// Post-install hook — run even if activation will fail later.
	if err := p.PostInstall(context.Background(), installPath); err != nil {
		log.Printf("[install] WARNING: post-install hook failed for %s@%s: %v", p.Name(), ver, err)
	}
	if err := i.Activate(p, installPath, ver); err != nil {
		log.Printf("[install] WARNING: activation failed for %s@%s (version installed, not activated): %v", p.Name(), ver, err)
		return &ActivationError{Tool: p.Name(), Version: ver, Err: err}
	}
	return nil
}

// ActivationError indicates that a version was installed successfully
// but could not be activated (shim/PATH update failed, typically because
// the currently active version's binary is locked by a running process).
// The version is fully installed and can be activated later.
type ActivationError struct {
	Tool    string
	Version string
	Err     error
}

func (e *ActivationError) Error() string {
	return fmt.Sprintf("installed %s@%s but activation failed: %v (use `xdev use %s@%s` after closing the conflicting process)", e.Tool, e.Version, e.Err, e.Tool, e.Version)
}

func (e *ActivationError) Unwrap() error { return e.Err }

// IsActivationError reports whether err is an ActivationError (version
// installed but not activated). Callers should treat this as a soft
// failure: the install succeeded, only the automatic switch failed.
func IsActivationError(err error) bool {
	var ae *ActivationError
	return errors.As(err, &ae)
}

// MirrorApply 应用镜像规则，返回镜像 URL
func (i *Installer) MirrorApply(tool, url string) string {
	if util.ShouldApplyMirror(url) {
		return i.mir.Apply(tool, url)
	}
	return url
}

// Uninstall removes a previously installed version.  It cleans up
// shims, env fragments, and the "current" symlink, then re-points
// "current" to another installed version when possible.
func (i *Installer) Uninstall(p plugin.Plugin, ver string) error {
	if ver == "" {
		return fmt.Errorf("uninstall requires an explicit version")
	}
	lock, err := util.AcquireToolLock(i.cfg.Core.XMDevDir, p.Name(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("uninstall lock %s: %w", p.Name(), err)
	}
	defer lock.Release()

	// Check if any other installed tool depends on this one
	for _, other := range i.reg.All() {
		if other.Name() == p.Name() {
			continue
		}
		for _, req := range other.RequiredPlugin() {
			if req == p.Name() {
				// Check if the dependent tool is actually installed
				depPath := other.InstallDir(i.cfg.Core.InstallDir, other.DefaultVersion())
				if fi, err := os.Stat(depPath); err == nil && fi.IsDir() {
					return fmt.Errorf("cannot uninstall %q: required by %q; uninstall %q first", p.Name(), other.Name(), other.Name())
				}
			}
		}
	}

	target := p.InstallDir(i.cfg.Core.InstallDir, ver)
	log.Printf("[install] Uninstalling: %s@%s from %s", p.Name(), ver, target)

	// 1. Remove the installation directory
	if err := os.RemoveAll(target); err != nil {
		log.Printf("[install] ERROR: uninstall failed for %s@%s: %v", p.Name(), ver, err)
		return err
	}

	// 2. Check if the uninstalled version was the active one
	activeVer, _ := i.currentVersion(p.Name())
	if activeVer == ver {
		// Remove the current symlink
		currentLink := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
		_ = os.Remove(currentLink)
		log.Printf("[install] Removed current symlink for %s", p.Name())

		// Try to re-point to another installed version
		if otherVer := i.findOtherVersion(p); otherVer != "" {
			otherPath := p.InstallDir(i.cfg.Core.InstallDir, otherVer)
			log.Printf("[install] Re-pointing %s to %s@%s", p.Name(), p.Name(), otherVer)
			if err := i.Activate(p, otherPath, otherVer); err != nil {
				log.Printf("[install] WARNING: failed to re-activate %s@%s: %v", p.Name(), otherVer, err)
			}
		} else {
			// No other version: clean up shims and env fragment
			log.Printf("[install] No other version of %s, cleaning up shims and env", p.Name())
			i.removeShims(p)
			i.removeEnvFragment(p)
			// Security (M-1): Clean up user-level environment variables (JAVA_HOME, GOROOT, etc.)
			// that were set during Activate. These point to the now-deleted installation
			// and would cause errors if left in the registry.
			if !i.noEnvWrite {
				i.removeUserEnvVars(p)
			}
			// direct 模式：从 PATH 中移除 current/<tool> 的 bin 子目录
			if i.cfg.GetPathMode() == "direct" {
				currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
				binPaths := env.ToolBinPaths(p.Name(), currentDir)
				if len(binPaths) > 0 {
					if err := env.RemoveCurrentBinPaths(binPaths); err != nil {
						log.Printf("[install] WARNING: failed to remove current bin paths from PATH: %v", err)
					}
				}
			}
		}
	}

	log.Printf("[install] Uninstall complete: %s@%s", p.Name(), ver)
	shim.InvalidateShimIndex()
	return nil
}

// IsUpgradeAvailable checks whether a newer version of the tool is
// available in the manifest compared to the currently active version.
// It returns (latestVersion, true) when an upgrade is available and
// ("", false) when the tool is already up to date or not installed/active.
func (i *Installer) IsUpgradeAvailable(toolName string) (string, bool) {
	p, ok := i.reg.Get(toolName)
	if !ok {
		return "", false
	}
	latest := p.DefaultVersion()
	if latest == "" {
		return "", false
	}
	current, ok := i.currentVersion(toolName)
	if !ok {
		return "", false
	}
	if util.CompareSemver(latest, current) > 0 {
		return latest, true
	}
	return "", false
}

// CurrentVersion returns the active version for a tool, or ("", false).
func (i *Installer) CurrentVersion(toolName string) (string, bool) {
	return i.currentVersion(toolName)
}

// ListInstalledTools returns the names of all tools that have at least
// one installed version under the install directory.
func (i *Installer) ListInstalledTools() []string {
	entries, err := os.ReadDir(i.cfg.Core.InstallDir)
	if err != nil {
		return nil
	}
	var tools []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		toolDir := filepath.Join(i.cfg.Core.InstallDir, e.Name())
		versions, err := os.ReadDir(toolDir)
		if err != nil {
			continue
		}
		for _, v := range versions {
			if v.IsDir() {
				tools = append(tools, e.Name())
				break
			}
		}
	}
	return tools
}

// currentVersion reads the current/<tool> symlink to determine the
// active version. Returns ("", false) when no active version exists.
func (i *Installer) currentVersion(toolName string) (string, bool) {
	return util.ActiveVersion(i.cfg.Core.XMDevDir, toolName)
}

// findOtherVersion returns the latest installed version of a tool
// (excluding the one being uninstalled), or "" if none exist.
func (i *Installer) findOtherVersion(p plugin.Plugin) string {
	dir := filepath.Join(i.cfg.Core.InstallDir, p.Name())
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if best == "" {
			best = e.Name()
			continue
		}
		// Compare using semver, not lexicographic ordering
		if util.CompareSemver(e.Name(), best) > 0 {
			best = e.Name()
		}
	}
	return best
}

// removeShims removes all shim entries for a plugin.
// Security (F-08 / D11): names failing shim validation are skipped
// (never used as paths), so legacy/corrupt entries cannot be targeted
// via traversal — while valid legacy entries are still removed.
func (i *Installer) removeShims(p plugin.Plugin) {
	for _, name := range p.Shims() {
		if err := util.ValidateShimName(name); err != nil {
			log.Printf("[install] WARNING: skipping shim removal for invalid name %q: %v", name, err)
			continue
		}
		shimPath := filepath.Join(i.cfg.Core.ShimDir, name)
		_ = os.Remove(shimPath)
		// Windows also has .exe variant
		if runtime.GOOS == "windows" {
			_ = os.Remove(shimPath + ".exe")
		}
		log.Printf("[install] Removed shim: %s", name)
	}
}

// removeEnvFragment removes the env fragment file for a plugin.
func (i *Installer) removeEnvFragment(p plugin.Plugin) {
	fragmentPath := filepath.Join(i.cfg.Core.EnvDir, p.Name()+".sh")
	_ = os.Remove(fragmentPath)
	log.Printf("[install] Removed env fragment: %s", fragmentPath)
}

// removeUserEnvVars deletes the user-level environment variables that were
// set by this tool during Activate. Security (M-1): Without this cleanup,
// uninstalling the last version of a tool leaves orphan env vars (e.g.,
// JAVA_HOME pointing to a deleted directory) in the Windows registry.
func (i *Installer) removeUserEnvVars(p plugin.Plugin) {
	// We don't have the installPath anymore (it was removed), but we can
	// infer the env var names from the plugin's EnvVars method by providing
	// a placeholder path. The keys are what matter, not the values.
	placeholderPath := filepath.Join(i.cfg.Core.InstallDir, p.Name(), "placeholder")
	vars := p.EnvVars(placeholderPath)
	for name := range vars {
		if name == "PATH" {
			continue
		}
		if !util.IsAllowedEnvVarName(name) {
			log.Printf("[install] WARNING: skipping deletion of non-whitelisted env var %q", name)
			continue
		}
		if err := env.DeleteUserEnvVar(name); err != nil {
			log.Printf("[install] WARNING: failed to delete user env var %s: %v", name, err)
		} else {
			log.Printf("[install] Deleted user env var: %s", name)
		}
	}
}

// fallbackLink tries, in order:
//
//  1. Windows directory junction (`mklink /J`) - no privileges
//  2. plain `ln -s` (in case the failure was on a network drive or
//     overlay filesystem where Symlink syscall fails for other
//     reasons).
//  3. recursive copy of installPath -> currentLink (always works
//     but uses disk space).
//
// Returns nil on the first success, otherwise a combined error.
func fallbackLink(src, dst string) error {
	// Junction on Windows
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(dst, src); err == nil {
			return nil
		}
	}
	// Try the platform's `ln` tool, just in case the syscall was
	// blocked by AppLocker or similar.
	if cmd, err := exec.LookPath("ln"); err == nil {
		lnCmd := exec.Command(cmd, "-s", src, dst)
		util.HideWindow(lnCmd)
		if err := lnCmd.Run(); err == nil {
			return nil
		}
	}
	// Last resort: copy the directory tree.  This is slow and
	// wastes disk, but it is bulletproof.
	return util.CopyDir(src, dst)
}

// InstallSelf 将当前运行的 xdev 二进制安装到 install/xdev/<version>/ 并创建 junction
// 这样 xdev 自身也通过 junction+PATH 机制管理，无需复制二进制到根目录
// 返回安装路径和是否为新安装
func InstallSelf(cfg *config.Config) (string, bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", false, fmt.Errorf("failed to get current binary path: %w", err)
	}

	ver := version.Version

	installDir := filepath.Join(cfg.Core.InstallDir, "xdev", ver)
	targetExe := filepath.Join(installDir, "xdev.exe")
	if runtime.GOOS != "windows" {
		targetExe = filepath.Join(installDir, "xdev")
	}

	// 检查是否已安装相同版本
	if fi, err := os.Stat(targetExe); err == nil && !fi.IsDir() {
		// 已存在，检查是否相同
		if exe == targetExe {
			// 已经在正确位置，只需确保 junction 存在
			ensureSelfJunction(cfg, installDir)
			// Also ensure xdev-gui is present
			installGuiBinary(exe, installDir)
			return installDir, false, nil
		}
		// 文件已存在但不是当前二进制，覆盖更新
		log.Printf("[self] 更新 xdev 二进制: %s -> %s", exe, targetExe)
	}

	// 创建安装目录
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", false, fmt.Errorf("failed to create install directory: %w", err)
	}

	// 复制二进制到安装目录
	if err := util.CopyFile(exe, targetExe); err != nil {
		return "", false, fmt.Errorf("failed to copy binary: %w", err)
	}
	log.Printf("[self] 已安装 xdev@%s 到 %s", ver, installDir)

	// Also copy xdev-gui binary if available
	installGuiBinary(exe, installDir)

	// 创建 junction
	ensureSelfJunction(cfg, installDir)

	return installDir, true, nil
}

// installGuiBinary finds and copies the xdev-gui binary to the install directory.
// It looks for xdev-gui.exe next to the current executable.
func installGuiBinary(currentExe string, installDir string) {
	if runtime.GOOS != "windows" {
		return // GUI binary is only relevant on Windows for now
	}

	guiExeName := "xdev-gui.exe"
	targetGuiExe := filepath.Join(installDir, guiExeName)

	// Check if xdev-gui.exe already exists and is up-to-date
	if fi, err := os.Stat(targetGuiExe); err == nil && !fi.IsDir() {
		// Already exists, check if source is newer
		srcDir := filepath.Dir(currentExe)
		srcGuiExe := filepath.Join(srcDir, guiExeName)
		if srcFi, err := os.Stat(srcGuiExe); err == nil {
			// Consider the binary up-to-date only if the source is not newer
			// than the existing copy. We intentionally do NOT use size comparison
			// because two different binaries can have the same size.
			if !srcFi.ModTime().After(fi.ModTime()) {
				return // already up-to-date
			}
		}
	}

	// Search for xdev-gui.exe in common locations
	searchPaths := []string{
		filepath.Join(filepath.Dir(currentExe), guiExeName), // Same directory as current exe
	}

	// If running from build/bin, also check the project root
	exeDir := filepath.Dir(currentExe)
	if filepath.Base(exeDir) == "bin" {
		searchPaths = append(searchPaths,
			filepath.Join(filepath.Dir(exeDir), guiExeName),               // build/
			filepath.Join(filepath.Dir(filepath.Dir(exeDir)), guiExeName), // project root
		)
	}

	for _, srcGuiExe := range searchPaths {
		if fi, err := os.Stat(srcGuiExe); err == nil && !fi.IsDir() {
			if err := util.CopyFile(srcGuiExe, targetGuiExe); err != nil {
				log.Printf("[self] 复制 xdev-gui 失败: %v", err)
			} else {
				log.Printf("[self] 已安装 xdev-gui 到 %s", installDir)
			}
			return
		}
	}

	log.Printf("[self] 未找到 xdev-gui.exe，跳过 GUI 二进制安装")
}

// ensureSelfJunction 确保 current/xdev junction 指向正确的安装目录
func ensureSelfJunction(cfg *config.Config, installDir string) {
	currentDir := filepath.Join(cfg.Core.XMDevDir, "current")
	_ = os.MkdirAll(currentDir, 0o755)

	currentLink := filepath.Join(currentDir, "xdev")

	// 安全检查：验证目标路径在 XDev 目录内
	normalizedInstall := strings.ToLower(filepath.Clean(installDir))
	normalizedXDev := strings.ToLower(filepath.Clean(cfg.Core.XMDevDir))
	if !strings.HasPrefix(normalizedInstall, normalizedXDev) {
		log.Printf("[self] WARNING: install dir %s is outside XDev home %s, skipping junction", installDir, cfg.Core.XMDevDir)
		return
	}

	// 检查现有 junction 是否已指向正确目标
	if fi, err := os.Lstat(currentLink); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			// 检查目标是否一致
			if target, err := os.Readlink(currentLink); err == nil {
				if filepath.Clean(target) == filepath.Clean(installDir) {
					return // junction 已正确
				}
			}
		}
		// 移除旧的链接/目录
		_ = os.Remove(currentLink)
	}

	// 创建 junction
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(currentLink, installDir); err != nil {
			log.Printf("[self] Junction 创建失败: %v，尝试 symlink", err)
			_ = os.Symlink(installDir, currentLink)
		}
	} else {
		_ = os.Symlink(installDir, currentLink)
	}
	log.Printf("[self] current/xdev -> %s", installDir)
}
