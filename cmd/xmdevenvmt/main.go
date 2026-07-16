// Package main is the entry point of xdev
// (XDev - Development Environment Manager).
//
// # Startup Sequence
//
//  1. main() calls cli.NewRootCommand() which:
//     a. Checks os.Args[0] basename for shim dispatch (shim.MaybeExec)
//     b. If the binary was invoked as a tool alias (e.g. "python"),
//        MaybeExec finds and exec's the real tool, then exits
//     c. Otherwise, builds the full Cobra command tree
//  2. root.Execute() runs the appropriate subcommand
//  3. If no subcommand is specified, the TUI is launched by default
//
// # Build Variants
//
// There are two build variants controlled by build tags:
//   - Default (this file): CLI/TUI only, no Wails dependency
//   - bindings (main_bindings.go): GUI build with Wails frontend bindings
//
// The GUI variant is built separately and produces xdev-gui.exe.
// Both variants share the same internal packages.
package main

import (
	"fmt"
	"os"

	"github.com/xmdemt/xdev/internal/cli"
)

const (
	toolName = "xdev"
	toolDesc = "XDev - Development Environment Manager - " +
		"Cross-platform dev environment manager (CLI/TUI/GUI)"
)

func main() {
	root := cli.NewRootCommand(toolName, toolDesc)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
