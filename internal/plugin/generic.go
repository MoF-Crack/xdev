// generic.go provides the Generic plugin type and shared helpers for
// manifest-driven tools.
//
// The Generic type is a Plugin implementation whose behavior is entirely
// driven by a manifest.Tool YAML struct. Most tools (python, node, go,
// rust, etc.) don't need custom install logic and can simply use Generic
// or embed it and override individual methods.
//
// # Usage
//
// Simple tools (no custom logic):
//
//	plugin.Register(plugin.NewGeneric(manifestTool))
//
// Tools with custom ArchiveFor (e.g. Java with dynamic URL generation):
//
//	type JavaPlugin struct { *plugin.Generic }
//	func (j *JavaPlugin) ArchiveFor(version string) (*manifest.Archive, error) { ... }
//
// # Version Resolution
//
// Generic.ResolveVersion delegates to the resolver package, which supports:
//   - Exact: "3.12.0"
//   - Latest: "latest"
//   - Caret: "^3.12" (compatible, >=3.12.0 <4.0.0)
//   - Tilde: "~3.12" (patch-level, >=3.12.0 <3.13.0)
//   - Prefix: "3.12" (matches highest 3.12.x)
//   - Range: ">=1.20 <2.0"
package plugin

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/resolver"
	"github.com/xmdemt/xdev/internal/util"
)

// HostOS returns the canonical OS name used in manifests
// (windows / darwin / linux).
func HostOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	}
	return runtime.GOOS
}

// HostArch returns the canonical CPU arch used in manifests.
// Manifests use x86_64 on Linux/macOS and amd64 on Windows.
func HostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			return "x86_64"
		}
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	}
	return runtime.GOARCH
}

// Generic is a Plugin implementation driven entirely by a manifest.Tool.
// Every M2 plugin that does not need custom Install/PostInstall logic
// can simply embed a *Generic and override individual methods when
// necessary.
type Generic struct {
	M *manifest.Tool
}

// NewGeneric wraps a manifest.Tool in a Plugin.
func NewGeneric(m *manifest.Tool) *Generic {
	return &Generic{M: m}
}

func (g *Generic) Name() string             { return g.M.Name }
func (g *Generic) DisplayName() string      { return g.M.DisplayName }
func (g *Generic) Description() string      { return g.M.Description }
func (g *Generic) Homepage() string         { return g.M.Homepage }
func (g *Generic) Category() string         { return g.M.Category }
func (g *Generic) Manifest() *manifest.Tool { return g.M }
func (g *Generic) Shims() []string          { return append([]string{}, g.M.Shims...) }
func (g *Generic) DefaultVersion() string   { return g.M.DefaultVersion() }

// ResolveVersion translates a user spec ("latest", "3.12", "^3.11")
// into a concrete version string that exists in the manifest.
func (g *Generic) ResolveVersion(spec string) (string, error) {
	parsed, err := resolver.Parse(spec)
	if err != nil {
		return "", err
	}
	available := g.M.Versions()
	return resolver.Resolve(parsed, available, g.M.Latest, g.M.LTS)
}

// ArchiveFor picks the right Archive for the current OS/Arch.
func (g *Generic) ArchiveFor(version string) (*manifest.Archive, error) {
	v, ok := g.M.FindVersion(version)
	if !ok {
		return nil, fmt.Errorf("version %s not in manifest", version)
	}
	a, ok := v.ArchiveForCurrent(HostOS(), HostArch())
	if !ok {
		return nil, fmt.Errorf("no archive for %s/%s in %s", HostOS(), HostArch(), version)
	}
	return a, nil
}

// InstallDir returns the canonical per-tool install directory:
//
//	<install_dir>/<tool>/<version>
//
// Security: The manifest's Name, InstallRoot, and the version string
// are validated with util.SafeName and the final path is checked with
// util.ValidateSubPath to prevent path traversal attacks (C-1).
func (g *Generic) InstallDir(installRoot, version string) string {
	base := g.M.Name

	// Validate tool name is a safe path component
	if _, err := util.SafeName(g.M.Name); err != nil {
		log.Printf("[plugin] CRITICAL: tool name %q is not a safe path component: %v — using sanitized fallback", g.M.Name, err)
		base = "unsafe-name-blocked"
	}

	if g.M.InstallRoot != "" {
		// Validate install_root doesn't escape the install directory
		candidate := filepath.Join(installRoot, g.M.InstallRoot, g.M.Name)
		if err := util.ValidateSubPath(installRoot, candidate); err != nil {
			log.Printf("[plugin] CRITICAL: install_root %q escapes install dir %q: %v — ignoring install_root", g.M.InstallRoot, installRoot, err)
		} else {
			base = filepath.Join(g.M.InstallRoot, g.M.Name)
		}
	}

	// Validate version is a safe path component
	if _, err := util.SafeName(version); err != nil {
		log.Printf("[plugin] CRITICAL: version %q is not a safe path component: %v — using sanitized fallback", version, err)
		version = "unsafe-version-blocked"
	}

	result := filepath.Join(installRoot, base, version)

	// Final safety check: the resolved path must be under installRoot
	if err := util.ValidateSubPath(installRoot, result); err != nil {
		log.Printf("[plugin] CRITICAL: InstallDir %q escapes install root %q — using safe fallback", result, installRoot)
		return filepath.Join(installRoot, base, "unsafe-path-blocked")
	}

	return result
}

// EnvVars interpolates {{install}} placeholders in the manifest's
// env_vars map.
func (g *Generic) EnvVars(installPath string) map[string]string {
	out := map[string]string{}
	for k, v := range g.M.EnvVars {
		out[k] = strings.ReplaceAll(v, "{{install}}", installPath)
	}
	return out
}

// Ensure *Generic satisfies the Plugin interface at compile time.
var _ Plugin = (*Generic)(nil)

// PostInstall is a no-op for Generic plugins.  Tools that need
// post-install setup (e.g. npm bootstrap) should override this.
func (g *Generic) PostInstall(_ context.Context, _ string) error { return nil }

// Deactivate is a no-op for Generic plugins.  Tools that need
// cleanup on deactivation should override this.
func (g *Generic) Deactivate(_ string) error { return nil }

// RequiredPlugin returns the plugin names that this tool depends on,
// as declared in the manifest's required_plugins field.
func (g *Generic) RequiredPlugin() []string {
	return g.M.RequiredPlugin
}
