// install.go defines the `xdev install`, `xdev uninstall`, `xdev use`,
// `xdev list`, `xdev current`, and `xdev which` commands.
//
// # install
//
// The install command supports three modes:
//   - `xdev install go@1.22.0` — install a specific tool and version
//   - `xdev install --from-local` — install all tools from .xdev.toml
//   - `xdev install --locked` — install exact versions from .xdev.lock
//
// After installation, the tool is activated (unless --no-activate is set)
// and the lockfile is updated. When --no-path-fix is set, registry writes
// are skipped for test isolation.
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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/lockfile"
	"github.com/xmdemt/xdev/internal/project"
)

// newInstallCmd handles `xdev install <tool>[@<ver>]`.
func newInstallCmd() *cobra.Command {
	var flagNoActivate bool
	var flagFromLocal bool
	var flagLocked bool
	cmd := &cobra.Command{
		Use:   "install <tool>[@<version>]",
		Short: "Download and install a tool version",
		Long: `install fetches the tool's archive from the configured mirror
(or upstream), verifies its SHA-256, extracts it under
<install_dir>/<tool>/<version> and activates it by creating a
symlink at <xmdevenv_dir>/current/<tool>.

Use --from-local to install all tools listed in .xdev.toml.
Use --locked to install only from .xdev.lock (exact versions).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagLocked {
				return installLocked(cmd)
			}
			if flagFromLocal {
				return installFromLocal(cmd)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a tool name, --from-local, or --locked flag")
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
			ins := installer.New(Cfg(), reg)
			ins.SetNoEnvWrite(NoPathFix())
			res, err := ins.InstallWithProgress(cmd.Context(), p, ver, nil, installer.InstallOptions{NoActivate: flagNoActivate})
			if err != nil {
				return err
			}
			fmt.Printf("✔ installed %s@%s to %s\n", res.Tool, res.Version, res.InstallPath)
			if res.Activated {
				fmt.Printf("✔ activated %s@%s (current -> %s)\n", res.Tool, res.Version, res.InstallPath)
			}
			// Update lockfile
			updateLockfile(name, res)
			fmt.Println("Tip: open a new terminal or `source ~/.zshrc` / `~/.bashrc` to use the new shims.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagNoActivate, "no-activate", false, "install but do not change the active version")
	cmd.Flags().BoolVar(&flagFromLocal, "from-local", false, "install all tools from .xdev.toml in the current directory")
	cmd.Flags().BoolVar(&flagLocked, "locked", false, "install only from .xdev.lock (exact pinned versions)")
	return cmd
}

// installFromLocal installs all tools listed in the nearest .xdev.toml.
func installFromLocal(cmd *cobra.Command) error {
	_, proj := project.FindConfig(".")
	if proj == nil || len(proj.Tools) == 0 {
		return fmt.Errorf("no .xdev.toml found or no tools configured")
	}
	_, reg := loadCatalogAndRegistry()
	ins := installer.New(Cfg(), reg)
	ins.SetNoEnvWrite(NoPathFix())

	var items []installer.BatchInstallItem
	for toolName, versionSpec := range proj.Tools {
		items = append(items, installer.BatchInstallItem{
			ToolName: toolName,
			Version:  versionSpec,
		})
	}

	var skipped, failed int
	results := ins.InstallMany(cmd.Context(), items)
	for _, r := range results {
		switch r.Status {
		case "unsupported":
			fmt.Printf("⚠ unsupported tool %q, skipping\n", r.Tool)
			skipped++
		case "already_installed":
			fmt.Printf("✔ %s@%s already installed\n", r.Tool, r.Version)
		case "failed":
			fmt.Printf("⚠ failed to install %s@%s: %v\n", r.Tool, r.Version, r.Error)
			failed++
		case "installed":
			fmt.Printf("✔ installed %s@%s\n", r.Tool, r.Version)
		}
	}
	if skipped > 0 || failed > 0 {
		return fmt.Errorf("install completed with %d skipped, %d failed", skipped, failed)
	}
	return nil
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
			// Clean up lockfile entry if present
			if configDir := lockfile.FindConfigDir("."); configDir != "" {
				if lf, err := lockfile.Load(configDir); err == nil && lf != nil {
					if _, exists := lf.Tools[name]; exists {
						lf.RemoveTool(name)
						if err := lockfile.Save(configDir, lf); err != nil {
							fmt.Fprintf(os.Stderr, "⚠ failed to update lockfile: %v\n", err)
						} else {
							fmt.Printf("✔ removed %s from lockfile\n", name)
						}
					}
				}
			}
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
					if changed, _ := env.CleanupUserPath(shimDir, Cfg().GetPathMode()); changed {
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
				rows = append(rows, []string{tool, strings.Join(vs, ", "), active})
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

// installLocked installs tools from .xdev.lock with exact pinned versions.
func installLocked(cmd *cobra.Command) error {
	lf, err := lockfile.Load(".")
	if err != nil {
		return fmt.Errorf("failed to read .xdev.lock: %w", err)
	}
	if lf == nil || len(lf.Tools) == 0 {
		return fmt.Errorf("no .xdev.lock found or no tools locked")
	}
	_, reg := loadCatalogAndRegistry()
	ins := installer.New(Cfg(), reg)
	ins.SetNoEnvWrite(NoPathFix())

	var items []installer.BatchInstallItem
	for toolName, locked := range lf.Tools {
		items = append(items, installer.BatchInstallItem{
			ToolName: toolName,
			Version:  locked.Resolved,
		})
	}

	var skipped, failed int
	results := ins.InstallMany(cmd.Context(), items)
	for _, r := range results {
		switch r.Status {
		case "unsupported":
			fmt.Printf("⚠ unsupported tool %q in lockfile, skipping\n", r.Tool)
			skipped++
		case "already_installed":
			fmt.Printf("✔ %s@%s already installed\n", r.Tool, r.Version)
		case "failed":
			fmt.Printf("⚠ failed to install %s@%s: %v\n", r.Tool, r.Version, r.Error)
			failed++
		case "installed":
			fmt.Printf("✔ installed %s@%s\n", r.Tool, r.Version)
		}
	}
	if skipped > 0 || failed > 0 {
		return fmt.Errorf("install --locked completed with %d skipped, %d failed", skipped, failed)
	}
	return nil
}

// updateLockfile updates or creates .xdev.lock after an install.
// It pins the resolved version, archive URL and SHA-256 (when known)
// so that `xdev install --locked` reproduces the exact environment.
// res.Spec is recorded as Version (the original intent) and res.Version
// as Resolved (the concrete version that was installed).
func updateLockfile(toolName string, res *installer.InstallResult) {
	dir := lockfile.FindConfigDir(".")
	if dir == "" {
		return // No project config, skip lockfile
	}
	lf, err := lockfile.Load(dir)
	if err != nil || lf == nil {
		lf = lockfile.New()
	}
	lf.AddTool(toolName, lockfile.LockedTool{
		Version:     res.Spec,
		Resolved:    res.Version,
		SHA256:      res.SHA256,
		URL:         res.URL,
		InstallPath: res.InstallPath,
	})
	if err := lockfile.Save(dir, lf); err != nil {
		fmt.Printf("⚠ failed to update .xdev.lock: %v\n", err)
	}
}
