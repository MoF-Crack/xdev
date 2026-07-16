package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

func newAvailableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "available",
		Short: "List every tool supported by xdev",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, reg := loadCatalogAndRegistry()
			tools := reg.All()
			printToolTable(tools, false)
			return nil
		},
	}
}

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search the catalog by name, display name or description",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, reg := loadCatalogAndRegistry()
			matches := reg.Search(args[0])
			if len(matches) == 0 {
				return fmt.Errorf("no tool matches %q", args[0])
			}
			printToolTable(matches, false)
			return nil
		},
	}
}

// printToolTable renders a compact text table without external
// dependencies.  We compute per-column widths to keep the output
// tidy in a normal-sized terminal.
func printToolTable(tools []plugin.Plugin, showStatus bool) {
	headers := []string{"Name", "Display", "Category", "Default", "Description"}
	rows := make([][]string, 0, len(tools))
	for _, t := range tools {
		rows = append(rows, []string{
			t.Name(),
			t.DisplayName(),
			t.Category(),
			t.DefaultVersion(),
			truncate(t.Description(), 50),
		})
	}
	_ = showStatus
	renderTable(headers, rows)
}

// renderTable is a tiny tablewriter replacement.
//
// It computes the width of every column from the widest cell and
// prints headers + rows separated by hyphens.  Right-most column is
// left unaligned so long descriptions can run on.
func renderTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				continue
			}
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	// Cap widths so very long descriptions don't blow the layout.
	for i := range widths {
		if i < len(widths)-1 && widths[i] > 30 {
			widths[i] = 30
		}
	}
	// Header
	printRow(headers, widths)
	// Separator
	parts := make([]string, len(widths))
	for i, w := range widths {
		parts[i] = strings.Repeat("-", w)
	}
	fmt.Println(strings.Join(parts, "  "))
	// Body
	for _, r := range rows {
		printRow(r, widths)
	}
}

func printRow(row []string, widths []int) {
	parts := make([]string, len(widths))
	for i := range widths {
		v := ""
		if i < len(row) {
			v = row[i]
		}
		if len(v) > widths[i] {
			v = v[:widths[i]-1] + "…"
		}
		parts[i] = v
	}
	fmt.Println(strings.Join(parts, "  "))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func loadCatalog() *manifest.Catalog {
	cfg := Cfg()
	cat, err := manifest.LoadCatalogCached(cfg.Core.ManifestDir, cfg.Core.CacheDir)
	if err != nil {
		fmt.Fprintf(stderr(), "warning: failed to load manifests from %s: %v\n",
			cfg.Core.ManifestDir, err)
		cat = &manifest.Catalog{}
	}
	return cat
}

// LoadRegistry creates a plugin registry and registers all built-in
// plugins from the given catalog.  Exported so main.go can use it
// when launching the GUI directly.
func LoadRegistry(cat *manifest.Catalog) *plugin.Registry {
	r := plugin.NewRegistry()
	RegisterAll(r, cat)
	return r
}

func loadRegistry(cat *manifest.Catalog) *plugin.Registry {
	return LoadRegistry(cat)
}
