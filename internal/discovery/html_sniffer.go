package discovery

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// htmlSniffer discovers new versions from an explicitly-configured
// HTML remote source (t.Remote.Type == "html").
type htmlSniffer struct{}

func (s *htmlSniffer) Name() string { return "html" }

func (s *htmlSniffer) Supports(t *manifest.Tool) bool {
	return t.Remote.Type == "html"
}

func (s *htmlSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	versions, err := remote.DiscoverVersions(ctx, t.Remote)
	if err != nil {
		return nil, fmt.Errorf("html sniffer: %w", err)
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
