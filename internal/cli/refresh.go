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

	// Update the latest field **only when it advances**.
	//
	// F-29（P1）：修复前是无条件 `t2.Latest = versions[0]`，而
	// t2.Versions() 返回的是 manifest.VersionList 的**磁盘顺序**
	// （LoadToolManifests 只按文件名排序工具、不按版本新旧排序，
	// 见 internal/manifest/index.go:91）——磁盘上版本列表若不是降序
	// （手工编辑、合并遗留、外部工具写入），这一行就会把 latest
	// **静默降级**成列表里的第一个（往往是旧版本），进而让
	// `xdev install <tool>`（默认版本 = latest）装回旧版本。
	//
	// 现在：newestVersion 走 manifest.SortVersionsByRelease 的**全序**
	// （semver 主键 → 发布日期次键 → 原串末键，票 27 的实现），
	// shouldUpdateLatest 再做一次「严格更高才写」的防线。
	if candidate := newestVersion(t2.VersionList); shouldUpdateLatest(candidate, t2.Latest) {
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

// newestVersion 返回 versions 里「最新」的那个版本串（空列表返回 ""）。
//
// F-29（P1）：判据是 manifest.SortVersionsByRelease 的**全序**（票 27：
// semver 主键 → 发布日期次键 → 原串末键），而不是磁盘顺序——旧 refresh 用
// `versions[0]` 取最高版本，在磁盘列表非降序时会把 latest 静默降级。
//
// 在副本上排序，不改动调用方的切片（refresh 随后还要用它打印预览）。
func newestVersion(versions []manifest.Version) string {
	if len(versions) == 0 {
		return ""
	}
	sorted := make([]manifest.Version, len(versions))
	copy(sorted, versions)
	manifest.SortVersionsByRelease(sorted)
	return sorted[0].Version
}

// shouldUpdateLatest 报告 candidate 是否应当**替换**当前的 latest 值。
//
// F-29（P1）：只在严格更高时才写回。两个作用：
//  1. 杜绝静默降级（候选低于现值时保持现值）；
//  2. 候选与现值相同时也不写回——顺便保住 manifest 的注释：
//     写回走的是整文件 manifest.MarshalTool（会丢注释并重排格式），
//     少写一次就少一次无谓的格式破坏。
func shouldUpdateLatest(candidate, current string) bool {
	if candidate == "" {
		return false
	}
	if current == "" {
		return true
	}
	return util.CompareSemver(candidate, current) > 0
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
