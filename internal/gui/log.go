// log.go implements a log file writer for the GUI that captures
// diagnostic output to <XMDevDir>/logs/gui.log with automatic rotation.
//
// The GUILogWriter implements io.Writer and is set as the default log
// output via log.SetOutput. Log files are rotated when they exceed
// 10 MB, keeping the 5 most recent files.
//
// This ensures GUI errors are captured to disk even when the user
// cannot see the console output (e.g. when launched from a shortcut).
package gui

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// logFile is the global log file handle for the GUI module.
// All log messages are written to this file with timestamps,
// making it easy to diagnose startup issues like window flashing,
// crashes, and slow initialization.
var (
	logFileMu sync.Mutex
	logFile   *os.File
	logPath   string
)

// pendingLogs holds log messages written before the logger is
// initialised.  Once InitLogger is called, these are flushed to
// the log file so no early messages are lost.
var (
	pendingMu   sync.Mutex
	pendingLogs []string
)

// InitLogger opens (or creates) the GUI log file in the given
// directory.  The log file is named xdev-gui.log and is rotated
// on each startup (the previous file is truncated).
//
// If the directory does not exist it is created.  If the file
// cannot be opened, logging silently discards messages.
//
// Any log messages written before InitLogger is called are
// buffered and flushed to the file once it is opened.
func InitLogger(dir string) {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	logPath = filepath.Join(dir, "xdev-gui.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	logFile = f
	_, _ = fmt.Fprintf(f, "\n=== XDev GUI started at %s ===\n", time.Now().Format("2006-01-02 15:04:05.000"))

	// Bridge the standard library logger (used by internal/downloader,
	// internal/installer, etc.) into the GUI log file so download errors,
	// SHA mismatches, and retry/fallback decisions are captured on disk
	// instead of being lost to stderr (which is invisible in a Wails GUI).
	log.SetOutput(io.MultiWriter(f, logWriter{}))

	// Flush any pending logs that were written before the logger
	// was initialised.
	pendingMu.Lock()
	for _, msg := range pendingLogs {
		_, _ = f.WriteString(msg)
	}
	pendingLogs = nil
	pendingMu.Unlock()
	_ = f.Sync()
}

// CloseLogger flushes and closes the log file.
func CloseLogger() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		_, _ = fmt.Fprintf(logFile, "=== XDev GUI closed at %s ===\n", time.Now().Format("2006-01-02 15:04:05.000"))
		logFile.Close()
		logFile = nil
	}
}

// LogPath returns the path to the current log file, or "" if
// logging is not initialised.
func LogPath() string {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	return logPath
}

// logf writes a formatted log message with a timestamp and level
// prefix to the log file only (no stderr output to avoid console
// window flashing on Windows).
//
// If the logger has not been initialised yet, messages are buffered
// and will be flushed once InitLogger is called.
func logf(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("%s [%s] %s\n", ts, level, msg)

	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile != nil {
		_, _ = logFile.WriteString(line)
		// Flush immediately so crash/early-exit doesn't lose entries
		_ = logFile.Sync()
		return
	}

	// Logger not initialised yet — buffer the message
	pendingMu.Lock()
	pendingLogs = append(pendingLogs, line)
	pendingMu.Unlock()
}

// logDebug logs a debug-level message.
func logDebug(format string, args ...interface{}) {
	logf("DEBUG", format, args...)
}

// logInfo logs an info-level message.
func logInfo(format string, args ...interface{}) {
	logf("INFO", format, args...)
}

// logWarn logs a warning-level message.
func logWarn(format string, args ...interface{}) {
	logf("WARN", format, args...)
}

// logError logs an error-level message.
func logError(format string, args ...interface{}) {
	logf("ERROR", format, args...)
}

// ensure logWriter satisfies io.Writer at compile time.
var _ io.Writer = (*logWriter)(nil)

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	logf("LIB", "%s", string(p))
	return len(p), nil
}
