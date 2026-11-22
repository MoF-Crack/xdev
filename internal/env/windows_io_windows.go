//go:build windows

package env

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const systemEnvironmentKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`

var (
	environmentBroadcastFn = broadcastEnvironmentChangeToWindows
	systemPathReadFn       = func() (string, error) { return readRegistryPath(registry.LOCAL_MACHINE, systemEnvironmentKey) }
	systemPathWriteFn      = func(path string) error {
		realUserRegistryMutationCount.Add(1)
		return writeRegistryPath(registry.LOCAL_MACHINE, systemEnvironmentKey, path)
	}
)

func readRegistryPath(root registry.Key, subkey string) (string, error) {
	key, err := registry.OpenKey(root, subkey, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", subkey, err)
	}
	defer key.Close()
	raw, _, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	return raw, err
}

// Keep REG_SZ literal (including percent references); use REG_EXPAND_SZ only
// for existing expandable values and newly created paths.
func writeRegistryPath(root registry.Key, subkey, path string) error {
	key, err := registry.OpenKey(root, subkey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open %s to update Path: %w", subkey, err)
	}
	defer key.Close()
	_, typ, err := key.GetValue("Path", nil)
	if errors.Is(err, registry.ErrNotExist) {
		typ = UserEnvExpandString
	} else if err != nil {
		return fmt.Errorf("query Path type: %w", err)
	}
	return setRegistryString(key, "Path", path, typ)
}

func setRegistryString(key registry.Key, name, raw string, typ uint32) error {
	switch typ {
	case UserEnvString:
		return key.SetStringValue(name, raw)
	case UserEnvExpandString:
		return key.SetExpandStringValue(name, raw)
	default:
		return fmt.Errorf("unsupported registry type %d for %s", typ, name)
	}
}

// UseEnvironmentBroadcastForTesting replaces only the notification operation.
// Tests must install it before mutations; the caller restores it after use.
func UseEnvironmentBroadcastForTesting(broadcast func()) (restore func()) {
	previous := environmentBroadcastFn
	environmentBroadcastFn = broadcast
	return func() { environmentBroadcastFn = previous }
}
