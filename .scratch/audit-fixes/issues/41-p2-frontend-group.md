# 41 P2 前端组

Status: resolved
Type: task
批次: B5
对应修复项: P2-24(G-07) + P2-25(G-08)（方案 §6）

## 方案要点

1. **P2-07 完成计数**：InstallProgressDialog.vue:60 `filter((r) => !r.active)` → `filter((r) => !r.active && !r.error)`（或标题拆「完成/失败」双计数）——修复 "5/5" 与红色 error 行矛盾。
2. **P2-08 CommandPalette**：show watch 的 visible 分支 `query.value = ''`（打开即清空）——修复 Esc/背景关闭后重开显示旧过滤结果。

## 守卫测试

vitest：进度框含失败行时计数正确；palette 重开后无旧过滤残留；e2e 无回归。

## Comments（Batch 5 实施记录）

- 改动：
  - **P2-24**（`frontend/src/components/InstallProgressDialog.vue`）— `completedCount` 过滤条件改 `filter((r) => !r.active && !r.error)`：失败行不再计入 `(N/M)` 完成数（原先会出现「5/5」与红色 error 行并存的矛盾显示）。
  - **P2-25**（`frontend/src/components/CommandPalette.vue`）— `show` watch 的 visible 分支清空 `query.value`（Esc/背景关闭后重开不再残留旧过滤结果）。
- 守卫测试：`InstallProgressDialog.spec.js`（+1：多行模式下失败行不计入 `.batch-count`，断言 `(1/3)` 含 1 成功 1 失败 1 进行中，另有 `(1/2)` 正向对照与 error 行确实渲染）；`CommandPalette.spec.js`（+1：重开后 query 为空且不显示旧过滤结果）。
- **红线先红后绿**：P2-24 红 —`expected '(2/3)' to be '(1/3)'`（失败行被计入完成数）；P2-25 红 —`expected 'settings' to be ''`（旧过滤词残留）。还原后均绿，无残留探针。
- 验证：全量 vitest 36 文件 / 236 用例全绿；`npx vite build` 成功；e2e 21/21 无回归。
