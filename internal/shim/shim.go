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
//  1. Direct match: current/<alias>/ directory contains the executable
//  2. Cross-tool scan: all current/*/ directories are searched for the alias
//     (e.g. `npm` resolves to current/node/bin/npm)
//
// Project-level .xdev.toml override was removed in Spec 012. Unix run
// uses unix.Exec (Spec 030) so the shim does not stay resident.
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
// On Windows, shims prefer a hard link to the xdev binary (same volume,
// no Developer Mode). If CreateHardLink fails (cross-volume, privilege),
// we copy. We also write <name>.exe so CreateProcess/Cmd PATH lookups
// work. FindInDir checks .exe/.cmd/.bat for Node.js shims.
package shim

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/xmdemt/xdev/internal/config"
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
// <alias> → <alias>.exe → <alias>.cmd → <alias>.bat →
// 然后扫描一层子目录：每个子目录的 bin/<alias>, bin/<alias>.exe, bin/<alias>.cmd, bin/<alias>.bat,
// 以及 <alias>, <alias>.exe, <alias>.cmd, <alias>.bat
//
// 最后一层兜底处理 strip:0 导致的嵌套布局，例如 archive 解压后多出一层
// toolname/ 目录（cangjie/ 或 go/），此时原始文件位于 dir/<toolname>/bin/<alias>。
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
	// Nested subdirectory scan: handle strip:0 layouts where the
	// archive extracts to dir/<toolname>/bin/<alias> instead of
	// dir/bin/<alias>.  Scan one level of subdirectories.
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(dir, e.Name())
			// Check sub/bin/<alias> variants first, then sub/<alias>.
			for _, c := range []string{
				filepath.Join(sub, "bin", alias),
				filepath.Join(sub, "bin", alias+exeSuffix()),
				filepath.Join(sub, "bin", alias+".cmd"),
				filepath.Join(sub, "bin", alias+".bat"),
				filepath.Join(sub, alias),
				filepath.Join(sub, alias+exeSuffix()),
				filepath.Join(sub, alias+".cmd"),
				filepath.Join(sub, alias+".bat"),
			} {
				if fi, err := os.Stat(c); err == nil {
					if !fi.IsDir() {
						return c, true
					}
				}
			}
		}
	}
	return "", false
}

// shimIndex maps shimName -> shimEntry with resolved path and tool name.
var (
	shimIndex     map[string]shimEntry
	shimIndexMu   sync.RWMutex
	shimIndexOnce sync.Once
)

// shimEntry holds the resolved path and the tool name for a shim.
type shimEntry struct {
	path     string
	toolName string
}

// InvalidateShimIndex clears the reverse index so it will be
// rebuilt on the next Lookup.  Call this after installing,
// uninstalling, or activating a tool.  It also rewrites the on-disk
// shim index (Spec 032) so short shim processes can mmap it instead
// of walking current/*.
func InvalidateShimIndex() {
	shimIndexMu.Lock()
	shimIndexOnce = sync.Once{}
	shimIndex = nil
	shimIndexMu.Unlock()
	writeDiskShimIndex(config.ResolveHomeDir())
}

// diskShimIndexName is the on-disk reverse index consumed by short
// shim processes (Spec 032). Format: one "alias<TAB>toolName<TAB>path"
// per line, sorted by alias, terminated by a blank line. The header
// line is "xdev-shim-index\tv1".
const diskShimIndexName = "shim-index.v1"

func diskShimIndexPath(xmdeHome string) string {
	return filepath.Join(xmdeHome, "cache", diskShimIndexName)
}

// writeDiskShimIndex serialises the current reverse index to disk.
// Best-effort: missing cache dir or write failure is logged, not fatal.
func writeDiskShimIndex(xmdeHome string) {
	shimIndexMu.RLock()
	idx := shimIndex
	shimIndexMu.RUnlock()
	if idx == nil {
		// Rebuild in-memory first so the on-disk file is authoritative.
		idx = buildShimIndexMap(xmdeHome)
		shimIndexMu.Lock()
		shimIndex = idx
		shimIndexMu.Unlock()
	}
	path := diskShimIndexPath(xmdeHome)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[shim] write disk index: mkdir: %v", err)
		return
	}
	var sb strings.Builder
	sb.WriteString("xdev-shim-index\tv1\n")
	names := make([]string, 0, len(idx))
	for k := range idx {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		e := idx[k]
		sb.WriteString(k)
		sb.WriteByte('\t')
		sb.WriteString(e.toolName)
		sb.WriteByte('\t')
		sb.WriteString(e.path)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		log.Printf("[shim] write disk index %s: %v", path, err)
	}
}

// loadDiskShimIndex reads the on-disk reverse index. Returns nil on
// any error (caller falls back to the in-process build).
func loadDiskShimIndex(xmdeHome string) map[string]shimEntry {
	data, err := os.ReadFile(diskShimIndexPath(xmdeHome))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "xdev-shim-index\tv1") {
		return nil
	}
	idx := make(map[string]shimEntry, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		idx[parts[0]] = shimEntry{toolName: parts[1], path: parts[2]}
	}
	return idx
}

// buildShimIndex reads all current/* directories and their
// executables, populating shimIndex with a shimName -> entry mapping.
func buildShimIndex(xmdeHome string) {
	idx := buildShimIndexMap(xmdeHome)
	shimIndexMu.Lock()
	shimIndex = idx
	shimIndexMu.Unlock()
}

// buildShimIndexMap is the pure scan that produces the reverse index
// without touching the package-level shimIndex.
func buildShimIndexMap(xmdeHome string) map[string]shimEntry {
	currentDir := filepath.Join(xmdeHome, "current")
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return make(map[string]shimEntry)
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
	return idx
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
	// Finally, scan one-level subdirectories for their bin/ folders.
	// This handles strip:0 archive layouts where files are nested
	// under dir/<toolname>/bin/ instead of dir/bin/.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subBin := filepath.Join(dir, e.Name(), "bin")
		if info, err := os.Stat(subBin); err == nil && info.IsDir() {
			addExecutables(idx, subBin, toolName)
		}
	}
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
	// Spec 032: prefer the on-disk reverse index so short shim
	// processes do not re-walk current/*. The in-process sync.Once
	// build is the fallback for long-lived CLI/GUI processes.
	if entry, ok := lookupDiskShimIndex(xmdeHome, alias); ok {
		return entry.path, entry.toolName, true
	}
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

func lookupDiskShimIndex(xmdeHome, alias string) (shimEntry, bool) {
	idx := loadDiskShimIndex(xmdeHome)
	if idx == nil {
		return shimEntry{}, false
	}
	e, ok := idx[alias]
	return e, ok
}

// ExitCodeError carries a child process exit code to the top-level
// caller, which can then call os.Exit() after running deferred cleanup.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

// run is implemented in run_unix.go (unix.Exec) and run_windows.go
// (CreateProcess + EnrichFromRegistry).

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}

// InstallShim creates a shim entry in shimDir pointing at selfPath.
//
// The behaviour depends on the OS:
//   - Unix  : symlink.
//   - Windows: hard link to xdev.exe, falling back to a copy. Also write
//     <name>.exe for CreateProcess/Cmd PATH lookups.
//
// Windows file-lock handling: a running process locks its .exe against
// overwrite/deletion, but Windows allows **renaming** a file that is
// open. Updating a copy-shim: rename old to <name>.old, then
// hardlink/copy. If dst is already a hard link to selfPath (SameFile),
// skip.
//
// Security (F-08): the name is validated before any filesystem write —
// a malicious manifest must not be able to write outside shimDir via
// `..\outside` or absolute shim names.
func InstallShim(shimDir, name, selfPath string) error {
	if err := util.ValidateShimName(name); err != nil {
		return fmt.Errorf("invalid shim name %q: %w", name, err)
	}
	// Containment check: shimDir/<name> must stay inside shimDir.
	target := filepath.Join(shimDir, name)
	if err := util.ValidateSubPath(shimDir, target); err != nil {
		return fmt.Errorf("shim path %q escapes shim dir %q: %w", target, shimDir, err)
	}

	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return err
	}
	if os.PathSeparator == '\\' {
		if err := installShimWindows(selfPath, filepath.Join(shimDir, name)); err != nil {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(name), ".exe") {
			if err := installShimWindows(selfPath, filepath.Join(shimDir, name+".exe")); err != nil {
				return err
			}
		}
		return nil
	}
	return os.Symlink(selfPath, filepath.Join(shimDir, name))
}

// installShimWindows places a Windows shim at dst.
// Prefer a hard link (one inode, all aliases share xdev.exe). If that
// fails, copy. Existing dst that is already the same file is a no-op.
func installShimWindows(selfPath, dst string) error {
	srcInfo, err := os.Stat(selfPath)
	if err != nil {
		return fmt.Errorf("shim source %s: %w", selfPath, err)
	}

	if dstInfo, statErr := os.Stat(dst); statErr == nil {
		if !dstInfo.IsDir() && os.SameFile(srcInfo, dstInfo) {
			return nil
		}
		oldPath := dst + ".old"
		_ = os.Remove(oldPath)
		if renameErr := os.Rename(dst, oldPath); renameErr != nil {
			return placeWindowsShim(selfPath, dst)
		}
		_ = os.Remove(oldPath)
	}

	return placeWindowsShim(selfPath, dst)
}

func placeWindowsShim(selfPath, dst string) error {
	if err := createHardLink(dst, selfPath); err == nil {
		return nil
	}
	return util.CopyFile(selfPath, dst)
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
