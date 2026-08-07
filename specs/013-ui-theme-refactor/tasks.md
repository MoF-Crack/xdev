# Tasks 013: UI 主题重构——三套配色方案

> **依据**：`specs/013-ui-theme-refactor/plan.md`

## Phase 1: 主题架构重构

- [ ] T1.1 `theme.css`：三套 `[data-theme="light|dark|eye-care"]` 配色方案 + 语义色 + shadow
- [ ] T1.2 `naive-theme.js`：`getNaiveThemeOverrides(themeName)` 支持三套，返回 `{ theme, overrides }`
- [ ] T1.3 `App.vue`：`isDark` → `themeName`，loadTheme 迁移旧布尔值，applyTheme 设 data-theme

## Phase 2: 主题选择器 + localStorage 迁移

- [ ] T2.1 `ThemeSettings.vue`：checkbox → 三选一按钮组带色块预览
- [ ] T2.2 `AppContent.vue`：`toggleTheme(boolean)` → `setTheme(string)`，prop/emit 更新
- [ ] T2.3 `SettingsPage.vue`：`:is-dark` → `:theme`，`@update:is-dark` → `@update:theme`

## Phase 3: 硬编码颜色清理

- [ ] T3.1 `CommandPalette.vue`、`InstallProgressDialog.vue`、`ConfirmAction.vue` 硬编码颜色替换
- [ ] T3.2 全局搜索确认无遗漏

## Phase 4: 构建验证

- [ ] T4.1 `npm run build` + `npm run test:unit` 通过
- [ ] T4.2 `wails build` 通过
- [ ] T4.3 交付用户实测三主题切换