package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// RotateLog checks if the log file at path exceeds maxSize bytes.
// If so, it renames the current file to path.1, path.1 to path.2, etc.,
// keeping at most maxBackups old files. The oldest backup is deleted.
// Returns nil if no rotation was needed or rotation succeeded.
func RotateLog(path string, maxSize int64, maxBackups int) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file doesn't exist, nothing to rotate
		}
		return fmt.Errorf("stat log file: %w", err)
	}
	if fi.Size() <= maxSize {
		return nil // no rotation needed
	}

	// Delete the oldest backup if it exists
	oldest := fmt.Sprintf("%s.%d", path, maxBackups)
	os.Remove(oldest)

	// Shift backups: .N-1 -> .N, .N-2 -> .N-1, ..., .1 -> .2
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		os.Rename(src, dst)
	}

	// Rename current file to .1
	if err := os.Rename(path, path+".1"); err != nil {
		return fmt.Errorf("rotate log: %w", err)
	}

	return nil
}

// OpenLogForAppend opens a log file for append, rotating if needed.
// This replaces the common pattern of os.OpenFile with O_APPEND.
func OpenLogForAppend(path string, maxSize int64, maxBackups int) (*os.File, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	// Rotate if needed before opening
	if err := RotateLog(path, maxSize, maxBackups); err != nil {
		// Log rotation failed — continue anyway, just open the file
	}

	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
