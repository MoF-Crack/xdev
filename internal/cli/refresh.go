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

	ranked := append([]manifest.Version(nil), t2.VersionList...)
	manifest.SortToolVersionsByRelease(toolName, ranked)
	versions := make([]string, 0, len(ranked))
	for _, v := range ranked {
		versions = append(versions, v.Version)
	}
	totalCount := len(versions)
	added := totalCount - existingCount
	if added < 0 {
		added = 0
	}

	if totalCount == 0 {
		fmt.Printf("⚠️  no versions discovered for %s\n", toolName)
		return nil
	}

	// Release precedence selects the candidate; promotion also requires a usable archive.
	if candidate := newestToolVersion(toolName, t2.VersionList); shouldUpdateToolLatest(toolName, t2.VersionList, candidate, t2.Latest) {
		previous := t2.Latest
		t2.Latest = candidate
		// 第七轮审计 F-29：写回必须走**文本级外科写**，不能整文件 MarshalTool 重排
		// ——后者会抹掉用户手写 manifest 里的全部注释（与票 17 已确立的
		// 「文本级外科写」原则冲突，见 UpdateArchiveSHA256InFile 的整段论证）。
		manifestDir := cfg.Core.ManifestDir
		outPath := filepath.Join(manifestDir, toolName+".yaml")
		if changed, err := manifest.UpdateLatestInFile(outPath, candidate); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ warning: failed to update Latest field: %v\n", err)
		} else if changed {
			fmt.Printf("  latest: %s -> %s\n", previous, candidate)
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

// newestVersion retains the generic numeric scheme for existing callers.
func newestVersion(versions []manifest.Version) string {
	return newestToolVersion("", versions)
}

func newestToolVersion(tool string, versions []manifest.Version) string {
	values := make([]string, 0, len(versions))
	for _, v := range versions {
		values = append(values, v.Version)
	}
	latest, _ := manifest.NewestToolVersion(tool, values)
	return latest
}

// shouldUpdateLatest retains the generic numeric scheme for existing callers.
func shouldUpdateLatest(versions []manifest.Version, candidate, current string) bool {
	return shouldUpdateToolLatest("", versions, candidate, current)
}

func shouldUpdateToolLatest(tool string, versions []manifest.Version, candidate, current string) bool {
	// refresh 的候选来自“已发现版本”列表，预发布处理与 scheduler 一致：
	// 稳定 latest 不被预发布劫持，同一预发布通道内允许推进。
	allowPre := current == "" ||
		(manifest.IsPrerelease(current) &&
			util.PrereleaseChannel(candidate) == util.PrereleaseChannel(current))
	return manifest.CanPromoteToolLatest(tool, versions, candidate, current, allowPre)
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
