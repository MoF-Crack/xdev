package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
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
	cat := loadCatalog()
	t, ok := cat.Tool(toolName)
	if !ok {
		return unsupportedToolErr(toolName)
	}
	if t.Remote.Type == "" || t.Remote.Type == "none" {
		return fmt.Errorf("tool %q has no remote source configured", toolName)
	}

	fmt.Printf("🔍 fetching versions for %s from %s...\n", toolName, t.Remote.Type)
	versions, err := remote.DiscoverVersions(cmd.Context(), t.Remote)
	if err != nil {
		return fmt.Errorf("refresh %s: %w", toolName, err)
	}
	if len(versions) == 0 {
		fmt.Printf("⚠️  no versions discovered for %s\n", toolName)
		return nil
	}

	// Merge discovered versions into the tool's version list.
	existing := map[string]bool{}
	for _, v := range t.VersionList {
		existing[v.Version] = true
	}
	added := 0
	for _, v := range versions {
		if !existing[v] {
			t.VersionList = append(t.VersionList, manifest.Version{
				Version: v,
				Stable:  true,
			})
			existing[v] = true
			added++
		}
	}

	// Update the latest field if a newer version was found.
	if len(versions) > 0 {
		t.Latest = versions[0]
	}

	// Write the updated manifest back to the local cache.
	cfg := Cfg()
	manifestDir := cfg.Core.ManifestDir
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	outPath := filepath.Join(manifestDir, toolName+".yaml")
	data, err := manifest.MarshalTool(t)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := util.AtomicWriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf("✔ %s: %d versions found, %d new → %s\n", toolName, len(versions), added, outPath)
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
	cat := loadCatalog()
	var tools []*manifest.Tool
	for _, t := range cat.Tools() {
		if t.Remote.Type != "" && t.Remote.Type != "none" {
			tools = append(tools, t)
		}
	}
	if len(tools) == 0 {
		fmt.Println("No tools with remote sources found.")
		return nil
	}
	fmt.Printf("Refreshing %d tools...\n", len(tools))
	sem := make(chan struct{}, 3) // concurrency limiter
	var wg sync.WaitGroup
	for i, t := range tools {
		wg.Add(1)
		go func(idx int, toolName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fmt.Printf("[%d/%d] Refreshing %s...\n", idx+1, len(tools), toolName)
			if err := refreshOne(cmd, toolName); err != nil {
				fmt.Fprintf(os.Stderr, "⚠ %s: %v\n", toolName, err)
			}
		}(i, t.Name)
	}
	wg.Wait()
	fmt.Println("✔ Refresh complete.")
	return nil
}
