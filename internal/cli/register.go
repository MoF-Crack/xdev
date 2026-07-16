// register.go wires every built-in plugin into the plugin registry.
//
// RegisterAll is called at startup (from both CLI and GUI entry points)
// to populate the registry with all supported tools. The registration
// order is:
//
//  1. Java — the only plugin with custom ArchiveFor logic (dynamic URL
//     generation for Oracle/Adoptium JDK downloads)
//  2. Generic tools — all other tools whose behavior is entirely driven
//     by their YAML manifest. This includes:
//     - M1 core: python
//     - M2 languages: node, go, rust, dotnet, php, ruby, lua, etc.
//     - M3 Android toolchain: platform-tools, build-tools, ndk, etc.
//     - M3 build tools: maven, gradle, cmake, ninja, make, git, etc.
//     - Flutter and package managers (uv)
//  3. User-installed Lua plugins — loaded from $XMDEV_HOME/plugins/,
//     supporting both xdev-native format and vfox-compatible format
//
// Tools that need a custom install sub-directory (e.g. Android SDK
// components that live under android/) set install_root in their
// manifest, which Generic.InstallDir honors.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/plugins/java"
)

// RegisterAll wires every built-in plugin into r, using the
// manifest.Tools loaded from disk.
func RegisterAll(r *plugin.Registry, catalog *manifest.Catalog) {
	// Java is the only plugin with custom ArchiveFor logic.
	if t, ok := catalog.Tool("java"); ok {
		r.Register(java.New(t))
	}

	// All other tools use plugin.Generic directly — their behavior is
	// entirely driven by their manifest YAML. Tools that need a custom
	// install sub-directory (e.g. Android SDK components) set
	// install_root in their manifest, which Generic.InstallDir honors.
	genericTools := []string{
		// M1 core
		"python",
		// M2 languages
		"node", "go", "rust", "dotnet", "php", "ruby", "lua", "perl",
		"dart", "kotlin", "bun", "deno", "zig", "julia", "haskell",
		"scala", "erlang", "typescript",
		// M3 Android toolchain (install_root: android-sdk)
		"android-platform-tools", "android-build-tools", "android-platform",
		"android-ndk", "android-cmdline-tools", "android-emulator",
		// M3 build tools
			"maven", "gradle", "cmake", "ninja", "make", "git", "llvm", "mingw", "sbt",
			// Flutter
			"flutter",
			// Package managers
			"uv",
	}
	for _, name := range genericTools {
		if t, ok := catalog.Tool(name); ok {
			r.Register(plugin.NewGeneric(t))
		}
	}

	// Scan $XMDEV_HOME/plugins/ for Lua plugins.
	registerLuaPlugins(r)

	// Scan $XMDEV_HOME/plugins/ for vfox-format Lua plugins.
	registerVfoxPlugins(r)
}

// pluginsDir resolves the user plugin directory ($XMDEV_HOME/plugins).
func pluginsDir() string {
	home := os.Getenv("XMDEV_HOME")
	if home == "" {
		if runtime.GOOS == "windows" {
			home = filepath.Join(os.Getenv("USERPROFILE"), ".xdev")
		} else {
			home = filepath.Join(os.Getenv("HOME"), ".xdev")
		}
	}
	return filepath.Join(home, "plugins")
}

// registerLuaPlugins scans the plugins directory for .lua files
// and registers them.  Errors are logged but do not prevent startup.
func registerLuaPlugins(r *plugin.Registry) {
	dir := pluginsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: failed to read plugins dir %s: %v\n", dir, err)
		}
		return
	}

	engine := plugin.NewLuaEngine()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := engine.LoadPlugin(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load Lua plugin %s: %v\n", e.Name(), err)
			continue
		}
		if _, exists := r.Get(p.Name()); exists {
			fmt.Fprintf(os.Stderr, "warning: Lua plugin %s conflicts with built-in plugin, skipping\n", p.Name())
			continue
		}
		r.Register(p)
	}
}

// registerVfoxPlugins scans the plugins directory for vfox-format
// Lua plugins and registers them.  A vfox plugin is identified by
// having a PLUGIN.info table instead of PLUGIN.name.  Errors are
// logged but do not prevent startup.
func registerVfoxPlugins(r *plugin.Registry) {
	dir := pluginsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	engine := plugin.NewLuaEngine()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := engine.LoadVfoxPlugin(path)
		if err != nil {
			continue
		}
		if _, exists := r.Get(p.Name()); exists {
			fmt.Fprintf(os.Stderr, "warning: vfox plugin %s conflicts with existing plugin, skipping\n", p.Name())
			continue
		}
		r.Register(p)
		fmt.Fprintf(os.Stderr, "info: loaded vfox plugin %s from %s\n", p.Name(), e.Name())
	}
}
