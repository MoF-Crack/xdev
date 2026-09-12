// install.go defines the `xdev install`, `xdev uninstall`, `xdev use`,
// `xdev list`, `xdev current`, and `xdev which` commands.
//
// # install
//
// The install command supports:
//   - `xdev install go@1.22.0` — install a specific tool and version
//
// After installation, the tool is activated (unless --no-activate is set).
// When --no-path-fix is set, registry writes are skipped for test isolation.
//
// # use
//
// The use command switches the active version of an already-installed tool.
// It updates the current/<tool> junction/symlink and cleans up stale PATH
// entries. The CleanupUserPath call is gated by !NoPathFix().
//
// # uninstall
//
// The uninstall command removes an installed version directory. If the
// uninstalled version was active, it attempts to re-point to another
// installed version, or cleans up shims and env vars if no other version
// exists.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// newInstallCmd handles `xdev install <tool>[@<ver>]`.
func newInstallCmd() *cobra.Command {
	var flagNoActivate bool
	var flagYes bool
	var flagForce bool
	cmd := &cobra.Command{
		Use:   "install <tool>[@<version>]",
		Short: "Download and install a tool version",
		Long: `install fetches the tool's archive from the configured mirror
(or upstream), verifies its SHA-256, extracts it under
<install_dir>/<tool>/<version> and activates it by creating a
symlink at <xmdevenv_dir>/current/<tool>.

If the version is already installed and complete, install only
re-activates it. If it is installed but incomplete (e.g. an
interrupted extraction), install fails with an "incomplete install"
error unless --force is passed, which removes the existing
directory and reinstalls from scratch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("requires a tool name")
			}
			name, ver := splitToolVer(args[0])
			_, reg := loadCatalogAndRegistry()
			p, ok := reg.Get(name)
			if !ok {
				return unsupportedToolErr(name)
			}
			if ver == "" {
				ver = p.DefaultVersion()
				if ver == "" {
					return fmt.Errorf("no default version for %s; specify @<version>", name)
				}
			}

			// Check if the version is unverified (sniff-discovered) and prompt user.
			if err := checkUnverifiedVersion(cmd, name, ver, flagYes); err != nil {
				return err
			}

			ins := installer.New(Cfg(), reg)
			ins.SetNoEnvWrite(NoPathFix())
			res, err := ins.InstallWithProgress(cmd.Context(), p, ver, nil, installer.InstallOptions{NoActivate: flagNoActivate, Force: flagForce})
			if err != nil {
				return err
			}
			fmt.Printf("✔ installed %s@%s to %s\n", res.Tool, res.Version, res.InstallPath)
			if res.Activated {
				fmt.Printf("✔ activated %s@%s (current -> %s)\n", res.Tool, res.Version, res.InstallPath)
			}
			fmt.Println("Tip: open a new terminal or `source ~/.zshrc` / `~/.bashrc` to use the new shims.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagNoActivate, "no-activate", false, "install but do not change the active version")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation prompts (e.g. for unverified versions)")
	cmd.Flags().BoolVar(&flagForce, "force", false, "remove the existing install directory (if any) and reinstall from scratch")
	cmd.Flags().BoolVar(&flagForce, "reinstall", false, "alias for --force")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <tool>[@<version>]",
		Short: "Remove an installed version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, ver := splitToolVer(args[0])
			if ver == "" {
				return fmt.Errorf("uninstall requires an explicit version (e.g. %s@3.12.0)", name)
			}
			_, reg := loadCatalogAndRegistry()
			p, ok := reg.Get(name)
			if !ok {
				return unsupportedToolErr(name)
			}
			ins := installer.New(Cfg(), reg)
			if err := ins.Uninstall(p, ver); err != nil {
				return err
			}
			fmt.Printf("✔ removed %s@%s\n", name, ver)
			fmt.Println("Tip: open a new terminal to reflect the uninstalled tool in your PATH.")
			return nil
		},
	}
}

func newUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <tool>[@<version>]",
		Short: "Switch the active version of an already-installed tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, ver := splitToolVer(args[0])
			_, reg := loadCatalogAndRegistry()
			p, ok := reg.Get(name)
			if !ok {
				return unsupportedToolErr(name)
			}
			if ver == "" {
				return fmt.Errorf("use requires a version, e.g. %s@3.12.0", name)
			}
			installPath := p.InstallDir(Cfg().Core.InstallDir, ver)
			if _, err := os.Stat(installPath); err != nil {
				return fmt.Errorf("%s@%s is not installed (run `xdev install %s@%s`)",
					name, ver, name, ver)
			}
			ins := installer.New(Cfg(), reg)
			ins.SetNoEnvWrite(NoPathFix())
			if err := ins.Activate(p, installPath, ver); err != nil {
				return err
			}
			// 清理用户 PATH 中残留的 xdev install 目录条目
			if !NoPathFix() {
				shimDir := filepath.Join(Cfg().Core.XMDevDir, "shims")
				if changed, _ := env.CleanupUserPath(Cfg().Core.XMDevDir, shimDir, Cfg().GetPathMode()); changed {
					fmt.Println("  (cleaned up stale PATH entries)")
				}
			}
			fmt.Printf("✔ %s@%s is now active\n", name, ver)
			fmt.Println("Tip: open a new terminal or `source ~/.zshrc` / `~/.bashrc` to use the updated environment.")
			return nil
		},
	}
}

func newWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which <tool>",
		Short: "Print the path that the shim would resolve to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, reg := loadCatalogAndRegistry()
			p, ok := reg.Get(args[0])
			if !ok {
				return unsupportedToolErr(args[0])
			}
			target := activeInstallPath(p.Name())
			if target == "" {
				return fmt.Errorf("no active version for %s (run `xdev install %s@<ver>`)", args[0], args[0])
			}
			fmt.Println(target)
			return nil
		},
	}
}

// newListCmd: list installed tools, optionally with --remote.
func newListCmd() *cobra.Command {
	var flagRemote bool
	cmd := &cobra.Command{
		Use:   "list [tool]",
		Short: "List installed tools (and, with --remote, available versions)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			installRoot := cfg.Core.InstallDir
			currentRoot := filepath.Join(cfg.Core.XMDevDir, "current")

			// Load sniff cache for upgrade indicators.
			sniffCache := loadSniffCache(cfg.Core.CacheDir)

			if len(args) == 1 {
				tool := args[0]
				return listTool(tool, installRoot, currentRoot, flagRemote)
			}

			entries, err := os.ReadDir(installRoot)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("(nothing installed yet)")
					return nil
				}
				return err
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			headers := []string{"Tool", "Installed", "Active"}
			var rows [][]string
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				tool := e.Name()
				versions, _ := os.ReadDir(filepath.Join(installRoot, tool))
				active := ""
				if target, err := os.Readlink(filepath.Join(currentRoot, tool)); err == nil {
					active = filepath.Base(target)
				}
				vs := make([]string, 0, len(versions))
				for _, v := range versions {
					if v.IsDir() {
						if v.Name() == active {
							vs = append(vs, "★ "+v.Name())
						} else {
							vs = append(vs, v.Name())
						}
					}
				}

				// Append upgrade indicator if a newer version was sniffed.
				upgradeStr := upgradeIndicator(tool, active, sniffCache)
				row := []string{tool, strings.Join(vs, ", "), active}
				if upgradeStr != "" {
					row[1] = row[1] + " " + upgradeStr
				}
				rows = append(rows, row)
			}
			renderTable(headers, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagRemote, "remote", false, "show all versions available remotely for the named tool")
	return cmd
}

func listTool(tool, installRoot, currentRoot string, remote bool) error {
	_, reg := loadCatalogAndRegistry()
	p, ok := reg.Get(tool)
	if !ok {
		return unsupportedToolErr(tool)
	}
	if remote {
		versions := p.Manifest().Versions()
		active := ""
		if target, err := os.Readlink(filepath.Join(currentRoot, tool)); err == nil {
			active = filepath.Base(target)
		}
		headers := []string{"Version", "Status"}
		rows := make([][]string, 0, len(versions))
		for _, v := range versions {
			status := ""
			if v == active {
				status = "★ active"
			} else if _, err := os.Stat(filepath.Join(installRoot, tool, v)); err == nil {
				status = "installed"
			}
			rows = append(rows, []string{v, status})
		}
		renderTable(headers, rows)
		return nil
	}

	// list local installed versions
	path := filepath.Join(installRoot, tool)
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("(no %s versions installed)\n", tool)
			return nil
		}
		return err
	}
	active := ""
	if target, err := os.Readlink(filepath.Join(currentRoot, tool)); err == nil {
		active = filepath.Base(target)
	}
	headers := []string{"Version", "Status"}
	rows := make([][]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		status := ""
		if e.Name() == active {
			status = "★ active"
		}
		rows = append(rows, []string{e.Name(), status})
	}
	renderTable(headers, rows)
	return nil
}

func newCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current [tool]",
		Short: "Print the currently active version of a tool (or all)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			currentRoot := filepath.Join(Cfg().Core.XMDevDir, "current")
			if len(args) == 1 {
				ver, ok := activeVersion(args[0])
				if !ok {
					return fmt.Errorf("no active version for %s", args[0])
				}
				fmt.Println(ver)
				return nil
			}
			entries, err := os.ReadDir(currentRoot)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("(no tools activated)")
					return nil
				}
				return err
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			for _, e := range entries {
				target, _ := os.Readlink(filepath.Join(currentRoot, e.Name()))
				fmt.Printf("%-16s %s\n", e.Name(), filepath.Base(target))
			}
			return nil
		},
	}
}

// splitToolVer splits "python@3.12.0" or "python" into ("python", "3.12.0").
func splitToolVer(s string) (string, string) {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// loadSniffCache reads the sniff cache JSON from the cache directory.
// Returns nil if the cache file doesn't exist or is malformed.
func loadSniffCache(cacheDir string) *discovery.SniffCache {
	path := filepath.Join(cacheDir, "sniff-cache.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c discovery.SniffCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if c.Tools == nil {
		return nil
	}
	return &c
}

// upgradeIndicator returns a string like "(↑3.13.0 available)" if a newer
// version was discovered for the tool, or "" if the tool is up to date.
func upgradeIndicator(tool, activeVersion string, cache *discovery.SniffCache) string {
	if cache == nil || activeVersion == "" {
		return ""
	}
	result, ok := cache.Tools[tool]
	if !ok || result.Unsupported || result.Error != "" || len(result.Versions) == 0 {
		return ""
	}
	// Find the latest version in the sniff results.
	latest := result.Versions[0]
	for _, v := range result.Versions[1:] {
		if util.CompareSemver(v, latest) > 0 {
			latest = v
		}
	}
	if util.CompareSemver(latest, activeVersion) > 0 {
		return fmt.Sprintf("(↑%s available)", latest)
	}
	return ""
}

// checkUnverifiedVersion checks if the specified version of a tool is
// marked as unverified (sniff-discovered).  If so, it prompts the user
// for confirmation unless --yes was passed.
func checkUnverifiedVersion(cmd *cobra.Command, tool, ver string, skipConfirm bool) error {
	cat := loadCatalog()
	t, ok := cat.Tool(tool)
	if !ok {
		return nil // Not in manifest — let the installer handle it.
	}
	v, found := t.FindVersion(ver)
	if !found || !v.Unverified {
		return nil
	}
	// Spec 039: npm-kind 安装不下载归档（依赖树与完整性由 npm 负责），
	// 「归档 URL 未验证」提示与确认不适用。
	if versionNPMManaged(t, v) {
		return nil
	}
	fmt.Printf("⚠ This version (%s@%s) was discovered by sniffing.\n", tool, ver)
	fmt.Printf("  The archive URL has not been verified — downloads may fail.\n")
	fmt.Printf("  If the download fails, update the manifest manually or wait for the next xdev release.\n")
	if !skipConfirm {
		fmt.Printf("Continue with installation? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return fmt.Errorf("installation cancelled by user")
		}
	}
	return nil
}

// versionNPMManaged reports whether the version's archives declare an
// npm-managed install (install_kind: npm); versions without archives
// fall back to the tool's other versions (mirrors the GUI's
// versionArchivesNPMManaged semantics, Spec 039).
func versionNPMManaged(t *manifest.Tool, v *manifest.Version) bool {
	for _, a := range v.Archives {
		if a.EffectiveKind() == manifest.InstallKindNPM {
			return true
		}
	}
	if len(v.Archives) > 0 {
		return false
	}
	for _, other := range t.VersionList {
		for _, a := range other.Archives {
			if a.EffectiveKind() == manifest.InstallKindNPM {
				return true
			}
		}
	}
	return false
}
