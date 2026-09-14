package discovery

import (
	"fmt"
	"github.com/xmdemt/xdev/internal/util"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// execCommand is the command constructor used by service installers.
// It is a variable so that tests can replace it with a mock.
var execCommand = exec.Command

// ---------------------------------------------------------------------------
// ServiceInstaller interface
// ---------------------------------------------------------------------------

// ServiceInstaller manages a platform-specific scheduled task that
// periodically runs `xdev sniff --all --headless` (FR-017).
type ServiceInstaller interface {
	// Install registers a system-level scheduled task.
	// schedule is a cron-like expression (platform-dependent).
	Install(schedule string) error

	// Uninstall removes the scheduled task.
	Uninstall() error

	// Status returns the current service status.
	Status() (*ServiceStatus, error)
}

// ServiceStatus describes the state of a sniff scheduled task.
type ServiceStatus struct {
	Installed bool      `json:"installed"`
	Schedule  string    `json:"schedule"`
	LastRun   time.Time `json:"last_run"`
	NextRun   time.Time `json:"next_run"`
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewServiceInstaller returns the platform-appropriate ServiceInstaller
// implementation.  On unsupported platforms it returns an error.
func NewServiceInstaller() (ServiceInstaller, error) {
	switch runtime.GOOS {
	case "windows":
		return &windowsTaskScheduler{}, nil
	case "linux":
		return &linuxCron{}, nil
	case "darwin":
		return &macosLaunchd{}, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// schedule helpers
// ---------------------------------------------------------------------------

const defaultSchedule = "0 3 * * *" // daily at 03:00

// normalizeSchedule converts a user-provided schedule string into a
// platform-appropriate format.  It accepts:
//   - "HH:MM" (e.g. "03:00") — converted to cron "M H * * *"
//   - A cron expression with at least minute+hour (e.g. "0 3 * * *")
//   - Empty string — returns the default schedule
func normalizeSchedule(schedule string) string {
	if schedule == "" {
		return defaultSchedule
	}
	// Check if it looks like HH:MM.
	if strings.Contains(schedule, ":") && !strings.Contains(schedule, " ") {
		parts := strings.Split(schedule, ":")
		if len(parts) == 2 {
			minute := strings.TrimLeft(parts[1], "0")
			hour := strings.TrimLeft(parts[0], "0")
			if minute == "" {
				minute = "0"
			}
			if hour == "" {
				hour = "0"
			}
			return minute + " " + hour + " * * *"
		}
	}
	return schedule
}

// cronFieldRanges 是 5 个 cron 字段各自的合法值域（分钟/小时/日/月/周）。
var cronFieldRanges = [][2]int{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 7},  // day of week (0 and 7 both mean Sunday)
}

// parseCronSchedule 严格校验 5 字段 cron 表达式（F-12）：
//   - 恰好 5 个空白分隔字段
//   - 每字段只允许数字、*、,、-、/（拒绝 ?、字母、换行与控制字符）
//   - 拒绝 shell 元字符（; & | $ ` " '）与任何非空白控制字符
//   - 数值必须在字段值域内（分钟 0-59、小时 0-23、日 1-31、月 1-12、周 0-7）
func parseCronSchedule(s string) error {
	if s == "" {
		return fmt.Errorf("cron schedule must not be empty")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("cron schedule %q contains control characters", s)
		}
	}
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return fmt.Errorf("cron schedule %q must have exactly 5 fields, got %d", s, len(fields))
	}
	for i, field := range fields {
		if err := validateCronField(field, cronFieldRanges[i][0], cronFieldRanges[i][1]); err != nil {
			return fmt.Errorf("cron field %d (%q): %w", i+1, field, err)
		}
	}
	return nil
}

// validateCronField 校验单个 cron 字段：只允许 [0-9*,/-]，数值在
// min..max 值域内，step（/n）必须大于 0。
func validateCronField(field string, min, max int) error {
	if field == "" {
		return fmt.Errorf("empty field")
	}
	for _, r := range field {
		switch {
		case r >= '0' && r <= '9':
		case r == '*' || r == ',' || r == '-' || r == '/':
		default:
			return fmt.Errorf("invalid character %q (only digits, *, ,, -, / allowed)", r)
		}
	}

	// 按逗号拆分各 step。
	for _, step := range strings.Split(field, ",") {
		if step == "" {
			return fmt.Errorf("empty list item")
		}
		// 拆分 /step 后缀。
		base := step
		stepVal := 1
		if idx := strings.Index(step, "/"); idx >= 0 {
			base = step[:idx]
			stepPart := step[idx+1:]
			if stepPart == "" {
				return fmt.Errorf("missing step value after '/'")
			}
			n, ok := parseInt(stepPart)
			if !ok || n <= 0 {
				return fmt.Errorf("invalid step value %q", stepPart)
			}
			stepVal = n
		}
		_ = stepVal

		// 单个数字或 * 或 a-b 范围。
		switch {
		case base == "*":
			// 任意值，OK。
		case strings.Contains(base, "-"):
			parts := strings.SplitN(base, "-", 2)
			if parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("invalid range %q", base)
			}
			lo, ok1 := parseInt(parts[0])
			hi, ok2 := parseInt(parts[1])
			if !ok1 || !ok2 {
				return fmt.Errorf("invalid range %q", base)
			}
			if lo < min || hi > max || lo > hi {
				return fmt.Errorf("range %q out of bounds (%d-%d)", base, min, max)
			}
		default:
			n, ok := parseInt(base)
			if !ok {
				return fmt.Errorf("invalid value %q", base)
			}
			if n < min || n > max {
				return fmt.Errorf("value %d out of bounds (%d-%d)", n, min, max)
			}
		}
	}
	return nil
}

// parseInt 解析纯数字字符串。
func parseInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// xdevPath returns the absolute path to the xdev executable.
func xdevPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve xdev path: %w", err)
	}
	return exe, nil
}

// ---------------------------------------------------------------------------
// Windows Task Scheduler
// ---------------------------------------------------------------------------

// windowsTaskScheduler uses schtasks.exe to register/unregister a
// daily task that invokes `xdev sniff --all --headless`.
type windowsTaskScheduler struct{}

func (w *windowsTaskScheduler) Install(schedule string) error {
	schedule = normalizeSchedule(schedule)
	// Parse HH:MM from the cron-like schedule (minute hour).
	// Default "0 3 * * *" → 03:00.
	parts := strings.Fields(schedule)
	if len(parts) < 2 {
		return fmt.Errorf("invalid schedule %q: expected at least minute and hour", schedule)
	}

	// Build schtasks command:
	//   schtasks /create /tn "XDevSniff" /tr "<xdev> sniff --all --headless"
	//            /sc DAILY /st HH:MM /f
	// Security (F-12): schtasks 参数以数组传递（非 shell 拼接），无命令注入面；
	// schedule 仅取前两个字段拼为 HH:MM，路径以引号包裹。
	exe, err := xdevPath()
	if err != nil {
		return err
	}

	hour := parts[1]
	minute := parts[0]
	// Pad to 2 digits.
	if len(hour) == 1 {
		hour = "0" + hour
	}
	if len(minute) == 1 {
		minute = "0" + minute
	}
	startTime := hour + ":" + minute

	args := []string{
		"/create",
		"/tn", "XDevSniff",
		"/tr", fmt.Sprintf(`"%s" sniff --all --headless`, exe),
		"/sc", "DAILY",
		"/st", startTime,
		"/f",
	}

	cmd := execCommand("schtasks", args...)
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /create failed: %v\n%s", err, output)
	}
	return nil
}

func (w *windowsTaskScheduler) Uninstall() error {
	cmd := execCommand("schtasks", "/delete", "/tn", "XDevSniff", "/f")
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /delete failed: %v\n%s", err, output)
	}
	return nil
}

func (w *windowsTaskScheduler) Status() (*ServiceStatus, error) {
	cmd := execCommand("schtasks", "/query", "/tn", "XDevSniff", "/fo", "LIST", "/v")
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Task not found or query failed → not installed.
		return &ServiceStatus{Installed: false}, nil
	}

	// Parse the verbose output for schedule info.
	lines := strings.Split(string(output), "\n")
	status := &ServiceStatus{Installed: true}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Schedule:") {
			status.Schedule = strings.TrimSpace(strings.TrimPrefix(line, "Schedule:"))
		}
		if strings.HasPrefix(line, "Start Time:") {
			status.Schedule = strings.TrimSpace(strings.TrimPrefix(line, "Start Time:"))
		}
		if strings.HasPrefix(line, "Last Run Time:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Last Run Time:"))
			if t, err := time.Parse("1/2/2006 3:04:05 PM", val); err == nil {
				status.LastRun = t
			}
		}
		if strings.HasPrefix(line, "Next Run Time:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Next Run Time:"))
			if t, err := time.Parse("1/2/2006 3:04:05 PM", val); err == nil {
				status.NextRun = t
			}
		}
	}
	return status, nil
}

// ---------------------------------------------------------------------------
// Linux Cron
// ---------------------------------------------------------------------------

// linuxCron manages a user crontab entry that runs the sniff command
// daily.
type linuxCron struct{}

func (l *linuxCron) Install(schedule string) error {
	schedule = normalizeSchedule(schedule)

	// Security (F-12): 严格校验 cron 表达式（5 字段、无 shell 元字符）。
	if err := parseCronSchedule(schedule); err != nil {
		return err
	}

	exe, err := xdevPath()
	if err != nil {
		return err
	}

	// crontab 行由 /bin/sh 执行 —— 可执行路径用单引号包裹并转义内部单引号，
	// 防止含空格/特殊字符的路径被注入。
	quotedExe := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	cronLine := schedule + " " + quotedExe + " sniff --all --headless\n"

	// Read existing crontab.
	existing, _ := l.readCrontab()

	// Remove any existing xdev sniff line.
	var filtered []string
	for _, line := range existing {
		if !strings.Contains(line, "xdev sniff") {
			filtered = append(filtered, line)
		}
	}

	// Append new line.
	filtered = append(filtered, cronLine)

	return l.writeCrontab(filtered)
}

func (l *linuxCron) Uninstall() error {
	existing, err := l.readCrontab()
	if err != nil {
		return nil // No crontab → nothing to uninstall.
	}
	var filtered []string
	for _, line := range existing {
		if !strings.Contains(line, "xdev sniff") {
			filtered = append(filtered, line)
		}
	}
	return l.writeCrontab(filtered)
}

func (l *linuxCron) Status() (*ServiceStatus, error) {
	existing, err := l.readCrontab()
	if err != nil {
		return &ServiceStatus{Installed: false}, nil
	}

	status := &ServiceStatus{Installed: false}
	for _, line := range existing {
		if strings.Contains(line, "xdev sniff") {
			status.Installed = true
			status.Schedule = strings.TrimSpace(strings.Split(line, " xdev")[0])
			break
		}
	}
	return status, nil
}

func (l *linuxCron) readCrontab() ([]string, error) {
	cmd := execCommand("crontab", "-l")
	util.HideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(output), "\n")
	// Remove trailing empty line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

func (l *linuxCron) writeCrontab(lines []string) error {
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") && len(lines) > 0 {
		content += "\n"
	}

	cmd := execCommand("crontab", "-")
	util.HideWindow(cmd)
	cmd.Stdin = strings.NewReader(content)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crontab write failed: %v\n%s", err, output)
	}
	return nil
}

// ---------------------------------------------------------------------------
// macOS Launchd
// ---------------------------------------------------------------------------

// macosLaunchd manages a ~/Library/LaunchAgents/com.xdev.sniff.plist
// that runs the sniff command daily.
type macosLaunchd struct{}

func (m *macosLaunchd) Install(schedule string) error {
	schedule = normalizeSchedule(schedule)

	// Parse HH:MM from cron schedule.
	parts := strings.Fields(schedule)
	if len(parts) < 2 {
		return fmt.Errorf("invalid schedule %q: expected at least minute and hour", schedule)
	}

	exe, err := xdevPath()
	if err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.xdev.sniff</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>sniff</string>
		<string>--all</string>
		<string>--headless</string>
	</array>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>%s</integer>
		<key>Minute</key>
		<integer>%s</integer>
	</dict>
	<key>RunAtLoad</key>
	<false/>
	<key>StandardOutPath</key>
	<string>/tmp/com.xdev.sniff.stdout</string>
	<key>StandardErrorPath</key>
	<string>/tmp/com.xdev.sniff.stderr</string>
</dict>
</plist>
`, exe, parts[1], parts[0])

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.xdev.sniff.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load the plist with launchctl.
	cmd := execCommand("launchctl", "load", plistPath)
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load failed: %v\n%s", err, output)
	}
	return nil
}

func (m *macosLaunchd) Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.xdev.sniff.plist")

	// Unload first.
	cmd := execCommand("launchctl", "unload", plistPath)
	util.HideWindow(cmd)
	_, _ = cmd.CombinedOutput()

	// Remove the plist file.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func (m *macosLaunchd) Status() (*ServiceStatus, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.xdev.sniff.plist")
	status := &ServiceStatus{Installed: false}

	if _, err := os.Stat(plistPath); err == nil {
		status.Installed = true
		status.Schedule = "daily at 03:00 (default)"

		// Check launchctl list for last run info.
		cmd := execCommand("launchctl", "list", "com.xdev.sniff")
		util.HideWindow(cmd)
		output, err := cmd.Output()
		if err == nil {
			// Parse "com.xdev.sniff    <PID>    <ExitCode>"
			// We only check that it's registered.
			_ = output
		}
	}

	return status, nil
}
