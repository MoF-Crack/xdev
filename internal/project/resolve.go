package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xmdemt/xdev/internal/resolver"
	"github.com/xmdemt/xdev/internal/util"
)

// ResolveInstalled 把项目声明的版本规格解析成**已安装**的具体版本目录名。
//
// # 关键约束：只在本机已安装集合上解析
//
// 这里刻意**不联网、不查 manifest 的完整版本表**，只在
// <installRoot>/<tool>/ 下的实际目录里挑。原因有三：
//
//  1. shim 是热路径（每次执行任何命令都会跑），不能有网络 I/O；
//  2. 语义更诚实：项目声明 3.11 但本机没装时，正确行为是**回落全局
//     current 并提示**，而不是假装能切换到一个不存在的版本；
//  3. 避免误触发下载 —— 版本管理器不该在用户执行 `python script.py`
//     时突然开始下载东西。
//
// 返回 (版本目录名, true) 或 ("", false)。
func ResolveInstalled(installRoot, tool, spec string) (string, bool) {
	if tool == "" || spec == "" {
		return "", false
	}
	toolDir := filepath.Join(installRoot, tool)

	// ListVersionDirNames 已过滤掉非版本目录
	// （gopath / export_templates / unsafe-version-blocked 等）。
	versions, err := util.ListVersionDirNames(toolDir)
	if err != nil || len(versions) == 0 {
		return "", false
	}

	sp, err := resolver.Parse(spec)
	if err != nil {
		return "", false
	}

	// 精确匹配：直接看目录是否存在，不走范围逻辑
	// （否则 "3.12.7" 会被当范围处理，语义不符）。
	if sp.Kind == resolver.KindExact {
		for _, v := range versions {
			if v == sp.Raw || v == spec {
				return v, true
			}
		}
		return "", false
	}

	// latest / lts / range：在已安装集合上取最高匹配。
	// latest/lts 需要调用方传入具体值；这里没有远端信息，
	// 传空串让 resolver 走「available 里的最高者」分支。
	if resolved, err := resolver.Resolve(sp, versions, "", ""); err == nil && resolved != "" {
		for _, v := range versions {
			if v == resolved {
				return v, true
			}
		}
	}

	// resolver 未命中时的兜底：按数值降序取第一个满足前缀的。
	// 覆盖 resolver 对非标准版本串（如 "21.0.1+9"）解析失败的情况。
	return fallbackMatch(versions, spec)
}

// fallbackMatch 在 resolver 解析失败时做保守匹配。
//
// 只处理「规格是版本前缀」这一种情况（3.11 → 3.11.x），
// 其余一律不匹配 —— 宁可不切版本，也不要切到用户没预期的版本。
func fallbackMatch(versions []string, spec string) (string, bool) {
	spec = strings.TrimSpace(strings.TrimPrefix(spec, "v"))
	if spec == "" {
		return "", false
	}
	var matched []string
	for _, v := range versions {
		if v == spec || strings.HasPrefix(v, spec+".") {
			matched = append(matched, v)
		}
	}
	if len(matched) == 0 {
		return "", false
	}
	sort.Slice(matched, func(i, j int) bool {
		return util.CompareSemver(matched[i], matched[j]) > 0
	})
	return matched[0], true
}

// DirFor 返回某个工具版本的安装目录（绝对路径）。
// 调用方用它拼 current 的等价路径与 envctx 的 installPath。
func DirFor(installRoot, tool, version string) string {
	return filepath.Join(installRoot, tool, version)
}

// Exists 报告某个工具版本目录是否真实存在。
func Exists(installRoot, tool, version string) bool {
	fi, err := os.Stat(DirFor(installRoot, tool, version))
	return err == nil && fi.IsDir()
}
