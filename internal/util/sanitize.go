// Package util provides common utility functions for the xdev project.
//
// This file (sanitize.go) implements path safety and input validation
// functions used to prevent path traversal, injection, and other
// security vulnerabilities throughout the codebase.
package util

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// windowsReservedNames lists Windows device names that cannot be used
// as file or directory names. Writing these names can cause unexpected
// behavior (e.g., hanging the I/O system, overwriting device output).
var windowsReservedNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}

// SafeName validates that a string is safe to use as a single path
// component (a file or directory name). It rejects:
//   - Empty strings
//   - Strings containing path separators (/ or \)
//   - Directory traversal sequences (.. or .)
//   - Strings containing a Windows drive letter prefix (e.g. C:)
//   - Windows reserved device names (CON, PRN, AUX, NUL, COM1-9, LPT1-9)
//   - Strings containing null bytes
//   - Strings with trailing dots or spaces (Windows strips these)
//
// Returns the input string unchanged if valid, or an error describing
// why the name is unsafe.
func SafeName(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("name must not be empty")
	}

	// Reject null bytes
	if strings.ContainsRune(s, '\x00') {
		return "", fmt.Errorf("name %q must not contain null bytes", s)
	}

	// Reject path separators
	if strings.ContainsAny(s, `/\`) {
		return "", fmt.Errorf("name %q must not contain path separators", s)
	}

	// Reject directory traversal
	if s == ".." || s == "." {
		return "", fmt.Errorf("name %q must not be a directory reference", s)
	}

	// Reject Windows drive letter prefix (e.g. "C:" or "c:")
	if len(s) >= 2 && s[1] == ':' {
		c := s[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return "", fmt.Errorf("name %q must not contain a drive letter", s)
		}
	}

	// Reject Windows reserved device names.
	//
	// 第十一轮审计（Round 10 报告待办 9）：**必须整体相等**，不能剥离扩展名。
	// 旧实现剥掉第一个 `.` 之前的部分再比对，于是 `con.txt` / `nul.txt` /
	// `aux.x.y` / `con.txt.bak` / `lpt9.txt` 这些**完全合法**的名字被误拒。
	//
	// 本机实测（`os.WriteFile` + `os.ReadDir` 目录枚举，Windows 10）：
	//
	//	分量          读回     真实目录项
	//	CON           "X"     true     ← 普通文件
	//	con           "X"     **false** ← 落进控制台设备
	//	NUL           ""      false    ← 落进空设备，数据丢失
	//	con.txt       "X"     true     ← 普通文件
	//	con.txt.bak   "X"     true     ← 普通文件
	//	lpt9.txt      "X"     true     ← 普通文件
	//
	// 即 `con` 与 `con.txt` 的差别**不是扩展名**，而是前者恰好等于设备名。
	// 该结论与 Round 10 在 `internal/extractor` 侧的实测校准一致（那边同样把
	// 「剥离扩展名」的误报改成了整体相等）。
	upper := strings.ToUpper(s)
	for _, reserved := range windowsReservedNames {
		if upper == reserved {
			return "", fmt.Errorf("name %q is a reserved device name", s)
		}
	}

	// Reject names made ENTIRELY of dots and/or spaces.
	//
	// 第十一轮审计（Round 10 报告待办 9）：旧实现写成
	//
	//	trimmed := strings.TrimRight(s, ". ")
	//	if trimmed != s && trimmed != "" { reject }
	//
	// 那个 `trimmed != ""` 让**全点/全空格**名字（`"..."`、`"   "`、`". ."`）
	// 恰好经 TrimRight 变成空串而被**放行**。旧注释声称「entire name is
	// dots/spaces (caught by "." check above)」，但上面的检查只拦精确的
	// `"."` 与 `".."`，`"..."` 不在其中 —— 该假设不成立。
	//
	// 危害：Windows 会把纯点/空格名静默折叠成**空名**或与其它名字合并
	// （Round 10 已实测 `bin.` 与 `bin` 折叠为同一目录项）。`SafeName` 是工具名、
	// 版本号、归档 format 与插件名的共享入口（约 23 个调用点）。
	//
	// 注意判据是「TrimRight 后为空」⇔「整个名字都由点/空格组成」，因此
	// `..foo`、`...x`、`.hidden` 这类**以点开头但并非纯点**的名字仍被放行
	// （Round 10 已确立它们是合法的）。
	if strings.TrimRight(s, ". ") == "" {
		return "", fmt.Errorf("name %q must not consist only of dots and/or spaces (Windows folds it to an empty or colliding name)", s)
	}

	// Reject a trailing dot or space on an otherwise valid name (Windows strips
	// these silently, which can cause two different names to collide on disk).
	if trimmed := strings.TrimRight(s, ". "); trimmed != s {
		return "", fmt.Errorf("name %q must not end with dots or spaces (Windows compatibility)", s)
	}

	return s, nil
}

// ValidateShimName validates a shim/alias name before it is used as a
// path component under the shims directory (Spec 021 D11 / F-08).
// It reuses SafeName semantics: rejects empty names, path separators,
// directory traversal (.. / .), NUL bytes, drive letters, Windows
// reserved device names, and trailing dots/spaces.
func ValidateShimName(name string) error {
	_, err := SafeName(name)
	return err
}

// ValidateEnvVarName validates an environment variable name for
// user/custom-tool writes (Spec 021 D12 / F-09, F-16): the name must be
// syntactically valid (IsValidEnvVarName) and must not be a protected
// system variable (IsProtectedSystemEnvVar, case-insensitive — PATH,
// Path, SystemRoot, windir, TEMP, TMP, COMSPEC, USERPROFILE, etc.).
func ValidateEnvVarName(name string) error {
	if !IsValidEnvVarName(name) {
		return fmt.Errorf("invalid environment variable name %q (must start with a letter or underscore and contain only letters, digits, and underscores)", name)
	}
	if IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("environment variable %q is a protected system variable", name)
	}
	return nil
}

// ValidateAbsPathUnder ensures target is an absolute path strictly inside
// base (lexical containment via filepath.Rel). Used to confine migration
// sources/destinations and profile paths to xdev-owned directories
// (Spec 021 D13 / F-10, F-11).
func ValidateAbsPathUnder(base, target string) error {
	absBase, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return fmt.Errorf("resolve base path: %w", err)
	}
	absTarget, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return fmt.Errorf("path %q is not relative to %q: %w", target, base, err)
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("path %q escapes directory %q", target, base)
	}
	return nil
}

// ValidateSubPath ensures that child is strictly under parent.
// It resolves both paths to absolute form, optionally follows symlinks,
// and checks that the relative path from parent to child does not
// escape upward (start with "..").
//
// This is the primary defense against path traversal attacks where
// an attacker-controlled path component (tool name, version, install_root,
// etc.) is joined with a trusted base directory.
func ValidateSubPath(parent, child string) error {
	absParent, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return fmt.Errorf("resolve parent path: %w", err)
	}
	absChild, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return fmt.Errorf("resolve child path: %w", err)
	}

	// Resolve symlinks on both sides if possible.
	// EvalSymlinks requires the path to exist. When only the parent exists
	// (typical for shimDir/<name> before the shim is created), resolving
	// the parent through a Darwin /var -> /private/var link while leaving
	// the child on the lexical path makes Rel() look like an escape.
	// Walk the child up until a real prefix exists, then stitch the
	// remaining components onto the resolved prefix.
	absParent = evalExistingPrefix(absParent)
	// 只解析 child 的**目录**，不跟随最后一段。
	//
	// 20261022 CI（macOS）实测：Activate 校验 current/<tool> 时，该路径本身
	// 已是指向 install/<tool>/<ver> 的符号链接。旧实现 EvalSymlinks 把整条
	// child 解析到安装树，于是每个合法激活都被判成 “escapes current dir”。
	// Windows 的 junction 没有暴露这一点，Linux/macOS 的 symlink 会。
	// 语义应当是：**链接路径本身**必须在 parent 内，而不是链接目标。
	absChild = filepath.Join(evalExistingPrefix(filepath.Dir(absChild)), filepath.Base(absChild))

	// Check via Rel that child does not escape parent
	rel, err := filepath.Rel(absParent, absChild)
	if err != nil {
		return fmt.Errorf("path %q is not relative to %q: %w", child, parent, err)
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path %q escapes directory %q", child, parent)
	}

	// Additional prefix check with separator to prevent /foo matching /foobar
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(absChild, absParent) &&
			!strings.HasPrefix(strings.ToLower(absChild), strings.ToLower(absParent)+string(os.PathSeparator)) {
			return fmt.Errorf("path %q is not under %q", child, parent)
		}
	} else {
		if absChild != absParent &&
			!strings.HasPrefix(absChild, absParent+string(filepath.Separator)) {
			return fmt.Errorf("path %q is not under %q", child, parent)
		}
	}

	return nil
}

// evalExistingPrefix follows symlinks for the longest existing prefix of
// path, then appends any remaining (not-yet-created) components. This
// keeps ValidateSubPath correct when parent is a Darwin /var symlink and
// child is a not-yet-created file under it.
func evalExistingPrefix(path string) string {
	var missing []string
	cur := path
	for {
		if ev, err := filepath.EvalSymlinks(cur); err == nil {
			if len(missing) == 0 {
				return ev
			}
			for i := len(missing) - 1; i >= 0; i-- {
				ev = filepath.Join(ev, missing[i])
			}
			return ev
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

// IsValidEnvVarName checks whether a string is a valid environment
// variable name that can be safely interpolated into shell output.
// A valid name must start with a letter or underscore, and contain
// only letters, digits, and underscores. This prevents shell injection
// via malicious env var keys like "FOO; rm -rf /".
func IsValidEnvVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

// AllowedEnvVarNames is the whitelist of environment variable names
// that xdev is permitted to write to the Windows registry via
// SetUserEnvVar. This prevents arbitrary registry writes that could
// corrupt the user's environment (e.g., overwriting PATH or SystemRoot).
//
// This map is the single source of truth used by both the CLI
// (win_path_windows.go) and the GUI (app_env_vars.go).
var AllowedEnvVarNames = map[string]bool{
	"JAVA_HOME":              true,
	// JDK_HOME 是部分工具链（旧版 Ant / 某些 IDE 插件）读取的 JAVA_HOME 别名。
	// 审查（E-04）：manifests/java.yaml 已声明该变量，白名单必须同步，
	// 否则 machine 层写入会被 IsAllowedEnvVarName 拒绝。
	"JDK_HOME":               true,
	"GOROOT":                 true,
	"GOPATH":                 true,
	"NODE_HOME":              true,
	"PYTHONHOME":             true,
	"LLVM_HOME":              true,
	"MINGW_HOME":             true,
	"LUA_HOME":               true,
	"DART_HOME":              true,
	"FLUTTER_HOME":           true,
	"FLUTTER_ROOT":           true,
	"DOTNET_ROOT":            true,
	"RUBY_HOME":              true,
	"PERL_HOME":              true,
	"KOTLIN_HOME":            true,
	"SCALA_HOME":             true,
	"GHC_HOME":               true,
	"ZIG_HOME":               true,
	"GRADLE_HOME":            true,
	"MAVEN_HOME":             true,
	"CMAKE_HOME":             true,
	"ANDROID_HOME":           true,
	"ANDROID_SDK_ROOT":       true,
	"ANDROID_NDK_HOME":       true,
	"ANDROID_BUILD_TOOLS":    true,
	"ANDROID_CMDLINE_TOOLS":  true,
	"ANDROID_PLATFORM":       true,
	"ANDROID_PLATFORM_TOOLS": true,
	"SBT_HOME":               true,
	"ERLANG_HOME":            true,
	"GIT_HOME":               true,
	"TYPESCRIPT_HOME":        true,
	"RUSTUP_HOME":            true,
	"CARGO_HOME":             true,
	"CANGJIE_HOME":           true,
	"PUB_CACHE":              true,
	// Spec 022 官方变量名（D1-D7 清单同步）：
	"PHPRC":                 true,
	"JULIA_DEPOT_PATH":      true,
	"DENO_INSTALL":          true,
	"BUN_INSTALL":           true,
	"UV_INSTALL_DIR":        true,
	"UV_CACHE_DIR":          true,
	"UV_TOOL_BIN_DIR":       true,
	"UV_TOOL_DIR":           true,
	"UV_PYTHON_INSTALL_DIR": true,
	"npm_config_prefix":     true,
}

// IsAllowedEnvVarName checks whether the given name is in the
// environment variable whitelist for registry writes.
func IsAllowedEnvVarName(name string) bool {
	return AllowedEnvVarNames[name]
}
