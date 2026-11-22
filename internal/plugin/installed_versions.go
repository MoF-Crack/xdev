package plugin

import (
	"path/filepath"

	"github.com/xmdemt/xdev/internal/util"
)

// InstalledVersionsDir returns the version parent defined by the plugin layout.
// InstallDir only constructs/validates paths; the safe anchor is never installed
// or resolved against a remote catalog. This also works for dynamic plugins
// with no default version or static manifest.
func InstalledVersionsDir(p Plugin, installRoot string) (string, error) {
	path, err := p.InstallDir(installRoot, "0")
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

// InstalledVersions reads only this plugin's version parent, never recursively
// searches unrelated install trees, and applies the shared auxiliary-dir filter.
func InstalledVersions(p Plugin, installRoot string) ([]string, error) {
	dir, err := InstalledVersionsDir(p, installRoot)
	if err != nil {
		return nil, err
	}
	return util.ListVersionDirNames(dir)
}
