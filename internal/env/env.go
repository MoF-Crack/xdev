// Package env encapsulates the various ways xdev can expose
// itself to the user's environment.
//
// Three concrete concerns live here:
//   - Shell RC injection (bash, zsh, fish) on macOS and Linux.
//   - Windows user PATH manipulation via the registry-equivalent
//     SetEnvironmentVariable API.
//   - Generation of tool-specific env fragments under
//     $XMDEV_HOME/env/*.sh that get sourced on every shim call.
package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ShellInfo describes the user's preferred shell, used to pick the
// right RC file to inject into.
type ShellInfo struct {
	Shell    string // bash | zsh | fish | powershell | pwsh | cmd
	RCFile   string // absolute path
	XMDevTag string // sentinel comment marking our injection
}

// EnvPair represents a name=value environment variable pair.
// Defined in a portable file so Windows and non-Windows registry helpers
// share the same type under go vet ./... on all CI platforms.
type EnvPair struct {
	Name  string
	Value string
}

const marker = "# >>> xdev init >>>"
const endMarker = "# <<< xdev init <<<"

// Legacy markers from before the rename (xmdevenvmt -> xdev).
// We check for these so that upgrading users get their old RC
// blocks replaced instead of accumulating duplicates.
const legacyMarker = "# >>> xmdevenvmt init >>>"
const legacyEndMarker = "# <<< xmdevenvmt init <<<"

// Marker returns the start-of-block comment that tags our shell
// injection, exposed for tests and the doctor command.
func Marker() string { return marker }

// EndMarker returns the end-of-block comment.
func EndMarker() string { return endMarker }

// DetectShell inspects $SHELL and (on Windows) $ComSpec / PATHEXT
// to figure out the user's shell.
func DetectShell() ShellInfo {
	if runtime.GOOS == "windows" {
		return detectWindowsShell()
	}
	return detectUnixShell()
}

func detectUnixShell() ShellInfo {
	home, _ := os.UserHomeDir()
	sh := os.Getenv("SHELL")
	name := filepath.Base(sh)
	switch name {
	case "zsh":
		return ShellInfo{Shell: "zsh", RCFile: filepath.Join(home, ".zshrc"), XMDevTag: marker}
	case "bash":
		// .bashrc on Linux, .bash_profile on macOS for login shells.
		if runtime.GOOS == "darwin" {
			return ShellInfo{Shell: "bash", RCFile: filepath.Join(home, ".bash_profile"), XMDevTag: marker}
		}
		return ShellInfo{Shell: "bash", RCFile: filepath.Join(home, ".bashrc"), XMDevTag: marker}
	case "fish":
		return ShellInfo{Shell: "fish", RCFile: filepath.Join(home, ".config", "fish", "config.fish"), XMDevTag: marker}
	default:
		if sh != "" {
			return ShellInfo{Shell: name, RCFile: filepath.Join(home, ".profile"), XMDevTag: marker}
		}
		return ShellInfo{Shell: "sh", RCFile: filepath.Join(home, ".profile"), XMDevTag: marker}
	}
}

func detectWindowsShell() ShellInfo {
	// PowerShell is the default on modern Windows.
	if _, err := exec.LookPath("pwsh"); err == nil {
		return ShellInfo{Shell: "pwsh", RCFile: pwshProfile(), XMDevTag: marker}
	}
	if _, err := exec.LookPath("powershell"); err == nil {
		return ShellInfo{Shell: "powershell", RCFile: psProfile(), XMDevTag: marker}
	}
	return ShellInfo{Shell: "cmd", RCFile: "", XMDevTag: marker}
}

func pwshProfile() string {
	appdata := os.Getenv("USERPROFILE")
	return filepath.Join(appdata, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
}

func psProfile() string {
	appdata := os.Getenv("USERPROFILE")
	return filepath.Join(appdata, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
}

// InjectShellRC appends (or replaces) the xdev block in the
// shell's RC file.  Idempotent: running it twice does not double up.
func InjectShellRC(info ShellInfo, xmdeHome, shimDir string) error {
	if info.RCFile == "" {
		return fmt.Errorf("no RC file for shell %s", info.Shell)
	}
	if err := os.MkdirAll(filepath.Dir(info.RCFile), 0o755); err != nil {
		return err
	}
	existing, err := os.ReadFile(info.RCFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	// Remove both legacy and current marker blocks.
	text = removeBlock(text, legacyMarker, legacyEndMarker)
	text = removeBlock(text, marker, endMarker)
	text += renderShellBlock(info.Shell, xmdeHome, shimDir)
	return os.WriteFile(info.RCFile, []byte(text), 0o644)
}

// RemoveShellRC strips any xdev block from the shell RC.
func RemoveShellRC(info ShellInfo) error {
	if info.RCFile == "" {
		return nil
	}
	data, err := os.ReadFile(info.RCFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := removeBlock(string(data), legacyMarker, legacyEndMarker)
	text = removeBlock(text, marker, endMarker)
	return os.WriteFile(info.RCFile, []byte(text), 0o644)
}

func removeBlock(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return s
	}
	j := strings.Index(s[i:], end)
	if j < 0 {
		return s
	}
	return s[:i] + s[i+j+len(end):]
}

func renderShellBlock(shell, xmdeHome, shimDir string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf(
			"%s\nset -gx XMDEV_HOME '%s'\nset -gx PATH '%s' $PATH\n%s\n",
			marker, shellSingleQuote(xmdeHome), shellSingleQuote(shimDir), endMarker,
		)
	case "powershell", "pwsh":
		return fmt.Sprintf(
			"%s\n$env:XMDEV_HOME = \"%s\"\n$env:Path = \"%s;\" + $env:Path\n%s\n",
			marker, psEscape(xmdeHome), psEscape(shimDir), endMarker,
		)
	default: // sh, bash, zsh
		return fmt.Sprintf(
			"%s\nexport XMDEV_HOME='%s'\nexport PATH='%s:$PATH'\n%s\n",
			marker, shellSingleQuote(xmdeHome), shellSingleQuote(shimDir), endMarker,
		)
	}
}

// shellSingleQuote wraps a string in single quotes for POSIX shells.
// Single quotes prevent all variable expansion and command substitution.
// Embedded single quotes are escaped using the '\” idiom.
func shellSingleQuote(s string) string {
	return strings.ReplaceAll(s, `'`, `'\''`)
}

// psEscape escapes a string for use in a PowerShell double-quoted string.
func psEscape(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "`", "``")
	s = strings.ReplaceAll(s, "$", "`$")
	return s
}

// WindowsPathPrepend returns a new PATH with the shimDir inserted
// at the beginning.  It does NOT touch the registry - callers should
// use SetWindowsUserPath for persistence.
func WindowsPathPrepend(path, shimDir string) string {
	sep := ";"
	parts := strings.Split(path, sep)
	out := []string{shimDir}
	for _, p := range parts {
		if p != shimDir {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// WriteEnvFragment writes a tiny shell-evaluable fragment for a tool
// that sets the necessary env vars (JAVA_HOME, ANDROID_HOME, ...).
//
// The fragment is sourced by shim.Lookup before execing the real
// binary.
func WriteEnvFragment(envDir, tool, version string, vars map[string]string) error {
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for k, v := range vars {
		fmt.Fprintf(&b, "export %s='%s'\n", k, shellSingleQuote(v))
	}
	path := filepath.Join(envDir, tool+".sh")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ToolBinPaths 返回指定工具安装目录下需要加入 PATH 的子目录列表。
// 不同工具有不同的 bin 目录结构：
//   - java: bin/
//   - node: / (根目录，node.exe 在根目录)
//   - python: /, Scripts/
//   - go: bin/ 或 go/bin/（兼容 strip:0 嵌套布局）
//   - cangjie: bin/ 或 cangjie/bin/（兼容 strip:0 嵌套布局）
//   - rust: .cargo/bin/
//   - flutter: bin/
//   - dotnet: / (根目录)
//   - android: platform-tools/, build-tools/<latest>/
func ToolBinPaths(toolName, installPath string) []string {
	var paths []string
	switch toolName {
	case "java":
		paths = append(paths, filepath.Join(installPath, "bin"))
	case "node":
		paths = append(paths, installPath)
	case "python":
		paths = append(paths, installPath, filepath.Join(installPath, "Scripts"))
	case "go":
		// Standard layout: installPath/bin/
		binDir := filepath.Join(installPath, "bin")
		if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
			paths = append(paths, binDir)
		} else {
			// Fallback: strip:0 nested layout: installPath/go/bin/
			nestedBin := filepath.Join(installPath, "go", "bin")
			if fi, err := os.Stat(nestedBin); err == nil && fi.IsDir() {
				paths = append(paths, nestedBin)
			} else {
				// Last resort: the root itself
				paths = append(paths, installPath)
			}
		}
	case "cangjie":
		// Standard layout: installPath/bin/
		binDir := filepath.Join(installPath, "bin")
		if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
			paths = append(paths, binDir)
		} else {
			// Fallback: strip:0 nested layout: installPath/cangjie/bin/
			nestedBin := filepath.Join(installPath, "cangjie", "bin")
			if fi, err := os.Stat(nestedBin); err == nil && fi.IsDir() {
				paths = append(paths, nestedBin)
			}
			// Also check the root directory (for tools without bin/)
			fi, err := os.Stat(installPath)
			if err == nil && fi.IsDir() {
				paths = append(paths, installPath)
			}
		}
	case "rust":
		paths = append(paths, filepath.Join(installPath, ".cargo", "bin"))
	case "flutter":
		paths = append(paths, filepath.Join(installPath, "bin"))
	case "dart":
		paths = append(paths, filepath.Join(installPath, "bin"))
	case "dotnet":
		paths = append(paths, installPath)
	case "android-platform-tools":
		paths = append(paths, filepath.Join(installPath, "platform-tools"))
		btDir := filepath.Join(installPath, "build-tools")
		if entries, err := os.ReadDir(btDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					paths = append(paths, filepath.Join(btDir, e.Name()))
				}
			}
		}
	case "android-ndk":
		paths = append(paths, installPath)
	case "gradle":
		paths = append(paths, filepath.Join(installPath, "bin"))
	case "maven":
		paths = append(paths, filepath.Join(installPath, "bin"))
	case "uv":
		paths = append(paths, installPath)
	default:
		binDir := filepath.Join(installPath, "bin")
		if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
			paths = append(paths, binDir)
		} else {
			paths = append(paths, installPath)
		}
	}
	return paths
}
