package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

func resolveInstallArchive(p plugin.Plugin, version, form string) (*manifest.Archive, error) {
	if form == "" {
		return p.ArchiveFor(version)
	}
	if m := p.Manifest(); m != nil {
		if v, ok := m.FindVersion(version); ok {
			if a, ok := v.ArchiveForCurrentWithForm(plugin.HostOS(), plugin.HostArch(), form); ok {
				copy := *a
				return &copy, nil
			}
		}
	}
	return nil, fmt.Errorf("no archive for %s@%s with form %q on %s/%s", p.Name(), version, form, plugin.HostOS(), plugin.HostArch())
}

// Selection identity excludes SHA pins because TOFU can fill those after the
// download. URL/platform/form/format/kind/strip determine the selected payload.
func selectedArchiveIdentity(a *manifest.Archive) string {
	encoded, _ := json.Marshal([]any{a.OS, a.Arch, a.Form, a.URL, a.Format, a.EffectiveKind(), a.Strip})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

const installSelectionFile = ".xdev-install-selection.json"

type installSelection struct {
	Identity string `json:"identity"`
	Form     string `json:"form"`
}

func verifyInstalledSelection(path string, a *manifest.Archive, requestedForm string) error {
	data, err := os.ReadFile(filepath.Join(path, installSelectionFile))
	if os.IsNotExist(err) && requestedForm == "" && a.Form == "" {
		return nil // retain legacy form-agnostic installations
	}
	var installed installSelection
	if err != nil || json.Unmarshal(data, &installed) != nil || installed.Identity != selectedArchiveIdentity(a) {
		return fmt.Errorf("%w: installed archive selection is unknown or differs from form %q; force repair is required", ErrIncompleteInstall, a.Form)
	}
	return nil
}

func recordInstalledSelection(path string, a *manifest.Archive) error {
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if a.Form == "" {
			return nil
		} // external legacy script installers
		return fmt.Errorf("selected form %q did not create its install directory", a.Form)
	}
	data, err := json.Marshal(installSelection{Identity: selectedArchiveIdentity(a), Form: a.Form})
	if err != nil {
		return err
	}
	return util.AtomicWriteFile(filepath.Join(path, installSelectionFile), data, 0644)
}

// Form-specific downloads use a separate cache entry for the exact selection.
// The same helper is used by downloading, extraction and executable dispatch.
func (i *Installer) selectedCacheArchivePath(tool, version string, a *manifest.Archive) (string, error) {
	if a.Form == "" {
		return i.cacheArchivePath(tool, version, a.Format)
	}
	if err := validateInstallComponent("version", version); err != nil {
		return "", err
	}
	return i.cacheArchivePath(tool, version+"-s-"+selectedArchiveIdentity(a)[:32], a.Format)
}

func beginInstallCommit(ctx context.Context, opts InstallOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.BeforeCommit != nil {
		return opts.BeforeCommit()
	}
	return nil
}
