# 25 loadVersions 三入口过期守卫

Status: resolved
Type: task
批次: B4
对应修复项: P1-22 + N-12（方案 §5 主题 C，复核修订版）

## 问题

AppContent.vue loadVersions（:933-944）无过期守卫（对照 toolDeps :883/:885 有）→ 快速连续打开两个抽屉旧响应覆写，可对 B 工具执行 A 的版本号。【N-12】openFavVersionManager（:444-454）与 openDashVersionManager（:463-474）**同型无守卫**。

## 方案

三入口各在落盘前比对**该页面自身**的展开名（对照 toolDeps 模式）：
- loadVersions → `selectedTool.value?.name === requestedName`
- openFavVersionManager → fav 展开条目比对
- openDashVersionManager → dashVersionExpanded 比对

## 守卫测试

交错 resolve 用例：B 打开后 A 慢响应不再覆写（三个入口各有断言）。

## Comments（Batch 4 实施记录）

- 改动：`frontend/src/AppContent.vue` 三入口落盘前加过期守卫（对照同文件 toolDeps 既有模式）——`loadVersions` 比对 `selectedTool.value?.name === toolName`；`openFavVersionManager` 比对 `favVersionExpanded.value === fav.name`；`openDashVersionManager` 比对 `dashVersionExpanded.value === tool.name`。守卫失败**静默丢弃** + `console.debug`；catch 分支的清零同样受守卫（防止错误路径污染新条目）。函数签名与调用方不变。
- 守卫测试：`frontend/test/AppContent.versionRace.spec.js`（LV1/LV2/LV3 + 正向对照组）——**真 mount AppContent**（shallow + 仅 mock naive-ui 的 useMessage/useDialog + deferred Wails stub，精确编排「B 先回、A 后回」），走真实组件代码路径（而非抽 `shouldApplyVersions` 小函数单测——票面允许两种方案，选集成级更强的一种）。
- **红线先红后绿**：临时把三处守卫置为 `if (false && ...)` 运行 → 3 failed（`expected [{version:'2.0.0'}] / received [{version:'1.0.0'}]`，即过期响应覆盖新工具列表）→ 还原后 4 passed；测试日志可见三次 `[AppContent] discarding stale ... response` 守卫命中。临时探针文件已删除并 grep 确认无残留。
- 验证：新 spec 4 passed；全量 vitest **35 文件 226 用例全绿**（+1 文件/+4 用例）；`npx vite build` 通过。
