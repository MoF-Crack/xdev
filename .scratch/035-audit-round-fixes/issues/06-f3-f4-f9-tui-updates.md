# 06 — F3+F4+F9: TUI 更新队列清理 + 视图遮蔽 + help 键

Status: done
Spec: 035（F3/F4/F9，P1/P1/P2）
Files: `internal/tui/app.go`、`internal/tui/updates.go`、`internal/tui/help.go`、`internal/tui/tui_test.go`
Blocks: none（F4 交互连贯依赖 F3 的取消清队列，同 ticket 内先做 F3）

## 改动（最小）

### F3 三处队列清理
1. `app.go` InstallResultMsg Err 分支（:265-268）：return 前 `m.updateQueue = nil; m.updateIdx = 0`。
2. `app.go` `handleInstallOverlayKey`（:485-494）：取消安装（Esc/c）时同样清。
3. `updates.go` `startToolUpdate` registry 未命中分支（:81-85）：清（含 updateIdx；该分支无后续 InstallResultMsg，必须原地清）。

### F4 键序 + 渲染
1. `app.go` Update()：installState 判定（:221-224）移到 updatesView 判定（:216-219）之前——安装中 Esc/c 先取消安装；取消后（F3 已清队列）再 Esc 退出视图。
2. `app.go` View() updatesView 分支（:302-305）：
```go
if m.updatesView {
    if m.installState != nil {
        // 既有 viewInstallOverlay 忽略 base（与主视图 overlay 行为一致，替换渲染）。
        return m.viewInstallOverlay(m.updatesViewString())
    }
    return m.updatesViewString()
}
```
3. `install.go` `viewInstallOverlay`：**install.go:144 的 `+"\n\n"` 保持不变**，在 144 与 145 之间插入批量行（updateQueue==nil 时输出与现状逐字节一致——普通单装不受影响）：
```go
b.WriteString(StyleBold.Render(fmt.Sprintf("  Installing %s@%s", s.Tool, s.Version)) + "\n\n")
if m.updateQueue != nil {
    b.WriteString(StyleDim.Render(fmt.Sprintf("  Batch %d/%d", m.updateIdx+1, len(m.updateQueue))) + "\n")
}
b.WriteString(fmt.Sprintf("  %s  %s\n", s.Phase.String(), bar))
```

### F9
`help.go` 键表 `{"r", ...}` 之后插 `{"x", "Available updates (sniff)"}`。

## 验收

- [ ] `go test ./internal/tui -count=1` 全绿，含新守卫：
  - Err 后 `updateQueue == nil`；取消键后同；registry 未命中后同
  - updatesView=true 且 installState!=nil 时按 Esc 走取消安装（不退出视图）
  - 批量安装中 `View()` 含 `Batch 2/5`；普通单装 View 无 `Batch`
- [ ] 手动冒烟（TUI）：x 进更新视图 → a 开始批量 → overlay 可见（进度 + Batch N/M）→ Esc 取消 → 队列不再续跑 → Esc 退出视图
