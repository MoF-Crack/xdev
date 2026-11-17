//go:build !desktop

package gui

import "fmt"

// Run gives a useful error in developer/maintenance builds. Distributed XDev
// binaries always use the desktop tag and contain CLI, TUI, GUI and shim modes.
func Run(_ *App, _ string) error {
	return fmt.Errorf("desktop runtime is not included: build XDev with -tags desktop,production (or use the project build script)")
}
