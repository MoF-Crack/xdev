//go:build windows

package env

import (
	"errors"
	"fmt"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
	"golang.org/x/sys/windows/registry"
)

var (
	userEnvSnapshotFn   = readUserEnvSnapshotFromRegistry
	userEnvTypedWriteFn = writeUserEnvTypedToRegistry
)

func ReadUserEnvSnapshot(name string) (UserEnvSnapshot, error) {
	return userEnvSnapshotFn(name)
}

func RestoreUserEnvSnapshot(name string, snapshot UserEnvSnapshot) error {
	if err := util.ValidateEnvVarName(name); err != nil {
		return err
	}
	if !snapshot.Exists {
		return userEnvDeleteFn(name)
	}
	if !strings.EqualFold(name, snapshot.Name) {
		return fmt.Errorf("snapshot name %q does not match %q", snapshot.Name, name)
	}
	if snapshot.Type != UserEnvString && snapshot.Type != UserEnvExpandString {
		return fmt.Errorf("unsupported environment registry type %d", snapshot.Type)
	}
	// Restoration intentionally bypasses manifest path/reference validation.
	return userEnvTypedWriteFn(snapshot.Name, snapshot.Raw, snapshot.Type)
}

// UseUserEnvBackendForTesting replaces all transaction reads/writes/deletes.
// It is process-scoped; callers must not swap backends concurrently.
func UseUserEnvBackendForTesting(backend UserEnvBackend) (restore func()) {
	previousRead, previousTyped := userEnvSnapshotFn, userEnvTypedWriteFn
	previousWrite, previousDelete := userEnvWriteFn, userEnvDeleteFn
	userEnvSnapshotFn, userEnvTypedWriteFn = backend.Read, backend.Write
	userEnvWriteFn = func(name, raw string) error { return backend.Write(name, raw, UserEnvExpandString) }
	userEnvDeleteFn = backend.Delete
	return func() {
		userEnvSnapshotFn, userEnvTypedWriteFn = previousRead, previousTyped
		userEnvWriteFn, userEnvDeleteFn = previousWrite, previousDelete
	}
}

func readUserEnvSnapshotFromRegistry(name string) (UserEnvSnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return UserEnvSnapshot{}, nil
	}
	if err != nil {
		return UserEnvSnapshot{}, err
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return UserEnvSnapshot{}, fmt.Errorf("enumerate environment names: %w", err)
	}
	for _, originalName := range names {
		if !strings.EqualFold(originalName, name) {
			continue
		}
		raw, valueType, err := key.GetStringValue(originalName)
		if err != nil {
			// A disappearing or unreadable enumerated value is an error, not absence.
			return UserEnvSnapshot{}, fmt.Errorf("read environment value %s: %w", originalName, err)
		}
		return UserEnvSnapshot{Exists: true, Name: originalName, Raw: raw, Type: valueType}, nil
	}
	return UserEnvSnapshot{}, nil
}

func writeUserEnvTypedToRegistry(name, raw string, valueType uint32) error {
	realUserRegistryMutationCount.Add(1)
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return setRegistryString(key, name, raw, valueType)
}
