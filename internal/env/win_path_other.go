//go:build !windows

package env

import (
	"fmt"
	"os"
	"strings"
)

// SetWindowsUserPath is a no-op on non-Windows platforms; shell RC
// files handle PATH injection there.
func SetWindowsUserPath(shimDir string) error {
	return nil
}

// RemoveFromWindowsUserPath is a no-op on non-Windows platforms.
func RemoveFromWindowsUserPath(shimDir string) error {
	return nil
}

// AppendToUserPath is a no-op on non-Windows platforms.
func AppendToUserPath(dir string) error {
	return nil
}

// RemoveFromUserPath is a no-op on non-Windows platforms.
func RemoveFromUserPath(dir string) error {
	return nil
}

// RemoveFromUserPathPrefix is a no-op on non-Windows platforms.
func RemoveFromUserPathPrefix(prefix string) error {
	return nil
}

// AddCurrentBinPaths is a no-op on non-Windows platforms.
func AddCurrentBinPaths(binPaths []string) error {
	return nil
}

// RemoveCurrentBinPaths is a no-op on non-Windows platforms.
func RemoveCurrentBinPaths(binPaths []string) error {
	return nil
}

// RemoveAllCurrentPaths is a no-op on non-Windows platforms.
func RemoveAllCurrentPaths(currentDir string) error {
	return nil
}

// ReadUserPath is a no-op on non-Windows platforms.
func ReadUserPath() (string, error) {
	if otherInMemoryUserPath != nil {
		return *otherInMemoryUserPath, nil
	}
	return "", nil
}

// WriteUserPath is a no-op on non-Windows platforms.
func WriteUserPath(path string) error {
	if otherInMemoryUserPath != nil {
		*otherInMemoryUserPath = path
	}
	return nil
}

// otherInMemoryUserPath 与 Windows 侧 UseInMemoryUserPathForTesting 对应：非 Windows
// 上 PATH 本身是 no-op，但跨平台测试（如 gui 的 InitConfig 守卫）仍需要同名钩子才能编译。
var otherInMemoryUserPath *string

// TransformUserPath（票 44）：非 Windows 上 PATH 读写是 no-op，但 transform
// 仍被调用并对内存槽位生效（跨平台测试可断言变换语义）；changed 透传。
func TransformUserPath(transform func(current string) (next string, changed bool, err error)) (bool, error) {
	current, _ := ReadUserPath()
	next, changed, err := transform(current)
	if err != nil {
		return changed, err
	}
	if !changed {
		return false, nil
	}
	return true, WriteUserPath(next)
}

// TransformSystemPath（票 44）：非 Windows 无系统级 PATH 注册表语义——恒 no-op。
func TransformSystemPath(transform func(current string) (next string, changed bool, err error)) (bool, error) {
	return false, nil
}

// UseInMemoryUserPathForTesting 见 Windows 侧实现；非 Windows 上 PATH 读写真身就是
// no-op，这里只提供一个可断言的内存槽位，保证同一份测试在两个平台都能编译运行。
func UseInMemoryUserPathForTesting(initial string) (get func() string, restore func()) {
	store := initial
	otherInMemoryUserPath = &store
	return func() string { return *otherInMemoryUserPath },
		func() { otherInMemoryUserPath = nil }
}

// BroadcastEnvironmentChange is a no-op on non-Windows platforms.
func BroadcastEnvironmentChange() {}

// CleanupUserPath is a no-op on non-Windows platforms.
// The home parameter anchors the xdev-owned entries to clean (ticket 11).
func CleanupUserPath(home, shimDir, pathMode string) (bool, error) {
	return false, nil
}

// withUserPathLock is a pass-through on non-Windows platforms: the PATH
// read-modify-write lock only guards the Windows registry-backed PATH.
// The stub exists so cross-platform files (e.g. migrate.go, which has no
// build tag) can call it without build-tag friction (ticket 10 / P0-11).
func withUserPathLock(fn func() error) error {
	return fn()
}

// SetUserEnvVar is not supported on non-Windows platforms; shell RC
// files handle persistent environment variables there.
func SetUserEnvVar(name, value string) error {
	return fmt.Errorf("persistent environment variable writes are not supported on this platform (Windows registry only); use shell RC files or 'xdev env' instead")
}

// SetUserEnvVarTrusted is not supported on non-Windows platforms.
func SetUserEnvVarTrusted(name, value string) error {
	return SetUserEnvVar(name, value)
}

// SetUserEnvVarRestore is not supported on non-Windows platforms（回滚与写入
// 同一语义：非 Windows 上持久环境变量一律不支持，错误信息一致）。
func SetUserEnvVarRestore(name, value string) error {
	return SetUserEnvVar(name, value)
}

// GetUserEnvVar reads from the process environment on non-Windows platforms.
func GetUserEnvVar(name string) (string, error) {
	return os.Getenv(name), nil
}

// GetUserEnvVarTrusted reads from the process environment on non-Windows platforms.
func GetUserEnvVarTrusted(name string) (string, error) {
	return GetUserEnvVar(name)
}

// GetUserEnvVarsBatch reads multiple env vars at once. On non-Windows this
// simply reads from the process environment (already fast, no syscall overhead).
func GetUserEnvVarsBatch(names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	for _, n := range names {
		result[n] = os.Getenv(n)
	}
	return result, nil
}

// DeleteUserEnvVar is not supported on non-Windows platforms.
func DeleteUserEnvVar(name string) error {
	return fmt.Errorf("persistent environment variable deletion is not supported on this platform (Windows registry only)")
}

// DeleteUserEnvVarTrusted is not supported on non-Windows platforms.
func DeleteUserEnvVarTrusted(name string) error {
	return DeleteUserEnvVar(name)
}

// ReadSystemPath is a no-op on non-Windows platforms.
func ReadSystemPath() (string, error) {
	return "", nil
}

// WriteSystemPath is a no-op on non-Windows platforms.
func WriteSystemPath(path string) error {
	return nil
}

// CleanupSystemPath is a no-op on non-Windows platforms.
func CleanupSystemPath() (bool, error) {
	return false, nil
}

// EnrichFromRegistry is a no-op on non-Windows platforms; the process
// environment is already consistent with the shell's environment.
func EnrichFromRegistry(envSlice []string) []string {
	return envSlice
}

// otherInMemoryUserEnv 与 Windows 侧的 UseInMemoryUserEnvForTesting 对应：让同一份
// 跨平台测试能在非 Windows 上编译并断言（返回内存映射而非进程环境）。
var otherInMemoryUserEnv map[string]string

// UseInMemoryUserEnvForTesting 见 Windows 侧实现；非 Windows 下用给定映射替换
// "用户环境变量枚举"的结果（本平台没有注册表写入，故无需担心污染）。
func UseInMemoryUserEnvForTesting(vars map[string]string) (restore func()) {
	snapshot := make(map[string]string, len(vars))
	for k, v := range vars {
		snapshot[k] = v
	}
	otherInMemoryUserEnv = snapshot
	return func() { otherInMemoryUserEnv = nil }
}

// GetAllUserEnvVars returns all user-level environment variables.
// On non-Windows platforms it reads from the process environment.
func GetAllUserEnvVars() map[string]string {
	if otherInMemoryUserEnv != nil {
		out := make(map[string]string, len(otherInMemoryUserEnv))
		for k, v := range otherInMemoryUserEnv {
			out[k] = v
		}
		return out
	}
	result := make(map[string]string)
	for _, pair := range os.Environ() {
		idx := strings.Index(pair, "=")
		if idx <= 0 {
			continue
		}
		result[pair[:idx]] = pair[idx+1:]
	}
	return result
}

// SetEnvVar is not supported on non-Windows platforms.
func SetEnvVar(name, value, level string) error {
	return fmt.Errorf("persistent environment variable writes are not supported on this platform (Windows registry only)")
}

// GetEnvVar reads from the process environment on non-Windows platforms.
func GetEnvVar(name, level string) (string, error) {
	return os.Getenv(name), nil
}

// DeleteEnvVar is not supported on non-Windows platforms.
func DeleteEnvVar(name, level string) error {
	return fmt.Errorf("persistent environment variable deletion is not supported on this platform (Windows registry only)")
}

// CleanupStaleGOROOT is a no-op on non-Windows platforms: the stale value
// lives in HKCU\Environment, which only exists on Windows.
func CleanupStaleGOROOT() error {
	return nil
}

// EnumEnvVars is a no-op on non-Windows platforms.
func EnumEnvVars(level string) ([]EnvPair, error) {
	return nil, nil
}

// UpdateShimDirInUserPath is a no-op on non-Windows platforms.
// PATH updates for shim directories are handled via shell RC injection there.
func UpdateShimDirInUserPath(oldDir, newDir string) {}
