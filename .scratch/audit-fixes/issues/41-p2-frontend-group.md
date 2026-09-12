# 41 P2 前端组

Status: open
Type: task
批次: B5
对应修复项: P2-24(G-07) + P2-25(G-08)（方案 §6）

## 方案要点

1. **P2-07 完成计数**：InstallProgressDialog.vue:60 `filter((r) => !r.active)` → `filter((r) => !r.active && !r.error)`（或标题拆「完成/失败」双计数）——修复 "5/5" 与红色 error 行矛盾。
2. **P2-08 CommandPalette**：show watch 的 visible 分支 `query.value = ''`（打开即清空）——修复 Esc/背景关闭后重开显示旧过滤结果。

## 守卫测试

vitest：进度框含失败行时计数正确；palette 重开后无旧过滤残留；e2e 无回归。
