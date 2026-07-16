// Package java provides the Java plugin, which resolves archives from
// pinned Adoptium Temurin assets declared in the manifest.
//
// Unlike the previous Oracle-template approach that dynamically
// generated download URLs (and bypassed SHA-256 verification for
// versions without pinned hashes), this implementation reads fixed
// archives directly from manifests/java.yaml. Every archive entry
// carries a real SHA-256, so the installer always verifies downloads
// against a trusted hash before extraction.
//
// A version whose archives list is empty (a placeholder) returns an
// error rather than synthesising a URL — this is a deliberate security
// boundary: no pinned SHA, no download.
package java

import (
	"fmt"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// Plugin is the Java tool plugin. It embeds *plugin.Generic for all
// default behaviour and overrides ArchiveFor / ArchiveForPlatform to
// return pinned Temurin archives from the manifest.
type Plugin struct {
	*plugin.Generic
}

// New creates a Java plugin backed by the given manifest tool.
func New(m *manifest.Tool) *Plugin {
	return &Plugin{Generic: plugin.NewGeneric(m)}
}

// ArchiveFor resolves the archive for the host platform, satisfying
// the plugin.Plugin interface. It delegates to ArchiveForPlatform
// with the host's OS/Arch.
func (p *Plugin) ArchiveFor(version string) (*manifest.Archive, error) {
	return p.ArchiveForPlatform(version, plugin.HostOS(), plugin.HostArch())
}

// ArchiveForPlatform looks up a pinned Temurin archive for the given
// version and platform from the manifest. It returns an error if:
//   - the version is not in the manifest
//   - the version has empty archives (placeholder — no pinned SHA)
//   - no archive matches the requested os/arch
func (p *Plugin) ArchiveForPlatform(version, osName, arch string) (*manifest.Archive, error) {
	v, ok := p.M.FindVersion(version)
	if !ok {
		return nil, fmt.Errorf("version %s not in manifest", version)
	}

	if len(v.Archives) == 0 {
		return nil, fmt.Errorf("placeholder version %s has no pinned archives — refusing to download without SHA-256", version)
	}

	a, ok := v.ArchiveForCurrent(osName, arch)
	if !ok {
		return nil, fmt.Errorf("no archive for %s/%s in version %s", osName, arch, version)
	}

	return a, nil
}

// Ensure *Plugin satisfies plugin.Plugin at compile time.
var _ plugin.Plugin = (*Plugin)(nil)
