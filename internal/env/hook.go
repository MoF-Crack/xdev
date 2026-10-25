// Package env manages shell environment integration for xdev.
//
// It provides two main capabilities:
//
//  1. Shell hook scripts (HookScript) — cd hooks that automatically run
//     `xdev hook --shell <shell>` on every directory change, so that
//     PATH and tool-specific environment variables are set correctly
//     for the project's .xdev.toml.
//
//  2. Environment output (HookOutput) — the shell-specific export statements
//     printed by `xdev hook` that set PATH and tool env vars.
//
// # Supported Shells
//
//   - Bash: wraps builtin cd with an alias, calls eval on each cd
//   - Zsh: uses add-zsh-hook chpwd, calls eval on each cd
//   - Fish: uses --on-variable PWD event, calls eval on each cd
//   - PowerShell: overrides the prompt function, compares $PWD.Path
//
// # Security
//
// All hook templates use shellQuote (bash/zsh/fish) or psQuote (PowerShell)
// to safely embed the xdev binary path. This prevents command injection
// if the path contains special characters. Errors from xdev hook are
// suppressed (2>/dev/null, || true) so a broken xdev never breaks the shell.
package env

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// HookScript returns the shell-specific cd hook script that calls
// `xdev hook --shell <shell>` on every directory change.
//
// The hook works by wrapping the builtin `cd` command (or the
// PROMPT_COMMAND / chpwd hook for zsh/fish) so that after changing
// directory, xdev can output the correct PATH and env-var exports
// for the project's .xdev.toml.
func HookScript(shell, xmdeHome string) string {
	switch shell {
	case "bash":
		return bashHook(xmdeHome)
	case "zsh":
		return zshHook(xmdeHome)
	case "fish":
		return fishHook(xmdeHome)
	case "powershell", "pwsh":
		return pwshHook(xmdeHome)
	default:
		return bashHook(xmdeHome)
	}
}

func bashHook(xmdeHome string) string {
	return fmt.Sprintf(`_xdev_cd_hook() {
  local ret=$?
  eval "$(%s hook --shell bash 2>/dev/null)" || true
  return $ret
}

if [[ -z "${_xdev_cd_hook_loaded:-}" ]]; then
  _xdev_original_cd() {
    builtin cd "$@" || return
    _xdev_cd_hook
  }
  alias cd='_xdev_original_cd'
  _xdev_cd_hook_loaded=1
  # Run once for the current directory
  _xdev_cd_hook
fi`, shellQuote(xdevBin(xmdeHome)))
}

func zshHook(xmdeHome string) string {
	return fmt.Sprintf(`_xdev_chpwd() {
  eval "$(%s hook --shell zsh 2>/dev/null)" || true
}

if [[ -z "${_xdev_chpwd_loaded:-}" ]]; then
  autoload -Uz add-zsh-hook
  add-zsh-hook chpwd _xdev_chpwd
  _xdev_chpwd_loaded=1
  # Run once for the current directory
  _xdev_chpwd
fi`, shellQuote(xdevBin(xmdeHome)))
}

func fishHook(xmdeHome string) string {
	return fmt.Sprintf(`function _xdev_chpwd --on-variable PWD
    eval (%s hook --shell fish 2>/dev/null); or true
end
# Run once for the current directory
_xdev_chpwd`, shellQuote(xdevBin(xmdeHome)))
}

func pwshHook(xmdeHome string) string {
	return fmt.Sprintf(`$global:_XdevPreviousPwd = $PWD.Path
function global:prompt {
  $currentPwd = $PWD.Path
  if ($currentPwd -ne $global:_XdevPreviousPwd) {
    $global:_XdevPreviousPwd = $currentPwd
    $output = & %s hook --shell pwsh 2>$null
    if ($LASTEXITCODE -eq 0 -and $output) {
      Invoke-Expression $output
    }
  }
  # Continue with the original prompt
  "PS $($executionContext.SessionState.Path.CurrentLocation)> "
}`, psQuote(xdevBin(xmdeHome)))
}

// shellQuote wraps a string in single quotes for safe embedding in
// bash/zsh/fish scripts. Any single quotes within the string are
// escaped using the standard pattern: ' -> '"'"'
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// psQuote wraps a string in single quotes for safe embedding in
// PowerShell. Single quotes are escaped by doubling them.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// xdevBin returns the path to the xdev binary, preferring the
// current/xdev junction if it exists.
func xdevBin(xmdeHome string) string {
	// Prefer current/xdev/xdev.exe (junction managed)
	exeName := "xdev"
	if os.PathSeparator == '\\' {
		exeName = "xdev.exe"
	}
	current := filepath.Join(xmdeHome, "current", "xdev", exeName)
	if fi, err := os.Stat(current); err == nil && !fi.IsDir() {
		return current
	}
	// Fallback: home dir
	home := filepath.Join(xmdeHome, exeName)
	if fi, err := os.Stat(home); err == nil && !fi.IsDir() {
		return home
	}
	// Last resort: just "xdev" and hope it's on PATH
	return "xdev"
}

// HookOutput returns the shell-specific export statements for the
// given tool paths and environment variables.  This is what `xdev hook`
// prints to stdout.
//
// Security (H-5, H-6): Fish PATH entries are properly quoted to prevent
// command substitution. Environment variable keys are validated to be
// safe identifiers before interpolation into shell output, preventing
// shell injection via malicious keys.
func HookOutput(shell string, binPaths []string, envVars map[string]string) string {
	if len(binPaths) == 0 && len(envVars) == 0 {
		return ""
	}

	var b strings.Builder

	switch shell {
	case "fish":
		// Fish uses set -gx
		if len(binPaths) > 0 {
			// Security (H-5): Quote each path to prevent Fish command
			// substitution via () or special characters in paths.
			quotedPaths := make([]string, len(binPaths))
			for i, p := range binPaths {
				quotedPaths[i] = fishQuote(p)
			}
			fmt.Fprintf(&b, "set -gx PATH %s $PATH\n", strings.Join(quotedPaths, " "))
		}
		for k, v := range envVars {
			if !util.IsValidEnvVarName(k) {
				log.Printf("[hook] WARNING: skipping invalid env var name %q in fish output", k)
				continue
			}
			fmt.Fprintf(&b, "set -gx %s '%s'\n", k, shellSingleQuote(v))
		}
	case "powershell", "pwsh":
		if len(binPaths) > 0 {
			pathStr := strings.Join(binPaths, ";")
			fmt.Fprintf(&b, "$env:Path = \"%s;\" + $env:Path\n", psEscape(pathStr))
		}
		for k, v := range envVars {
			if !util.IsValidEnvVarName(k) {
				log.Printf("[hook] WARNING: skipping invalid env var name %q in powershell output", k)
				continue
			}
			fmt.Fprintf(&b, "$env:%s = \"%s\"\n", k, psEscape(v))
		}
	default: // bash, zsh, sh
		if len(binPaths) > 0 {
			pathStr := strings.Join(binPaths, ":")
			// 2026-10-25 修复（P0-1）：`$PATH` 必须**在单引号外**。
			//
			// 旧实现是 `export PATH='%s:$PATH'`，而 shellSingleQuote 只做
			// `'` → `'\''` 的转义、**不会加外层引号** —— 于是整个表达式变成
			// `export PATH='/a:/b:$PATH'`，单引号内的 `$PATH` 不展开，
			// eval 后 PATH 直接变成字面量 `'/a:/b:$PATH'`，把用户原有
			// PATH **彻底覆盖掉**（不是追加）。
			//
			// 正确写法：路径部分引号包裹（防空格/特殊字符），`:$PATH` 拼在引号外。
			fmt.Fprintf(&b, "export PATH='%s':\"$PATH\"\n", shellSingleQuote(pathStr))
		}
		for k, v := range envVars {
			if !util.IsValidEnvVarName(k) {
				log.Printf("[hook] WARNING: skipping invalid env var name %q in shell output", k)
				continue
			}
			fmt.Fprintf(&b, "export %s='%s'\n", k, shellSingleQuote(v))
		}
	}

	return b.String()
}

// fishQuote wraps a string in single quotes for safe embedding in Fish
// shell commands. Fish uses backslash escaping within single quotes.
func fishQuote(s string) string {
	// Replace backslash first, then single quote
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
