//go:build windows

package env

// UseUserPathBackendForTesting installs a caller-owned PATH backend while keeping
// the real lock implementation on a private temporary lock path. The write
// callback must model atomic single-value writes: an error leaves its value and
// registry type unchanged. Restore after all test goroutines have finished.
func UseUserPathBackendForTesting(read func() (string, error), write func(string) error) func() {
	_, restore := UseInMemoryUserPathForTesting("")
	userPathReadFn, userPathWriteFn = read, write
	return restore
}
