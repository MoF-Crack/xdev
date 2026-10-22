# 08 i18n 7 键 + phaseKeyMap + toast

Status: resolved
Type: task
批次: B1
对应修复项: P1-36（方案 §5 主题 F）

## 问题

frontend/src/i18n/locales/en.ts + zh.ts 缺 7 键：settings.check_update、ai.command、ai.available、ai.unavailable、favorites.details、favorites.query_failed、custom_tools.preview_failed（复核亲验双文件零命中且全部在用：AppContent.vue:431/:1409、AboutPage、DashboardPage、IsolationPage、FavoritesPage）→ 原始键名上屏 + aria-label 损坏。

## 方案

1. 两文件补 7 键（About 按钮复用 btn.check_update 文案亦可）。
2. InstallProgressDialog phaseKeyMap 补 Cancelled/Paused/error 映射。
3. useSniffSession 硬编码 toast 改 t()。
4. i18n.spec.js 增加全量 t() 键扫描守卫（可能暴露更多缺键——先红后补，属预期）。

## 验收

vitest i18n 相关全绿；扫描守卫通过；e2e 无回归。

## Comments（Batch 1 实施记录）

- 改动：`frontend/src/i18n/locales/en.ts` + `zh.ts` 补齐 7 键（settings.check_update 复用 btn.check_update 文案；ai.command/available/unavailable；favorites.details/query_failed；custom_tools.preview_failed）+ 6 个 useSniffSession toast 键（updates.sniffed_tool/sniff_strategy_save_failed/service_installed/service_install_failed/service_uninstalled/service_uninstall_failed）+ 3 个 phase 键（installing_cancelled/paused/error）。
- `InstallProgressDialog.vue` phaseKeyMap 补 Cancelled/Paused/error/Error → installing_* 映射（useInstall.js 本地设置这三态，原直落显原始英文词）。
- `useSniffSession.js` 7 处硬编码 toast 全部改 t()（含 Sniffed {tool} 插值；'Sniff failed: ' 复用既有 updates.sniff_failed）。
- `test/i18n.spec.js` 新增 I1 全量 t() 字面键扫描守卫（正则扫 src/**/*.{vue,js,ts}，键集 ⊆ en ∪ zh 且 en/zh 相等；动态拼接前缀 `'form.' + f` / `'detail.deps_reason_' + code` 以 `/_|\.$/` 结尾特征显式豁免并文档化——其全部展开键已存在）。扫描实际暴露的缺键恰为 7 键（补齐后绿），未发现 7 之外的缺键。
- 守卫测试：I1 = i18n.spec.js "resolves every static t() literal key..."；I2 = 键集 parity 守卫自动覆盖（16 新键双语言）；I3 = InstallProgressDialog.spec.js 新增 it.each 三态（Cancelled/Paused/error → installing_cancelled/paused/error）；I4 = 全量 vitest 215 用例 + e2e 21/21 无回归。
