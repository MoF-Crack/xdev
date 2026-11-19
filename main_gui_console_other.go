//go:build !bindings && (!windows || !desktop)

package main

// Non-Windows desktops and maintenance builds keep their existing streams.
// A maintenance build must print its missing-desktop error to the terminal.
func detachGUIConsole() error     { return nil }
func reportGUIStartupError(error) {}
