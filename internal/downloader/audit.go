// Package downloader — audit logging for download operations.
//
// Every download (whether from cache or network) is logged to a JSONL
// file for post-incident analysis and SIEM integration.
package downloader

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// AuditEntry records a single download event.
type AuditEntry struct {
	Timestamp time.Time `json:"ts"`
	Tool      string    `json:"tool,omitempty"`
	Version   string    `json:"ver,omitempty"`
	URL       string    `json:"url"`
	SHA256    string    `json:"sha256,omitempty"`
	Source    string    `json:"source"` // "download" | "cache" | "resume"
	Verified  bool      `json:"verified"`
	Error     string    `json:"error,omitempty"`
	Size      int64     `json:"size,omitempty"`
}

var (
	auditMu      sync.Mutex
	auditLogPath string
)

// SetAuditLogPath sets the path for the audit log file.
// If the path is empty, audit logging is disabled.
func SetAuditLogPath(path string) {
	auditMu.Lock()
	defer auditMu.Unlock()
	auditLogPath = path
}

// LogAudit writes an audit entry to the JSONL log file.
// Errors during logging are silently ignored to avoid disrupting downloads.
func LogAudit(entry AuditEntry) {
	auditMu.Lock()
	defer auditMu.Unlock()

	if auditLogPath == "" {
		return
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[audit] ERROR: failed to marshal audit entry: %v", err)
		return
	}

	f, err := util.OpenLogForAppend(auditLogPath, 50*1024*1024, 3) // 50 MiB max, 3 backups
	if err != nil {
		log.Printf("[audit] ERROR: failed to open audit log: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		log.Printf("[audit] ERROR: failed to write audit entry: %v", err)
	}
}
