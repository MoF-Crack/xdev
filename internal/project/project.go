// Package project 实现项目级工具版本声明。
//
// # 为什么需要它
//
// xdev 传统上是「全局单激活」模型：shim.Lookup 只查 $XMDEV_HOME/current/<alias>，
// CWD 完全不参与解析。于是 A 项目要 python 3.11、B 项目要 3.12 时，
// 用户只能来回 `xdev use` —— 这是「版本管理器」最核心价值的缺失。
//
// 本包提供项目级声明：从 CWD 向上找到配置文件，解析出 tool → version，
// 由 shim 优先按它解析，未命中时回落到全局 current。
//
// # 配置文件格式
//
// 文件名 `.xdev.yaml`（也接受 `.xdev.yml`），从 CWD 逐级向上查找：
//
//	# 项目根/.xdev.yaml
//	tools:
//	  python: "3.11"
//	  node: "^20"
//	  go: latest
//
// 版本值直接复用 internal/resolver 的语义（latest / lts / 3.12 / ^3.12 / ~3.11）。
//
// # 为什么用 YAML 而不是 TOML
//
// 历史上的 `.xdev.toml` 从未真正实现（Spec 012 删掉了 internal/project 与
// internal/lockfile，cli/hook.go 的 hook 命令退化为 `return nil`）。
// 重新实现时选 YAML 是因为仓库已经依赖 gopkg.in/yaml.v3（manifest 在用），
// 而引入 TOML 需要新增依赖并同步整个 vendor/ 目录 —— 对「加一个配置文件」
// 这个需求来说代价不成比例。若将来确需 TOML，在 parseFile 里按扩展名分派即可。
//
// # 安全边界
//
// 项目配置只影响**版本选择**，不引入任何新的代码执行路径：
//   - 声明的版本必须是**已安装**的（解析在已安装集合上做，不联网）
//   - 未安装时 shim 回落到全局 current 并在 stderr 提示一次
//   - 配置文件向上查找的深度有上限（maxWalkDepth），避免异常深的目录树
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileName 是项目配置的默认文件名。
const ConfigFileName = ".xdev.yaml"

// altConfigFileNames 是同样接受的别名（按优先级排列）。
var altConfigFileNames = []string{".xdev.yml"}

// maxWalkDepth 限制向上查找的层数。
//
// 不设上限时，从很深的目录（如 node_modules 内部）向上 walk 到盘根
// 会做几十次 Stat；虽然每次很便宜，但 shim 是热路径，值得设个界。
// 64 层足以覆盖任何正常项目结构。
const maxWalkDepth = 64

// Config 是解析后的项目配置。
type Config struct {
	// Root 是配置文件所在目录（项目根）。
	Root string

	// Path 是配置文件的绝对路径。
	Path string

	// Tools 是 tool 名 → 版本规格（原始字符串，尚未解析成具体版本）。
	Tools map[string]string
}

// ErrNotFound 表示从给定目录向上没有找到任何项目配置。
var ErrNotFound = errors.New("no project config found")

// Find 从 startDir 向上查找项目配置。
//
// 找不到时返回 ErrNotFound（调用方据此回落到全局 current，
// 这是**正常**路径，不是错误 —— 绝大多数调用都该走这条）。
func Find(startDir string) (*Config, error) {
	if startDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		startDir = wd
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	dir := abs
	for depth := 0; depth < maxWalkDepth; depth++ {
		for _, name := range append([]string{ConfigFileName}, altConfigFileNames...) {
			candidate := filepath.Join(dir, name)
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				cfg, err := parseFile(candidate, dir)
				if err != nil {
					return nil, err
				}
				return cfg, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // 到达盘根
		}
		dir = parent
	}
	return nil, ErrNotFound
}

// parseFile 读取并解析一个配置文件。
func parseFile(path, root string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw struct {
		Tools map[string]string `yaml:"tools"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// 规范化：工具名小写、版本去空白、空条目丢弃。
	// 不在这里校验版本语法 —— 非法值由 resolver.Parse 在解析阶段报错，
	// 那样错误信息能带上工具名，比在这里泛泛地说「格式不对」有用。
	tools := make(map[string]string, len(raw.Tools))
	for name, spec := range raw.Tools {
		name = strings.ToLower(strings.TrimSpace(name))
		spec = strings.TrimSpace(spec)
		if name == "" || spec == "" {
			continue
		}
		tools[name] = spec
	}

	return &Config{Root: root, Path: path, Tools: tools}, nil
}

// ToolNames 返回配置中声明的工具名（排序，便于稳定输出/测试）。
func (c *Config) ToolNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.Tools))
	for n := range c.Tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Spec 返回某个工具的版本规格；未声明时 ok=false。
func (c *Config) Spec(tool string) (string, bool) {
	if c == nil {
		return "", false
	}
	spec, ok := c.Tools[strings.ToLower(strings.TrimSpace(tool))]
	return spec, ok
}
