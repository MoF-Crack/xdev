// Package plugin defines the contract between the core engine and
// every supported tool, along with a registry for plugin lookup.
//
// # Plugin Interface
//
// A plugin is any object that satisfies the Plugin interface. Each plugin
// knows the shape of its archives, how to enumerate versions, what shims
// to create, and what env vars to set on activation. The core engine
// (installer package) handles the actual downloading, extracting, and
// shimming — plugins only provide metadata and tool-specific logic.
//
// # Plugin Implementations
//
// There are four concrete plugin types:
//   - GenericPlugin: behavior fully driven by the YAML manifest; no custom logic
//   - JavaPlugin: embeds Generic but overrides ArchiveFor for dynamic URL generation
//   - LuaPlugin: behavior defined by Lua functions (available, archive, env_vars)
//   - VfoxPlugin: compatibility layer for version-fox Lua plugin format
//
// # Registration
//
// All built-in plugins are registered in RegisterAll (registry.go), which
// is called at startup from both CLI and GUI entry points. User-installed
// Lua plugins from $XMDEV_HOME/plugins/ are also loaded during registration.
//
// # Registry
//
// The Registry type provides thread-safe lookup by tool name, enumeration
// of all plugins, and case-insensitive search. It is the primary way the
// CLI and GUI layers access plugin instances.
package plugin

import (
	"context"
	"sort"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
)

// Plugin is the contract every supported tool implements.
//
// The contract is intentionally high-level: each Plugin knows the
// shape of its archives, how to enumerate versions, what shims to
// create, and what env vars to set on activation. The core engine
// (installer package) does the actual downloading, extracting,
// installing and shimming.
//
// Implementations:
//   - GenericPlugin: YAML-driven, no custom logic (most tools)
//   - JavaPlugin: custom ArchiveFor for Oracle/Adoptium URLs
//   - LuaPlugin: behavior defined by Lua functions
//   - VfoxPlugin: version-fox Lua format compatibility
type Plugin interface {
	// Metadata methods provide display and categorization information.

	// Name returns the canonical tool identifier (e.g. "go", "python").
	// This is used as the directory name under install/ and current/.
	Name() string

	// DisplayName returns the human-readable name (e.g. "Go", "Python").
	DisplayName() string

	// Description returns a one-line summary of the tool.
	Description() string

	// Homepage returns the tool's website URL.
	Homepage() string

	// Category returns the tool classification for grouping in the UI.
	// Common values: "language", "mobile", "build", "system".
	Category() string

	// Manifest returns the tool's YAML manifest data (read-only).
	// The manifest contains version lists, archive URLs, and metadata.
	Manifest() *manifest.Tool

	// ResolveVersion turns a user spec ("latest", "3.12", "^3.11",
	// ">=1.20") into a concrete version string that exists in the
	// manifest. Returns an error if no matching version is found.
	ResolveVersion(spec string) (string, error)

	// DefaultVersion is the version that gets activated on a fresh
	// `xdev install <tool>` (no @version specified).
	DefaultVersion() string

	// ArchiveFor picks the right Archive for the current OS/Arch
	// from the manifest's version list. Returns an error if no
	// archive matches the current platform.
	ArchiveFor(version string) (*manifest.Archive, error)

	// InstallDir returns the absolute path where this tool's
	// installation should live, e.g. "E:/XDev/install/go/1.22.0".
	InstallDir(root, version string) string

	// Shims lists the binary names that need a shim entry in the
	// shims/ directory. For example, Go returns ["go", "gofmt", "godoc"].
	Shims() []string

	// EnvVars returns the env-var map that should be exported when
	// this tool is active. Values may contain "{{install}}" template
	// variables that are replaced at activation time.
	// Example: {"GOROOT": "{{install}}", "GOPATH": "{{install}}\\..\\gopath"}
	EnvVars(installPath string) map[string]string

	// PostInstall is called after a tool is extracted but before
	// activation. It can perform setup tasks like npm install,
	// pip bootstrap, etc. Return nil for a no-op.
	PostInstall(ctx context.Context, installPath string) error

	// Deactivate is called when a tool version is being switched
	// away from or uninstalled. It can perform cleanup tasks.
	// Return nil for a no-op.
	Deactivate(installPath string) error

	// RequiredPlugin returns the names of built-in plugin names that
	// this plugin depends on. For example, "flutter" depends on
	// "android-sdk-tools" and "java". Returns nil/empty when there are
	// no requirements.
	RequiredPlugin() []string
}

// Registry holds the set of registered plugins, indexed by tool name.
// It provides lookup, enumeration, and search capabilities used by
// the CLI, TUI, and GUI layers.
type Registry struct {
	plugins map[string]Plugin
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{plugins: map[string]Plugin{}}
}

// Register adds p to the registry, indexed by its Name().
func (r *Registry) Register(p Plugin) {
	r.plugins[p.Name()] = p
}

// Get returns the plugin for the given tool name.
func (r *Registry) Get(name string) (Plugin, bool) {
	p, ok := r.plugins[name]
	return p, ok
}

// All returns every registered plugin in a stable order.
func (r *Registry) All() []Plugin {
	out := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns every registered tool name in a stable order.
func (r *Registry) Names() []string {
	all := r.All()
	out := make([]string, 0, len(all))
	for _, p := range all {
		out = append(out, p.Name())
	}
	return out
}

// Search returns plugins whose name, display name or description
// contain q (case-insensitive).
func (r *Registry) Search(q string) []Plugin {
	ql := strings.ToLower(q)
	var out []Plugin
	for _, p := range r.All() {
		if strings.Contains(strings.ToLower(p.Name()), ql) || strings.Contains(strings.ToLower(p.DisplayName()), ql) || strings.Contains(strings.ToLower(p.Description()), ql) {
			out = append(out, p)
		}
	}
	return out
}
