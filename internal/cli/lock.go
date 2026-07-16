package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/lockfile"
	"github.com/xmdemt/xdev/internal/project"
)

func newLockCmd() *cobra.Command {
	var (
		flagCheck bool
		flagJSON  bool
	)

	cmd := &cobra.Command{
		Use:   "lock [tool...]",
		Short: "Generate or verify .xdev.lock from .xdev.toml",
		Long: `Generate or verify the project lockfile (.xdev.lock).

Without flags, regenerates .xdev.lock from the nearest .xdev.toml.
With --check, verifies that the lockfile matches .xdev.toml.
With --json, outputs the lockfile status in JSON format.`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := lockfile.FindConfigDir(".")
			if dir == "" {
				return fmt.Errorf("no .xdev.toml found in current directory or any parent; run 'xdev init' first")
			}

			_, projCfg := project.FindConfig(dir)
			if projCfg == nil || len(projCfg.Tools) == 0 {
				return fmt.Errorf("no tools configured in .xdev.toml; add tools with 'xdev local <tool>@<version>'")
			}

			if flagCheck {
				return lockCheck(dir, projCfg, flagJSON)
			}

			return lockGenerate(cmd.Context(), Cfg(), dir, projCfg, args, flagJSON)
		},
	}

	cmd.Flags().BoolVar(&flagCheck, "check", false, "verify lockfile matches .xdev.toml instead of regenerating")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output in JSON format")

	return cmd
}

// lockGenerate regenerates .xdev.lock from .xdev.toml.
func lockGenerate(ctx context.Context, cfg *config.Config, dir string, projCfg *project.ProjectConfig, args []string, jsonOutput bool) error {
	// Load catalog and registry
	_, reg := loadCatalogAndRegistry()

	// Filter tools if specific names provided
	toolsToLock := projCfg.Tools
	if len(args) > 0 {
		toolsToLock = map[string]string{}
		for _, name := range args {
			if spec, ok := projCfg.Tools[name]; ok {
				toolsToLock[name] = spec
			} else {
				return fmt.Errorf("tool %q not found in .xdev.toml", name)
			}
		}
	}

	lf := lockfile.New()
	for toolName, spec := range toolsToLock {
		p, ok := reg.Get(toolName)
		if !ok {
			fmt.Fprintf(os.Stderr, "⚠ unsupported tool %q, skipping\n", toolName)
			continue
		}

		ver, err := p.ResolveVersion(spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ failed to resolve %s@%s: %v, skipping\n", toolName, spec, err)
			continue
		}

		installPath := p.InstallDir(cfg.Core.InstallDir, ver)
		archive, err := p.ArchiveFor(ver)
		var sha256, url string
		if err == nil {
			sha256 = archive.SHA256
			url = archive.URL
		}

		lf.AddTool(toolName, lockfile.LockedTool{
			Version:     spec,
			Resolved:    ver,
			SHA256:      sha256,
			URL:         url,
			InstallPath: installPath,
		})
	}

	if err := lockfile.Save(dir, lf); err != nil {
		return fmt.Errorf("save lockfile: %w", err)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(lf, "", "2")
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		fmt.Printf("✔ generated .xdev.lock with %d tools\n", len(lf.Tools))
	}
	return nil
}

// lockCheck verifies that the lockfile matches .xdev.toml.
func lockCheck(dir string, projCfg *project.ProjectConfig, jsonOutput bool) error {
	lf, err := lockfile.Load(dir)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}
	if lf == nil {
		return fmt.Errorf("no .xdev.lock found; run 'xdev lock' to generate one")
	}

	type mismatch struct {
		Tool     string `json:"tool"`
		TomlSpec string `json:"toml_spec"`
		LockSpec string `json:"lock_spec"`
	}
	var mismatches []mismatch
	var stale []string

	for toolName, spec := range projCfg.Tools {
		locked, ok := lf.Tools[toolName]
		if !ok {
			mismatches = append(mismatches, mismatch{toolName, spec, "(missing)"})
			continue
		}
		if locked.Version != spec {
			mismatches = append(mismatches, mismatch{toolName, spec, locked.Version})
		}
	}
	for toolName := range lf.Tools {
		if _, ok := projCfg.Tools[toolName]; !ok {
			stale = append(stale, toolName)
		}
	}

	if jsonOutput {
		result := struct {
			OK         bool        `json:"ok"`
			Mismatches []mismatch  `json:"mismatches,omitempty"`
			Stale      []string    `json:"stale,omitempty"`
		}{len(mismatches) == 0 && len(stale) == 0, mismatches, stale}
		data, _ := json.MarshalIndent(result, "", "2")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	if len(mismatches) == 0 && len(stale) == 0 {
		fmt.Println("✔ lockfile is in sync with .xdev.toml")
		return nil
	}

	for _, m := range mismatches {
		fmt.Fprintf(os.Stderr, "✗ %s: .xdev.toml=%q .xdev.lock=%q\n", m.Tool, m.TomlSpec, m.LockSpec)
	}
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "✗ %s: in lockfile but not in .xdev.toml\n", s)
	}
	return fmt.Errorf("lockfile is out of sync with .xdev.toml")
}
