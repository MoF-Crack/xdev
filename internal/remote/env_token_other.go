//go:build !windows

package remote

// windowsUserEnv is a no-op on non-Windows platforms.  On Windows,
// user-level environment variables stored in HKCU\Environment are
// not visible to processes launched before the variable was added
// (e.g. GUI launched from Windows Explorer).  The windowsUserEnv
// function reads them directly from the registry as a fallback.
func windowsUserEnv(key string) string {
	return ""
}
