package discovery

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// npmSniffer discovers new versions from an explicitly-configured
// npm remote source (t.Remote.Type == "npm"). It mirrors the
// json/html sniffers: delegation to remote.DiscoverVersions with the
// tool's RemoteSource config.
type npmSniffer struct{}

func (s *npmSniffer) Name() string { return "npm" }

func (s *npmSniffer) Supports(t *manifest.Tool) bool {
	return t.Remote.Type == "npm"
}

func (s *npmSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	versions, err := remote.DiscoverVersions(ctx, t.Remote)
	if err != nil {
		return nil, fmt.Errorf("npm sniffer: %w", err)
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
