package env

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

const (
	UserEnvString       uint32 = 1 // REG_SZ
	UserEnvExpandString uint32 = 2 // REG_EXPAND_SZ
)

// UserEnvSnapshot distinguishes an absent value from an existing empty value.
// Raw is unexpanded; Name and Type preserve the original registry representation.
type UserEnvSnapshot struct {
	Exists bool
	Name   string
	Raw    string
	Type   uint32
}

// UserEnvBackend is an isolated I/O boundary for transaction fault injection.
// Read must distinguish absence (Exists=false, nil) from an unreadable value.
// Write must preserve an existing value's name under case-insensitive lookup.
type UserEnvBackend struct {
	Read   func(name string) (UserEnvSnapshot, error)
	Write  func(name, raw string, valueType uint32) error
	Delete func(name string) error
}

// WithUserPathTransactionLock holds the existing user PATH lock without the
// legacy fail-open fallback. Multi-resource callers must retain it until their
// last PATH write and any compensation have completed.
func WithUserPathTransactionLock(fn func() error) error {
	return withUserEnvTransactionLock(fn)
}

// ApplyUserEnvVars validates and snapshots every target before writing any of
// them. The existing PATH lock also serializes user-environment transactions.
// A failed single-value registry write leaves that value unchanged. Only earlier
// successful writes are undone, in reverse order.
func ApplyUserEnvVars(vars map[string]string) error {
	if len(vars) == 0 {
		return nil
	}
	names := make([]string, 0, len(vars))
	seen := make(map[string]string, len(vars))
	for name := range vars {
		if err := util.ValidateEnvVarName(name); err != nil {
			return err
		}
		folded := strings.ToUpper(name)
		if previous, ok := seen[folded]; ok {
			return fmt.Errorf("conflicting environment variable names %q and %q", previous, name)
		}
		seen[folded] = name
		names = append(names, name)
	}
	sort.Strings(names)
	return withUserEnvTransactionLock(func() error {
		originals := make(map[string]UserEnvSnapshot, len(names))
		for _, name := range names {
			original, err := ReadUserEnvSnapshot(name)
			if err != nil {
				return fmt.Errorf("snapshot user env var %s: %w", name, err)
			}
			originals[name] = original
		}
		for index, name := range names {
			if err := SetUserEnvVarTrusted(name, vars[name]); err != nil {
				failures := []error{fmt.Errorf("write user env var %s: %w", name, err)}
				for j := index - 1; j >= 0; j-- {
					previous := names[j]
					if restoreErr := RestoreUserEnvSnapshot(previous, originals[previous]); restoreErr != nil {
						failures = append(failures, fmt.Errorf("restore user env var %s: %w", previous, restoreErr))
					}
				}
				return errors.Join(failures...)
			}
		}
		return nil
	})
}
