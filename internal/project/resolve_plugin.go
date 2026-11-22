package project

import "github.com/xmdemt/xdev/internal/plugin"

// ResolvePluginInstalled resolves a project pin against the plugin's actual
// installation layout and returns its validated directory. It only reads the
// installed set; it does not resolve remote versions or install anything.
func ResolvePluginInstalled(installRoot string, p plugin.Plugin, spec string) (string, bool) {
	versions, err := plugin.InstalledVersions(p, installRoot)
	if err != nil {
		return "", false
	}
	version, ok := resolveInstalledVersions(p.Name(), spec, versions)
	if !ok {
		return "", false
	}
	dir, err := p.InstallDir(installRoot, version)
	return dir, err == nil
}
