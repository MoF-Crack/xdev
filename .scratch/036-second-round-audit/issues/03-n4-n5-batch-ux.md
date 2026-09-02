# 03 — N4+N5: 批量进度框闪烁 + 取消双 toast

Status: done
Spec: 036（N4/N5，P3）
Files: `frontend/src/AppContent.vue`、`frontend/src/composables/useBatchUpdates.js`、`frontend/test/composables/useBatchUpdates.spec.js`
Blocked by: 02（useBatchUpdates 签名同卡改动，先做 02 再做本卡）

## 改动

1. **batchActive ref（AppContent）**：`const batchActive = ref(false);` 传入 useBatchUpdates。
2. **useBatchUpdates**：循环开始 `batchActive.value = true`；`finally { batchActive.value = false; setShowProgress(false); }`（与 02 的 finally 共用）。
3. **handleInstallComplete（AppContent:1000）**：`showProgress.value = false;` 改 `if (!batchActive.value) showProgress.value = false;`（message.success + refreshInstallViews 保留——逐项成功提示可接受）。
4. **handleInstallCancel（AppContent:1008）**：同加 `if (!batchActive.value)` 守卫。
5. **cancelDownload（AppContent:1019）**：`message.warning(t('download_cancelled'))` 改 `if (!batchActive.value) message.warning(t('download_cancelled'))`（单装路径不变）。

## 验收

- [ ] TC-N4-01：batchActive 循环期间 true、finally 后 false（spec；watch batchActive 调用序列）
- [ ] TC-N4-02：2 项 completed → setShowProgress 序列无 false→true 相邻抖动（spec）
- [ ] TC-N5-01：批量中 cancel → message.warning 仅一次（install_all_cancelled），download_cancelled 未被调（AppContent 无 mount 基建：代码走查 + MS-N5 冒烟）
- [ ] MS-N5：GUI 嗅探≥2 更新 → 全部更新 → 进度框 Cancel → 仅一条取消文案
- [ ] `npx vitest run` 全绿；`npx vite build` 通过
