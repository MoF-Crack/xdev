//go:build windows

// goroot_cleanup_windows.go implements self-healing for the stale GOROOT
// user environment variable left behind by the pre-v2 go.yaml manifest
// (audit-fixes ticket 01 / X-07).
//
// Historical bug: manifests/go.yaml used to declare `GOROOT: '{{install}}'`
// while the Go archives keep their top-level `go/` directory (strip: 0), so
// the real toolchain root is `{{install}}\go`, one level below the value that
// was written to HKCU\Environment. A GOROOT without src/ makes every
// `go build` fail with "package fmt is not in std" — on this layout the bug
// is 100% reproducible.
//
// Two things make the residue sticky:
//
//   - EnrichFromRegistry overrides the process environment of every shim
//     subprocess with HKCU\Environment values, so clearing the variable in
//     a shell is undone on the next shim invocation — the registry VALUE
//     itself must be deleted;
//   - Go >= 1.26 self-derives GOROOT from the go binary location
//     (findGOROOT walks up two levels and validates with the isGOROOT
//     probe), so once the value is gone the toolchain works again.
package env

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// isGOROOTDir reports whether dir looks like a real Go toolchain root,
// mirroring the isGOROOT probe used by cmd/go's findGOROOT
// (cmd/go/internal/cfg/cfg.go): the directory must contain pkg/tool.
func isGOROOTDir(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "pkg", "tool"))
	return err == nil && fi.IsDir()
}

// CleanupStaleGOROOT removes the HKCU GOROOT value when it provably points
// one level above a real Go toolchain (the pre-v2 nested `go/` layout):
//
//	delete iff  !isGOROOTDir(V)  &&  isGOROOTDir(V + "\go")
//
// Anything else is left untouched — a valid GOROOT, a manually installed
// secondary toolchain, or a value whose origin we cannot determine are all
// kept (never use a blunt "V != {{install}} ⇒ delete" rule). Idempotent;
// no-op when GOROOT is absent.
func CleanupStaleGOROOT() error {
	v, err := GetUserEnvVar("GOROOT")
	if err != nil || v == "" {
		// Not set (or unreadable) — nothing to clean.
		return nil
	}
	if isGOROOTDir(v) {
		// Value already points at a real toolchain root — keep it.
		return nil
	}
	if !isGOROOTDir(filepath.Join(v, "go")) {
		// Neither V nor V\go is a GOROOT — unknown provenance, keep it.
		return nil
	}
	if err := DeleteUserEnvVar("GOROOT"); err != nil {
		return fmt.Errorf("deleting stale GOROOT %q from HKCU\\Environment: %w", v, err)
	}
	log.Printf("[env] removed stale GOROOT %q — the real toolchain is at %s\\go and the go command self-derives GOROOT", v, v)
	// Let new processes (Explorer-spawned terminals) pick up the change
	// immediately instead of inheriting the stale value from their parents.
	BroadcastEnvironmentChange()
	return nil
}
