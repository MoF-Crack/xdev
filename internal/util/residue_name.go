package util

import "strings"

// residueNameMarkers 是 xdev 自己产生的旁路/临时名字标记。
//
// 第十轮审计 P3-8：这些条目过去会被 `ReadDir(current)` 的目录扫描当成
// **一个工具**（实测 `[tool tool.stale-demo]`）。第十一轮补了回收。
//
// 20261021 审计（B2）：定义从 `internal/shim` 移到 `util`，因为
// `util.LooksLikeVersionDirName` 也需要同一份判据；`shim` 已 import `util`，
// 反向依赖会成环。`shim.IsStaleResidueName` 保留为委托，调用点不变。
//
// 形式是**后缀片段**（`tool.stale-xyz`、`tool.tmp-123`），不是前导点，
// 因此必须用 Contains 而不是 HasPrefix。
var residueNameMarkers = []string{".stale-", ".tmp-"}

// IsResidueName 报告 name 是否为 xdev 自己的旁路/临时条目（大小写不敏感，
// 因为 Windows 对这些名字大小写不敏感）。
func IsResidueName(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range residueNameMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
