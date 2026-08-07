package discovery

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// gitHubSniffer discovers new versions from an explicitly-configured
// GitHub remote source (t.Remote.Type == "github").  Results are
// marked Unverified: true per FR-009 — all discovered versions from
// sniffing are unverified, regardless of source.
type gitHubSniffer struct{}

func (s *gitHubSniffer) Name() string { return "github" }

// Supports returns true when the tool has an explicit GitHub remote
// configuration.
func (s *gitHubSniffer) Supports(t *manifest.Tool) bool {
	return t.Remote.Type == "github"
}

// Sniff delegates to remote.DiscoverVersions and wraps the version
// strings into []DiscoveredVersion.  Archives stay empty — the
// caller (SniffNow) may call FillArchivePlaceholders if configured.
//
// ETag / If-None-Match is handled internally by the remote package's
// TTL cache.  For cross-process ETag persistence, the Scheduler uses
// a TTL-based skip (LastSniff + sniff.interval) instead (SC-007).
func (s *gitHubSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	versions, err := remote.DiscoverVersions(ctx, t.Remote)
	if err != nil {
		return nil, fmt.Errorf("github sniffer: %w", err)
	}

	out := make([]DiscoveredVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, DiscoveredVersion{
			Version:    v,
			Source:     s.Name(),
			Unverified: true, // FR-009: all discovered versions unverified
		})
	}
	return out, nil
}
