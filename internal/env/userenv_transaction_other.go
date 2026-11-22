//go:build !windows

package env

import (
	"fmt"
	"os"
)

func withUserEnvTransactionLock(fn func() error) error { return withUserPathLock(fn) }

func ReadUserEnvSnapshot(name string) (UserEnvSnapshot, error) {
	value, exists := os.LookupEnv(name)
	return UserEnvSnapshot{Exists: exists, Name: name, Raw: value, Type: UserEnvString}, nil
}

func RestoreUserEnvSnapshot(name string, snapshot UserEnvSnapshot) error {
	return fmt.Errorf("persistent environment variable writes are not supported on this platform (Windows registry only)")
}
