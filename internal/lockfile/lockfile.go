// Package lockfile manages .xdev.lock files that pin exact tool versions,
// checksums, and download URLs for reproducible environments.
//
// # File Format
//
// Lockfiles use TOML format (via github.com/pelletier/go-toml/v2) and
// are named .xdev.lock. They are placed in the project root directory
// alongside .xdev.toml.
//
// Example:
//
//	version = 1
//	created_at = 2026-07-04T10:30:00Z
//
//	[tools.go]
//	version = "1.22"
//	resolved = "1.22.5"
//	sha256 = "abc123..."
//	url = "https://go.dev/dl/go1.22.5.windows-amd64.zip"
//	install_path = "E:\\XDev\\install\\go\\1.22.5"
//
// # Thread Safety
//
// AddTool and RemoveTool are protected by a sync.Mutex so concurrent
// goroutines can safely update the lockfile. Save uses atomic write
// (temp file + rename) to prevent corruption from crashes.
//
// # Reproducible Installs
//
// `xdev install --locked` reads the lockfile and installs exactly the
// pinned versions, ignoring the manifest's latest version. This ensures
// that every member of a team gets the same toolchain.
//
// # Project Discovery
//
// FindConfigDir walks upward from a starting directory looking for
// .xdev.toml or .xdev.lock, stopping at the first directory containing
// either. This enables the cd hook to locate the project root.
package lockfile

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/xmdemt/xdev/internal/util"
)

const FileName = ".xdev.lock"

// Lockfile represents the contents of a .xdev.lock file.
type Lockfile struct {
	Version        int                   `toml:"version"`
	CreatedAt      time.Time             `toml:"created_at"`
	Tools          map[string]LockedTool `toml:"tools"`
	ConfirmedTasks map[string]string      `toml:"confirmed_tasks,omitempty"` // Security (C-7): task hash → confirmed_at
	HMAC           string                `toml:"hmac,omitempty"`            // Security (H-13): integrity check
	mu             sync.Mutex            `toml:"-"`                        // protects Tools map mutations
}

// LockedTool holds the pinned information for a single tool.
type LockedTool struct {
	Version     string `toml:"version"`
	Resolved    string `toml:"resolved"`
	SHA256      string `toml:"sha256"`
	URL         string `toml:"url"`
	InstallPath string `toml:"install_path"`
}

// Load reads a .xdev.lock file from the given directory.
// Returns nil if the file does not exist.
// Security (H-13): Verifies the HMAC integrity check if present.
// A mismatch indicates tampering with sha256/url fields and returns an error.
// Security (M-5): Acquires a shared file lock during read to prevent
// concurrent writers from corrupting the file while it is being read.
func Load(dir string) (*Lockfile, error) {
	path := filepath.Join(dir, FileName)

	// Security (M-5): Acquire shared lock for reading
	lockPath := path + ".lock"
	fl, err := util.AcquireSharedFileLock(lockPath, 0)
	if err != nil {
		log.Printf("[lockfile] WARNING: could not acquire shared lock on %s: %v — proceeding without lock", lockPath, err)
		fl = nil
	} else {
			defer fl.Release()
		}

	// Security (L-2): Clean up stale .tmp files from crashed atomic writes
	if matches, err := filepath.Glob(filepath.Join(dir, "*.tmp")); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lf := &Lockfile{}
	if err := toml.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lf.Tools == nil {
		lf.Tools = map[string]LockedTool{}
	}
	if lf.ConfirmedTasks == nil {
		lf.ConfirmedTasks = map[string]string{}
	}

	// Security (H-13): Verify HMAC if present
	if lf.HMAC != "" {
		expectedHMAC := lf.computeHMAC()
		if expectedHMAC != lf.HMAC {
			return nil, fmt.Errorf("lockfile integrity check failed: HMAC mismatch — the file may have been tampered with (sha256/url fields are security-sensitive)")
		}
		log.Printf("[lockfile] Integrity check passed for %s", path)
	}

	return lf, nil
}

// Save writes the lockfile to the given directory atomically (temp + rename).
// Security (H-13): Computes and stores an HMAC of the Tools map before saving
// to detect tampering with sha256/url fields on subsequent loads.
// Security (M-5): Acquires an exclusive file lock during write to prevent
// concurrent writers from losing each other's updates.
func Save(dir string, lf *Lockfile) error {
	// Compute HMAC before marshaling
	lf.HMAC = lf.computeHMAC()

	path := filepath.Join(dir, FileName)

	// Security (M-5): Acquire exclusive lock for writing
	lockPath := path + ".lock"
	fl, err := util.AcquireFileLock(lockPath, 30*time.Second)
	if err != nil {
		log.Printf("[lockfile] WARNING: could not acquire exclusive lock on %s: %v — proceeding without lock", lockPath, err)
	} else {
		defer fl.Release()
	}
	data, err := toml.Marshal(lf)
	if err != nil {
		return err
	}
	if err := util.AtomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}

// New creates a new empty lockfile.
func New() *Lockfile {
	return &Lockfile{
		Version:        1,
		CreatedAt:      time.Now().UTC(),
		Tools:          map[string]LockedTool{},
		ConfirmedTasks: map[string]string{},
	}
}

// AddTool adds or updates a tool entry in the lockfile.
func (lf *Lockfile) AddTool(name string, tool LockedTool) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	lf.Tools[name] = tool
}

// RemoveTool removes a tool from the lockfile.
func (lf *Lockfile) RemoveTool(name string) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	delete(lf.Tools, name)
}

// FindConfigDir searches from dir upward for a directory containing
// .xdev.toml or .xdev.lock, returning the directory path.
// Returns "" if none found.
func FindConfigDir(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".xdev.toml")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// IsTaskConfirmed checks whether a task with the given command hash has
// been previously confirmed by the user. Security (C-7): This prevents
// arbitrary command execution from untrusted .xdev.toml files without
// user confirmation.
func (lf *Lockfile) IsTaskConfirmed(taskName, commandHash string) bool {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.ConfirmedTasks == nil {
		return false
	}
	key := taskName + ":" + commandHash
	_, ok := lf.ConfirmedTasks[key]
	return ok
}

// ConfirmTask records that the user has confirmed execution of a task
// with the given command hash. The value is the confirmation timestamp.
func (lf *Lockfile) ConfirmTask(taskName, commandHash string) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.ConfirmedTasks == nil {
		lf.ConfirmedTasks = map[string]string{}
	}
	key := taskName + ":" + commandHash
	lf.ConfirmedTasks[key] = time.Now().UTC().Format(time.RFC3339)
}

// computeHMAC computes an HMAC-SHA256 over the security-sensitive fields
// of the lockfile (the Tools map). This detects tampering with sha256
// and url fields that could redirect `xdev install --locked` to a
// malicious source. Uses the machine-specific HMAC key from util.
func (lf *Lockfile) computeHMAC() string {
	if lf.Tools == nil {
		return ""
	}
	// Serialize the Tools map to JSON for deterministic ordering
	data, err := json.Marshal(lf.Tools)
	if err != nil {
		return ""
	}
	return util.ComputeHMAC(data)
}
