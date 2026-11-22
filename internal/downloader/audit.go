// Package downloader records one final event for each logical archive request.
package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

type AuditEntry struct {
	Timestamp  time.Time `json:"ts"`
	Tool       string    `json:"tool,omitempty"`
	Version    string    `json:"ver,omitempty"`
	URL        string    `json:"url"`
	SHA256     string    `json:"sha256,omitempty"`
	Source     string    `json:"source"`
	Verified   bool      `json:"verified"`
	Error      string    `json:"error,omitempty"`
	ErrorClass string    `json:"errorClass,omitempty"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Size       int64     `json:"size"`
	DurationMS int64     `json:"durationMs"`
	Fallback   bool      `json:"fallback,omitempty"`
	Direct     bool      `json:"direct,omitempty"`
}

// AuditRequest owns one logical request (including cache, retries and fallbacks).
// An installer may own it and pass it through Options.Audit; Download then only
// updates its result. Finish is idempotent, even across concurrent callers.
type AuditRequest struct {
	mu       sync.Mutex
	entry    AuditEntry
	started  time.Time
	finished bool
}

func NewAuditRequest(entry AuditEntry) *AuditRequest {
	return &AuditRequest{entry: entry, started: time.Now()}
}

// Update sets outcome details; missing identity fields preserve the request's
// original identity. Use this on an accepted cache hit before Finish(nil).
func (a *AuditRequest) Update(entry AuditEntry) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return
	}
	if entry.Tool != "" {
		a.entry.Tool = entry.Tool
	}
	if entry.Version != "" {
		a.entry.Version = entry.Version
	}
	if entry.URL != "" {
		a.entry.URL = entry.URL
	}
	if entry.Source != "" {
		a.entry.Source = entry.Source
	}
	if entry.SHA256 != "" {
		a.entry.SHA256 = entry.SHA256
	}
	a.entry.Verified = entry.Verified
	a.entry.Size = entry.Size
	a.entry.Fallback = entry.Fallback
	a.entry.Direct = entry.Direct
}
func (a *AuditRequest) setClass(class string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.entry.ErrorClass = class
	}
}
func (a *AuditRequest) attempt(raw string, direct bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return
	}
	a.entry.URL = raw
	a.entry.Source = "download"
	a.entry.Size = 0
	a.entry.SHA256 = ""
	a.entry.Verified = false
	a.entry.ErrorClass = ""
	a.entry.Direct = direct
}
func (a *AuditRequest) progress(size int64, resume bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.entry.Size = size
		if resume {
			a.entry.Source = "resume"
		}
	}
}
func (a *AuditRequest) success(hash string, verified bool, size int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.entry.SHA256 = hash
		a.entry.Verified = verified
		a.entry.Size = size
		a.entry.ErrorClass = ""
	}
}
func (a *AuditRequest) markFallback() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.entry.Fallback = true
	}
}

func (a *AuditRequest) Finish(err error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.finished {
		a.mu.Unlock()
		return
	}
	a.finished = true
	entry := a.entry
	entry.Event = "final"
	entry.DurationMS = time.Since(a.started).Milliseconds()
	entry.Status = "success"
	if entry.Source == "" {
		entry.Source = "download"
	}
	if err != nil {
		entry.Status = "failed"
		entry.Verified = false
		if errors.Is(err, context.Canceled) {
			entry.ErrorClass = "cancelled"
			entry.Status = "cancelled"
		} else if errors.Is(err, context.DeadlineExceeded) {
			entry.ErrorClass = "timeout"
		} else if entry.ErrorClass == "" {
			entry.ErrorClass = classifyAuditError(err)
		}
		entry.Error = auditErrorSummary(entry.ErrorClass)
	} else {
		entry.Error = ""
		entry.ErrorClass = ""
	}
	a.mu.Unlock()
	LogAudit(entry)
}

func classifyAuditError(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "signature"):
		return "signature"
	case strings.Contains(s, "sha256") || strings.Contains(s, "sha-256") || strings.Contains(s, "cached hash"):
		return "checksum"
	case strings.Contains(s, "http "):
		return "http"
	case strings.Contains(s, "another download"):
		return "busy"
	case strings.Contains(s, "empty url") || strings.Contains(s, "empty destination") || strings.Contains(s, "invalid") || strings.Contains(s, "must use https") || strings.Contains(s, "private network"):
		return "invalid_request"
	default:
		return "download"
	}
}

// Errors may include credentials in arbitrary transport text, ETags or file
// names. Persist only a diagnostic selected from a fixed local vocabulary.
func auditErrorSummary(class string) string {
	switch class {
	case "signature":
		return "Manifest signature verification failed"
	case "checksum":
		return "Archive checksum verification failed"
	case "tofu":
		return "Archive differs from the saved checksum"
	case "http":
		return "Server returned an unsuccessful HTTP status"
	case "busy":
		return "Partial download is already locked"
	case "cancelled":
		return "Request was cancelled"
	case "timeout":
		return "Request timed out"
	case "invalid_request":
		return "Invalid download request"
	case "network":
		return "Network transfer failed"
	case "rename":
		return "Cannot publish the downloaded archive"
	case "filesystem":
		return "Cannot access the local download files"
	default:
		return "Download failed"
	}
}

// Only the origin is safe to persist: signed path components, userinfo,
// query parameters, fragments and non-hierarchical URLs can carry secrets.
func auditOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "invalid URL"
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

var (
	auditMu      sync.Mutex
	auditLogPath string
)

func SetAuditLogPath(path string) { auditMu.Lock(); defer auditMu.Unlock(); auditLogPath = path }

// LogAudit is retained for existing producers. URL AND free-text errors are
// scrubbed here so callers cannot accidentally bypass final-event redaction.
// Append/open/write/close failures are diagnosed but never abort a download.
func LogAudit(entry AuditEntry) {
	auditMu.Lock()
	defer auditMu.Unlock()
	if auditLogPath == "" {
		return
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Event == "" {
		entry.Event = "final"
	}
	if entry.Status == "" {
		entry.Status = "success"
		if entry.Error != "" {
			entry.Status = "failed"
		}
	}
	entry.URL = auditOrigin(entry.URL)
	if entry.Error != "" {
		if entry.ErrorClass == "" {
			entry.ErrorClass = classifyAuditError(errors.New(entry.Error))
		}
		entry.Error = auditErrorSummary(entry.ErrorClass)
	}
	entry.Tool = sanitizeAuditText(entry.Tool)
	entry.Version = sanitizeAuditText(entry.Version)
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[audit] ERROR: failed to marshal audit entry: %s", sanitizedReason(err))
		return
	}
	f, err := util.OpenLogForAppend(auditLogPath, 50*1024*1024, 3)
	if err != nil {
		log.Printf("[audit] ERROR: failed to open audit log: %s", sanitizedReason(err))
		return
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		log.Printf("[audit] ERROR: failed to write audit entry: %s", sanitizedReason(err))
	}
	if err := f.Close(); err != nil {
		log.Printf("[audit] ERROR: failed to close audit log: %s", sanitizedReason(err))
	}
}

var auditSecretPattern = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|password|token|api[_-]?key)\s*[:=]\s*[^\r\n]*`)

func sanitizeAuditText(text string) string {
	if text == "" {
		return ""
	}
	text = urlInTextPattern.ReplaceAllStringFunc(text, sanitizedURL)
	text = redactProxyCredentials(text)
	text = auditSecretPattern.ReplaceAllString(text, "$1=<redacted>")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, text)
}
