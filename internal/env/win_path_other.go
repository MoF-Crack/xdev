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
	return "", nil
}

// WriteUserPath is a no-op on non-Windows platforms.
func WriteUserPath(path string) error {
	return nil
}

// BroadcastEnvironmentChange is a no-op on non-Windows platforms.
func BroadcastEnvironmentChange() {}

// CleanupUserPath is a no-op on non-Windows platforms.
func CleanupUserPath(shimDir string, pathMode string) (bool, error) {
	return false, nil
}

// SetUserEnvVar is not supported on non-Windows platforms; shell RC
// files handle persistent environment variables there.
func SetUserEnvVar(name, value string) error {
	return fmt.Errorf("persistent environment variable writes are not supported on this platform (Windows registry only); use shell RC files or 'xdev env' instead")
}

// GetUserEnvVar reads from the process environment on non-Windows platforms.
func GetUserEnvVar(name string) (string, error) {
	return os.Getenv(name), nil
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

// GetAllUserEnvVars returns all user-level environment variables.
// On non-Windows platforms it reads from the process environment.
func GetAllUserEnvVars() map[string]string {
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

// EnumEnvVars is a no-op on non-Windows platforms.
func EnumEnvVars(level string) ([]EnvPair, error) {
	return nil, nil
}

// UpdateShimDirInUserPath is a no-op on non-Windows platforms.
// PATH updates for shim directories are handled via shell RC injection there.
func UpdateShimDirInUserPath(oldDir, newDir string) {}
