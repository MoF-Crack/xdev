package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Index 描述 manifests/index.yaml —— 内置工具目录索引（供快速列举，
// 无需解析每个 manifest）。
type Index struct {
	Version    int             `yaml:"version"`
	Generated  string          `yaml:"generated"`
	Categories []IndexCategory `yaml:"categories"`
}

// IndexCategory 是索引里的一个分类分组。
type IndexCategory struct {
	Name        string      `yaml:"name"`
	DisplayName string      `yaml:"display_name"`
	Tools       []IndexTool `yaml:"tools"`
}

// IndexTool 是索引里的一条工具条目。
type IndexTool struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Manifest    string `yaml:"manifest"`
}

// indexCategoryOrder 是索引里分类的规范顺序（票 18 / P2-15）：
// 已知分类按此顺序输出（即使某个分类当前没有工具，也保留空分组，
// 以免手工维护的空占位被重新生成时抹掉），未知分类按名称追加在后。
var indexCategoryOrder = []string{
	"language",
	"mobile",
	"build",
	"package-manager",
	"development_tools",
	"system",
}

// indexCategoryDisplay 是分类的展示名。未知分类回退为下划线转空格 + 首字母大写。
var indexCategoryDisplay = map[string]string{
	"language":          "Programming Languages",
	"mobile":            "Mobile Development",
	"build":             "Build Tools",
	"package-manager":   "Package Managers",
	"development_tools": "Development Tools",
	"system":            "System Tools",
}

const indexHeader = `# XDev Tool Index
# This file provides a catalog of all available tools.
# It is used for quick lookup and listing without loading
# every individual manifest file.
`

// LoadToolManifests reads every tool manifest in dir, skipping non-tool YAML
// (index.yaml and anything without a `name`). Results are sorted by tool name.
//
// 生成端（`xdev manifest index`）与守卫端共用本函数，避免两侧各写一份解析逻辑而漂移。
func LoadToolManifests(dir string) ([]*Tool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read manifest dir %s: %w", dir, err)
	}
	var tools []*Tool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || e.Name() == "index.yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var t Tool
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		if strings.TrimSpace(t.Name) == "" {
			continue // 非工具 YAML
		}
		tools = append(tools, &t)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// BuildIndexYAML 由工具清单生成 index.yaml 内容（票 18 / P2-15）。
//
// 规则（确定性，便于守卫与 review）：
//   - 忽略没有名字的条目（index.yaml 自身等非工具 YAML）；
//   - 分类按 indexCategoryOrder 输出，未知分类按名称排序追加；
//   - 每个分类内工具按 name 排序；
//   - manifest 文件名固定为 "<name>.yaml"（与磁盘文件名一致性由守卫断言）；
//   - 日期取传入的 generated（UTC，YYYY-MM-DD），保证可重现。
func BuildIndexYAML(tools []*Tool, generated time.Time) ([]byte, error) {
	byCat := map[string][]IndexTool{}
	for _, t := range tools {
		if t == nil || strings.TrimSpace(t.Name) == "" {
			continue
		}
		cat := strings.TrimSpace(t.Category)
		if cat == "" {
			cat = "system"
		}
		display := t.DisplayName
		if strings.TrimSpace(display) == "" {
			display = t.Name
		}
		byCat[cat] = append(byCat[cat], IndexTool{
			Name:        t.Name,
			DisplayName: display,
			Manifest:    t.Name + ".yaml",
		})
	}

	seen := map[string]bool{}
	var order []string
	for _, c := range indexCategoryOrder {
		order = append(order, c)
		seen[c] = true
	}
	var extra []string
	for c := range byCat {
		if !seen[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	var b strings.Builder
	b.WriteString(indexHeader)
	fmt.Fprintf(&b, "\nversion: 1\ngenerated: %q\n\ncategories:\n", generated.UTC().Format("2006-01-02"))
	for _, cat := range order {
		tools := byCat[cat]
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		fmt.Fprintf(&b, "  - name: %s\n    display_name: %s\n", cat, indexDisplayName(cat))
		if len(tools) == 0 {
			b.WriteString("    tools: []\n")
			continue
		}
		b.WriteString("    tools:\n")
		for _, t := range tools {
			fmt.Fprintf(&b, "      - name: %s\n        display_name: %s\n        manifest: %s\n",
				t.Name, t.DisplayName, t.Manifest)
		}
	}
	return []byte(b.String()), nil
}

// indexDisplayName 返回分类展示名（未知分类回退为可读的标题式名称）。
//
// 第八轮审计 V5：首字母大写必须按 **rune** 切分。旧实现用 `w[:1]`（字节切片）
// 取首字符，对多字节 UTF-8 会切出半个码位：实测 `indexDisplayName("工具")`
// → `"\ufffd\xb7\xa5具"`（乱码），`"über-build"` → `"\ufffd\xbcber-build"`，
// `"Ωmega"`/`"日本語ツール"` 同样被破坏。分类名可能来自用户手写 manifest，
// 非 ASCII 完全合法。
func indexDisplayName(cat string) string {
	if d, ok := indexCategoryDisplay[cat]; ok {
		return d
	}
	words := strings.Fields(strings.ReplaceAll(cat, "_", " "))
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	if len(words) == 0 {
		return cat
	}
	return strings.Join(words, " ")
}

// ParseIndexYAML 解析 index.yaml 内容（守卫用；生成端与解析端同源，
// 避免守卫自己复制一份结构而漂移）。
func ParseIndexYAML(data []byte) (*Index, error) {
	var idx Index
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index yaml: %w", err)
	}
	return &idx, nil
}

// ManifestNames 返回索引中出现的所有 manifest 文件名（去重后保持首次出现顺序）。
func (idx *Index) ManifestNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range idx.Categories {
		for _, t := range c.Tools {
			if t.Manifest == "" || seen[t.Manifest] {
				continue
			}
			seen[t.Manifest] = true
			out = append(out, t.Manifest)
		}
	}
	return out
}
