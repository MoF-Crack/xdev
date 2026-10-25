package discovery

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// jsonSniffer discovers new versions from an explicitly-configured
// JSON remote source (t.Remote.Type == "json").
//
// 审查（Go 1.27 嗅探失败）：同时支持 `golang` 类型 —— 它同样是「manifest
// 显式配置的远程 JSON 索引」，区别只在于响应形状（go.dev 返回顶层数组）。
// 两者都委托 remote.DiscoverVersions 按 Type 分派，无需第二套 sniffer。
type jsonSniffer struct{}

func (s *jsonSniffer) Name() string { return "json" }

func (s *jsonSniffer) Supports(t *manifest.Tool) bool {
	return t.Remote.Type == "json" || t.Remote.Type == "golang"
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
