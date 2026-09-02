# 01 — N1+N3: TUI 更新视图 errMsg 渲染 + 死代码删除

Status: done
Spec: 036（N1/N3，P2）
Files: `internal/tui/updates.go`、`internal/tui/tui_test.go`
Blocks: none

## 改动（最小）

1. **N1**：`updatesViewString` 在页脚 `[a]/[r]/[Esc]` 行之前内嵌 errMsg（**不带** "(press Esc to dismiss)"——updates 视图 Esc 是退视图非清 errMsg）：
```go
if m.errMsg != "" {
    sb.WriteString("\n" + StyleError.Render(fmt.Sprintf("  Error: %s", m.errMsg)) + "\n")
}
```
复用既有 `StyleError`（styles.go:186）；fmt 已 import。
2. **N3**：删除 `updates.go:133-135` 的 `if m.updateQueue != nil { 更新中: N/M }` 块——F4 替换渲染后不可达（installState 清空路径都同步清队列），批量进度由 overlay `Batch N/M` 行（install.go）单一承担。

## 验收

- [ ] TC-N1-01：`newTUITestModel` + `updatesView=true` + `errMsg="boom"` → `View()` 输出含 `Error: boom`
- [ ] TC-N1-02：errMsg 为空时输出不含 `Error:`
- [ ] `go test ./internal/tui -count=1` 全绿（N3 无专属用例：既有 TC-F4-02 Batch 行断言防退化 + 代码走查）
- [ ] `git diff` 确认 updates.go 仅两处改动（增 errMsg 块 + 删死代码块）
