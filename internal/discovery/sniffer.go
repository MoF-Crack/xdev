// Package discovery implements automatic tool-version sniffing and
// discovery, with five built-in sniffers and four user-selectable
// trigger strategies (interval, startup, service, manual).
//
// # Sniffer Architecture
//
// The SnifferRegistry holds exactly five built-in sniffers (GitHub,
// JSON, HTML, npm, ArchivePattern) and exposes no public Register
// method (NFR-005/NFR-009). Matching follows a fixed priority:
//
//  1. Explicit remote: configuration → GitHub/JSON/HTML/npm sniffer
//     (whichever matches the tool's Remote.Type)
//  2. No remote but existing archives with GitHub URL pattern →
//     ArchivePatternSniffer
//  3. Otherwise → silently marked unsupported (no error)
//
// # Discovered Version Lifecycle
//
//   - Sniffers return []DiscoveredVersion with Source set to the
//     sniffer Name.  All discovered versions have Unverified: true
//     (FR-009 — the archive URL has not been verified regardless of
//     source).
//   - Versions are merged into the on-disk manifest YAML (only
//     VersionList is appended, existing fields are never overwritten).
//   - Archive entries use a zero SHA-256 placeholder ("0000...", 64
//     zeros) that reuses the existing skip-verification mechanism.
//   - Sniff results are cached in $XMDEV_HOME/cache/sniff-cache.json.
//
// # Thread Safety
//
// The Scheduler guards cache file operations with atomic writes
// (util.AtomicWriteFile).  The rate limiter (rateLimiter) uses a
// sync.Mutex.  The SnifferRegistry is immutable after construction
// and safe for concurrent reads.
package discovery

import (
	"context"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// DiscoveredVersion holds one version discovered by a Sniffer.
// Archives may be empty (for GitHub/JSON/HTML sniffers) or filled
// (for ArchivePatternSniffer when auto_archive_fill is true).
type DiscoveredVersion struct {
	Version     string             `json:"version"`
	ReleaseDate string             `json:"release_date"`
	Archives    []manifest.Archive `json:"archives"`
	Source      string             `json:"source"`
	Unverified  bool               `json:"unverified"`
}

// Sniffer discovers new versions of a tool from a remote source.
type Sniffer interface {
	// Name returns a human-readable identifier (e.g. "github").
	Name() string

	// Supports reports whether this sniffer can handle the tool.
	// Called by SnifferRegistry to determine the first matching
	// sniffer for a tool.
	Supports(t *manifest.Tool) bool

	// Sniff queries the remote source and returns discovered versions.
	// Returning an empty slice without error is valid (the tool has no
	// new versions).
	Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error)
}

// SnifferRegistry holds the ordered list of built-in sniffers.
// It is populated at construction time and exposes no public Register
// method (NFR-005 / NFR-009).
type SnifferRegistry struct {
	sniffers []Sniffer
}

// newSnifferRegistry creates the registry with exactly five built-in
// sniffers: GitHub, JSON, HTML, npm, ArchivePattern (in priority order).
func newSnifferRegistry() *SnifferRegistry {
	return &SnifferRegistry{
		sniffers: []Sniffer{
			&gitHubSniffer{},
			&jsonSniffer{},
			&htmlSniffer{},
			&npmSniffer{},
			&archivePatternSniffer{},
		},
	}
}

// Sniff finds the first Sniffer that Supports the tool and calls its
// Sniff method.  If no sniffer matches, the tool is silently marked
// unsupported (empty result, nil error).  Priority follows the
// registry order: explicit remote: configuration > ArchivePattern
// derivation > unsupported (FR-005).
func (r *SnifferRegistry) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	for _, s := range r.sniffers {
		if s.Supports(t) {
			return s.Sniff(ctx, t)
		}
	}
	// No snifter supports this tool — it is unsupported.  Return
	// empty without error so the caller can record it as such.
	return nil, nil
}

// SniffCache is the on-disk cache format stored at
// $XMDEV_HOME/cache/sniff-cache.json (FR-008).
type SniffCache struct {
	Tools       map[string]ToolSniffResult `json:"tools"`
	RateLimit   RateLimitState             `json:"rate_limit"`
	LastSniffAt time.Time                  `json:"last_sniff_at"`
	Partial     bool                       `json:"partial"`
	Stale       bool                       `json:"stale"` // true when remote failed and cache is from a previous run
}

// ToolSniffResult records the last sniff result for a single tool.
type ToolSniffResult struct {
	LastSniff   time.Time `json:"last_sniff"`
	Etag        string    `json:"etag"`
	Versions    []string  `json:"versions"`
	Unsupported bool      `json:"unsupported"`
	Error       string    `json:"error,omitempty"`
}

// RateLimitState holds the cross-process token bucket state.
type RateLimitState struct {
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
}
