//go:build !bindings && windows && desktop

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var guiFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")
var guiMessageBox = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// Detach this GUI process only. A console created for an Explorer launch closes
// when its last client detaches; an existing terminal stays attached to its own
// shell. Hiding or closing a console window could affect unrelated CLI clients.
func detachGUIConsole() error {
	result, _, err := guiFreeConsole.Call()
	if result != 0 {
		return nil
	}
	if err != syscall.Errno(0) {
		return fmt.Errorf("FreeConsole: %w", err)
	}
	return fmt.Errorf("FreeConsole failed")
}

// After detaching, stderr may refer to the closed console. Preserve a visible
// diagnostic for WebView/runtime failures as well as the startup log.
func reportGUIStartupError(err error) {
	if err == nil {
		return
	}
	text, convErr := syscall.UTF16PtrFromString("XDev could not start.\n\n" + strings.ReplaceAll(err.Error(), "\x00", ""))
	if convErr != nil {
		return
	}
	title, _ := syscall.UTF16PtrFromString("XDev startup error")
	const mbOK = 0
	const mbIconError = 0x10
	guiMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbOK|mbIconError)
}
