// Package shim implements the single-binary-multiple-entry dispatch mechanism.
//
// The xdev executable doubles as a "shim" for every supported tool.
// When launched as e.g. `python` (via a hardlink or copy in the shims/
// directory), the runtime detects that os.Args[0] basename is "python"
// instead of "xdev" and looks up the real underlying executable in
//
//	$XMDEV_HOME/current/<tool>/<binary>
//
// to exec it. This approach means only one binary needs to be shipped,
// updated, and maintained — all tool shims are simply copies or symlinks
// of the same xdev binary.
//
// # Shim Resolution Order
//
// When a shim alias is invoked, Lookup resolves it in this order:
//  1. Project-level version from .xdev.toml (if a project config is found)
//  2. Direct match: current/<alias>/ directory contains the executable
//  3. Cross-tool scan: all current/*/ directories are searched for the alias
//     (e.g. `npm` resolves to current/node/bin/npm)
//
// # Reverse Index
//
// The cross-tool scan uses a lazily-built reverse index (shimIndex) that
// maps alias names to their resolved paths and tool names. The index is
// built on first access and must be invalidated after install/uninstall/
// activate operations by calling InvalidateShimIndex().
//
// # Windows Considerations
//
// On Windows, shims are full copies of the xdev binary (both <name> and
// <name>.exe) because Windows shells auto-append .exe. This trades disk
// space for reliability — no Developer Mode is required for symlinks.
// FindInDir checks for .exe, .cmd, and .bat extensions to support
// Node.js .cmd shims and similar patterns.
package shim

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/project"
	"github.com/xmdemt/xdev/internal/util"
)

// MaybeExec is the entry point used from main.
//
//   - If os.Args[0] basename is the main tool name ("xdev")
//     it returns (false, nil) and the caller should run the CLI.
//   - Otherwise it looks up the alias, exec's the real tool, and
//     returns (true, err) so the caller can exit with the proper
//     status code.
func MaybeExec(selfName string) (bool, error) {
	// The main binary may be named "xdev", "xdev.exe", or carry a
	// version suffix like "xdev-1.0.0" / "xdev-1.0.0.exe".  All of
	// these should run the CLI rather than being treated as a shim
	// alias.
	base := strings.TrimSuffix(selfName, exeSuffix())
	if base == "xdev" || strings.HasPrefix(base, "xdev-") || base == "xdev-gui" || strings.HasPrefix(base, "xdev-gui-") {
		return false, nil
	}
	// Wails internal commands should not be intercepted by the shim
	// dispatcher.  When Wails builds the project, it invokes the
	// binary with os.Args[0] set to "wailsbindings" to generate
	// frontend bindings.
	if selfName == "wailsbindings" || selfName == "wailsbindings.exe" {
		return false, nil
	}
	// Windows shims are copied twice: once as "node" and once as
	// "node.exe".  When invoked as "node.exe" the runtime sees the
	// full name; we strip the suffix so Lookup finds the alias.
	alias := strings.TrimSuffix(selfName, exeSuffix())
	target, _, args, err := Lookup(alias, os.Args[1:])
	if err != nil {
		return true, err
	}
	if err := run(target, args); err != nil {
		return true, err
	}
	return true, nil
}

// Lookup resolves a tool alias to the real underlying executable.
//
// resolution order:
//  1. $XMDEV_HOME/current/<alias> is a directory -> look inside it
//     for the first executable that matches the alias name
//  2. $XMDEV_HOME/current/<alias>/bin/<alias> direct match
//  3. fallback: $XMDEV_HOME/current/<alias>/<alias>
//  4. fallback: every other $XMDEV_HOME/current/*/ directory's
//     bin/ or root that contains a file matching the alias.  This
//     lets `npm.exe` resolve to current/node/npm.exe on a system
//     where `node` was installed but the shim is named `npm`.
//
// On Windows we look for both "<alias>" and "<alias>.exe".
//
// The returned toolName identifies which tool directory the target
// belongs to (e.g. "node" for both "node" and "npm" aliases).
func Lookup(alias string, args []string) (string, string, []string, error) {
	xmdeHome := config.ResolveHomeDir()

	// Project-level version takes priority when cd hook is active.
	// Check if there's a .xdev.toml that specifies this tool.
	if projDir, proj := project.FindConfig("."); proj != nil {
		if versionSpec, ok := proj.Tools[alias]; ok {
			// Try to find the installed version for this tool
			if target, toolName, ok := lookupProjectTool(xmdeHome, alias, versionSpec); ok {
				return target, toolName, args, nil
			}
			// Fall through to global lookup
			_ = projDir
		}
	}

	cur := filepath.Join(xmdeHome, "current", alias)
	// First, look for a dedicated "<alias>/" sub-directory under
	// current/.  Most tools live in their own dedicated directory.
	if info, err := os.Stat(cur); err == nil && info.IsDir() {
		if target, ok := FindInDir(cur, alias); ok {
			return target, alias, args, nil
		}
		// Fall through to cross-tool scan - the alias may not
		// exist inside its own folder but be part of another
		// installed tool (e.g. `npm` is part of the node dir).
	}
	// Cross-tool scan: walk every other "current/<tool>/" and see
	// if any of them hosts an executable with this alias.
	if target, toolName, ok := scanFor(xmdeHome, alias); ok {
		return target, toolName, args, nil
	}
	return "", "", nil, fmt.Errorf("shim alias %q not installed (run `xdev install %s` first)", alias, alias)
}

// FindInDir 在指定目录中查找与 alias 匹配的可执行文件
// 查找顺序：bin/<alias> → bin/<alias>.exe → bin/<alias>.cmd → bin/<alias>.bat →
// <alias> → <alias>.exe → <alias>.cmd → <alias>.bat
func FindInDir(dir, alias string) (string, bool) {
	// Try the most common locations first.
	//
	// Windows-specific note: many toolchains (notably Node.js)
	// ship their CLI helpers as ".cmd" or ".bat" shims rather
	// than real .exe binaries.  Go's exec.LookPath / exec.Command
	// on Windows will automatically launch a .cmd shim if the
	// .exe is missing, so accepting it here is fine.
	candidates := []string{
		filepath.Join(dir, "bin", alias),
		filepath.Join(dir, "bin", alias+exeSuffix()),
		filepath.Join(dir, "bin", alias+".cmd"),
		filepath.Join(dir, "bin", alias+".bat"),
		filepath.Join(dir, alias),
		filepath.Join(dir, alias+exeSuffix()),
		filepath.Join(dir, alias+".cmd"),
		filepath.Join(dir, alias+".bat"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil {
			if !fi.IsDir() {
				return c, true
			}
		}
	}
	return "", false
}

// shimIndex maps shimName -> shimEntry with resolved path and tool name.
var (
	shimIndex   map[string]shimEntry
	shimIndexMu sync.RWMutex
	shimIndexOnce sync.Once
)

// shimEntry holds the resolved path and the tool name for a shim.
type shimEntry struct {
	path     string
	toolName string
}

// InvalidateShimIndex clears the reverse index so it will be
// rebuilt on the next Lookup.  Call this after installing,
// uninstalling, or activating a tool.
func InvalidateShimIndex() {
	shimIndexMu.Lock()
	defer shimIndexMu.Unlock()
	shimIndexOnce = sync.Once{}
	shimIndex = nil
}

// buildShimIndex reads all current/* directories and their
// executables, populating shimIndex with a shimName -> entry mapping.
func buildShimIndex(xmdeHome string) {
	currentDir := filepath.Join(xmdeHome, "current")
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return
	}
	idx := make(map[string]shimEntry)
	for _, e := range entries {
		full := filepath.Join(currentDir, e.Name())
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		// e.Name() is the tool name (e.g. "node", "java")
		toolName := e.Name()
		// Scan the tool directory for executables and register
		// each one in the reverse index.
		scanDirForIndex(idx, full, toolName)
	}
	shimIndexMu.Lock()
	shimIndex = idx
	shimIndexMu.Unlock()
}

// scanDirForIndex walks a single tool directory and adds every
// executable candidate to the index.
func scanDirForIndex(idx map[string]shimEntry, dir, toolName string) {
	// Check bin/ subdirectory first.
	binDir := filepath.Join(dir, "bin")
	if info, err := os.Stat(binDir); err == nil && info.IsDir() {
		addExecutables(idx, binDir, toolName)
	}
	// Then the tool root itself.
	addExecutables(idx, dir, toolName)
}

// addExecutables lists files in dir and adds them to the index.
func addExecutables(idx map[string]shimEntry, dir, toolName string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Strip common Windows extensions to get the shim alias.
		alias := name
		for _, ext := range []string{".exe", ".cmd", ".bat"} {
			if strings.HasSuffix(strings.ToLower(alias), ext) {
				alias = alias[:len(alias)-len(ext)]
				break
			}
		}
		if alias == "" {
			continue
		}
		fullPath := filepath.Join(dir, name)
		// Only record the first match for each alias.
		if _, exists := idx[alias]; !exists {
			idx[alias] = shimEntry{path: fullPath, toolName: toolName}
		}
	}
}

func scanFor(xmdeHome, alias string) (string, string, bool) {
	// Build the index lazily on first call via sync.Once.
	shimIndexOnce.Do(func() {
		buildShimIndex(xmdeHome)
	})

	shimIndexMu.RLock()
	defer shimIndexMu.RUnlock()
	if entry, ok := shimIndex[alias]; ok {
		return entry.path, entry.toolName, true
	}
	return "", "", false
}

// ExitCodeError carries a child process exit code to the top-level
// caller, which can then call os.Exit() after running deferred cleanup.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

func run(target string, args []string) error {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
		// Security (M-14): Inherit process env vars AND inject registry-level
		// env vars that may have been set by xdev but not yet picked up by the
		// current shell (e.g., JAVA_HOME set via Activate but current terminal
		// hasn't received WM_SETTINGCHANGE). This ensures shims always run with
		// the correct tool environment regardless of when the shell was started.
		cmd.Env = os.Environ()
		cmd.Env = env.EnrichFromRegistry(cmd.Env)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", target, err)
	}
	return nil
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}

// lookupProjectTool tries to find the executable for a tool alias
// based on the project-level version specification.  It resolves
// the version spec and looks in the install directory.
func lookupProjectTool(xmdeHome, alias, versionSpec string) (string, string, bool) {
	// For project-level lookup, we need to find the installed version.
	// We look in install/<alias>/ for directories matching the spec.
	installDir := filepath.Join(xmdeHome, "install", alias)
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return "", "", false
	}

	// Try exact match first, then prefix match
	var candidates []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v := e.Name()
		// Exact match
		if v == versionSpec {
			dir := filepath.Join(installDir, v)
			if target, ok := FindInDir(dir, alias); ok {
				return target, alias, true
			}
		}
		// Prefix match (e.g. "20" matches "20.18.0")
		if strings.HasPrefix(v, versionSpec) {
			candidates = append(candidates, v)
		}
	}

	// Sort candidates and try the latest one
	if len(candidates) > 0 {
		// Simple sort: last element is typically the highest version
		v := candidates[len(candidates)-1]
		dir := filepath.Join(installDir, v)
		if target, ok := FindInDir(dir, alias); ok {
			return target, alias, true
		}
	}

	return "", "", false
}

// InstallShim creates a shim entry in shimDir pointing at selfPath.
//
// The behaviour depends on the OS:
//   - Unix  : we create a symlink (one byte in size).
//   - Windows: we copy the binary (a hard copy is the only reliable
//     mechanism when developer mode is off, and we keep the surface
//     simple by always copying).  On Windows we ALSO create a copy
//     with a `.exe` suffix so that the standard
//     CreateProcess/Cmd API can find the shim via PATH lookups
//     without prompting the user to pick an associated app.
func InstallShim(shimDir, name, selfPath string) error {
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return err
	}
	// On Windows, copying is the most reliable approach.
	if os.PathSeparator == '\\' {
		// Copy both "<name>" (for callers that add the extension
		// themselves) and "<name>.exe" (for callers that don't).
		// Most Windows shells (PowerShell, pwsh, cmd) auto-append
		// .exe when looking up PATH entries, so the .exe variant
		// is the one actually used in practice.
		if err := util.CopyFile(selfPath, filepath.Join(shimDir, name)); err != nil {
			return err
		}
		// Avoid double-copying when name already ends in .exe.
		if !strings.HasSuffix(strings.ToLower(name), ".exe") {
			if err := util.CopyFile(selfPath, filepath.Join(shimDir, name+".exe")); err != nil {
				return err
			}
		}
		return nil
	}
	// On Unix a single symlink is enough; the shell does not
	// auto-append extensions.
	return os.Symlink(selfPath, filepath.Join(shimDir, name))
}

// PathPrepend returns a new PATH string with shimDir inserted at the
// front.  This is used for shell-RC injection.
func PathPrepend(path, shimDir string) string {
	if path == "" {
		return shimDir
	}
	sep := string(os.PathListSeparator)
	parts := strings.Split(path, sep)
	out := []string{shimDir}
	for _, p := range parts {
		if p == shimDir {
			// already in there, skip
			out = out[:0]
			out = append(out, shimDir)
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, sep)
}
