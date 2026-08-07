//go:build windows

// win_path_windows.go provides Windows-specific PATH and environment variable
// management via the Windows registry (HKCU\Environment and HKLM\...\Environment).
//
// # Registry Operations
//
// All registry operations use raw syscall.NewLazyDLL / Proc.Call() rather than
// golang.org/x/sys/windows, keeping dependencies minimal. The key operations are:
//
//   - ReadUserPath / WriteUserPath: read/write the "Path" value in HKCU\Environment
//   - ReadSystemPath / WriteSystemPath: read/write the "Path" value in HKLM\...\Environment
//   - SetUserEnvVar: write arbitrary user-level env vars to HKCU\Environment
//   - BroadcastEnvironmentChange: send WM_SETTINGCHANGE so new processes see changes
//
// # Environment Variable Whitelist
//
// SetUserEnvVar is restricted to a predefined set of ~30 allowed variable names
// (JAVA_HOME, GOROOT, GOPATH, NODE_HOME, PYTHON_HOME, etc.). This prevents
// arbitrary env var writes that could break the system. To add support for new
// tool home variables, the allowedEnvVarNames map must be updated in code.
//
// # PATH Cleanup
//
// CleanupUserPath performs aggressive cleanup of the user PATH:
//   - Removes install/ subdirectory entries (should not be in PATH directly)
//   - Removes current/ entries in shim mode (preserving current\xdev)
//   - Removes legacy xmdevenvmt paths
//   - Removes bare XDev root entries (e.g. "E:\XDev" without subdirectory)
//   - Fixes double-backslash and quoted/corrupted entries
//   - Ensures shimDir is first in shim mode, removed in direct mode
//
// # BroadcastEnvironmentChange
//
// Uses SendMessageTimeoutW with HWND_BROADCAST, a 5-second timeout, and
// SMTO_ABORTIFHUNG. This is the standard Windows pattern but may not notify
// all running processes immediately — newly opened terminals will see changes.
package env

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/xmdemt/xdev/internal/util"
)

var (
	advapi32              = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW     = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW  = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW    = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey       = advapi32.NewProc("RegCloseKey")
	procRegEnumValueW     = advapi32.NewProc("RegEnumValueW")
	procRegDeleteValueW   = advapi32.NewProc("RegDeleteValueW")
)

const (
	_HKEY_CURRENT_USER    = 0x80000001
	_HKEY_LOCAL_MACHINE   = 0x80000002
	_KEY_QUERY_VALUE      = 0x0001
	_KEY_SET_VALUE        = 0x0002
	_REG_EXPAND_SZ        = 2
	_ERROR_FILE_NOT_FOUND = 2
	_ERROR_MORE_DATA      = 234
	_ERROR_NO_MORE_ITEMS  = 259
)

// SetWindowsUserPath prepends shimDir to the user's PATH environment
// variable stored in the Windows registry (HKCU\Environment).  If
// shimDir is already present the function returns nil immediately.
//
// After modifying the registry we broadcast WM_SETTINGCHANGE so that
// new processes pick up the change without a reboot.
func SetWindowsUserPath(shimDir string) error {
	shimDir = filepath.Clean(shimDir)

	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	// Already present?
	for _, p := range strings.Split(current, ";") {
		if filepath.Clean(p) == shimDir {
			return nil
		}
	}

	newPath := shimDir + ";" + current
	if err := WriteUserPath(newPath); err != nil {
		return fmt.Errorf("write user PATH: %w", err)
	}
	BroadcastEnvironmentChange()
	return nil
}

// RemoveFromWindowsUserPath removes shimDir from the user's PATH
// environment variable stored in the Windows registry.
func RemoveFromWindowsUserPath(shimDir string) error {
	shimDir = filepath.Clean(shimDir)

	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	parts := strings.Split(current, ";")
	var kept []string
	for _, p := range parts {
		if filepath.Clean(p) != shimDir {
			kept = append(kept, p)
		}
	}

	newPath := strings.Join(kept, ";")
	if err := WriteUserPath(newPath); err != nil {
		return fmt.Errorf("write user PATH: %w", err)
	}
	BroadcastEnvironmentChange()
	return nil
}

// AppendToUserPath 将指定目录追加到用户级 PATH 末尾（如果尚未存在）。
// 用于将工具的 bin 目录加入 PATH，使得命令在终端中直接可用。
// 安全检查：验证路径存在且是目录，防止无效路径或文件被写入 PATH。
func AppendToUserPath(dir string) error {
	dir = filepath.Clean(dir)

	// 安全检查：路径必须存在且是目录
	if fi, err := os.Stat(dir); err != nil {
		return fmt.Errorf("path does not exist: %s", dir)
	} else if !fi.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	// 已存在则跳过
	for _, p := range strings.Split(current, ";") {
		if strings.EqualFold(filepath.Clean(p), dir) {
			return nil
		}
	}

	// 追加到末尾
	newPath := current + ";" + dir
	if err := WriteUserPath(newPath); err != nil {
		return fmt.Errorf("write user PATH: %w", err)
	}
	BroadcastEnvironmentChange()
	return nil
}

// RemoveFromUserPath 从用户级 PATH 中移除指定目录。
func RemoveFromUserPath(dir string) error {
	dir = filepath.Clean(dir)

	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	parts := strings.Split(current, ";")
	var kept []string
	for _, p := range parts {
		if filepath.Clean(p) != dir {
			kept = append(kept, p)
		}
	}

	newPath := strings.Join(kept, ";")
	if err := WriteUserPath(newPath); err != nil {
		return fmt.Errorf("write user PATH: %w", err)
	}
	BroadcastEnvironmentChange()
	return nil
}

// AddCurrentBinPaths 将 current/<tool> 的 bin 子目录添加到用户 PATH。
// binPaths 由 ToolBinPaths(toolName, currentDir) 生成。
// 这些路径使用 current/ 前缀，切换版本时只需改 Junction，PATH 不变。
func AddCurrentBinPaths(binPaths []string) error {
	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	parts := strings.Split(current, ";")
	existing := make(map[string]bool)
	for _, p := range parts {
		existing[strings.ToLower(filepath.Clean(p))] = true
	}

	changed := false
	for _, bp := range binPaths {
		cleanBP := filepath.Clean(bp)
		if !existing[strings.ToLower(cleanBP)] {
			parts = append(parts, cleanBP)
			changed = true
		}
	}

	if changed {
		if err := WriteUserPath(strings.Join(parts, ";")); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
	}
	return nil
}

// RemoveCurrentBinPaths 从用户 PATH 中移除 current/<tool> 的 bin 子目录。
func RemoveCurrentBinPaths(binPaths []string) error {
	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	toRemove := make(map[string]bool)
	for _, bp := range binPaths {
		toRemove[strings.ToLower(filepath.Clean(bp))] = true
	}

	parts := strings.Split(current, ";")
	var kept []string
	changed := false
	for _, p := range parts {
		if toRemove[strings.ToLower(filepath.Clean(p))] {
			changed = true
			continue
		}
		kept = append(kept, p)
	}

	if changed {
		if err := WriteUserPath(strings.Join(kept, ";")); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
	}
	return nil
}

// RemoveAllCurrentPaths 从用户 PATH 中移除所有 current/ 前缀的条目。
// 用于从 direct 模式切换回 shim 模式时清理。
func RemoveAllCurrentPaths(currentDir string) error {
	current, err := ReadUserPath()
	if err != nil {
		return fmt.Errorf("read user PATH: %w", err)
	}

	currentPrefix := strings.ToLower(filepath.Clean(currentDir))
	if !strings.HasSuffix(currentPrefix, "\\") && !strings.HasSuffix(currentPrefix, "/") {
		currentPrefix += "\\"
	}

	parts := strings.Split(current, ";")
	var kept []string
	changed := false
	for _, p := range parts {
		if strings.HasPrefix(strings.ToLower(filepath.Clean(p)), currentPrefix) {
			changed = true
			continue
		}
		kept = append(kept, p)
	}

	if changed {
		if err := WriteUserPath(strings.Join(kept, ";")); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
	}
	return nil
}

// ReadUserPath reads the "Path" value from HKCU\Environment using the
// Windows registry API.  Returns "" if the value does not exist.
func ReadUserPath() (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	// First call to get the size
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return "", err
	}

	var bufSize uint32
	var valType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW (size) failed: error %d", ret)
	}

	if bufSize == 0 {
		return "", nil
	}

	// Second call to get the actual data
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// WriteUserPath writes the "Path" value to HKCU\Environment as
// REG_EXPAND_SZ so that %USERPROFILE% etc. are expanded at runtime.
// Security (M-2): Rejects paths that exceed the Windows environment block
// limit (~32,767 characters) to prevent silent truncation by CreateProcess.
func WriteUserPath(path string) error {
	// Security (M-2): Check PATH length before writing
	const maxWinRegValueLen = 32767
	if len(path) > maxWinRegValueLen {
		return fmt.Errorf("PATH length %d exceeds Windows registry limit of %d characters — remove unused paths before adding new ones", len(path), maxWinRegValueLen)
	}
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW (set) failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW failed: error %d", ret)
	}
	return nil
}

// allowedEnvVarNames is kept as a local alias for util.AllowedEnvVarNames
// for backward compatibility with any code that references it directly.
// The canonical whitelist is maintained in internal/util/sanitize.go.
// Note: EnumEnvVars, SetEnvVar, and DeleteEnvVar no longer enforce this
// whitelist — only IsProtectedSystemEnvVar is enforced.
var allowedEnvVarNames = util.AllowedEnvVarNames

// SetUserEnvVar sets a user-level environment variable in HKCU\Environment.
// The value is stored as REG_EXPAND_SZ so that %USERPROFILE% etc. are expanded at runtime.
// Only variable names in the util.AllowedEnvVarNames whitelist are accepted; others
// are rejected to prevent overwriting critical system variables.
// Use SetUserEnvVarTrusted for installer-managed vars from manifests.
func SetUserEnvVar(name, value string) error {
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist — refusing to write to registry", name)
	}
	return SetUserEnvVarTrusted(name, value)
}

// SetUserEnvVarTrusted writes a user-level env var without the whitelist check.
// This is used by the installer for env vars declared in tool manifests —
// the manifest is the trusted source, so any *_HOME / *_ROOT var it declares
// is safe to write. The whitelist remains in effect for the GUI manual editor.
//
// Security (F-16 / D12): even this "trusted" boundary rejects protected
// system variables (PATH, SystemRoot, TEMP, ...) and syntactically invalid
// names — a malicious or hand-edited manifest must not be able to clobber
// the system environment. All built-in manifests use *_HOME / *_ROOT style
// names, so existing tools are unaffected.
func SetUserEnvVarTrusted(name, value string) error {
	if err := util.ValidateEnvVarName(name); err != nil {
		return fmt.Errorf("refusing to write env var via trusted path: %w", err)
	}
	// Security (M-4): Validate env var value
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("env var %q value contains null bytes — refusing to write to registry", name)
	}
	// Warn about suspicious %VAR% references that will be expanded at runtime
	if strings.Contains(value, "%") {
		// Check for unknown %VAR% references
		for _, ref := range extractEnvRefs(value) {
			if !util.IsAllowedEnvVarName(ref) && ref != "USERPROFILE" && ref != "APPDATA" && ref != "LOCALAPPDATA" && ref != "ProgramFiles" && ref != "SystemRoot" && ref != "TEMP" && ref != "TMP" {
				log.Printf("[env] WARNING: env var %q value contains %%%s%% reference which is not in the whitelist — this may expand unexpectedly at runtime", name, ref)
			}
		}
	}
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW (set env var) failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW (env var %s) failed: error %d", name, ret)
	}
		return nil
	}

	// GetUserEnvVar reads a user-level environment variable from HKCU\Environment.
	// Returns the value as a string, or ("", error) if the variable doesn't exist
	// or cannot be read. Only variable names in the allowed whitelist are accepted.
	// Security (H-10): Used by the installer to save original values for rollback.
	// Use GetUserEnvVarTrusted for installer-managed vars from manifests.
	func GetUserEnvVar(name string) (string, error) {
		if !util.IsAllowedEnvVarName(name) {
			return "", fmt.Errorf("env var %q is not in the allowed whitelist — refusing to read from registry", name)
		}
		return GetUserEnvVarTrusted(name)
	}

	// GetUserEnvVarTrusted reads a user-level env var without the whitelist check.
	// Used by the installer for env vars declared in tool manifests.
	func GetUserEnvVarTrusted(name string) (string, error) {
		all := GetAllUserEnvVars()
		v, ok := all[name]
		if !ok {
			return "", fmt.Errorf("env var %q not found", name)
		}
		return v, nil
	}

	// GetUserEnvVarsBatch reads a subset of user environment variables in a
	// single registry open/close cycle (SC-004 optimization for hostsnapshot).
	func GetUserEnvVarsBatch(names []string) (map[string]string, error) {
		all := GetAllUserEnvVars()
		result := make(map[string]string, len(names))
		for _, n := range names {
			if util.IsAllowedEnvVarName(n) {
				result[n] = all[n]
			}
		}
		return result, nil
	}

	// DeleteUserEnvVar deletes a user-level environment variable from HKCU\Environment.
	// Only variable names in the allowed whitelist are accepted.
	// Security (M-1): Used by the installer to clean up env vars when the last
	// version of a tool is uninstalled (e.g., removing JAVA_HOME when no Java remains).
	func DeleteUserEnvVar(name string) error {
		if !util.IsAllowedEnvVarName(name) {
			return fmt.Errorf("env var %q is not in the allowed whitelist — refusing to delete from registry", name)
		}
		return DeleteUserEnvVarTrusted(name)
	}

	// DeleteUserEnvVarTrusted deletes a user-level env var without the whitelist check.
	// Used by the installer for env vars declared in tool manifests.
	func DeleteUserEnvVarTrusted(name string) error {
		var key uintptr
		envName, err := syscall.UTF16PtrFromString(`Environment`)
		if err != nil {
			return err
		}

		ret, _, _ := procRegOpenKeyExW.Call(
			_HKEY_CURRENT_USER,
			uintptr(unsafe.Pointer(envName)),
			0,
			_KEY_SET_VALUE,
			uintptr(unsafe.Pointer(&key)),
		)
		if ret != 0 {
			return fmt.Errorf("RegOpenKeyExW (delete env var) failed: error %d", ret)
		}
		defer procRegCloseKey.Call(key)

		valueName, err := syscall.UTF16PtrFromString(name)
		if err != nil {
			return err
		}

		ret, _, _ = procRegDeleteValueW.Call(
			key,
			uintptr(unsafe.Pointer(valueName)),
		)

		const _ERROR_FILE_NOT_FOUND = 2
		if ret != 0 && ret != _ERROR_FILE_NOT_FOUND {
			return fmt.Errorf("RegDeleteValueW (env var %s) failed: error %d", name, ret)
		}

		return nil
	}

// getRegKey returns the root HKEY and sub-key path for the given level.
func getRegKey(level string) (rootKey uintptr, subKey string, err error) {
	switch level {
	case "user":
		return _HKEY_CURRENT_USER, `Environment`, nil
	case "machine":
		return _HKEY_LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, nil
	default:
		return 0, "", fmt.Errorf("unsupported environment variable level %q (use 'user' or 'machine')", level)
	}
}

// SetEnvVar sets a Windows registry environment variable at the given level
// ("user" or "machine"). The value is stored as REG_EXPAND_SZ.
// Protected system variables (PATH, SystemRoot, etc.) are rejected.
// The change is broadcast via WM_SETTINGCHANGE for immediate effect.
func SetEnvVar(name, value, level string) error {
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("env var %q value contains null bytes — refusing to write to registry", name)
	}

	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return fmt.Errorf("RegOpenKeyExW (set %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW (env var %s) failed: error %d", name, ret)
	}

	BroadcastEnvironmentChange()
	return nil
}

// GetEnvVar reads a Windows registry environment variable at the given level
// ("user" or "machine"). Returns the value as a string, or ("", nil) if the
// variable doesn't exist.
func GetEnvVar(name, level string) (string, error) {
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return "", err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW (get %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}

	// First call: determine buffer size
	var bufSize uint32
	var regType uint32
	procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)

	if bufSize == 0 {
		return "", nil
	}

	// Second call: read the value
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (env var %s) failed: error %d", name, ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// DeleteEnvVar deletes a Windows registry environment variable at the given
// level ("user" or "machine"). Protected system variables (PATH, SystemRoot,
// etc.) are rejected. The change is broadcast via WM_SETTINGCHANGE.
func DeleteEnvVar(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return fmt.Errorf("RegOpenKeyExW (delete %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	ret, _, _ = procRegDeleteValueW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
	)

	const _ERROR_FILE_NOT_FOUND = 2
	if ret != 0 && ret != _ERROR_FILE_NOT_FOUND {
		return fmt.Errorf("RegDeleteValueW (env var %s) failed: error %d", name, ret)
	}

	BroadcastEnvironmentChange()
	return nil
}

// EnumEnvVars lists all environment variable values at the given registry level
// ("user" or "machine"). No whitelist filtering — all variables at the level are
// returned so the caller can present a complete view.
func EnumEnvVars(level string) ([]EnvPair, error) {
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return nil, err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return nil, err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return nil, fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return nil, fmt.Errorf("RegOpenKeyExW (enum %s) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	var pairs []EnvPair
	buf := make([]uint16, 32768)
	nameBuf := make([]uint16, 32768)

	for index := uint32(0); ; index++ {
		bufSize := uint32(len(buf))
		nameSize := uint32(len(nameBuf))
		ret, _, _ = procRegEnumValueW.Call(
			key,
			uintptr(index),
			uintptr(unsafe.Pointer(&nameBuf[0])),
			uintptr(unsafe.Pointer(&nameSize)),
			0,
			0,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&bufSize)),
		)
		if ret != 0 {
			break
		}
		name := syscall.UTF16ToString(nameBuf[:nameSize])
		value := syscall.UTF16ToString(buf[:bufSize/2])
		pairs = append(pairs, EnvPair{Name: name, Value: value})
	}
	return pairs, nil
}

	// ReadSystemPath reads the "Path" value from HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment.
// Returns "" if the value does not exist. Requires admin privileges for write access.
func ReadSystemPath() (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW HKLM failed: error %d (may need admin privileges)", ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return "", err
	}

	var bufSize uint32
	var valType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW HKLM (size) failed: error %d", ret)
	}

	if bufSize == 0 {
		return "", nil
	}

	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW HKLM (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// WriteSystemPath writes the "Path" value to HKLM\...\Environment as REG_EXPAND_SZ.
// Requires admin privileges.
func WriteSystemPath(path string) error {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW HKLM (set) failed: error %d (may need admin privileges)", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW HKLM failed: error %d", ret)
	}
	return nil
}

// CleanupSystemPath cleans up the system PATH by:
//  1. Fixing double-backslash paths (\\)
//  2. Removing quoted entries
//  3. Removing corrupted entries (e.g. entries that start with " but don't end with ")
//
// Returns (changed, error). Requires admin privileges to write.
func CleanupSystemPath() (bool, error) {
	current, err := ReadSystemPath()
	if err != nil {
		return false, err
	}

	parts := strings.Split(current, ";")
	var cleanParts []string
	changed := false

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			changed = true
			continue
		}

		// Fix double-backslash
		if strings.Contains(trimmed, "\\\\") {
			trimmed = strings.ReplaceAll(trimmed, "\\\\", "\\")
			changed = true
		}

		// Remove surrounding quotes
		if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
			trimmed = trimmed[1 : len(trimmed)-1]
			changed = true
		}

		// Skip corrupted entries that start with quote but don't close it
		// e.g. "D:\\Env\\jdk17\bin;D:\\Env\\jdk17\lib"
		if strings.HasPrefix(trimmed, "\"") && !strings.HasSuffix(trimmed, "\"") {
			// This is a corrupted entry — the quote opened but never closed,
			// meaning it swallowed the next entry. Try to split it.
			// Remove the leading quote
			trimmed = strings.TrimPrefix(trimmed, "\"")
			changed = true
		}

		if trimmed == "" {
			changed = true
			continue
		}

		cleanParts = append(cleanParts, trimmed)
	}

	if changed {
		newPath := strings.Join(cleanParts, ";")
		if err := WriteSystemPath(newPath); err != nil {
			return false, err
		}
		BroadcastEnvironmentChange()
	}

	return changed, nil
}

// CleanupUserPath cleans up the user PATH by:
//  1. Removing xdev install directory entries (\xdev\install\)
//  2. Removing xdev current directory entries (\xdev\current\) — only in shim mode, except \xdev\current\xdev
//  3. Removing legacy xmdevenvmt shim paths (\ximulocaldev\xmdevenvmt\shims)
//  4. Removing legacy xmdevenvmt source/build directory entries
//  5. Removing bare XDev root directory entries (e.g. E:\XDev) — replaced by current\xdev junction
//  6. Fixing double-backslash paths (\\)
//  7. Removing quoted paths
//  8. Ensuring shimDir is the first entry (shim mode only)
//
// pathMode should be "shim" or "direct".
// Returns true if any changes were made.
func CleanupUserPath(shimDir string, pathMode string) (bool, error) {
	current, err := ReadUserPath()
	if err != nil {
		return false, err
	}

	parts := strings.Split(current, ";")
	var cleanParts []string
	changed := false
	shimDirClean := strings.ToLower(filepath.Clean(shimDir))

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			changed = true
			continue
		}

		// Fix double-backslash
		if strings.Contains(trimmed, "\\\\") {
			trimmed = strings.ReplaceAll(trimmed, "\\\\", "\\")
			changed = true
		}

		// Remove surrounding quotes
		trimmed = strings.Trim(trimmed, "\"")

		normalized := strings.ToLower(filepath.Clean(trimmed))

		// Skip xdev install directory entries (e.g. E:\XDev\install\java\25)
		if strings.Contains(normalized, "\\xdev\\install\\") {
			changed = true
			continue
		}

		// Skip xdev current directory entries (e.g. E:\XDev\current\java\bin)
		// Only in shim mode — in direct mode these are needed
		// 但保留 \xdev\current\xdev（xdev 自身的 junction 路径）
		if pathMode == "shim" && strings.Contains(normalized, "\\xdev\\current\\") {
			if !strings.HasSuffix(normalized, "\\xdev\\current\\xdev") && normalized != "xdev\\current\\xdev" {
				changed = true
				continue
			}
		}

		// Skip legacy xmdevenvmt shim paths
		if strings.Contains(normalized, "\\ximulocaldev\\xmdevenvmt\\shims") {
			changed = true
			continue
		}

		// Skip legacy xmdevenvmt source/build directory entries
		if strings.Contains(normalized, "\\reverseeng\\xmdevenvmt") {
			changed = true
			continue
		}

		// Skip bare XDev root directory entries (e.g. E:\XDev)
		// 旧的实现将 xdev.exe 复制到根目录并添加根目录到 PATH
		// 新实现使用 current/xdev junction，不再需要根目录在 PATH 中
		// 条件：路径以 \xdev 结尾，且不包含子目录（即恰好是根目录）
		if isXDevRoot(normalized) {
			changed = true
			continue
		}

		cleanParts = append(cleanParts, trimmed)
	}

	// Ensure shim directory is first (shim mode only)
	if pathMode == "shim" {
		hasShimDir := false
		for _, p := range cleanParts {
			if strings.ToLower(filepath.Clean(p)) == shimDirClean {
				hasShimDir = true
				break
			}
		}
		if !hasShimDir {
			cleanParts = append([]string{shimDir}, cleanParts...)
			changed = true
		} else if strings.ToLower(filepath.Clean(cleanParts[0])) != shimDirClean {
			// shim dir exists but not first, move it to front
			var rest []string
			for _, p := range cleanParts {
				if strings.ToLower(filepath.Clean(p)) == shimDirClean {
					continue
				}
				rest = append(rest, p)
			}
			cleanParts = append([]string{shimDir}, rest...)
			changed = true
		}
	} else {
		// direct mode: remove shim directory from PATH (not needed)
		var withoutShim []string
		for _, p := range cleanParts {
			if strings.ToLower(filepath.Clean(p)) == shimDirClean {
				changed = true
				continue
			}
			withoutShim = append(withoutShim, p)
		}
		cleanParts = withoutShim
	}

	if changed {
		newPath := strings.Join(cleanParts, ";")
		if err := WriteUserPath(newPath); err != nil {
			return false, err
		}
		BroadcastEnvironmentChange()
	}

	return changed, nil
}

// isXDevRoot 判断路径是否为 XDev 根目录（如 E:\XDev）
// 条件：路径以 \xdev 结尾，且 \xdev 之前是卷标（如 e:）或路径分隔符
// 这样可以匹配 E:\XDev 但不会匹配 E:\XDev\shims 或 E:\XDev\current\xdev
func isXDevRoot(normalized string) bool {
	// 匹配 X:\xdev 格式（卷标 + \xdev）
	if len(normalized) >= 3 {
		// 检查是否以 :\xdev 结尾（如 e:\xdev）
		suffix := "\\xdev"
		if strings.HasSuffix(normalized, suffix) {
			prefix := normalized[:len(normalized)-len(suffix)]
			// 前缀应该是卷标（如 e:）
			if len(prefix) == 2 && prefix[1] == ':' {
				return true
			}
		}
	}
	return false
}

// BroadcastEnvironmentChange sends WM_SETTINGCHANGE with "Environment"
// so that new processes see the updated PATH without a reboot.
//
// Security (L-12): The return value of SendMessageTimeoutW is checked.
// If the broadcast times out or fails (e.g. a target window is hung),
// a warning is printed to stderr advising the user to restart their
// terminal. This is a best-effort notification — Windows does not
// guarantee that all running processes receive WM_SETTINGCHANGE.
func BroadcastEnvironmentChange() {
	user32 := syscall.NewLazyDLL("user32.dll")
	sendMsg := user32.NewProc("SendMessageTimeoutW")

	const (
		HWND_BROADCAST   = 0xFFFF
		WM_SETTINGCHANGE = 0x001A
		SMTO_ABORTIFHUNG = 0x0002
	)

	envStr, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		log.Printf("[env] BroadcastEnvironmentChange: failed to encode 'Environment': %v", err)
		return
	}

	// SendMessageTimeoutW returns: (result LRESULT, unused uintptr, errno syscall.Errno)
	// When the timeout expires, errno is ERROR_TIMEOUT (0x5B4) and result is 0.
	ret, _, callErr := sendMsg.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envStr)),
		SMTO_ABORTIFHUNG,
		5000,
		0,
	)

	const ERROR_TIMEOUT = 0x5B4
	if callErr != syscall.Errno(0) && callErr != syscall.Errno(ERROR_TIMEOUT) {
		// Non-timeout error — broadcast likely failed entirely
		log.Printf("[env] BroadcastEnvironmentChange: SendMessageTimeoutW failed: errno %d", callErr)
		fmt.Fprintln(os.Stderr, "⚠ Environment change broadcast failed — open a new terminal to see updated PATH.")
	} else if callErr == syscall.Errno(ERROR_TIMEOUT) || ret == 0 {
		// One or more target windows timed out — changes may not have propagated
		log.Printf("[env] BroadcastEnvironmentChange: timed out or no response (errno %d, result %d)", callErr, ret)
		fmt.Fprintln(os.Stderr, "⚠ Some applications did not respond to environment change broadcast — open a new terminal to see updated PATH.")
	}
}

// UpdateShimDirInUserPath replaces the old shim directory in user PATH with the new one.
func UpdateShimDirInUserPath(oldDir, newDir string) {
	oldShimDir := filepath.Join(oldDir, "shims")
	newShimDir := filepath.Join(newDir, "shims")

	current, err := ReadUserPath()
	if err != nil {
		return
	}

	parts := strings.Split(current, ";")
	changed := false
	for i, p := range parts {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(oldShimDir)) {
			parts[i] = newShimDir
			changed = true
		}
	}

	if changed {
		newPath := strings.Join(parts, ";")
		WriteUserPath(newPath)
		BroadcastEnvironmentChange()
	}
}

// GetAllUserEnvVars enumerates all user-level environment variables
// from HKCU\Environment and returns them as a map[string]string.
func GetAllUserEnvVars() map[string]string {
	result := make(map[string]string)

	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return result
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return result
	}
	defer procRegCloseKey.Call(key)

	index := uint32(0)
	for {
		nameBufSize := uint32(256)
		nameBuf := make([]uint16, nameBufSize)

		var valType uint32
		var dataBufSize uint32

		ret, _, _ = procRegEnumValueW.Call(
			key,
			uintptr(index),
			uintptr(unsafe.Pointer(&nameBuf[0])),
			uintptr(unsafe.Pointer(&nameBufSize)),
			0,
			uintptr(unsafe.Pointer(&valType)),
			0,
			uintptr(unsafe.Pointer(&dataBufSize)),
		)

		if ret == _ERROR_NO_MORE_ITEMS {
			break
		}
		if ret != 0 && ret != _ERROR_MORE_DATA {
			break
		}

		varName := syscall.UTF16ToString(nameBuf[:nameBufSize])

		// Read the value using RegQueryValueExW
		valueNamePtr, err := syscall.UTF16PtrFromString(varName)
		if err != nil {
			index++
			continue
		}

		var valBufSize uint32
		ret2, _, _ := procRegQueryValueExW.Call(
			key,
			uintptr(unsafe.Pointer(valueNamePtr)),
			0,
			uintptr(unsafe.Pointer(&valType)),
			0,
			uintptr(unsafe.Pointer(&valBufSize)),
		)
		if ret2 != 0 && ret2 != _ERROR_MORE_DATA {
			index++
			continue
		}

		if valBufSize > 0 {
			valBuf := make([]uint16, valBufSize/2+1)
			procRegQueryValueExW.Call(
				key,
				uintptr(unsafe.Pointer(valueNamePtr)),
				0,
				uintptr(unsafe.Pointer(&valType)),
				uintptr(unsafe.Pointer(&valBuf[0])),
				uintptr(unsafe.Pointer(&valBufSize)),
			)
			result[varName] = syscall.UTF16ToString(valBuf)
		} else {
			result[varName] = ""
		}

		index++
	}

	return result
}

// EnrichFromRegistry merges registry-level user environment variables into the
// given environment slice. For each variable in the util.AllowedEnvVarNames
// whitelist, it reads the current value from HKCU\Environment and overrides the
// corresponding entry in the slice (or appends it if not present).
//
// Security (M-14): This ensures that shim subprocesses always see the latest
// tool home variables (JAVA_HOME, GOROOT, etc.) even when the parent shell
// hasn't received WM_SETTINGCHANGE after an xdev activate/deactivate. Only
// whitelisted variable names are read from the registry — arbitrary env vars
// are never injected.
func EnrichFromRegistry(envSlice []string) []string {
	// Build a lookup map from the current env slice for fast override.
	existing := make(map[string]int, len(envSlice))
	for i, pair := range envSlice {
		idx := strings.IndexByte(pair, '=')
		if idx <= 0 {
			continue
		}
		existing[strings.ToUpper(pair[:idx])] = i
	}

	// Read each whitelisted var from the registry and merge.
	for name := range util.AllowedEnvVarNames {
		regVal, err := readRegistryEnvVarRaw(name)
		if err != nil {
			// Variable doesn't exist in registry — skip.
			continue
		}
		key := strings.ToUpper(name)
		if idx, ok := existing[key]; ok {
			// Override existing value with registry value.
			envSlice[idx] = name + "=" + regVal
		} else {
			// Append new entry.
			envSlice = append(envSlice, name+"="+regVal)
		}
	}

	return envSlice
}

// readRegistryEnvVarRaw reads a single environment variable from HKCU\Environment
// without the whitelist check (the caller is responsible for that). Returns
// an error if the variable does not exist or cannot be read.
func readRegistryEnvVarRaw(name string) (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}

	// First call: determine buffer size.
	var bufSize uint32
	var regType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", fmt.Errorf("env var %q not found", name)
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW (size) failed: error %d", ret)
	}
	if bufSize == 0 {
		return "", fmt.Errorf("env var %q not found", name)
	}

	// Second call: read the value.
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// extractEnvRefs extracts %VAR% references from a string
func extractEnvRefs(s string) []string {
	var refs []string
	i := 0
	for i < len(s) {
		if s[i] == '%' {
			j := i + 1
			for j < len(s) && s[j] != '%' {
				j++
			}
			if j < len(s) && j > i+1 {
				refs = append(refs, s[i+1:j])
				i = j + 1
			} else {
				i++
			}
		} else {
			i++
		}
	}
	return refs
}

// setxSetPath is a fallback that uses the `setx` command to set the
// user PATH.  It is not used by default (the registry API is
// preferred) but is kept as documentation.
func _setxSetPath(path string) error {
	cmd := exec.Command("setx", "Path", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
