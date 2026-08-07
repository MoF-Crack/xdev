package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

func newRefreshCmd() *cobra.Command {
	var flagAll bool
	cmd := &cobra.Command{
		Use:   "refresh <tool>",
		Short: "Fetch latest version list from remote and update the local manifest cache",
		Long: `refresh contacts the remote source configured in the tool's manifest
(e.g. GitHub Releases) and writes the discovered versions into the
local manifest cache so that subsequent installs can use them.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if flagAll {
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("requires exactly 1 argument, or use --all")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagAll {
				return refreshAll(cmd)
			}
			return refreshOne(cmd, args[0])
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "Refresh all tools that have a remote source")
	return cmd
}

func refreshOne(cmd *cobra.Command, toolName string) error {
	cfg := Cfg()

	// Load tool catalog and check the tool exists.
	cat := loadCatalog()
	t, ok := cat.Tool(toolName)
	if !ok {
		return unsupportedToolErr(toolName)
	}

	// Count existing versions before sniffing.
	existingCount := len(t.VersionList)

	// Create scheduler and discover new versions.
	s := discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)

	remoteType := t.Remote.Type
	if remoteType == "" {
		remoteType = "auto"
	}
	fmt.Printf("🔍 fetching versions for %s from %s...\n", toolName, remoteType)

	if err := s.SniffNow(cmd.Context(), toolName); err != nil {
		return fmt.Errorf("refresh %s: %w", toolName, err)
	}

	// Reload catalog to get updated version list.
	cat2 := loadCatalog()
	t2, ok2 := cat2.Tool(toolName)
	if !ok2 {
		return unsupportedToolErr(toolName)
	}

	versions := t2.Versions()
	totalCount := len(versions)
	added := totalCount - existingCount
	if added < 0 {
		added = 0
	}

	if totalCount == 0 {
		fmt.Printf("⚠️  no versions discovered for %s\n", toolName)
		return nil
	}

	// Update the latest field if a newer version was found.
	if len(versions) > 0 {
		t2.Latest = versions[0]
		// Write updated manifest (only Latest field change).
		manifestDir := cfg.Core.ManifestDir
		outPath := filepath.Join(manifestDir, toolName+".yaml")
		if data, err := manifest.MarshalTool(t2); err == nil {
			if err := util.AtomicWriteFile(outPath, data, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "⚠ warning: failed to update Latest field: %v\n", err)
			}
		}
	}

	fmt.Printf("✔ %s: %d versions available, %d new\n", toolName, totalCount, added)
	// Show a preview of discovered versions.
	preview := versions
	if len(preview) > 10 {
		preview = preview[:10]
	}
	fmt.Printf("  latest: %s\n", strings.Join(preview, ", "))
	if len(versions) > 10 {
		fmt.Printf("  ... and %d more\n", len(versions)-10)
	}
	return nil
}

func refreshAll(cmd *cobra.Command) error {
	cfg := Cfg()
	s := discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)

	fmt.Println("Refreshing all tools...")
	if err := s.SniffAll(cmd.Context()); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ refresh completed with errors: %v\n", err)
		return err
	}
	fmt.Println("✔ Refresh complete.")
	return nil
}
