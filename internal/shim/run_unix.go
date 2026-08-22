//go:build !windows

package shim

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// run replaces the current process with the real tool. The shim must
// not stay resident for the child lifetime (Spec 030).
func run(target string, args []string) error {
	argv := make([]string, 0, 1+len(args))
	argv = append(argv, target)
	argv = append(argv, args...)
	if err := unix.Exec(target, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", target, err)
	}
	return nil
}
