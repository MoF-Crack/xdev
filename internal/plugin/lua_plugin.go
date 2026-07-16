package plugin

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/resolver"
	"github.com/xmdemt/xdev/internal/util"
)

// luaCallSpecs holds the Lua function name mapping that differs
// between xdev-format and vfox-format plugins.
type luaCallSpecs struct {
	archiveFn  string // "archive" (xdev) or "install" (vfox)
	envVarsFn  string // "env_vars" (xdev) or "env_keys" (vfox)
	descPrefix string // "Lua plugin: " or "vfox plugin: "
	vfoxFormat bool   // false: available() returns strings; true: may return tables with "version"
}

// luaBasePlugin is the shared implementation for Lua-based plugins.
// Both LuaPlugin (xdev format) and VfoxPlugin (vfox format) embed it
// and differ only in the luaCallSpecs they pass.
type luaBasePlugin struct {
	path        string
	name        string
	displayName string
	category    string
	shims       []string
	hasEnv      bool
	engine      *LuaEngine
	specs       luaCallSpecs
}

// Ensure *luaBasePlugin satisfies the Plugin interface at compile time.
var _ Plugin = (*luaBasePlugin)(nil)

func (p *luaBasePlugin) Name() string           { return p.name }
func (p *luaBasePlugin) DisplayName() string    { return p.displayName }
func (p *luaBasePlugin) Description() string    { return p.specs.descPrefix + p.name }
func (p *luaBasePlugin) Homepage() string       { return "" }
func (p *luaBasePlugin) Category() string       { return p.category }
func (p *luaBasePlugin) Shims() []string        { return append([]string{}, p.shims...) }
func (p *luaBasePlugin) DefaultVersion() string { return "latest" }

func (p *luaBasePlugin) Manifest() *manifest.Tool { return nil }

func (p *luaBasePlugin) ResolveVersion(spec string) (string, error) {
	versions, err := p.engine.callAvailable(p)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available for %s", p.name)
	}

	parsed, err := resolver.Parse(spec)
	if err != nil {
		return "", err
	}

	latest := versions[len(versions)-1]
	return resolver.Resolve(parsed, versions, latest, "")
}

func (p *luaBasePlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	goos := HostOS()
	arch := HostArch()

	la, err := p.engine.callArchive(p, version, goos, arch)
	if err != nil {
		return nil, err
	}

	return &manifest.Archive{
		OS:     goos,
		Arch:   arch,
		Format: la.Format,
		URL:    la.URL,
		Strip:  la.Strip,
	}, nil
}

// InstallDir returns the install directory for this Lua plugin.
//
// Security: p.name and version are validated with util.SafeName and
// the final path is checked with util.ValidateSubPath to prevent
// path traversal attacks (C-2).
func (p *luaBasePlugin) InstallDir(installRoot, version string) string {
	safeName := p.name
	if _, err := util.SafeName(p.name); err != nil {
		log.Printf("[plugin] CRITICAL: Lua plugin name %q is not a safe path component: %v — using fallback", p.name, err)
		safeName = "unsafe-plugin-blocked"
	}

	safeVersion := version
	if _, err := util.SafeName(version); err != nil {
		log.Printf("[plugin] CRITICAL: version %q is not a safe path component: %v — using fallback", version, err)
		safeVersion = "unsafe-version-blocked"
	}

	result := filepath.Join(installRoot, safeName, safeVersion)

	if err := util.ValidateSubPath(installRoot, result); err != nil {
		log.Printf("[plugin] CRITICAL: InstallDir %q escapes install root %q — using safe fallback", result, installRoot)
		return filepath.Join(installRoot, "unsafe-path-blocked")
	}

	return result
}

func (p *luaBasePlugin) EnvVars(installPath string) map[string]string {
	if !p.hasEnv {
		return map[string]string{}
	}
	out, err := p.engine.callEnvVars(p, installPath)
	if err != nil {
		return map[string]string{}
	}
	for k, v := range out {
		out[k] = strings.ReplaceAll(v, "{{install}}", installPath)
	}
	return out
}

func (p *luaBasePlugin) PostInstall(_ context.Context, _ string) error { return nil }
func (p *luaBasePlugin) Deactivate(_ string) error                     { return nil }

// RequiredPlugin returns nil for Lua plugins; dependency management
// is not yet supported for the Lua plugin format.
func (p *luaBasePlugin) RequiredPlugin() []string { return nil }

// LuaPlugin is an xdev-format Lua plugin.
type LuaPlugin struct {
	*luaBasePlugin
}

// Ensure *LuaPlugin satisfies the Plugin interface at compile time.
var _ Plugin = (*LuaPlugin)(nil)
