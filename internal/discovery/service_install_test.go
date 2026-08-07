package discovery

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// execCommand mock
// ---------------------------------------------------------------------------

// mockCmd records the commands that would have been executed.
type mockCmd struct {
	name string
	args []string
}

var (
	mockCmds   []mockCmd
	mockCmdErr error
)

func mockExecCommand(name string, args ...string) *exec.Cmd {
	mockCmds = append(mockCmds, mockCmd{name: name, args: args})
	cmd := exec.Command("go", "version") // harmless command
	return cmd
}

func resetMock() {
	mockCmds = nil
	mockCmdErr = nil
}

// ---------------------------------------------------------------------------
// Factory test
// ---------------------------------------------------------------------------

func TestNewServiceInstaller(t *testing.T) {
	si, err := NewServiceInstaller()
	if err != nil {
		// On unsupported platforms, an error is acceptable.
		t.Logf("NewServiceInstaller: %v (expected on unsupported platforms)", err)
		return
	}
	if si == nil {
		t.Fatal("expected non-nil ServiceInstaller")
	}
}

// ---------------------------------------------------------------------------
// Windows Task Scheduler
// ---------------------------------------------------------------------------

func TestWindowsTaskScheduler_Install(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	w := &windowsTaskScheduler{}
	err := w.Install("")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if len(mockCmds) == 0 {
		t.Fatal("expected at least one command to be executed")
	}

	// Check that schtasks was called with /create.
	cmd := mockCmds[0]
	if !strings.HasSuffix(cmd.name, "schtasks") && cmd.name != "schtasks" {
		t.Errorf("expected schtasks command, got %s", cmd.name)
	}

	foundCreate := false
	foundTn := false
	for _, arg := range cmd.args {
		if arg == "/create" {
			foundCreate = true
		}
		if arg == "XDevSniff" {
			foundTn = true
		}
	}
	if !foundCreate {
		t.Errorf("expected /create argument in schtasks")
	}
	if !foundTn {
		t.Errorf("expected XDevSniff task name in schtasks")
	}
}

func TestWindowsTaskScheduler_Uninstall(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	w := &windowsTaskScheduler{}
	err := w.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	if len(mockCmds) == 0 {
		t.Fatal("expected a command to be executed")
	}

	cmd := mockCmds[0]
	foundDelete := false
	foundTn := false
	for _, arg := range cmd.args {
		if arg == "/delete" {
			foundDelete = true
		}
		if arg == "XDevSniff" {
			foundTn = true
		}
	}
	if !foundDelete {
		t.Errorf("expected /delete argument in schtasks")
	}
	if !foundTn {
		t.Errorf("expected XDevSniff task name")
	}
}

func TestWindowsTaskScheduler_Status(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	w := &windowsTaskScheduler{}
	status, err := w.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	_ = status
}

// ---------------------------------------------------------------------------
// Linux Cron
// ---------------------------------------------------------------------------

func TestLinuxCron_Install(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	l := &linuxCron{}
	err := l.Install("")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
}

func TestLinuxCron_Status(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	l := &linuxCron{}
	status, err := l.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
}

// ---------------------------------------------------------------------------
// macOS Launchd
// ---------------------------------------------------------------------------

func TestMacosLaunchd_Install(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	m := &macosLaunchd{}
	err := m.Install("")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
}

func TestMacosLaunchd_Status(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-specific test")
	}

	resetMock()
	savedFn := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = savedFn }()

	m := &macosLaunchd{}
	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
}

// ---------------------------------------------------------------------------
// Non-platform-specific tests
// ---------------------------------------------------------------------------

func TestXdevPath(t *testing.T) {
	path, err := xdevPath()
	if err != nil {
		t.Fatalf("xdevPath failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
	// Verify the file exists or is a valid path.
	if _, err := os.Stat(path); err != nil {
		t.Logf("xdevPath points to %s (might be test binary): %v", path, err)
	}
}

// TestParseCronSchedule covers the strict 5-field cron validator (F-12).
func TestParseCronSchedule(t *testing.T) {
	valid := []string{
		"0 3 * * *",
		"* * * * *",
		"*/15 8-18 * * 1-5",
		"0,30 0,12 1,15 * 0",
		"5 4 * * 7",
		"59 23 31 12 7",
	}
	for _, s := range valid {
		if err := parseCronSchedule(s); err != nil {
			t.Errorf("parseCronSchedule(%q) should be valid, got: %v", s, err)
		}
	}

	invalid := []string{
		"",
		"0 3 * *",         // 4 fields
		"0 3 * * * *",     // 6 fields
		"0 3 * * a",       // letter
		"0 3 * * *; rm -rf /",
		"0 3 * * * & evil",
		"0 3 * * * | cat",
		"0 3 * * * $HOME",
		"0 3 * * * `ls`",
		"0 3 * * * \"x\"",
		"0\n3 * * *",      // newline
		"60 3 * * *",      // minute out of range
		"0 24 * * *",      // hour out of range
		"0 3 32 * *",      // day out of range
		"0 3 * 13 *",      // month out of range
		"0 3 * * 8",       // weekday out of range
		"0 3 * * ?",       // '?' not allowed
		"0 3 * * *-",      // dangling range
		"0 3 */0 * *",     // zero step
	}
	for _, s := range invalid {
		if err := parseCronSchedule(s); err == nil {
			t.Errorf("parseCronSchedule(%q) should be rejected", s)
		}
	}
}

// TestNormalizeScheduleStillValid verifies normalizeSchedule output passes
// the strict validator (HH:MM conversion and default schedule).
func TestNormalizeScheduleStillValid(t *testing.T) {
	for _, s := range []string{"", "03:00", "0 3 * * *", "*/10 9-17 * * 1-5"} {
		norm := normalizeSchedule(s)
		if err := parseCronSchedule(norm); err != nil {
			t.Errorf("normalizeSchedule(%q) = %q should pass parseCronSchedule, got: %v", s, norm, err)
		}
	}
}
