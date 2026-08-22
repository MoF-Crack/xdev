//go:build windows

package shim

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/xmdemt/xdev/internal/env"
)

// run starts the real tool and waits. Windows has no execve overlay.
// EnrichFromRegistry injects HKCU tool homes the current shell may
// not have received after WM_SETTINGCHANGE (M-14).
func run(target string, args []string) error {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env.EnrichFromRegistry(os.Environ())

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", target, err)
	}
	return nil
}
