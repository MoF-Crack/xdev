# 08 — F2b: useBatchUpdates 批量更新 + AppContent 装配 + i18n

Status: done
Spec: 035（F2 主体，P0）
Files: `frontend/src/composables/useBatchUpdates.js`（新）、`frontend/src/AppContent.vue`、`frontend/src/i18n/locales/en.ts`、`frontend/src/i18n/locales/zh.ts`、`frontend/test/composables/useBatchUpdates.spec.js`（新）
Blocked by: 04（F11 超时 emit 同批）、07（waitForCompletion）

## 改动

### 新 composable（依赖全注入，逻辑与 solution.md F2 v4 伪码一致）

核心循环分支（五值完备）：`completed→done++/loadUpdates`；`cancelled|disposed→cancelled=true,break`；`error|idle→failed++`（idle=事件全丢，保守计失败）；`!started→failed++`。`finally { setShowProgress(false) }` 全路径复位。PreviewInstall 抛错→按 unverified 弹确认（不跳过）；唯一形态自动选定 formFilter；每项 unverified 单独弹确认（FR-018）。

### AppContent.vue
删除原 `installAllUpdates`（:289-310）；装配：
```js
const { installAllUpdates } = useBatchUpdates({
  // 箭头包装使测试注入点显式（Wails 绑定方法不依赖 this，直接传引用亦可——
  // 先例 usePackageManagers.js:94-95 即调用式）；装配处保留包装便于统一 mock。
  previewInstall: (tool, version) => window.go.gui.App.PreviewInstall(tool, version),
  installConfirmed: runInstallConfirmed,          // useInstall 解构新增 waitForCompletion 一并解构
  waitForCompletion,
  dialog, message, t,
  loadUpdates,
  availableUpdates,
  setShowProgress: (v) => { showProgress.value = v },
});
```
`@install-all-updates="installAllUpdates"` 模板绑定不变。

### i18n（en/zh 同步）
- 新增 `btn.skip`（Skip / 跳过）
- 新增 `updates.install_all_summary`（"Updates finished: {done} succeeded, {failed} failed, {skipped} skipped" / "更新结束：成功 {done}，失败 {failed}，跳过 {skipped}"）
- 新增 `updates.install_all_cancelled`（"Update batch cancelled: {done} succeeded, {failed} failed, {skipped} skipped" / "批量更新已取消：成功 {done}，失败 {failed}，跳过 {skipped}"）
- 删除 `updates.install_all_done`（唯一引用随旧函数移除）

## 验收

- [ ] `npx vitest run` 全绿，`useBatchUpdates.spec.js` 分支覆盖：确认→装、拒绝→skip、PreviewInstall 抛错→仍弹确认、唯一形态自动选定、!started→failed、completed→done、error/idle→failed、cancelled→break+warning 文案、disposed→break、finally 复位 showProgress（dialog mock 用 `vi.fn((opts) => opts)` 先例 usePackageManagers.spec.js:20）
- [ ] `npx vite build` 通过
- [ ] 手动冒烟（GUI）：嗅探出 ≥2 更新 → 全部更新 → 逐项确认对话框 → 进度框逐项推进 → 汇总文案；中途取消 → 批量中止 + 取消文案；旧构建（无 PreviewInstall）不整体跳过
