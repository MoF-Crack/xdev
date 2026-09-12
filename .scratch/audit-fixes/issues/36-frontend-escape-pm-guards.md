# 36 前端 Escape + PM 守卫组（P1-37/38）

Status: resolved
Type: task
批次: B5
对应修复项: P1-37(G-05) + P1-38(G-06)（方案 §5 主题 F）

## 方案要点

1. **P1-37 Escape 劫持**：AppContent 的 `@escape` 处理改 `if (showCommandPalette.value) setCommandPaletteVisible(false)`（选 AppContent 侧守卫——保住 AppShell.spec.js:98-103 既有断言；e2e 的 Escape 全部作用于有自处理器的抽屉/对话框，不受影响）。
2. **P1-38 PM 更新 TOCTOU**：updatePackageManager 入口即置位 `pmOperating.value = catalogName`；unverified 分支由 onPositiveClick（转 updatePm，finally 复位）与 onNegativeClick（复位）持有复位权；直连路径 finally 复位。**该弹窗不得开 closable**（ESC/遮罩绕过 onNegativeClick → pmOperating 永不复位——按钮永久锁死）。

## 守卫测试

Esc 各对话框焦点返还契约恢复；慢速 IPC 下连点两次 dialog.warning 只弹一次（含弹窗期间守卫持续）。

## Comments（Batch 5 实施记录）

- 改动：
  - **G-05**（`frontend/src/AppContent.vue` @escape）— 改为 `if (showCommandPalette) setCommandPaletteVisible(false)`：面板未开时不再无条件调用（原逻辑在 `await nextTick()` 后把焦点抢到 Ctrl+K 触发器，破坏自行处理 Escape 的抽屉/对话框的焦点返还）。
  - **G-06**（`frontend/src/composables/usePackageManagers.js`）— 入口 `if (pmOperating.value) return;` 之后**立即**置位 `pmOperating.value = catalogName`（慢 IPC 期间即已守卫）；unverified 分支复位权交弹窗（`onPositiveClick → updatePm`（其 finally 复位）、`onNegativeClick → 复位`）；直连路径与失败路径仍由 updatePm 的 finally 复位（三条复位路径逐条核对）。
  - **偏差（已复核，采纳）**：除票面要求的 `closable: false` 外，额外显式设置 `closeOnEsc: false, maskClosable: false`。依据 naive-ui 2.44 源码：dialog 的这三个属性默认均为 `true`，且 **ESC（Modal.handleEsc）与遮罩点击（handleClickoutside）都走 `doUpdateShow(false)`，不经过 onNegativeClick** —— 只关 closable 仍会让 `pmOperating` 永久锁死（正是票卡复核注记点名的失败模式）。
- 守卫测试：`frontend/test/AppContent.escape.spec.js`（新增 3 例：走模板真实 `@escape` 绑定；面板未开时不抢焦点（红→绿）；面板打开时正常关闭并把焦点还给触发器）；`usePackageManagers.spec.js`（+5 例：慢 IPC 双击只弹一次、弹窗期间守卫持续并含三属性断言、取消释放守卫、直连路径 finally 释放、失败路径释放）；`InstallProgressDialog.spec.js`（+1）；`CommandPalette.spec.js`（+1）。
- **红线先红后绿**：G-05 红 —`does not steal focus for a palette that is not open`（`expected <button> to be <button>`，焦点被抢到 paletteTrigger）；G-06 红 — 3 例失败（`expected '' to be 'uv'`；`onNegativeClick is not a function`），并用一次性 spec 量化旧行为：`GetAvailableVersions=2, dialog.warning=2, pmOperating=""`（双击弹两个对话框且守卫为空中）；修复后 `dialog.warning.mock.calls.length === 1` ✓。临时文件与备份已清理。
- 验证：四个 spec 28 用例全绿；**全量 vitest 36 文件 236 用例全绿**；`npx vite build` 成功；**e2e 21/21**（含 Escape 相关的抽屉焦点、命令面板两例）。
- 附注（非本次引入）：`InstallProgressDialog.spec.js` 的 error 用例会刷出大量 naive-ui/vueuc 的 `[Vue warn]: Wrong type passed as event handler ...` 告警（基线同样存在，会淹没真实告警）——建议后续单独清理，不在本票范围。
