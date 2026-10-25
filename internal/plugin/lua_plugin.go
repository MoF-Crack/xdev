package plugin

import (
	"context"
	"fmt"
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

	// 票 20 / F-05: "latest" used to be resolved to versions[len-1] (the
	// table tail), which silently picked the OLDEST version for plugins
	// that list versions newest-first (the common vfox layout). Leaving
	// the hint empty makes resolver.Resolve apply its KindLatest →
	// highest(available) semantics, which orders the list itself.
	//
	// Deterministic fallback: the resolver's highest() can only order
	// semver-parsable entries. When nothing in the list parses (e.g.
	// {"r29","r28"}) keep the old table-tail behaviour rather than depend
	// on an unspecified order for unparsable entries. The plugin's order
	// is never mutated.
	latest := ""
	if !hasParsableSemver(versions) {
		latest = versions[len(versions)-1]
	}
	return resolver.Resolve(parsed, versions, latest, "")
}

// hasParsableSemver reports whether at least one entry is a version the
// resolver can order. resolver.Parse is the exported entry point that
// mirrors the resolver's internal parseSemver: a plain semver string
// (with optional "v" prefix or pre-release) parses as KindExact, while
// non-semver plugin versions such as "r29" fail to parse. The
// "latest"/"lts" keywords also parse but are not versions, so only
// KindExact counts.
func hasParsableSemver(versions []string) bool {
	for _, v := range versions {
		if parsed, err := resolver.Parse(v); err == nil && parsed.Kind == resolver.KindExact {
			return true
		}
	}
	return false
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
//
// 20261021 审计 P1-5（阶段 2）：校验失败返回 ErrUnsafeInstallPath。
func (p *luaBasePlugin) InstallDir(installRoot, version string) (string, error) {
	if _, err := util.SafeName(p.name); err != nil {
		return "", fmt.Errorf("%w: plugin name %q: %v", ErrUnsafeInstallPath, p.name, err)
	}
	if _, err := util.SafeName(version); err != nil {
		return "", fmt.Errorf("%w: version %q: %v", ErrUnsafeInstallPath, version, err)
	}

	result := filepath.Join(installRoot, p.name, version)

	if err := util.ValidateSubPath(installRoot, result); err != nil {
		return "", fmt.Errorf("%w: %q escapes %q: %v", ErrUnsafeInstallPath, result, installRoot, err)
	}

	return result, nil
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

// PathAppend returns nil: Lua 插件格式尚未支持 path_append 声明。
// 需要追加 PATH 的工具应改用 manifest 格式（Generic 插件）。
func (p *luaBasePlugin) PathAppend(_ string) []string { return nil }

// RequiredPlugin returns nil for Lua plugins; dependency management
// is not yet supported for the Lua plugin format.
func (p *luaBasePlugin) RequiredPlugin() []string { return nil }

// LuaPlugin is an xdev-format Lua plugin.
type LuaPlugin struct {
	*luaBasePlugin
}

// Ensure *LuaPlugin satisfies the Plugin interface at compile time.
var _ Plugin = (*LuaPlugin)(nil)
