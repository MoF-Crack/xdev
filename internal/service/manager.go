// Package service manages long-running development services
// (databases, caches, web servers, etc.) installed via xdev manifests.
//
// # Service Discovery
//
// Services are discovered by scanning the install directory for
// subdirectories containing a .xdev-service marker file. The marker
// is a key=value file with fields:
//
//	type=database
//	version=8.0.36
//	port=3306
//	exec=mysql/bin/mysqld
//	datadir=data
//
// # Lifecycle
//
// Start launches the service as a background process, redirecting
// stdout/stderr to log files. On Windows, CREATE_NEW_CONSOLE is used
// to give the service its own console window. Stop kills the process
// via Process.Kill() (no graceful shutdown support currently).
//
// # Security
//
// parseServiceMarker validates that exec and datadir paths stay within
// the service install directory using isSubPath(), which detects ".."
// traversal. On Windows, path comparison is case-insensitive.
// Services with invalid markers get Status = "invalid".
//
// # Environment Variables
//
// When a service starts, the following environment variables are set:
//   - <NAME>_PORT — the service port number
//   - <NAME>_DATA_DIR — the resolved data directory path
//   - All marker key=value pairs except _exec (which is the resolved
//     executable path, used internally only)
//
// # Known Limitations
//
//   - Log files are append-mode and never rotated (unbounded growth)
//   - Stop uses SIGKILL equivalent (no graceful shutdown)
//   - discoverInstalled() scans the filesystem on every List/Status call
//     (cached for 5 seconds to avoid redundant I/O)
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
)

// Service represents a runnable development service.
type Service struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // database | web-server | cache | queue | search | storage
	Version string            `json:"version"`
	Status  string            `json:"status"` // running | stopped | error
	Port    int               `json:"port"`
	DataDir string            `json:"dataDir"`
	Config  map[string]string `json:"config"`
	Process *os.Process       `json:"-"`
	Pid     int               `json:"pid,omitempty"`
}

// Manager tracks and controls installed services.
type Manager struct {
	cfg      *config.Config
	services map[string]*Service
	mu       sync.RWMutex

	// discoverCache tracks when discoverInstalled was last run.
	// Security (L-4): Caching avoids redundant filesystem scans on every
	// List/Status/Start call. The cache expires after discoverTTL so that
	// newly installed services are picked up within a reasonable window.
	discoverLast time.Time
}

// discoverTTL is the time-to-live for the service discovery cache.
// After this duration, the next call to discoverInstalled will rescan.
const discoverTTL = 5 * time.Second

// NewManager creates a service Manager backed by the given config.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:      cfg,
		services: make(map[string]*Service),
	}
}

// discoverIfNeeded runs discoverInstalled only if the cache has expired.
// This avoids redundant filesystem scans on every List/Status/Start call.
func (m *Manager) discoverIfNeeded() {
	if time.Since(m.discoverLast) > discoverTTL {
		m.discoverInstalled()
	}
}

// discoverInstalled scans the install directory for services that
// have a .xdev-service marker file in their root.
func (m *Manager) discoverInstalled() {
	installDir := m.cfg.Core.InstallDir
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		serviceDir := filepath.Join(installDir, e.Name())
		marker := filepath.Join(serviceDir, ".xdev-service")
		data, err := os.ReadFile(marker)
		if err != nil {
			continue
		}
		svc := parseServiceMarker(e.Name(), serviceDir, data)
		if svc != nil {
			m.mu.Lock()
			if existing, ok := m.services[svc.Name]; ok {
				// Preserve running state if already tracked
				if existing.Status == "running" {
					svc.Status = "running"
					svc.Process = existing.Process
					svc.Pid = existing.Pid
				}
			}
			m.services[svc.Name] = svc
			m.mu.Unlock()
		}
	}
	m.discoverLast = time.Now()
}

// parseServiceMarker reads the .xdev-service marker and returns a Service.
// The marker format is key=value lines:
//
//	type=database
//	version=8.0.36
//	port=3306
//	exec=mysql/bin/mysqld
func parseServiceMarker(name, dir string, data []byte) *Service {
	svc := &Service{
		Name:    name,
		Status:  "stopped",
		Config:  make(map[string]string),
		DataDir: filepath.Join(dir, "data"),
	}
	var execRel string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "type":
			svc.Type = v
		case "version":
			svc.Version = v
		case "port":
			fmt.Sscanf(v, "%d", &svc.Port)
		case "exec":
			execRel = v
		case "datadir":
			if v != "" {
				svc.DataDir = v
			}
		default:
			svc.Config[k] = v
		}
	}
		if execRel != "" {
				execPath := filepath.Join(dir, execRel)
				// Security (H-2): Resolve symlinks before checking isSubPath.
				// Without this, a malicious archive could include a symlink at
				// the exec path that resolves to a binary outside the install
				// directory (e.g., a symlink mysql/bin/mysqld -> /usr/bin/powershell).
				if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
					execPath = resolved
				}
				// Validate that the exec path stays within the service install
				// directory to prevent path traversal attacks (e.g. exec=../../bin/malicious)
				if !isSubPath(dir, execPath) {
					fmt.Fprintf(os.Stderr, "⚠ service %q: exec path %q escapes install directory %q — rejected\n", name, execRel, dir)
					svc.Status = "invalid"
				} else {
					svc.Config["_exec"] = execPath
				}
			}
			// Validate datadir stays within the service install directory
			if svc.DataDir != "" && svc.DataDir != filepath.Join(dir, "data") {
				// Security (H-2): Resolve symlinks for datadir too
				dataDirResolved := svc.DataDir
				if resolved, err := filepath.EvalSymlinks(dataDirResolved); err == nil {
					dataDirResolved = resolved
				}
				if !isSubPath(dir, dataDirResolved) {
					fmt.Fprintf(os.Stderr, "⚠ service %q: datadir %q escapes install directory %q — reset to default\n", name, svc.DataDir, dir)
					svc.DataDir = filepath.Join(dir, "data")
				}
			}
		return svc
}

// List returns all discovered services.
func (m *Manager) List() []*Service {
	m.discoverIfNeeded()

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Service, 0, len(m.services))
	for _, s := range m.services {
		out = append(out, s)
	}
	return out
}

// Start launches the named service as a background process.
func (m *Manager) Start(name string) error {
	m.discoverIfNeeded()

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	if svc.Status == "running" {
		return fmt.Errorf("service %q is already running (pid %d)", name, svc.Pid)
	}

	execPath := svc.Config["_exec"]
	if execPath == "" {
		return fmt.Errorf("service %q has no executable configured", name)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(svc.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir %s: %w", svc.DataDir, err)
	}

	cmd := exec.Command(execPath)
	util.HideWindow(cmd)

	// Set working directory to the service install dir
	cmd.Dir = filepath.Dir(execPath)

	// Build environment
	cmd.Env = os.Environ()
	if svc.Port > 0 {
		cmd.Env = appendEnv(cmd.Env, svc.Name+"_PORT", fmt.Sprintf("%d", svc.Port))
	}
	cmd.Env = appendEnv(cmd.Env, svc.Name+"_DATA_DIR", svc.DataDir)
	for k, v := range svc.Config {
		if k == "_exec" {
			continue
		}
		cmd.Env = appendEnv(cmd.Env, k, v)
	}

	// Redirect stdout/stderr to log files
	logDir := filepath.Join(m.cfg.Core.XMDevDir, "logs", name)
	os.MkdirAll(logDir, 0o755)

	stdoutLog, err := util.OpenLogForAppend(filepath.Join(logDir, "stdout.log"), 50*1024*1024, 3) // 50 MiB max, 3 backups
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	stderrLog, err := util.OpenLogForAppend(filepath.Join(logDir, "stderr.log"), 50*1024*1024, 3) // 50 MiB max, 3 backups
	if err != nil {
		stdoutLog.Close()
		return fmt.Errorf("open stderr log: %w", err)
	}
	cmd.Stdout = stdoutLog
	cmd.Stderr = stderrLog

	// On Windows, CREATE_NEW_CONSOLE creates a new console for the service process.
	// Must use a build-tagged helper: syscall.SysProcAttr.CreationFlags is Windows-only.
	util.DetachConsole(cmd)

	if err := cmd.Start(); err != nil {
		stdoutLog.Close()
		stderrLog.Close()
		svc.Status = "error"
		return fmt.Errorf("start service %q: %w", name, err)
	}

	svc.Process = cmd.Process
	svc.Pid = cmd.Process.Pid
	svc.Status = "running"

	// Release the process so it continues after we exit
	go func() {
		cmd.Wait()
		stdoutLog.Close()
		stderrLog.Close()
		m.mu.Lock()
		if m.services[name] != nil && m.services[name].Pid == cmd.Process.Pid {
			m.services[name].Status = "stopped"
			m.services[name].Process = nil
			m.services[name].Pid = 0
		}
		m.mu.Unlock()
	}()

	return nil
}

// Stop terminates the named service.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	if svc.Status != "running" || svc.Process == nil {
		return fmt.Errorf("service %q is not running", name)
	}

	if err := svc.Process.Kill(); err != nil {
		return fmt.Errorf("kill service %q (pid %d): %w", name, svc.Pid, err)
	}

	svc.Status = "stopped"
	svc.Process = nil
	svc.Pid = 0
	return nil
}

// Status returns the current status of the named service.
func (m *Manager) Status(name string) (*Service, error) {
	m.discoverIfNeeded()

	// Use full Lock (not RLock) because this method conditionally mutates
	// svc.Status, svc.Process, and svc.Pid when a dead process is detected.
	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.services[name]
	if !ok {
		return nil, fmt.Errorf("service %q not found", name)
	}

	// Verify process is still alive
	if svc.Status == "running" && svc.Process != nil {
		// Check if the process is still running using signal 0 (null signal),
		// which checks process existence without actually delivering a signal.
		if err := svc.Process.Signal(syscall.Signal(0)); err != nil {
			// Process is dead
			svc.Status = "stopped"
			svc.Process = nil
			svc.Pid = 0
		}
	}

	return svc, nil
}

// ReadLogs reads and returns the content of the stdout log file for the
// named service.
func (m *Manager) ReadLogs(name string) (string, error) {
	logPath := filepath.Join(m.cfg.Core.XMDevDir, "logs", name, "stdout.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("read log %s: %w", logPath, err)
	}
	return string(data), nil
}

// LogPath returns the file system path to the stdout log file for the
// named service.
func (m *Manager) LogPath(name string) string {
	return filepath.Join(m.cfg.Core.XMDevDir, "logs", name, "stdout.log")
}

// Restart stops and then starts the named service.
func (m *Manager) Restart(name string) error {
	if err := m.Stop(name); err != nil {
		return err
	}
	return m.Start(name)
}

// IsRunning reports whether the named service is currently running.
func (m *Manager) IsRunning(name string) bool {
	svc, err := m.Status(name)
	if err != nil {
		return false
	}
	return svc.Status == "running"
}

// appendEnv adds or replaces an env var in the environment list.
func appendEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// isSubPath checks that child is within the parent directory.
// On Windows, the comparison is case-insensitive to match the filesystem.
func isSubPath(parent, child string) bool {
	absParent, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return false
	}
	absChild, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return false
	}
	// Use filepath.Rel to detect path traversal
	rel, err := filepath.Rel(absParent, absChild)
	if err != nil {
		return false
	}
	// If the relative path starts with "..", it escapes the parent
	if strings.HasPrefix(rel, "..") {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absChild, absParent) ||
			strings.HasPrefix(strings.ToLower(absChild), strings.ToLower(absParent)+string(os.PathSeparator))
	}
	return strings.HasPrefix(absChild, absParent+string(os.PathSeparator)) || absChild == absParent
}
