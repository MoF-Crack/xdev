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
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	PhaseInstalling                       // Running an executable/script installer (rustup 等，无确定进度)
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
		// 20261021 审计 P1-5（阶段 2）：InstallDir 校验失败必须中止该项，
		// 不能把伪路径带进后续 Stat/安装流程。
		installPath, dirErr := p.InstallDir(i.cfg.Core.InstallDir, ver)
		if dirErr != nil {
			results[idx] = BatchInstallResult{
				Tool:    item.ToolName,
				Version: ver,
				Status:  "failed",
				Error:   dirErr,
			}
			continue
		}
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
			depPath, dirErr := depPlugin.InstallDir(i.cfg.Core.InstallDir, depPlugin.DefaultVersion())
			if dirErr != nil {
				return nil, dirErr
			}
			// F-27: a mere os.Stat + IsDir accepted an EMPTY (or binary-less)
			// dependency directory — exactly what a truncated extraction
			// leaves behind — so the dependent tool installed against a
			// broken prerequisite. Use the same integrity rule as the rest
			// of the installer (InstallWithProgress / InstallMany).
			if integrity := VerifyInstallIntegrity(depPlugin, depPath); !integrity.OK {
				log.Printf("[install] required plugin %q is present at %s but incomplete: %s",
					depName, depPath, integrity.Reason)
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

	// 20261021 审计 P1-5（阶段 1）：在**任何文件系统写入之前**拒绝非法
	// tool/version。插件 `InstallDir` 的 SafeName 失败是 fail-open 的
	// （替换成 `unsafe-version-blocked` 后继续安装，宿主实测留下过该目录）。
	// 这里集中 fail-closed，保证新代码不再产生伪版本目录；阶段 2 再把
	// 插件接口改为返回 error。
	if err := validateInstallComponent("tool name", p.Name()); err != nil {
		return nil, err
	}
	if err := validateInstallComponent("version", ver); err != nil {
		return nil, err
	}

	arch, err := p.ArchiveFor(ver)
	if err != nil {
		log.Printf("[install] ERROR: archive resolution failed for %s@%s: %v", p.Name(), ver, err)
		return nil, err
	}
	log.Printf("[install] Archive: url=%s format=%s os=%s arch=%s", arch.URL, arch.Format, arch.OS, arch.Arch)

	installPath, err := p.InstallDir(i.cfg.Core.InstallDir, ver)
	if err != nil {
		return nil, err
	}
	log.Printf("[install] Install path: %s", installPath)

	// Capture archive metadata once for lockfile pinning. We record the
	// upstream URL (pre-mirror) and manifest SHA-256 so downstream callers
	// (the CLI lockfile writer) can persist reproducible version pins.
	resSpec := spec
	resURL := arch.URL
	resSHA := arch.SHA256

	// Ticket 26 (A-07): force reinstall defers the removal of the existing
	// install tree until the replacement archive has been downloaded and
	// verified (see the installLock critical section below).
	forceReinstall := false

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
			// Force reinstall (ticket 26 / A-07): DO NOT delete the existing
			// install here. Removing before the download meant a failed
			// download (offline / 404 / hash mismatch) destroyed a working
			// installation and left `current` dangling. The removal now
			// happens after the archive is downloaded and verified, inside
			// the install lock, right before extraction.
			log.Printf("[install] Force reinstall requested for %s@%s — deferring removal until the new archive is verified", p.Name(), ver)
			forceReinstall = true
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

	// Spec 039: npm-kind installs never download the archive — the entry is
	// metadata only and npm fetches the dependency tree itself (verifying
	// registry integrity per package).
	if ResolveKind(arch) != manifest.InstallKindNPM {
		log.Printf("[install] Starting download phase")
		if err := i.downloadWithProgress(ctx, p.Name(), ver, arch, onProgress, opts.BatchIndex, opts.BatchTotal); err != nil {
			log.Printf("[install] ERROR: download failed for %s@%s: %v", p.Name(), ver, err)
			return nil, err
		}
	}

	// Security (M-3 fix): serialize the remove/extract/verify span for the
	// same tool across processes. Two concurrent `xdev install go@1.22`
	// runs interleave stat/RemoveAll/extract and can corrupt the install
	// tree (the integrity gate can race past a still-writing process).
	// NOTE: the lock is released before Activate — Activate acquires the
	// same tool lock internally, and Windows named mutexes are reentrant
	// only per-thread: a goroutine migration between the two acquisitions
	// would self-deadlock for the full 30s Activate timeout.
	installLock, lockErr := util.AcquireToolLock(i.cfg.Core.XMDevDir, p.Name(), 5*time.Minute)
	if lockErr != nil {
		return nil, fmt.Errorf("install lock %s: %w", p.Name(), lockErr)
	}
	// The critical section (install-by-kind + integrity gate + post-install
	// hook) runs under the install lock and RELEASES it before activation,
	// which acquires the same lock internally.
	err = func() error {
		defer installLock.Release()
		log.Printf("[install] Download complete, starting install-by-kind")

		// Ticket 26 (A-07): the deferred force-reinstall removal. By this
		// point the archive is downloaded, SHA-verified and cached, so a
		// failure here is recoverable by re-running install; a failure
		// BEFORE this point leaves the previous installation intact.
		if forceReinstall {
			log.Printf("[install] Force reinstall: removing previous install dir %s (archive verified)", installPath)
			if rmErr := os.RemoveAll(installPath); rmErr != nil {
				return fmt.Errorf("force reinstall: remove existing install dir: %w", rmErr)
			}
		}

		// 阶段语义：archive 走"解压"；executable/script（rustup 等滚动安装器）
		// 走"正在安装"——运行安装器可能耗时数分钟（下载 toolchain）且无确定
		// 进度，percent=-1 通知前端保持当前进度条、仅切换阶段文案。
		if onProgress != nil {
			if ResolveKind(arch) == manifest.InstallKindArchive {
				onProgress(PhaseExtracting, 0, "", "")
			} else {
				onProgress(PhaseInstalling, -1, "", "")
			}
		}
		if err := i.installByKind(ctx, p, arch, ver, installPath); err != nil {
			log.Printf("[install] ERROR: install-by-kind failed for %s@%s: %v", p.Name(), ver, err)
			return err
		}
		// Hard gate for archive extracts and npm installs. Executable/script
		// installers may populate the install tree asynchronously or outside
		// installPath (e.g. rustup), so binary-presence checks would
		// false-fail them. npm is synchronous — the tree is complete when
		// the npm CLI returns (Spec 039).
		kind := ResolveKind(arch)
		if kind == manifest.InstallKindArchive || kind == manifest.InstallKindNPM {
			if err := ensureInstallIntegrity(p, installPath, arch.Strip); err != nil {
				log.Printf("[install] ERROR: %v", err)
				// Best-effort cleanup so the broken dir is not left as "installed".
				_ = os.RemoveAll(installPath)
				return err
			}
			log.Printf("[install] Install-by-kind complete (integrity OK)")
		} else {
			log.Printf("[install] Install-by-kind complete (kind=%s, skip extract integrity)", kind)
		}

		// Post-install hook
		if err := p.PostInstall(ctx, installPath); err != nil {
			log.Printf("[install] WARNING: post-install hook failed for %s@%s: %v", p.Name(), ver, err)
		}
		return nil
	}()
	if err != nil {
		return nil, err
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
	// F-25: reject an unsafe tool/version BEFORE anything touches the cache.
	// 上面的 cacheArchivePath 已经给出精确错误；这里改用 E 版本并把诊断错误也
	// 检查一遍（**防御性**：若日后有人删掉上面的预检，这里仍然会以可读错误失败，
	// 而不是带着空 dest 继续走到 downloader 的 "empty destination"）。
	url, fallbackURLs, dest, urlShaMap, optErr := i.BuildDownloadOptionsE(tool, ver, a)
	if optErr != nil {
		log.Printf("[install] ERROR: %v", optErr)
		return optErr
	}
	log.Printf("[install] Cache destination: %s", dest)

	progressDesc := fmt.Sprintf("%s@%s", tool, ver)
	if batchTotal > 0 {
		progressDesc = fmt.Sprintf("[%d/%d] %s", batchIndex, batchTotal, progressDesc)
	}
	if _, err := os.Stat(dest); err == nil {
		// Cache hit — verify SHA-256 if available
		if a.SHA256 != "" && !util.IsAllZeroHash(a.SHA256) {
			if cacheHash, err := util.FileSHA256(dest); err == nil {
				// F-24: accept the cache when it matches the OFFICIAL hash OR
				// the manifest-declared MIRROR hash. A mirror may legitimately
				// serve a byte-different rebuild under mirror_sha256; pre-fix
				// only a.SHA256 was compared, so such a cache was judged a
				// mismatch, deleted and re-downloaded on every install.
				// runInstaller and the URLShaMap already use this
				// "official OR mirror" rule — this makes the cache-hit branch
				// consistent with them. A placeholder mirror hash is ignored.
				matchedOfficial := strings.EqualFold(cacheHash, a.SHA256)
				matchedMirror := false
				if !matchedOfficial && a.MirrorSHA256 != "" && !util.IsAllZeroHash(a.MirrorSHA256) {
					matchedMirror = strings.EqualFold(cacheHash, a.MirrorSHA256)
				}
				if !matchedOfficial && !matchedMirror {
					expPreview := a.SHA256
					if len(expPreview) > 16 {
						expPreview = expPreview[:16] + "..."
					}
					gotPreview := cacheHash
					if len(gotPreview) > 16 {
						gotPreview = gotPreview[:16] + "..."
					}
					mirrorPreview := a.MirrorSHA256
					if len(mirrorPreview) > 16 {
						mirrorPreview = mirrorPreview[:16] + "..."
					}
					log.Printf("[install] Cache SHA-256 mismatch! expected=%s (mirror=%s) got=%s — re-downloading", expPreview, mirrorPreview, gotPreview)
					os.Remove(dest)
					// Fall through to download
				} else {
					if matchedMirror {
						log.Printf("[install] Cache hit (verified against the mirror hash): %s", dest)
					} else {
						log.Printf("[install] Cache hit (verified): %s", dest)
					}
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
	// Ticket 13 (P0-02): the downloader needs the exact signed-map slot
	// (<version> → <os>/<arch>) and the unsigned-version verdict so its
	// three signature gates behave correctly. Derivation is shared with
	// the GUI pipeline (DeriveUnsignedVersion).
	var toolManifest *manifest.Tool
	if p, ok := i.reg.Get(tool); ok {
		toolManifest = p.Manifest()
	}
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
		ToolName:             tool,
		Version:              ver,
		OSArch:               a.OS + "/" + a.Arch,
		UnsignedVersion:      DeriveUnsignedVersion(toolManifest, ver),
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
		embeddedHit := false
		if embeddedSig, err := signer.ReadEmbeddedSignature(tool); err == nil && embeddedSig != nil {
			if sigJSON, err := json.Marshal(embeddedSig); err == nil {
				manifestSig = sigJSON
				embeddedHit = true
				log.Printf("[install] Found embedded manifest signature for %s", tool)
			}
		}
		// Ticket 13 (N-03): a disk manifest under third-party/ must NOT be
		// able to downgrade a BUILT-IN tool to the third-party trust model
		// (TOFU fallback instead of a mandatory signature). The built-in
		// side is: an embedded manifest for this tool (embeddedHit covers
		// the signed case) or a top-level manifest in the manifest dir.
		// Only tools that exist SOLELY under third-party/ may fall back.
		if !embeddedHit && !isBuiltinManifest(tool, i.cfg.Core.ManifestDir) {
			if _, err := os.Stat(filepath.Join(thirdPartyDir, tool+".yaml")); err == nil {
				isThirdParty = true
				log.Printf("[install] Third-party manifest detected for %s (TOFU fallback if no signature)", tool)
			}
		} else {
			log.Printf("[install] %s is a built-in tool — a third-party manifest with the same name cannot downgrade it to TOFU (rename the third-party manifest)", tool)
		}
	}
	return manifestSig, isThirdParty
}

// isBuiltinManifest reports whether tool belongs to the built-in manifest
// set: an embedded manifest, or a top-level manifest in the manifest
// directory. Ticket 13 (N-03) — the same-name trust downgrade guard.
func isBuiltinManifest(tool, manifestDir string) bool {
	if manifest.HasEmbeddedManifest(tool) {
		return true
	}
	if _, err := os.Stat(filepath.Join(manifestDir, tool+".yaml")); err == nil {
		return true
	}
	return false
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

	// Ticket 26 (A-11/D-10): pass the entry's ORIGINAL os/arch. Substituting
	// "any" with the host platform made the update target a concrete entry
	// that may not exist, so the TOFU pin was silently not written back and
	// the next install re-ran TOFU. The manifest-side matcher now falls back
	// to an "any" entry when the requested key is the host platform.
	osName := a.OS
	arch := a.Arch

	// Write to on-disk manifest. Ticket 13 (N-04): use the configured
	// manifest dir (the pre-fix code hard-coded <home>/manifests, so a
	// custom --manifest-dir deployment pinned the hash into the wrong tree
	// and the next install re-ran TOFU instead of verifying).
	manifestDir := i.cfg.Core.ManifestDir
	if err := manifest.UpdateArchiveSHA256(manifestDir, tool, ver, osName, arch, actualSHA); err != nil {
		// Host-platform fallback (ticket 26 / A-11): entries declared with
		// the host platform but requested as "any" (or vice versa) must still
		// pin. Retry once with the host platform before giving up.
		hostOS, hostArch := plugin.HostOS(), plugin.HostArch()
		if (osName == "" || osName == "any" || arch == "" || arch == "any") && (osName != hostOS || arch != hostArch) {
			if retryErr := manifest.UpdateArchiveSHA256(manifestDir, tool, ver, hostOS, hostArch, actualSHA); retryErr == nil {
				log.Printf("[install] TOFU: pinned %s@%s via host-platform fallback (%s/%s)", tool, ver, hostOS, hostArch)
				osName, arch = hostOS, hostArch
			} else {
				return fmt.Errorf("UpdateArchiveSHA256: %w (host-platform fallback also failed: %v)", err, retryErr)
			}
		} else {
			return fmt.Errorf("UpdateArchiveSHA256: %w", err)
		}
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
		// F-21: parts[0] is an arbitrary token from a sidecar file and may
		// be shorter than 16 chars (a truncated/corrupt pin such as "abc").
		// The pre-fix code sliced [:16] unconditionally and panicked BEFORE
		// the caller's os.Remove — so the broken pin was never cleared and
		// every subsequent install crashed the same way. Truncate only when
		// the value is actually longer (same pattern as the cache-hit
		// preview above).
		pinPreview := parts[0]
		if len(pinPreview) > 16 {
			pinPreview = pinPreview[:16] + "..."
		}
		hashPreview := cacheHash
		if len(hashPreview) > 16 {
			hashPreview = hashPreview[:16] + "..."
		}
		return false, fmt.Sprintf("cache sidecar pin mismatch for %s (cached=%s got=%s)",
			dest, pinPreview, hashPreview)
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

// BuildDownloadOptionsE 构造下载所需的 URL、回退 URL 列表与缓存目标路径，
// 并把**诊断错误作为第五个返回值**交给调用方。
//
// 返回 (primaryURL, fallbackURLs, cacheDestPath, urlShaMap, err)。
// 当 err != nil 时 cacheDestPath 一定为空（**fail closed**），调用方应直接呈现 err。
//
// # 为什么是这个签名（而不是四返回值 + 访问器）
//
// 第十一轮先用「实例字段 `lastBuildOptionsErr` + 访问器」承载诊断信息。它能工作，
// 但有**隐含的单线程假设**：两个 goroutine 同时调用会互相覆盖，A 调用方读到 B 的
// 错误 —— 既是真实数据竞争，也**比没有诊断更糟**（把排查引向无关的工具/版本）。
//
// 第十二轮改成第五个返回值；第十三轮把四返回值版**内联掉**（它当时已是纯兼容壳，
// 生产代码只剩一处调用）。留两个版本就有「行为分叉」的长期风险 ——
// 这正是本仓库 Round 8-11 反复出现的失败模式。
func (i *Installer) BuildDownloadOptionsE(tool, ver string, a *manifest.Archive) (string, []string, string, map[string]string, error) {
	return i.buildDownloadOptions(tool, ver, a)
}

// buildDownloadOptions 是真正的实现（见 BuildDownloadOptionsE 的文档）。
//
// 第十一轮审计（Round 10 报告待办 7）：`dest == ""` 时**原因不再丢失**。
// 早先的做法是：缓存路径不安全时把 dest 置空，让 `downloader.Download` 以
// `"empty destination"` 失败 —— **fail closed 是对的**，但用户看到的错误是
// `empty destination`，完全无法定位真实原因（工具名非法？版本号含分隔符？format 为空？）。
func (i *Installer) buildDownloadOptions(tool, ver string, a *manifest.Archive) (string, []string, string, map[string]string, error) {
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

	// F-25: the cache destination embeds the manifest-controlled tool name
	// and version. An unsafe component must never produce a path outside
	// CacheDir. BuildDownloadOptions cannot return an error (the GUI calls
	// it), so an unsafe name yields an EMPTY destination: downloader.Download
	// rejects "" with "empty destination", i.e. the operation fails closed
	// instead of writing outside the cache. The install entry points
	// (InstallWithProgress / ExtractAndActivate) reject unsafe names earlier
	// with a precise error, so this is defence in depth.
	dest, nameErr := i.cacheArchivePath(tool, ver, a.Format)
	var diagErr error
	if nameErr != nil {
		log.Printf("[install] ERROR: refusing unsafe cache path: %v", nameErr)
		dest = ""
		// 精确原因作为返回值交给调用方（不再写共享字段 —— 并发下会互相覆盖）。
		diagErr = fmt.Errorf(
			"refusing to build a cache path for %s@%s (format %q): %w", tool, ver, a.Format, nameErr)
	}

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

	return primary, fallbackURLs, dest, shaMap, diagErr
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

// validateInstallComponent 在写任何文件之前校验 tool/version 是安全的
// 单路径分量（20261021 审计 P1-5 阶段 1）。
//
// 这不是“防御性重复”：`plugin.InstallDir` 在 SafeName 失败时是 fail-open 的
// （替换成 unsafe-version-blocked 后继续），因此在安装器入口集中 fail-closed
// 才是当前接口下唯一能保证“非法值零副作用”的位置。
func validateInstallComponent(kind, value string) error {
	if _, err := util.SafeName(value); err != nil {
		return fmt.Errorf("refusing install: unsafe %s %q: %w", kind, value, err)
	}
	return nil
}

// cacheArchiveName builds the on-disk cache file name
// "<tool>-<version>.<format>" for a downloaded archive.
//
// F-25: tool and ver are manifest-controlled and were previously interpolated
// into the path with no validation, so a version such as "../../../../evil"
// escaped CacheDir entirely (the download then landed outside the cache).
// plugin.Generic.InstallDir and luaBasePlugin.InstallDir already run the
// version through util.SafeName; this applies the same rule to the cache path.
//
// It returns ("", error) when either component is unsafe. Callers must treat
// that as a hard failure and must NOT fall back to an unsanitised name.
//
// 第九轮审计 P3-10：**空的 format 必须被拒绝**。
//
// 旧实现容忍空 format（注释称「产出 "tool-ver."，后续由 extractor 的 format 分支
// 拒绝」），但那个名字带**尾随点**，而 Windows 会静默剥离尾随点/空格：
//
//	cacheArchiveName("go", "1.0",     "zip") -> "go-1.0.zip"
//	cacheArchiveName("go", "1.0.zip", "")    -> "go-1.0.zip." == "go-1.0.zip"
//
// 两者**折叠成同一个缓存文件**：后写者覆盖先写者，其 sidecar pin 在下次安装时
// 与内容不匹配（缓存串味）。已在本机实测碰撞（写 go-1.0.zip 后再写
// go-1.0.zip.，目录里文件数 = 1）。
//
// util.SafeName 的文档正是针对这个危害；归档名必须服从同一规则，而不是指望
// extractor 事后拒绝——那时字节已经落盘。
//
// 影响面已核对：`install_kind: npm` 不调本函数；`executable`/`script` 本就要求
// format 非空（`manifest.Archive.Installable()`）；`extract` 与
// `BuildDownloadOptions` 自动继承 fail-closed；既有测试无用例声明 `Format: ""`。
func cacheArchiveName(tool, ver, format string) (string, error) {
	if _, err := util.SafeName(tool); err != nil {
		return "", fmt.Errorf("unsafe tool name %q for cache path: %w", tool, err)
	}
	if _, err := util.SafeName(ver); err != nil {
		return "", fmt.Errorf("unsafe version %q for cache path: %w", ver, err)
	}
	if strings.TrimSpace(format) == "" {
		return "", fmt.Errorf("empty archive format for cache path %s-%s — refusing to build a trailing-dot name that collides on Windows (P3-10)", tool, ver)
	}
	if _, err := util.SafeName(format); err != nil {
		return "", fmt.Errorf("unsafe archive format %q for cache path: %w", format, err)
	}
	return fmt.Sprintf("%s-%s.%s", tool, ver, format), nil
}

// cacheArchivePath is the error-returning variant used by the install paths:
// an unsafe tool/version is a hard, actionable failure rather than a path
// escaping the cache directory.
func (i *Installer) cacheArchivePath(tool, ver, format string) (string, error) {
	name, err := cacheArchiveName(tool, ver, format)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(i.cfg.Core.CacheDir, name)
	// Defence in depth: the joined path must stay inside CacheDir.
	if err := util.ValidateSubPath(i.cfg.Core.CacheDir, dest); err != nil {
		return "", fmt.Errorf("cache path %q escapes %q: %w", dest, i.cfg.Core.CacheDir, err)
	}
	return dest, nil
}

// extract unpacks a cached archive to the destination directory.
// It delegates to extractor.Extract which supports zip, tar.gz, tar.xz,
// 7z, and zstd formats. The strip parameter controls how many leading
// path components to remove (e.g. strip=1 removes the top-level directory
// common in Go and Python archives).
func (i *Installer) extract(tool string, a *manifest.Archive, ver, dest string) error {
	// The cache filename uses the tool name (matching downloadWithProgress).
	// F-25: the name is validated so a traversal version cannot read/write
	// outside CacheDir.
	src, err := i.cacheArchivePath(tool, ver, a.Format)
	if err != nil {
		log.Printf("[install] ERROR: refusing unsafe cache path: %v", err)
		return err
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		// Ticket 26 (A-09): NO glob fallback. The removed glob
		// (`*-<ver>.<fmt>`) could match ANOTHER tool's cache file — a
		// malformed name such as "lld_is_a_generic_driver._" would be
		// extracted into this tool's install tree (wrong content, wrong
		// integrity outcome). A cache miss is now a hard, actionable error.
		log.Printf("[install] ERROR: cache miss for extraction, expected: %s", src)
		return fmt.Errorf("cache miss: %s not found — clear the cache entry and reinstall (`xdev install %s@%s`)", src, tool, ver)
	}
	log.Printf("[install] Extracting: src=%s dest=%s format=%s strip=%d", src, dest, a.Format, a.Strip)
	err = extractor.Extract(src, dest, a.Format, a.Strip)
	if err != nil {
		log.Printf("[install] ERROR: extraction failed: %v", err)
		// F-23: a failed extraction can leave a partially written tree
		// behind (e.g. the archive's first entry extracts, a later one is
		// corrupt). Because VerifyInstallIntegrity only requires AT LEAST
		// ONE declared shim to be present, such a leftover tree looked
		// "completely installed" to the next run — which then skipped the
		// download/extract entirely and activated garbage. Remove the
		// partial tree so a retry starts clean.
		//
		// RemoveAll is safe here: `dest` is the install tree, never the
		// current/<tool> link (extraction targets install/<tool>/<ver>).
		if rmErr := os.RemoveAll(dest); rmErr != nil {
			log.Printf("[install] WARNING: failed to clean partial install tree %s: %v", dest, rmErr)
		} else {
			log.Printf("[install] Removed partial install tree after extraction failure: %s", dest)
		}
	} else {
		log.Printf("[install] Extraction complete: %s", dest)
	}
	return err
}

// ResolveKind returns the effective install kind for an archive, applying
// a conservative URL-based heuristic when the manifest doesn't declare
// install_kind explicitly. This lets older manifests (e.g. rust.yaml
// before the kind field was added) benefit from executable/script
// routing without re-signing — the signature payload only covers
// tool+sha256map+timestamp, not kind.
// Spec 035 F10: exported so the GUI preview logic (PreviewInstall) uses
// the SAME kind resolution as the installer instead of a diverging
// copy of the rule. A nil archive safely defaults to archive kind.
func ResolveKind(a *manifest.Archive) string {
	if a == nil {
		return manifest.InstallKindArchive
	}
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
//   - npm: run the npm CLI against the install dir (no download; Spec 039)
func (i *Installer) installByKind(ctx context.Context, p plugin.Plugin, a *manifest.Archive, ver, installPath string) error {
	kind := ResolveKind(a)
	switch kind {
	case manifest.InstallKindExecutable, manifest.InstallKindScript:
		return i.runInstaller(ctx, p, a, ver, installPath)
	case manifest.InstallKindNPM:
		return i.installViaNPM(ctx, p, a, ver, installPath)
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
	// F-25: validate the cache path components before building it.
	src, err := i.cacheArchivePath(p.Name(), ver, a.Format)
	if err != nil {
		return err
	}
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
		// Ticket 38 (P2-03): a mirror URL may legitimately serve a
		// byte-identical rebuild under its own mirror_sha256 — accept the
		// SHA-256 OR the declared mirror SHA-256 (whichever the manifest
		// provided). Pre-fix only a.SHA256 was accepted, so an installer
		// fetched from a mirror was rejected even when its hash matched the
		// manifest's mirror entry.
		matched := strings.EqualFold(got, a.SHA256)
		if !matched && a.MirrorSHA256 != "" && !util.IsAllZeroHash(a.MirrorSHA256) {
			matched = strings.EqualFold(got, a.MirrorSHA256)
			if matched {
				log.Printf("[install] %s SHA-256 verified against the mirror hash", p.Name())
			}
		}
		if !matched {
			return fmt.Errorf("SHA-256 mismatch for %s: expected %s (mirror %s), got %s", p.Name(), a.SHA256, a.MirrorSHA256, got)
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

	kind := ResolveKind(a)
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
// fallback chain is: junction → symlink → `ln -s`. There is deliberately NO
// directory-copy fallback (F-22): a real directory at current/<tool> breaks
// util.ActiveVersion, which resolves the path with os.Readlink.
//
// The replacement link is created under a temporary name and then renamed
// over the old one, so a failure never destroys the previously active link.
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

	// Security (M-5 fix): p.Name() comes from the manifest YAML and is
	// used to build current/<name>. A manifest named "..\evil" must never
	// create a link outside current/ — validate as a safe single path
	// component and re-check containment before any filesystem write.
	if _, err := util.SafeName(p.Name()); err != nil {
		return fmt.Errorf("refusing to activate tool with unsafe name %q: %w", p.Name(), err)
	}
	// 20261021 审计 P1-5（阶段 1）：版本同样进入 current 的解析与安装树
	// 定位，必须在落任何文件之前拒绝非法值。
	if _, err := util.SafeName(ver); err != nil {
		return fmt.Errorf("refusing to activate tool %q with unsafe version %q: %w", p.Name(), ver, err)
	}

	currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		log.Printf("[install] ERROR: failed to create current dir %s: %v", currentDir, err)
		return err
	}
	currentLink := filepath.Join(currentDir, p.Name())
	if err := util.ValidateSubPath(currentDir, currentLink); err != nil {
		return fmt.Errorf("activation path %q escapes current dir: %w", currentLink, err)
	}
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

	// --- Step 1: Create junction/symlink ---
	//
	// F-22: build the replacement link under a TEMPORARY name first and only
	// swap it in once it exists. Pre-fix the old link was removed
	// unconditionally before any link method ran, so an activation whose link
	// creation failed left the tool permanently un-activated even though the
	// previous version was still fully installed. With this order the old link
	// survives every failure of link creation itself.
	junctionCreated := false
	// F-22/P1-24: the swap target must be a UNIQUELY named temporary entry,
	// not a fixed "<link>.tmp" — concurrent activations of the same tool
	// would otherwise share the path and clobber each other. os.CreateTemp
	// yields an atomically unique name; the placeholder file is removed and
	// the link is created in its place.
	tmpFile, tmpErr := os.CreateTemp(currentDir, p.Name()+".tmp-*")
	if tmpErr != nil {
		return fmt.Errorf("activate %s: reserve temporary link name: %w", p.Name(), tmpErr)
	}
	tmpLink := tmpFile.Name()
	tmpFile.Close()
	removeLinkEntry(tmpLink, i.cfg.Core.XMDevDir)

	if err := linkCurrent(installPath, tmpLink); err != nil {
		log.Printf("[install] ERROR: all link methods failed for %s -> %s: %v", currentLink, installPath, err)
		removeLinkEntry(tmpLink, i.cfg.Core.XMDevDir)
		// The previous link was never touched — nothing to restore.
		return fmt.Errorf("symlink %s -> %s (all link methods failed: %v)", currentLink, installPath, err)
	}

	// The new link exists. Swap it in; removeLinkEntry only unlinks the
	// current entry and never follows it into the install tree.
	removeLinkEntry(currentLink, i.cfg.Core.XMDevDir)
	if err := os.Rename(tmpLink, currentLink); err != nil {
		log.Printf("[install] ERROR: failed to swap in the new link %s: %v", currentLink, err)
		removeLinkEntry(tmpLink, i.cfg.Core.XMDevDir)
		// F-22: restore the previous link instead of leaving the tool
		// un-activated.
		i.rollbackActivation(currentLink, junctionExisted, oldJunctionTarget, p)
		return fmt.Errorf("activate %s: replace current link: %w", p.Name(), err)
	}
	junctionCreated = true

	// --- Step 2: PATH mode dispatch ---
	//
	// 2026-10-25（P0-4 / P3）：显式 switch。
	//   - direct：把每个工具的 current/<tool>/bin 直接写进 PATH
	//   - shim / auto：都走 shim 分支
	//
	// auto 为什么与 shim 同分支：auto = shim + cd hook 项目级切换。
	// 版本选择发生在**运行时**（shim.Lookup 读 .xdev.yaml），
	// 与安装时写什么 PATH 无关，所以安装阶段与 shim 完全一致。
	pathsAdded := false
	switch i.cfg.GetPathMode() {
	case "direct":
		currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
		binPaths := env.ToolBinPaths(p.Name(), currentDir)
		// P1（2026-10-25）：合并 manifest 声明的 path_append。
		// 这些是包管理器自己的 bin（$GOPATH/bin、$CARGO_HOME/bin 等），
		// 不在 current/<tool>/bin 下，不追加的话 `go install` / `cargo install`
		// 装完会 command not found。
		binPaths = append(binPaths, p.PathAppend(installPath)...)
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
	default: // "shim"（GetPathMode 已保证只可能是 shim 或 direct）
		shimExe := i.ResolveShimExe()
		log.Printf("[install] Creating shims in %s (exe=%s)", i.cfg.Core.ShimDir, shimExe)
		// F-26: align with removeShims — an invalid shim NAME is skipped with
		// a warning instead of aborting the whole loop, so one bad entry no
		// longer prevents the remaining valid shims from being installed
		// (pre-fix the first invalid name failed activation outright and
		// rolled everything back). Genuine install failures are collected and
		// reported together; if any occurred, the previous state is restored.
		var shimErrs []string
		for _, name := range p.Shims() {
			if err := util.ValidateShimName(name); err != nil {
				log.Printf("[install] WARNING: skipping invalid shim name %q: %v", name, err)
				continue
			}
			log.Printf("[install] Installing shim: %s", name)
			if err := shim.InstallShim(i.cfg.Core.ShimDir, name, shimExe); err != nil {
				log.Printf("[install] ERROR: shim install failed for %s: %v", name, err)
				shimErrs = append(shimErrs, fmt.Sprintf("%s: %v", name, err))
			}
		}
		if len(shimErrs) > 0 {
			// H-9 Rollback: restore previous junction
			i.rollbackActivation(currentLink, junctionExisted, oldJunctionTarget, p)
			return fmt.Errorf("shim install failed for %d shim(s) [%s] (activation rolled back)",
				len(shimErrs), strings.Join(shimErrs, "; "))
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
	// 第十二轮审计（Round 11 报告 §七-④）：**激活成功后顺带回收 `.stale-*` 残留**。
	//
	// 之前只在 `buildShimIndexMap` 里回收，而那个函数只在「重建 shim 反向索引」时
	// 才跑 —— 长期不触发索引重建的用户（例如只用 `xdev use` 切换、从不安装新 shim）
	// 残留永远不会被回收。激活是**必然经过**的路径，在这里补一次覆盖。
	//
	// best-effort：失败绝不影响激活结果（回收是卫生工作）。
	_ = shim.ReapStaleResidue(i.cfg.Core.XMDevDir)

	// Ticket 01 (audit-fixes / X-07): for the go tool, self-heal the stale
	// GOROOT registry value left by the pre-v2 manifest (GOROOT pointed one
	// level above the real toolchain because the archives keep their
	// top-level go/ directory). The manifest no longer writes GOROOT —
	// Go >= 1.26 self-derives it from the go binary location — so existing
	// installs recover on the next `xdev use go` / activate.
	if p.Name() == "go" && !i.noEnvWrite {
		if err := env.CleanupStaleGOROOT(); err != nil {
			// Non-fatal: activation itself succeeded.
			log.Printf("[install] WARNING: stale GOROOT cleanup failed: %v", err)
		}
	}

	// 广播 WM_SETTINGCHANGE 让新终端感知 PATH 变化
	if !i.noEnvWrite {
		env.BroadcastEnvironmentChange()
	}

	_ = junctionCreated
	_ = envVarsWritten

	return nil
}

// linkCurrent creates the current/<tool> link at `link` pointing at `target`.
// It is a package-level variable so tests can force the "every link method
// failed" path, which is otherwise only reachable on a hostile filesystem or
// when the target binary is locked.
//
// F-22: the chain is junction (Windows) → symlink → fallbackLink. The old
// chain ended in util.CopyDir, which produced a real directory COPY: that
// violates the "current must be a link" contract because
// util.ActiveVersion resolves the path with os.Readlink and therefore
// reported ("", false) forever — the tool looked broken and no later
// `xdev use` could repair it.
var linkCurrent = defaultLinkCurrent

// defaultLinkCurrent implements the platform link chain.
func defaultLinkCurrent(target, link string) error {
	// Windows 上优先使用 Junction（不需要管理员权限，比 SymbolicLink 更可靠）
	if runtime.GOOS == "windows" {
		if err := util.CreateJunction(link, target); err == nil {
			return nil
		} else {
			log.Printf("[install] Junction failed, trying symlink: %v", err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		log.Printf("[install] Symlink failed, trying fallbacks: %v", err)
		return fallbackLink(target, link)
	}
	return nil
}

// removeLinkEntry removes a current/<tool> entry (junction, symlink or stale
// leftover) WITHOUT following it into the install tree: os.Remove unlinks the
// reparse point itself and never recurses into its target. This is why the
// code must never use os.RemoveAll here.
//
// 第八轮审计 P1-3（F-22 的自引入回归）：旧版本 xdev 的 CopyDir 兜底会在
// `current/<tool>` 留下一个**真实目录**（非 reparse point）。os.Remove 对**非空**
// 目录必定失败（Windows: "The directory is not empty"），随后
// `os.Rename(tmpLink, currentLink)` 也无法覆盖已存在的目录
// （"Access is denied"），`rollbackActivation` 走同一个函数同样失败 —— 于是
// `xdev install`/`xdev use` **每次重试都是同一条错误链，永远修不好**，
// 正是 F-22 注释里声称已消除的症状（实测 H10）。
//
// 修复：显式识别这个已知的遗留形态（真实目录、非 symlink、非 junction），把它
// **重命名**到一个不再被 `ActiveVersion` 解析的旁路名字（`.stale-<随机>`）后
// 再 RemoveAll。先 rename 是关键：`current/` 下立刻不再有该条目，安装流程可以
// 继续；即便随后的删除失败，残留也只是一个不影响解析的旁路目录，且删除是
// 有界的（只删这个我们已确认是遗留产物的目录）。
//
// 绝不在此对**链接**调用 RemoveAll —— 那会顺着 reparse point 删掉安装树。
// removeAllResidue 是测试接缝：让归档后的删除失败，从而保留归档条目，
// 用于断言“归档位置确实由显式 xmdeHome 决定”。生产路径就是 os.RemoveAll。
var removeAllResidue = os.RemoveAll

func removeLinkEntry(path, xmdeHome string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return // nothing there
	}
	if err := os.Remove(path); err == nil {
		return
	} else if !isLegacyRealDir(fi) {
		// 链接/junction/文件：os.Remove 已尽力，失败只告警（绝不 RemoveAll）。
		log.Printf("[install] WARNING: failed to remove link entry %s: %v", path, err)
		return
	}

	// 遗留的真实目录（旧 CopyDir 产物）：rename 到旁路名后再删。
	//
	// 第十二轮收口（Round 12 报告 §七-③）：**rename 的目标改到 `current/` 之外**。
	//
	// 旧实现是 `path + ".stale-" + <nano>`，也就是残留**留在 `current/` 里**。
	// 这导致一组互相矛盾的需求：回收方要「够旧就删」，而并发写方可能刚 rename 完、
	// 正要去 RemoveAll —— 为保证不误删在途条目，回收方只能按年龄等 1 小时，
	// 于是刚产生的残留必然在 `current/` 里停留一小时，期间仍是一个需要被所有扫描方
	// 过滤掉的污染源。
	//
	// 搬到 `current/` 之外（`<xmdeHome>/.stale-residue/`）就解开了这对矛盾：
	// 归档目录里的条目**永远不可能在途**，因此可以安全删除；而 `current/`
	// 从此**根本不会**出现 `.stale-*`（除历史遗留，由 shim.ReapStaleResidue 清）。
	stale, staleErr := shim.ResidueArchivePath(xmdeHome, filepath.Base(path))
	if staleErr != nil {
		// 归档目录建不出来：退回「就地改名 + 立即删」（旧行为），至少不阻塞安装。
		log.Printf("[install] WARNING: cannot create residue archive dir: %v — falling back to in-place rename", staleErr)
		stale = path + ".stale-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if rerr := os.Rename(path, stale); rerr != nil {
		log.Printf("[install] WARNING: failed to remove legacy directory %s: %v", path, rerr)
		return
	}
	log.Printf("[install] removed legacy CopyDir directory at %s (moved to %s)", path, stale)
	// 归档目录里的条目不在 current/ 下，可以直接删；失败也没关系 ——
	// 它已经不再污染 current/，shim.ReapStaleResidue 会按年龄收尾。
	if rerr := removeAllResidue(stale); rerr != nil {
		log.Printf("[install] WARNING: could not fully delete legacy directory %s (archived; will be reaped): %v", stale, rerr)
	}
}

// isLegacyRealDir 报告该 FileInfo 是否为「旧版 CopyDir 兜底留下的真实目录」——
// 即：是目录，且不是 symlink、不是 Windows junction（Lstat 报 ModeIrregular）。
// 只有这一种形态才允许走 rename+RemoveAll 的清理路径。
func isLegacyRealDir(fi os.FileInfo) bool {
	if fi == nil || !fi.IsDir() {
		return false
	}
	m := fi.Mode()
	if m&os.ModeSymlink != 0 {
		return false
	}
	if m&os.ModeIrregular != 0 { // Windows junction
		return false
	}
	return true
}

// rollbackActivation restores the previous junction/symlink after a failed
// activation step. This is part of the H-9 fix for partial-activation failures.
func (i *Installer) rollbackActivation(currentLink string, junctionExisted bool, oldTarget string, p plugin.Plugin) {
	log.Printf("[install] Rolling back activation for %s", currentLink)
	// removeLinkEntry (not os.RemoveAll): a legacy CopyDir fallback left a
	// REAL directory here, and RemoveAll would delete the installed files.
	removeLinkEntry(currentLink, i.cfg.Core.XMDevDir)
	if junctionExisted && oldTarget != "" {
		if err := linkCurrent(oldTarget, currentLink); err != nil {
			log.Printf("[install] WARNING: failed to restore previous link during rollback: %v", err)
		} else {
			log.Printf("[install] Restored previous junction: %s -> %s", currentLink, oldTarget)
		}
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
		// 2026-10-25 修复（P0-3）：写侧必须与删侧对齐白名单。
		//
		// 旧行为：写侧只靠 SetUserEnvVarTrusted 做语法/保护变量校验，**不过白名单**；
		// 删侧（removeUserEnvVars:2386）却用 util.IsAllowedEnvVarName 过滤。
		// 后果：manifest 声明了白名单外的变量（新工具/用户自定义 manifest/Lua 插件）时
		//   写成功 → 卸载时被跳过 → 永久残留；
		//   且 EnrichFromRegistry 也只合并白名单变量 → shim 子进程读不到。
		// 表现为「变量写了但没用、删又删不掉」。
		//
		// 现改为写侧同样拒绝：显式报错（触发回滚）而不是静默写入一个永远删不掉的变量。
		if !util.IsAllowedEnvVarName(name) {
			log.Printf("[install] ERROR: refusing to write non-whitelisted env var %q "+
				"(add it to util.AllowedEnvVarNames if it is intentional)", name)
			firstErr = fmt.Errorf("env var %q is not in the allowed whitelist", name)
			break
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
				// P2-4（第六轮审计）：回滚必须用 Restore 变体——原值先于 xdev 存在
				//（用户自己的 GOROOT=C:\Go / 含 %VAR% 的原值），对回滚值复跑 X-08
				// 写时校验会拒绝恢复，导致「安装失败 + manifest 值留驻 + 用户原值
				// 永久丢失」的更坏终态。
				if err := env.SetUserEnvVarRestore(name, orig); err != nil {
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
	// Ticket 19 (audit-fixes / B-02, A-04): the ENTIRE pre-activation span —
	// extraction, the executable/script installer run, the integrity gate and
	// the post-install hook — runs under the same per-tool lock used by
	// InstallWithProgress, InstallNPMPackage and Activate. Without it a GUI
	// ExtractAndActivate (local-tool migration / custom-tool install) could
	// interleave with a concurrent force install of the same tool and leave a
	// half-written tree behind.
	//
	// The lock is released BEFORE Activate, which takes the same lock
	// internally (nested acquisition self-deadlocks — Unix flock is per open
	// file description, the Windows named mutex is reentrant only
	// per-thread; see the InstallWithProgress comment for the full story).
	//
	// Known residual (F-03, disclosed): the Windows named mutex is
	// thread-affine, so a goroutine migration between acquisition and release
	// can still let a concurrent installer slip through. The minimal
	// hardening (Release return-value logging) lives in mutex_windows.go.
	toolLock, lockErr := util.AcquireToolLock(i.cfg.Core.XMDevDir, p.Name(), 5*time.Minute)
	if lockErr != nil {
		return fmt.Errorf("extract lock %s: %w", p.Name(), lockErr)
	}
	err := func() error {
		defer toolLock.Release()

		kind := ResolveKind(a)
		switch kind {
		case manifest.InstallKindExecutable, manifest.InstallKindScript:
			// 可执行/脚本安装器：运行安装器（含 SHA/sidecar pin 校验），
			// 跳过解压与完整性硬门（安装器可能异步/在 installPath 外写文件）。
			if err := i.runInstaller(context.Background(), p, a, ver, installPath); err != nil {
				return fmt.Errorf("installer run failed: %w", err)
			}
		case manifest.InstallKindNPM:
			// Spec 039: npm-kind 工具没有「已下载归档」可解压——调用方应改走
			// InstallNPMPackage（预检 → npm install → 完整性门 → 激活）。
			return fmt.Errorf("npm-kind tool %s must be installed via InstallNPMPackage (no archive to extract)", p.Name())
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
		return nil
	}()
	if err != nil {
		return err
	}

	// Activation runs WITHOUT the extract lock (it re-acquires the same lock).
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
	// 20261021 审计 P1-5（阶段 1）：卸载路径同样用 SafeName 校验
	// tool/version，避免把非法值带进 install/current 的路径拼接。
	if err := validateInstallComponent("tool name", p.Name()); err != nil {
		return err
	}
	if err := validateInstallComponent("version", ver); err != nil {
		return err
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
				depPath, dirErr := other.InstallDir(i.cfg.Core.InstallDir, other.DefaultVersion())
				if dirErr != nil {
					// 依赖工具的路径不合法：视为未安装（不能阻止卸载）。
					log.Printf("[install] WARNING: cannot resolve install dir for dependent tool %q: %v", other.Name(), dirErr)
					continue
				}
				if fi, err := os.Stat(depPath); err == nil && fi.IsDir() {
					return fmt.Errorf("cannot uninstall %q: required by %q; uninstall %q first", p.Name(), other.Name(), other.Name())
				}
			}
		}
	}

	target, err := p.InstallDir(i.cfg.Core.InstallDir, ver)
	if err != nil {
		return err
	}
	log.Printf("[install] Uninstalling: %s@%s from %s", p.Name(), ver, target)

	// 第十一轮审计（Round 10 报告待办 5 / P3-7）：**必须在删除目标之前**读活跃版本。
	//
	// 旧代码把 `currentVersion`（→ `util.ActiveVersion`）放在 `os.RemoveAll(target)`
	// **之后**。它能工作，只是因为 `ActiveVersion` 当时**不检查目标是否存在** ——
	// 删掉 install/<tool>/<ver> 后 `current/<tool>` 变成悬空链接，而旧实现仍
	// 报「已激活」，于是 `activeVer == ver` 成立、下面的重指向/清理分支才会执行。
	//
	// 也就是说：Uninstall 一直**依赖那个幻影行为**来判断「被卸载的是当前活跃版本」。
	// P3-7 修复后 `ActiveVersion` 正确地返回 ("", false)，这个依赖就断了 ——
	// 卸载活跃版本时既不重指向也不清理 shim/环境变量，`current/<tool>` 永久悬空。
	// 这是「修掉一个 bug 暴露出另一个隐藏依赖」的典型形态。
	//
	// 正确做法：在删除前把活跃版本读出来，之后只用这个快照判断。
	activeVer, _ := i.currentVersion(p.Name())

	// 审查（卸载失败 unlinkat Access is denied）：**删除目录前先探测占用**。
	//
	// 旧实现直接 os.RemoveAll(target)，而 shim（E:\XDev\shims\node.exe 等）
	// 仍指向 current/<tool> → install/<tool>/<ver>/。只要用户在该版本下跑过
	// 工具（典型：一个没关的 node REPL / 后台 dev server），Windows 就无法
	// 删除正在执行的 exe，报 `unlinkat ...\node.exe: Access is denied`。
	//
	// 更糟的是 RemoveAll 是逐个删的 —— 失败时目录已被部分删除，留下
	// 一个既不能用也无法干净重装的脏状态（实测：仅剩 node.exe 一个文件）。
	//
	// 因此改为「先探测可删性，被占用就立即失败且不动任何东西」。
	if locked, lockErr := installDirLocked(target); lockErr != nil {
		return fmt.Errorf("uninstall %s@%s: cannot verify whether %s is in use: %w",
			p.Name(), ver, target, lockErr)
	} else if locked {
		return fmt.Errorf(
			"cannot uninstall %s@%s: a file under %s is in use by a running process. "+
				"Close any running %s process (or restart the terminal that launched it) and retry. "+
				"Nothing was deleted.",
			p.Name(), ver, target, p.Name())
	}

	// 确认可删后再停用：让后续启动不再指向待删目录。
	if activeVer == ver {
		_ = p.Deactivate(target)
	}
	i.removeShims(p)

	// 1. Remove the installation directory
	if err := os.RemoveAll(target); err != nil {
		log.Printf("[install] ERROR: uninstall failed for %s@%s: %v", p.Name(), ver, err)
		if isFileLockedError(err) {
			return fmt.Errorf(
				"uninstall %s@%s failed: a file under %s became in use during deletion "+
					"(this is the usual cause of \"Access is denied\" on Windows). "+
					"Close any running %s process and retry; the directory may be partially deleted: %w",
				p.Name(), ver, target, p.Name(), err)
		}
		return err
	}

	// 2. Check if the uninstalled version was the active one
	if activeVer == ver {
		// Remove the current symlink
		currentLink := filepath.Join(i.cfg.Core.XMDevDir, "current", p.Name())
		_ = os.Remove(currentLink)
		log.Printf("[install] Removed current symlink for %s", p.Name())

		// Try to re-point to another installed version
		if otherVer := i.findOtherVersion(p); otherVer != "" {
			otherPath, dirErr := p.InstallDir(i.cfg.Core.InstallDir, otherVer)
			if dirErr != nil {
				return dirErr
			}
			log.Printf("[install] Re-pointing %s to %s@%s", p.Name(), p.Name(), otherVer)
			// Ticket 03 (audit-fixes / P0-05): Release the uninstall lock
			// before re-acquiring inside Activate — Activate takes the
			// same tool lock internally, and a nested acquisition self-
			// deadlocks (Unix flock locks are per open-file-description,
			// so a second acquire from the same process blocks the full
			// 30s timeout; the Windows named mutex is reentrant only
			// per-thread, and goroutine migration between the two
			// acquisitions hangs the same way). Same pattern as the
			// InstallWithProgress critical section (Spec 037).
			// ToolLock.Release is idempotent (nil release check), so the
			// deferred release at the top of Uninstall becomes a no-op.
			// The lock is intentionally NOT re-acquired afterwards: the
			// remainder of Uninstall only touches shim-index memory state.
			lock.Release()
			if err := i.Activate(p, otherPath, otherVer); err != nil {
				log.Printf("[install] WARNING: failed to re-activate %s@%s: %v", p.Name(), otherVer, err)
			}
		} else {
			// No other version: clean up shims
			//
			// 2026-10-25：这里曾调 i.removeEnvFragment(p)，但 env 片段
			// 的**写入方** WriteEnvFragment 早已无任何调用方（死代码，已删）。
			// 删除一个从未被写过的文件是纯粹的无效操作，一并移除。
			log.Printf("[install] No other version of %s, cleaning up shims and env", p.Name())
			i.removeShims(p)
			// Security (M-1): Clean up user-level environment variables (JAVA_HOME, GOROOT, etc.)
			// that were set during Activate. These point to the now-deleted installation
			// and would cause errors if left in the registry.
			//
			// P2-3（第六轮审计）：removeUserEnvVars 的删除不广播 WM_SETTINGCHANGE——
			// RemoveCurrentBinPaths 自带广播，但注册表环境变量的删除没有；Explorer
			// 派生的新终端会继续持有已删除的 JAVA_HOME/GOROOT（正是 goroot_cleanup
			// 注释里描述的陈旧值效应）。本分支是 Uninstall 中唯一的环境变量删除点，
			// 在分支末尾广播一次覆盖 env 删除与（可能的）direct 模式 PATH 清理。
			envTouched := false
			if !i.noEnvWrite {
				i.removeUserEnvVars(p)
				envTouched = true
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
			if envTouched {
				env.BroadcastEnvironmentChange()
				log.Printf("[install] Broadcast environment change after %s env cleanup", p.Name())
			}
		}
	}

	log.Printf("[install] Uninstall complete: %s@%s", p.Name(), ver)
	shim.InvalidateShimIndex()
	// 第十二轮审计（Round 11 报告 §七-④）：卸载成功后同样回收 `.stale-*` 残留。
	// 卸载是产生残留的另一条主要路径（removeLinkEntry 的 rename+RemoveAll 可能
	// 因文件被占用而只完成 rename），在这里补一次覆盖。
	// best-effort：失败绝不影响卸载结果。
	_ = shim.ReapStaleResidue(i.cfg.Core.XMDevDir)
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
		// 20261021 审计 P1-4：与 CLI/GUI 共用版本判据；只含辅助目录的
		// 工具（Godot/export_templates）与空目录（perl）不算已安装。
		versions, verr := util.ListVersionDirNames(filepath.Join(i.cfg.Core.InstallDir, e.Name()))
		if verr != nil || len(versions) == 0 {
			continue
		}
		tools = append(tools, e.Name())
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
	versions, err := util.ListVersionDirNames(dir)
	if err != nil {
		return ""
	}
	var best string
	for _, v := range versions {
		if best == "" {
			best = v
			continue
		}
		// Compare using semver, not lexicographic ordering
		if util.CompareSemver(v, best) > 0 {
			best = v
		}
	}
	return best
}

// installDirLocked 探测 dir 内是否有「被其它进程占用」的文件。
//
// 审查（卸载 node.exe 报 unlinkat Access is denied）：Windows 不允许删除
// 正在执行的可执行文件映像。旧实现直接 RemoveAll，失败时目录已被部分删除，
// 留下既不能用也无法干净重装的脏状态。
//
// 探测手法：递归遍历目录，对每个文件尝试以**零共享模式**打开
// （Windows: FILE_SHARE_NONE；其它平台：O_RDWR）。只要有一个文件打不开，
// 就说明被占用。
//
// 注：最初尝试过「把目录 rename 到临时名」探测，但 Windows 对含有运行中
// 映像的目录仍允许 rename（rename 只改目录项，不动已有句柄），实测无效。
//
// 返回 (locked, err)：locked=true 表示被占用；err 仅在探测本身出错时返回。
// dir 不存在时返回 (false, nil)。
func installDirLocked(dir string) (bool, error) {
	if _, statErr := os.Stat(dir); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}

	locked := false
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 遍历本身出错不当作占用（交给后续 RemoveAll 报真实错误）。
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !fileInUse(path) {
			return nil
		}
		locked = true
		return fs.SkipAll
	})
	if walkErr != nil {
		return false, walkErr
	}
	return locked, nil
}

// isFileLockedError 判断 RemoveAll 失败是否由「文件被占用」引起。
//
// 审查（卸载 node.exe 报 unlinkat Access is denied）：Windows 不允许删除
// 正在执行的可执行文件映像，对映射中的 DLL 也返回共享冲突。Go 把这些
// 系统错误包成 *os.PathError / *os.LinkError，底层 errno 通常是
// ERROR_ACCESS_DENIED (5) 或 ERROR_SHARING_VIOLATION (32)。
//
// 用于把裸 errno 换成带处理建议的文案 —— 用户看到
// `unlinkat ...: Access is denied` 无法知道该做什么。
func isFileLockedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// 5 = ERROR_ACCESS_DENIED, 32 = ERROR_SHARING_VIOLATION,
		// 33 = ERROR_LOCK_VIOLATION
		if errno == 5 || errno == 32 || errno == 33 {
			return true
		}
	}
	// 回退：某些平台/包装层只给出字符串。
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "being used by another process") ||
		strings.Contains(msg, "sharing violation") ||
		strings.Contains(msg, "text file busy")
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
//
// F-22: the old chain ended in a recursive util.CopyDir of installPath ->
// dst. That produced a REAL DIRECTORY at current/<tool>, which violates the
// "current must be a link" contract — util.ActiveVersion resolves the path
// with os.Readlink and so returned ("", false) forever, leaving the tool
// permanently un-activated with no way to repair it. The copy fallback is
// gone: this function now reports an explicit error instead.
//
// The `ln` invocation passes `-sT` where supported so the link is created AT
// dst rather than INSIDE it (plain `ln -s src dst` puts the link at
// dst/<basename> when dst already exists as a directory).
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
	var lnErr error
	if cmd, err := exec.LookPath("ln"); err == nil {
		lnCmd := exec.Command(cmd, "-sT", src, dst)
		util.HideWindow(lnCmd)
		if runErr := lnCmd.Run(); runErr == nil {
			return nil
		} else {
			lnErr = runErr
		}
	} else {
		lnErr = err
	}
	// No copy fallback (see above) — fail loudly instead of materialising a
	// plain directory that ActiveVersion can never resolve.
	return fmt.Errorf("cannot create link %s -> %s: junction and symlink both failed (directory-copy fallback removed: current must be a link): %v", dst, src, lnErr)
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

	// 创建 junction
	ensureSelfJunction(cfg, installDir)

	return installDir, true, nil
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
