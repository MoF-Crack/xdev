// Package gui implements the Wails-based desktop GUI for xdev.
//
// The GUI is built with Wails v2 (Go backend) + Vue 3 (frontend).
// Communication between Go and JavaScript happens via:
//   - Wails bindings: exported methods on the App struct
//   - Wails events: real-time updates (install progress, etc.)
//
// # Startup
//
// The Run() function creates a Wails application with:
//   - Custom dark theme (Naive UI integration)
//   - Frameless window with custom title bar
//   - Single-instance enforcement (prevents multiple GUI windows)
//   - Parent process monitoring (auto-closes if launcher dies)
//
// # Single Instance
//
// On Windows, the GUI uses a named mutex to ensure only one instance
// runs at a time. If a second instance is launched, it activates the
// existing window and exits.
//
// # Parent Process Monitoring
//
// The GUI monitors its parent process (typically xdev.exe) and
// automatically closes if the parent dies, preventing orphan windows.
// This uses NtQueryInformationProcess on Windows to get the parent PID.
package gui

import (
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/xmdemt/xdev/frontend"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProc   = kernel32.NewProc("OpenProcess")
	procTerminate  = kernel32.NewProc("TerminateProcess")
	procCloseHand  = kernel32.NewProc("CloseHandle")
	ntdll          = syscall.NewLazyDLL("ntdll.dll")
	procNtQuery   = ntdll.NewProc("NtQueryInformationProcess")
)

const (
	processQueryLimitedInfo = 0x1000
	stillActive            = 259
	processBasicInfo       = 0
)

// processBasicInformation is the output struct for
// NtQueryInformationProcess(ProcessBasicInformation).
type processBasicInformation struct {
	Reserved1       uintptr
	PebBaseAddress  uintptr
	Reserved2       [2]uintptr
	UniqueProcessID uintptr
	InheritedFrom   uintptr // InheritedFromUniqueProcessId → parent PID
}

// getParentPID returns the parent PID of the given process using
// NtQueryInformationProcess.  This avoids spawning cmd.exe / wmic.
func getParentPID(pid int) (int, bool) {
	handle, _, _ := procOpenProc.Call(
		uintptr(processQueryLimitedInfo),
		0,
		uintptr(pid),
	)
	if handle == 0 {
		return 0, false
	}
	defer procCloseHand.Call(handle)

	var pbi processBasicInformation
	var retLen uintptr
	status, _, _ := procNtQuery.Call(
		handle,
		uintptr(processBasicInfo),
		uintptr(unsafe.Pointer(&pbi)),
		unsafe.Sizeof(pbi),
		uintptr(unsafe.Pointer(&retLen)),
	)
	if status != 0 {
		return 0, false
	}
	return int(pbi.InheritedFrom), true
}

// isProcessAliveNative checks whether a process with the given PID
// is still running using OpenProcess — no cmd.exe / tasklist needed.
func isProcessAliveNative(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, _, _ := procOpenProc.Call(
		uintptr(processQueryLimitedInfo),
		0,
		uintptr(pid),
	)
	if handle == 0 {
		return false
	}
	defer procCloseHand.Call(handle)

	var exitCode uintptr
	kernel32.NewProc("GetExitCodeProcess").Call(handle, uintptr(unsafe.Pointer(&exitCode)))
	return exitCode == stillActive
}

// killStaleWebView2 finds and terminates any stale msedgewebview2.exe
// processes that may be left over from previous xdev-gui sessions.
//
// This implementation uses only Windows syscalls (no cmd.exe / wmic /
// tasklist) to avoid flashing console windows on screen.
//
// IMPORTANT: only processes whose parent PID no longer exists
// (i.e. orphans whose parent xdev-gui has died) are killed, to
// avoid disrupting other WebView2 applications.
func killStaleWebView2() {
	if runtime.GOOS != "windows" {
		return
	}
	logInfo("[killStaleWebView2] scanning for orphan msedgewebview2.exe processes")
	start := time.Now()

	snap, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateToolhelp32Snapshot").Call(
		0x00000002, // TH32CS_SNAPPROCESS
		0,
	)
	if snap == ^uintptr(0) {
		logWarn("[killStaleWebView2] CreateToolhelp32Snapshot failed, skipping cleanup")
		return
	}
	defer procCloseHand.Call(snap)

	type processEntry32 struct {
		Size            uint32
		Usage           uint32
		ProcessID       uint32
		DefaultHeapID   uintptr
		ModuleID        uint32
		Threads         uint32
		ParentProcessID uint32
		PriClass        int32
		Flags           uint32
		ExeFile         [syscall.MAX_PATH]uint16
	}

	var entry processEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	proc32First := syscall.NewLazyDLL("kernel32.dll").NewProc("Process32FirstW")
	proc32Next := syscall.NewLazyDLL("kernel32.dll").NewProc("Process32NextW")

	ret, _, _ := proc32First.Call(snap, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		logWarn("[killStaleWebView2] Process32First failed, no processes enumerated")
		return
	}

	killed := 0
	total := 0
	for {
		exeName := syscall.UTF16ToString(entry.ExeFile[:])
		if exeName == "msedgewebview2.exe" {
			total++
			ppid := int(entry.ParentProcessID)
			alive := isProcessAliveNative(ppid)
			logDebug("[killStaleWebView2] msedgewebview2.exe PID=%d PPID=%d parentAlive=%v",
				entry.ProcessID, ppid, alive)
			if !alive {
				handle, _, _ := procOpenProc.Call(
					0x0001, // PROCESS_TERMINATE
					0,
					uintptr(entry.ProcessID),
				)
				if handle != 0 {
					procTerminate.Call(handle, 1)
					procCloseHand.Call(handle)
					killed++
					logInfo("[killStaleWebView2] killed orphan PID=%d (PPID=%d was dead)",
						entry.ProcessID, ppid)
				} else {
					logWarn("[killStaleWebView2] failed to open PID=%d for termination",
						entry.ProcessID)
				}
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ret, _, _ := proc32Next.Call(snap, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}
	logInfo("[killStaleWebView2] done in %v: found %d msedgewebview2 processes, killed %d orphans",
		time.Since(start), total, killed)
}

// prewarmWebView2 prepares the runtime environment for a clean
// Wails startup.  It:
//
//  1. Kills stale msedgewebview2.exe processes left over from
//     previous sessions.
//  2. Sets environment variables to disable WebView2 telemetry.
func prewarmWebView2() {
	if runtime.GOOS == "windows" {
		logInfo("[prewarmWebView2] starting WebView2 pre-warm sequence")
		killStaleWebView2()

		os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
			"--disable-features=msSmartScreenProtection,msTelemetryEnabled "+
				"--disable-background-networking "+
				"--disable-component-update "+
				"--disable-domain-reliability "+
				"--no-pings "+
				"--no-default-browser-check "+
				"--disable-sync "+
				"--metrics-recording-only "+
				"--disable-breakpad")
		logInfo("[prewarmWebView2] set WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS to disable telemetry")
		logInfo("[prewarmWebView2] pre-warm complete")
	}
}

// Run starts the Wails GUI application.
// webviewDir is the directory where WebView2 stores its user data.
// If empty, WebView2 uses its default location.
func Run(app *App, webviewDir string) error {
	logInfo("[Run] GUI starting, webviewDir=%s", webviewDir)

	// Ensure the WebView2 data directory exists before launching.
	if webviewDir != "" {
		if err := os.MkdirAll(webviewDir, 0o755); err != nil {
			logError("[Run] failed to create webviewDir %s: %v", webviewDir, err)
		} else {
			logInfo("[Run] webviewDir ensured: %s", webviewDir)
		}
	}

	// Kill stale WebView2 orphan processes *before* Wails starts,
	// so they don't appear as ghost windows during startup.
	prewarmWebView2()

	logInfo("[Run] calling wails.Run() with StartHidden=true, Width=1024, Height=768")
	start := time.Now()

	err := wails.Run(&options.App{
		Title:     "XDev - Development Environment Manager",
		Width:     1024,
		Height:    768,
		MinWidth:  800,
		MinHeight: 600,
		BackgroundColour: &options.RGBA{R: 10, G: 22, B: 40, A: 255},
		AssetServer: &assetserver.Options{
			Assets: frontend.DistFS,
		},
		OnStartup:  app.startup,
		OnDomReady: app.domReady,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		// StartHidden: true hides the main window during WebView2
		// initialisation.  Without this, WebView2's child processes
		// (GPU, Renderer, Network) briefly flash visible 800x1000
		// windows on screen.  We show the window in domReady after
		// the Hide+Show hack has completed.
		StartHidden: true,
		// Frameless removes the native Windows title bar so we can
		// use a custom title bar that matches the dark theme and
		// prevents double-click-to-close bugs on the native title bar.
		Frameless: true,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			WebviewUserDataPath:  webviewDir,
		},
	})

	if err != nil {
		logError("[Run] wails.Run() returned error after %v: %v", time.Since(start), err)
	} else {
		logInfo("[Run] wails.Run() exited normally after %v", time.Since(start))
	}

	return err
}
