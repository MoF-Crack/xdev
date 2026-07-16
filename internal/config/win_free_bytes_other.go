//go:build !windows

package config

// winFreeBytesImpl is a no-op on non-Windows platforms; DriveFreeBytes
// will return (0, false) so the caller falls back to the user home.
func winFreeBytesImpl(drive string) (int64, bool) {
	return 0, false
}

// driveTotalBytesImpl is a no-op on non-Windows platforms.
func driveTotalBytesImpl(drive string) (int64, bool) {
	return 0, false
}
