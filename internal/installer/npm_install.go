// npm_install.go implements the "npm" install kind (Spec 039).
//
// Tools whose archive entries declare install_kind: npm are NOT installed
// by extracting a downloaded archive — the npm package tarball only ships
// lib/*.js without its 180+ runtime dependencies, so a bare extract would
// produce a broken install. Instead the install is delegated to the npm
// CLI, run against the per-version install directory:
//
//	npm install --global --prefix <installPath> <package>@<version>
//
// npm then resolves the full dependency tree, verifies every package
// against the registry's integrity signature, and generates its standard
// launcher shims (Windows: <installPath>/dsh.cmd + dsh.ps1 + dsh;
// Unix: <installPath>/bin/dsh). Those layouts are already covered by the
// integrity-check candidates and the shim dispatcher's FindInDir, so no
// downstream changes are needed.
//
// # Pre-install dependency check (安装前检查 npm 依赖)
//
// Before npm runs, the installer verifies the npm toolchain:
//  1. Node.js + npm must be present — xdev-managed Node (current/node)
//     preferred, then system node/npm from PATH (xdev's own shim dir is
//     skipped: a stale shim must not count as a Node.js install);
//  2. if the manifest declares depends: node with min_version, the probed
//     Node version must satisfy it.
//
// Both failures are hard errors with an actionable message
// ("xdev install node").
//
// # No archive download
//
// The manifest archive entry for an npm-kind tool is metadata only: it
// seeds the sniff URL template and documents the registry tarball. The
// installer never downloads it, and npm's own registry integrity check
// replaces the archive SHA-256 / TOFU mechanism.
package installer

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/hostscan"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// npmInstallTimeout bounds the npm install subprocess. dsh pulls 180+
// packages, so this must be far more generous than a normal archive
// download; npm performs its own retries and per-request timeouts.
const npmInstallTimeout = 15 * time.Minute

// npmVersionCharset bounds the version string that is spliced into the
// npm package spec argv (no shell involved — argv injection is impossible —
// but the charset keeps npm from interpreting crafted specs).
var npmVersionCharset = regexp.MustCompile(`^[0-9A-Za-z.+-]+$`)

// npmNodeVersionProbe runs `<nodeBin> --version` and returns the trimmed
// output ("v22.14.0" style). It is a package-level var so tests can stub
// the probe without a real Node.js runtime.
var npmNodeVersionProbe = func(ctx context.Context, nodeBin string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, nodeBin, "--version")
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// nodeToolchain describes the resolved Node.js + npm pair used for an
// npm-kind install.
type nodeToolchain struct {
	nodeDir string // directory containing node (and its npm) — prepended to PATH
	nodeBin string // absolute path to the node executable
	npmCLI  string // absolute path to npm-cli.js — preferred (node <npm-cli.js> ...)
	npmBin  string // absolute path to an npm launcher (npm.cmd / npm) — fallback
	managed bool   // true when the Node.js is xdev-managed (current/node)
}

// npmRunner returns the command name and argv prefix that invoke npm.
//
// Preference order:
//  1. node <npm-cli.js> — deterministic, no cmd.exe/batch quoting involved
//     (this is exactly what npm.cmd does internally);
//  2. the npm launcher binary; .cmd/.bat launchers on Windows are wrapped
//     in `cmd /c` because CreateProcess cannot run batch files directly.
func (tc nodeToolchain) npmRunner() (string, []string, error) {
	if tc.npmCLI != "" {
		return tc.nodeBin, []string{tc.npmCLI}, nil
	}
	if tc.npmBin != "" {
		lower := strings.ToLower(tc.npmBin)
		if runtime.GOOS == "windows" && (strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat")) {
			return "cmd", []string{"/c", tc.npmBin}, nil
		}
		return tc.npmBin, nil, nil
	}
	return "", nil, fmt.Errorf("no npm found next to node %s", tc.nodeBin)
}

// resolveNodeToolchain locates Node.js + npm for an npm-kind install:
//
//  1. xdev-managed Node first: <XMDEV_HOME>/current/node (the active
//     junction target). This keeps the install inside the xdev-managed
//     toolchain even when the system has another Node.js.
//  2. Otherwise node/npm from PATH — but paths inside xdev's shim or
//     install directories are skipped: without current/node the shims
//     are dead links, not a usable Node.js.
//
// An empty result error is a hard install failure (the caller turns it
// into the actionable "xdev install node" message).
func (i *Installer) resolveNodeToolchain() (nodeToolchain, error) {
	// 1. xdev-managed node.
	if i.cfg.Core.XMDevDir != "" {
		nodeDir := filepath.Join(i.cfg.Core.XMDevDir, "current", "node")
		if fi, err := os.Stat(nodeDir); err == nil && fi.IsDir() {
			if tc, ok := nodeToolchainFromDir(nodeDir); ok {
				tc.managed = true
				return tc, nil
			}
		}
	}

	// 2. System node/npm from PATH, skipping xdev-managed directories.
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return nodeToolchain{}, fmt.Errorf("Node.js not found: install Node.js first (`xdev install node`) or make sure node is on PATH")
	}
	if i.isXdevManagedPath(nodeBin) {
		return nodeToolchain{}, fmt.Errorf("Node.js not found (only an inactive xdev shim on PATH): install Node.js first (`xdev install node`)")
	}
	npmBin, npmErr := exec.LookPath("npm")
	if npmErr != nil {
		return nodeToolchain{}, fmt.Errorf("npm not found next to node %s: install Node.js (which bundles npm) via `xdev install node`", nodeBin)
	}
	if i.isXdevManagedPath(npmBin) {
		return nodeToolchain{}, fmt.Errorf("npm not found (only an inactive xdev shim on PATH): install Node.js via `xdev install node`")
	}

	nodeDir := filepath.Dir(filepath.Clean(nodeBin))
	tc, ok := nodeToolchainFromDir(nodeDir)
	if !ok {
		// PATH found the binaries but the directory layout probe missed —
		// fall back to what LookPath gave us.
		tc = nodeToolchain{nodeDir: nodeDir, nodeBin: nodeBin, npmBin: npmBin}
	}
	return tc, nil
}

// isXdevManagedPath reports whether the executable path lives inside
// xdev's shim or install directories (which are xdev's own symlinks, not
// a real external install).
func (i *Installer) isXdevManagedPath(path string) bool {
	if p := i.cfg.Core.ShimDir; p != "" && hostscan.InsideDir(path, p) {
		return true
	}
	if p := i.cfg.Core.InstallDir; p != "" && hostscan.InsideDir(path, p) {
		return true
	}
	return false
}

// nodeToolchainFromDir probes a Node.js installation directory for the
// node binary, npm-cli.js, and an npm launcher. Layouts:
//
//	Windows: <dir>/node.exe, <dir>/npm.cmd, <dir>/node_modules/npm/bin/npm-cli.js
//	Unix:    <dir>/bin/node, <dir>/bin/npm,  <dir>/lib/node_modules/npm/bin/npm-cli.js
func nodeToolchainFromDir(dir string) (nodeToolchain, bool) {
	tc := nodeToolchain{nodeDir: dir}

	nodeCandidates := []string{
		filepath.Join(dir, "node.exe"),   // Windows root layout
		filepath.Join(dir, "bin", "node"), // Unix layout
		filepath.Join(dir, "node"),
	}
	for _, c := range nodeCandidates {
		if isRegularFile(c) {
			tc.nodeBin = c
			break
		}
	}
	if tc.nodeBin == "" {
		return tc, false
	}

	for _, c := range []string{
		filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js"),     // Windows
		filepath.Join(dir, "lib", "node_modules", "npm", "bin", "npm-cli.js"), // Unix
	} {
		if isRegularFile(c) {
			tc.npmCLI = c
			break
		}
	}
	for _, c := range []string{
		filepath.Join(dir, "npm.cmd"),     // Windows
		filepath.Join(dir, "bin", "npm"),  // Unix
		filepath.Join(dir, "npm"),
	} {
		if isRegularFile(c) {
			tc.npmBin = c
			break
		}
	}
	if tc.npmCLI == "" && tc.npmBin == "" {
		return tc, false
	}
	return tc, true
}

func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// npmPackageSpec returns the npm package name for the tool: the manifest's
// remote.package (type: npm) when declared, otherwise the tool name.
// The name is validated: npm package names only contain [a-zA-Z0-9@/._-].
func npmPackageSpec(p plugin.Plugin) (string, error) {
	name := ""
	if m := p.Manifest(); m != nil && m.Remote.Type == "npm" {
		name = strings.TrimSpace(m.Remote.Package)
	}
	if name == "" {
		name = p.Name()
	}
	if !validNpmPackageName(name) {
		return "", fmt.Errorf("npm install: invalid npm package name %q for tool %q", name, p.Name())
	}
	return name, nil
}

// validNpmPackageName accepts scoped ("@scope/pkg") and plain ("pkg")
// npm package names. Conservative charset — no whitespace, quotes,
// backslashes, or traversal sequences; scope markers only at the start.
func validNpmPackageName(s string) bool {
	if s == "" || len(s) > 214 {
		return false
	}
	if strings.Contains(s, "..") || strings.Contains(s, "//") {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '@' || c == '/' || c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	// A scope marker is only valid as a "@scope/name" prefix; bare "@",
	// leading/trailing separators, and misplaced markers are rejected.
	if s == "@" || s[0] == '/' || s[len(s)-1] == '/' ||
		s[len(s)-1] == '@' || strings.Contains(s[1:], "@") {
		return false
	}
	return true
}

// nodeMinVersion returns the declared minimum Node.js version for the
// tool (manifest depends entry with tool: node), or "".
func nodeMinVersion(p plugin.Plugin) string {
	m := p.Manifest()
	if m == nil {
		return ""
	}
	for _, d := range m.Depends {
		if d.Tool == "node" && d.MinVersion != "" {
			return d.MinVersion
		}
	}
	return ""
}

// checkNPMPrerequisites validates the npm toolchain before install:
// presence of node + npm (hard), and — when the manifest declares a
// depends: node minimum — the probed node version (hard).
func (i *Installer) checkNPMPrerequisites(ctx context.Context, p plugin.Plugin, tc nodeToolchain) error {
	toolName := p.Name()

	minVer := nodeMinVersion(p)
	if minVer == "" {
		return nil
	}
	got, err := npmNodeVersionProbe(ctx, tc.nodeBin)
	if err != nil {
		return fmt.Errorf("cannot install %s: Node.js version check failed (%s --version): %w — install a working Node.js first (`xdev install node`)", toolName, tc.nodeBin, err)
	}
	gotCore := strings.TrimPrefix(strings.TrimSpace(got), "v")
	if !looksLikeSemverCore(gotCore) {
		// Unparseable version output: presence was verified, the npm
		// install itself will surface any incompatibility. Don't block.
		return nil
	}
	if util.CompareSemver(gotCore, minVer) < 0 {
		return fmt.Errorf("cannot install %s: Node.js %s or newer is required, found %s — run `xdev install node` to get a managed Node.js", toolName, minVer, gotCore)
	}
	return nil
}

// looksLikeSemverCore reports whether s starts with a numeric major
// (e.g. "22.14.0", "26.8.1"). "22oops" or "" are rejected.
func looksLikeSemverCore(s string) bool {
	if s == "" {
		return false
	}
	end := strings.IndexByte(s, '.')
	first := s
	if end > 0 {
		first = s[:end]
	}
	for _, c := range first {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(first) > 0
}

// installViaNPM runs the npm install for an npm-kind archive entry:
//
//	node <npm-cli.js> install --global --prefix <installPath> <pkg>@<ver> --no-fund --no-audit
//
// Environment: the node directory is prepended to PATH (so npm.cmd-style
// launchers and package postinstall scripts find `node`), and the xdev
// proxy configuration is injected when not already set (mirrors
// runInstaller's proxy handling).
func (i *Installer) installViaNPM(ctx context.Context, p plugin.Plugin, a *manifest.Archive, ver, installPath string) error {
	pkg, err := npmPackageSpec(p)
	if err != nil {
		return err
	}
	if !npmVersionCharset.MatchString(ver) {
		return fmt.Errorf("npm install: version %q contains characters outside [0-9A-Za-z.+-] — refusing to pass it to npm", ver)
	}

	tc, err := i.resolveNodeToolchain()
	if err != nil {
		return fmt.Errorf("cannot install %s: %w", p.Name(), err)
	}
	if err := i.checkNPMPrerequisites(ctx, p, tc); err != nil {
		return err
	}

	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return fmt.Errorf("npm install: create install dir: %w", err)
	}

	name, prefix, err := tc.npmRunner()
	if err != nil {
		return fmt.Errorf("cannot install %s: %w", p.Name(), err)
	}
	args := append(append([]string{}, prefix...),
		"install", "--global", "--prefix", installPath,
		pkg+"@"+ver,
		"--no-fund", "--no-audit",
	)

	// PATH: node dir first so npm launchers/scripts resolve `node` to the
	// same toolchain that is running the install.
	env := os.Environ()
	if tc.nodeDir != "" {
		env = prependEnvPath(env, tc.nodeDir)
	}
	// Proxy injection (same contract as runInstaller): respect the user's
	// explicit env, otherwise let npm use the xdev-configured proxy.
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

	runCtx, cancel := context.WithTimeout(ctx, npmInstallTimeout)
	defer cancel()

	log.Printf("[install] npm install: tool=%s pkg=%s@%s prefix=%s (node=%s managed=%v)",
		p.Name(), pkg, ver, installPath, tc.nodeBin, tc.managed)
	if err := i.runner.Run(runCtx, name, args, env, installPath); err != nil {
		return fmt.Errorf("npm install failed for %s@%s: %w (npm output above)", p.Name(), ver, err)
	}
	log.Printf("[install] npm install complete: %s@%s -> %s", p.Name(), ver, installPath)
	return nil
}

// prependEnvPath inserts dir at the front of the PATH entry in a
// "KEY=value" environment slice. When PATH is missing a new entry is
// appended.
func prependEnvPath(env []string, dir string) []string {
	const prefix = "PATH="
	out := make([]string, 0, len(env)+1)
	inserted := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			e = prefix + dir + string(os.PathListSeparator) + e[len(prefix):]
			inserted = true
		}
		out = append(out, e)
	}
	if !inserted {
		out = append(out, prefix+dir)
	}
	return out
}

// InstallNPMPackage is the self-contained npm-kind install entry used by
// the GUI download-manager bypass: pre-flight checks → npm install →
// integrity gate → post-install hook → activate. It takes the same
// cross-process tool lock as the archive flow and returns
// Activated=false with ActivationError set when the version installs but
// cannot be switched (locked binary), mirroring InstallWithProgress.
func (i *Installer) InstallNPMPackage(ctx context.Context, p plugin.Plugin, a *manifest.Archive, ver, installPath string) (*InstallResult, error) {
	installLock, lockErr := util.AcquireToolLock(i.cfg.Core.XMDevDir, p.Name(), 5*time.Minute)
	if lockErr != nil {
		return nil, fmt.Errorf("install lock %s: %w", p.Name(), lockErr)
	}
	err := func() error {
		defer installLock.Release()
		if err := i.installViaNPM(ctx, p, a, ver, installPath); err != nil {
			return err
		}
		// npm is synchronous: when it returns the tree is complete, so
		// the same integrity hard gate as archive extracts applies.
		if err := ensureInstallIntegrity(p, installPath, a.Strip); err != nil {
			log.Printf("[install] ERROR: %v", err)
			_ = os.RemoveAll(installPath)
			return err
		}
		if err := p.PostInstall(ctx, installPath); err != nil {
			log.Printf("[install] WARNING: post-install hook failed for %s@%s: %v", p.Name(), ver, err)
		}
		return nil
	}()
	if err != nil {
		return nil, err
	}

	res := &InstallResult{Tool: p.Name(), Version: ver, Spec: ver, URL: a.URL, SHA256: a.SHA256, InstallPath: installPath}
	if err := i.Activate(p, installPath, ver); err != nil {
		log.Printf("[install] WARNING: activation failed for %s@%s (version installed, not activated): %v", p.Name(), ver, err)
		res.Activated = false
		res.ActivationError = err.Error()
		return res, nil
	}
	res.Activated = true
	return res, nil
}
