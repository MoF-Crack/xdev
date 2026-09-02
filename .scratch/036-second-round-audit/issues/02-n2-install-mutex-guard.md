# 02 — N2: 安装互斥守卫（收敛点 + 批量三层防护）

Status: done
Spec: 036（N2，P2）
Files: `frontend/src/AppContent.vue`、`frontend/src/composables/useBatchUpdates.js`、`frontend/src/i18n/locales/en.ts`、`frontend/src/i18n/locales/zh.ts`、`frontend/test/composables/useBatchUpdates.spec.js`
Blocks: none（03 与本卡同批前端改动，先做本卡）

## 改动

1. **收敛点守卫（AppContent）**：`startInstallConfirmed`（:956）与 `startRepairConfirmed`（:979）开头：
```js
if (installing.value) {
  message.warning(t('install.busy'));
  return;
}
```
覆盖全部入口（行按钮/弹层/抽屉/版本管理器/形态选择器/修复/unverified 确认）。批量适配器（:295）不受影响：批量串行，适配器调用时 active 必为 false。
2. **useBatchUpdates 入口守卫**：签名增 `active`（useInstall 的 active ref）；`installAllUpdates` 开头 `if (active.value) { message.warning(t('install.busy')); return; }`
3. **逐项预检**：循环每项 `installConfirmed` 前 `if (active.value) { cancelled = true; break; }`（用户经 Run in Background 后门插入单项安装时批量让位，避免逐项撞守卫弹 N 条警告）。
4. **started=false 不关进度框**：既有 `if (!started) { failed++; setShowProgress(false); continue; }` 删 `setShowProgress(false)`（finally 兜底）。
5. **i18n**：`install.busy`（en: "An installation is already in progress" / zh: "已有安装进行中"）。
6. AppContent 装配处传入 `active: installing`。

## 验收

- [ ] TC-N2-01：active=true 时 installAllUpdates 早退——dialog 零调用、message.warning（install.busy）被调、installConfirmed 零调用（spec）
- [ ] TC-N2-02：installConfirmed 返回 false（IPC 错误路径）→ failed++ 且 setShowProgress 序列中该项后无 false（spec；finally 的最终 false 除外）
- [ ] TC-N2-03：第 1 项 completed 后置 active=true（模拟用户插入单项安装）→ 第 2 项 installConfirmed 零调用 + message.warning(install_all_cancelled)（spec）
- [ ] AppContent 收敛点守卫无 mount 基建：代码走查 + MS-N2 冒烟（后台运行中点单项安装 → busy 提示而非静默）
- [ ] `npx vitest run` 全绿；`npx vite build` 通过
