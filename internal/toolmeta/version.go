package toolmeta

import "strings"

// versionPrefixes 是版本输出中的可识别前缀集合（全部小写）。
// 取 env/scan_local.go 与 gui/app_local_detect.go 两处前缀列表的并集：
//   - env 独有：java / javac / python / node
//   - gui 独有：cmake version / gnu make / apache maven / apache ant(tm) version /
//     dart sdk version: / git version / (gcc) / version / npm / pip / yarn / pnpm / gradle
//
// 并集的语义：只增加可识别前缀，不会减少任何一侧的可解析输入。
var versionPrefixes = []string{
	"v", "go", "java ", "javac ", "python ", "node ", "npm ",
	"yarn ", "pnpm ", "gradle ", "pip ",
	"cmake version ", "gnu make ", "apache maven ", "apache ant(tm) version ",
	"dart sdk version: ", "git version ", "(gcc) ", "version ",
}

// ExtractVersion 从字符串中提取版本号（gui 版逻辑，spec R3 并集收口）。
// 例： "jdk-17.0.2" → "17.0.2"；"2.47.0.windows.1" → "2.47.0"。
func ExtractVersion(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range versionPrefixes {
		if strings.HasPrefix(strings.ToLower(s), prefix) && len(s) > len(prefix) {
			rest := s[len(prefix):]
			if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				s = rest
				break
			}
		}
	}

	// 查找第一个版本号模式
	parts := strings.Fields(s)
	for _, p := range parts {
		if IsVersionLike(p) {
			if stripped := StripWindowsSuffix(p); stripped != "" {
				return stripped
			}
			return p
		}
	}
	return ""
}

// ExtractVersionFromOutput 从工具输出中解析版本号（gui 版逻辑）：
//  1. 逐行尝试 ExtractVersion，返回第一个成功提取的版本（跳过空行）；
//  2. 没有任何行可提取时，回退到旧行为：整个输出的首行原文（截断 60）。
//
// 该函数独立以便单元测试；被 env/gui 的版本探测及迁移/刷新路径共用。
func ExtractVersionFromOutput(output string) string {
	text := strings.TrimSpace(output)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ver := ExtractVersion(line); ver != "" {
			return ver
		}
	}
	// 回退：首行原文（截断 60）
	first := text
	if idx := strings.Index(first, "\n"); idx > 0 {
		first = first[:idx]
	}
	first = strings.TrimSpace(first)
	if len(first) > 60 {
		first = first[:60] + "..."
	}
	return first
}

// StripWindowsSuffix 移除 Windows 风格版本后缀 ".windows.1"，
// 如 "2.47.0.windows.1" → "2.47.0"；无后缀则原样返回。
func StripWindowsSuffix(s string) string {
	idx := strings.Index(s, ".windows.")
	if idx > 0 {
		return s[:idx]
	}
	return s
}

// IsVersionLike 判断字符串是否形如版本号：
// 允许 v/go 前缀、1-3 个点、.windows. 后缀；必须以数字开头。
func IsVersionLike(s string) bool {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(strings.ToLower(s), "go")
	if idx := strings.Index(s, ".windows."); idx > 0 {
		s = s[:idx]
	}
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return false
	}
	dots := strings.Count(s, ".")
	return dots >= 1 && dots <= 3
}