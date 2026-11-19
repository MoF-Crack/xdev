//go:build !bindings && windows && desktop

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var testConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

func ownConsoleClients() uint32 {
	var processes [64]uint32
	count, _, _ := testConsoleProcessList.Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	return uint32(count)
}

type guiConsoleTestResult struct {
	Before        uint32
	After         uint32
	ChildDetached bool
	Error         string
}

// Real FreeConsole runs only in a child that this test launched, never in the
// test runner or a user's existing terminal. A broker verifies shared-console
// detachment leaves the caller's console intact.
func TestGUIConsoleDetachOwnedProcess(t *testing.T) {
	if mode := os.Getenv("XDEV_GUI_CONSOLE_TEST_MODE"); mode != "" {
		out := guiConsoleTestResult{Before: ownConsoleClients()}
		switch mode {
		case "detach", "no-console":
			if err := detachGUIConsole(); err != nil {
				out.Error = err.Error()
			}
			out.After = ownConsoleClients()
			out.ChildDetached = out.After == 0 && out.Error == "" && (out.Before > 0 || mode == "no-console")
		case "broker":
			childPath := os.Getenv("XDEV_GUI_CONSOLE_RESULT") + ".child"
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGUIConsoleDetachOwnedProcess$")
			cmd.Env = append(os.Environ(), "XDEV_GUI_CONSOLE_TEST_MODE=detach", "XDEV_GUI_CONSOLE_RESULT="+childPath)
			if err := cmd.Run(); err != nil {
				out.Error = fmt.Sprintf("owned shared-console child: %v", err)
			}
			var child guiConsoleTestResult
			data, err := os.ReadFile(childPath)
			if err != nil {
				out.Error = err.Error()
			} else if err = json.Unmarshal(data, &child); err != nil {
				out.Error = err.Error()
			}
			out.ChildDetached = child.ChildDetached
			out.After = ownConsoleClients()
		default:
			os.Exit(91)
		}
		data, _ := json.Marshal(out)
		if err := os.WriteFile(os.Getenv("XDEV_GUI_CONSOLE_RESULT"), data, 0600); err != nil {
			os.Exit(92)
		}
		os.Exit(0)
	}
	for _, mode := range []string{"detach", "broker", "no-console"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			resultFile := filepath.Join(t.TempDir(), "console-result.json")
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGUIConsoleDetachOwnedProcess$")
			cmd.Env = append(os.Environ(), "XDEV_GUI_CONSOLE_TEST_MODE="+mode, "XDEV_GUI_CONSOLE_RESULT="+resultFile)
			flags := uint32(0x10)
			if mode == "no-console" {
				flags = 0x08000000
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
			if err := cmd.Run(); err != nil {
				t.Fatalf("owned console fixture: %v", err)
			}
			data, err := os.ReadFile(resultFile)
			if err != nil {
				t.Fatal(err)
			}
			var result guiConsoleTestResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Error != "" || !result.ChildDetached || (mode != "no-console" && result.Before == 0) {
				t.Fatalf("GUI failed to detach real console: %+v", result)
			}
			if mode == "broker" && result.After == 0 {
				t.Fatalf("GUI detached or destroyed caller's shared console: %+v", result)
			}
		})
	}
}
