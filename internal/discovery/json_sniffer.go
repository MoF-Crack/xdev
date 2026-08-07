package discovery

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// jsonSniffer discovers new versions from an explicitly-configured
// JSON remote source (t.Remote.Type == "json").
type jsonSniffer struct{}

func (s *jsonSniffer) Name() string { return "json" }

func (s *jsonSniffer) Supports(t *manifest.Tool) bool {
	return t.Remote.Type == "json"
}

func (s *jsonSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	versions, err := remote.DiscoverVersions(ctx, t.Remote)
	if err != nil {
		return nil, fmt.Errorf("json sniffer: %w", err)
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
