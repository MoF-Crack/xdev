package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/manifest"
)

// newManifestIndexCmd 实现 `xdev manifest index`（票 18 / P2-15）：
// 依据 manifests/*.yaml 重新生成 index.yaml。
//
// 为什么需要它：index.yaml 手工维护必然漂移——实测（2026-09-12）它只收录 37 个
// 工具（磁盘上有 52 个）、日期停在 2026-06-05。守卫（manifests 完整性守卫）会断言
// 覆盖率与引用完整性，本命令是修复漂移的唯一入口，保证「生成端与守卫端同源」。
func newManifestIndexCmd() *cobra.Command {
	var (
		manifestsDir string
		outPath      string
		date         string
		check        bool
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Regenerate manifests/index.yaml from the manifest files",
		Long: `Regenerate the tool index (manifests/index.yaml) from the tool manifests.

The index lists every tool grouped by category. It must stay in sync with the
manifest files: the manifests-integrity guard fails when a tool is missing from
the index, when an entry points at a non-existent manifest, or when the
recorded date is in the future.

Use --check to verify without writing (exit code 1 when regeneration would
change the file).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestsDir == "" {
				manifestsDir = "manifests"
			}
			if outPath == "" {
				outPath = filepath.Join(manifestsDir, "index.yaml")
			}
			gen := time.Now().UTC()
			if date != "" {
				parsed, err := time.Parse("2006-01-02", date)
				if err != nil {
					return fmt.Errorf("--date must be YYYY-MM-DD: %w", err)
				}
				gen = parsed
			}

			tools, err := loadToolManifests(manifestsDir)
			if err != nil {
				return err
			}
			content, err := manifest.BuildIndexYAML(tools, gen)
			if err != nil {
				return err
			}

			if check {
				existing, err := os.ReadFile(outPath)
				if err != nil {
					return fmt.Errorf("read %s: %w", outPath, err)
				}
				if !indexEquivalent(existing, content) {
					return fmt.Errorf("%s is out of date — run `xdev manifest index` to regenerate", outPath)
				}
				fmt.Fprintf(os.Stdout, "✅ %s is up to date (%d tools)\n", outPath, len(tools))
				return nil
			}

			if err := os.WriteFile(outPath, content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", outPath, err)
			}
			fmt.Fprintf(os.Stdout, "✅ wrote %s (%d tools, generated %s)\n",
				outPath, len(tools), gen.Format("2006-01-02"))
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "manifests", "directory holding tool manifests")
	cmd.Flags().StringVar(&outPath, "out", "", "output path (default: <manifests-dir>/index.yaml)")
	cmd.Flags().StringVar(&date, "date", "", "generated date YYYY-MM-DD (default: today UTC)")
	cmd.Flags().BoolVar(&check, "check", false, "only verify that the index is up to date")
	return cmd
}

// loadToolManifests 读取目录下的工具 manifest（跳过 index.yaml 等无 name 的非工具 YAML）。
// 实现委托给 manifest.LoadToolManifests：生成端与守卫端共用同一解析逻辑。
func loadToolManifests(dir string) ([]*manifest.Tool, error) {
	return manifest.LoadToolManifests(dir)
}

// indexEquivalent 比较两份索引内容，忽略生成日期差异（--check 不应因日期不同而失败）。
func indexEquivalent(existing, regenerated []byte) bool {
	a, err := manifest.ParseIndexYAML(existing)
	if err != nil {
		return false
	}
	b, err := manifest.ParseIndexYAML(regenerated)
	if err != nil {
		return false
	}
	aNames, bNames := a.ManifestNames(), b.ManifestNames()
	sort.Strings(aNames)
	sort.Strings(bNames)
	if len(aNames) != len(bNames) {
		return false
	}
	for i := range aNames {
		if aNames[i] != bNames[i] {
			return false
		}
	}
	// 分类归属也必须一致（同一工具被挪到别的分类也算过期）。
	aCat := map[string]string{}
	for _, c := range a.Categories {
		for _, t := range c.Tools {
			aCat[t.Name] = c.Name
		}
	}
	for _, c := range b.Categories {
		for _, t := range c.Tools {
			if aCat[t.Name] != c.Name {
				return false
			}
		}
	}
	return true
}
