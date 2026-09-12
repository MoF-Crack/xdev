# 36 前端 Escape + PM 守卫组（P1-37/38）

Status: open
Type: task
批次: B5
对应修复项: P1-37(G-05) + P1-38(G-06)（方案 §5 主题 F）

## 方案要点

1. **P1-37 Escape 劫持**：AppContent 的 `@escape` 处理改 `if (showCommandPalette.value) setCommandPaletteVisible(false)`（选 AppContent 侧守卫——保住 AppShell.spec.js:98-103 既有断言；e2e 的 Escape 全部作用于有自处理器的抽屉/对话框，不受影响）。
2. **P1-38 PM 更新 TOCTOU**：updatePackageManager 入口即置位 `pmOperating.value = catalogName`；unverified 分支由 onPositiveClick（转 updatePm，finally 复位）与 onNegativeClick（复位）持有复位权；直连路径 finally 复位。**该弹窗不得开 closable**（ESC/遮罩绕过 onNegativeClick → pmOperating 永不复位——按钮永久锁死）。

## 守卫测试

Esc 各对话框焦点返还契约恢复；慢速 IPC 下连点两次 dialog.warning 只弹一次（含弹窗期间守卫持续）。
