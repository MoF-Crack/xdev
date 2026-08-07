# Plan 013: UI 主题重构——三套配色方案

> **状态**：待用户批准
> **依据**：`specs/013-ui-theme-refactor/spec.md`

## 执行策略

单一 designer lane 主导，因为配色方案、CSS 变量、naive-ui 映射和主题选择器 UI 需要一致的设计意图，拆分给多个代理会导致风格不统一。

```
Phase 1: 主题架构重构（CSS 变量 + naive-ui 映射 + 主题模型）
    ↓
Phase 2: 主题选择器 UI + localStorage 迁移
    ↓
Phase 3: 硬编码颜色清理 + 视觉一致性
    ↓
Phase 4: 构建验证
```

## Phase 1: 主题架构重构

### 1.1 theme.css — 三套配色方案
- `:root` 保留为 dark 默认值（向后兼容）
- `[data-theme="dark"]` 显式声明深色覆盖
- `[data-theme="light"]` 浅色覆盖（spec 中的蓝色调方案）
- `[data-theme="eye-care"]` 护眼覆盖（暖米黄 + 柔和绿方案）
- 三套各自的语义色（success/warning/error/info）+ soft 变体
- 三套各自的 shadow 值
- 间距/圆角/字体/动效 token 不变（三套共用）

### 1.2 naive-theme.js — 三套映射
- `getNaiveThemeOverrides(themeName)` 接受 `'light'|'dark'|'eye-care'`
- dark → `darkTheme` 基底 + 现有 dark 覆盖
- light → `lightTheme` 基底 + 现有 light 覆盖
- eye-care → `lightTheme` 基底 + 护眼色覆盖（所有 common/Button/Menu/Card/Input/DataTable 等组件 token）
- 返回 `{ theme, overrides }` 二元组，简化 App.vue 逻辑

### 1.3 App.vue — 主题模型重构
- `isDark: ref(boolean)` → `themeName: ref<'light'|'dark'|'eye-care'>`
- `loadTheme()`：读取 localStorage，迁移旧布尔值（`true`→`'dark'`，`false`→`'light'`）
- `applyTheme()`：设 `data-theme` 属性为 themeName 值
- `currentTheme` computed → 从 `getNaiveThemeOverrides` 返回的 theme 取
- `currentThemeOverrides` computed → 从 `getNaiveThemeOverrides` 返回的 overrides 取
- `xdev-theme-change` 事件改为传递 themeName 字符串

## Phase 2: 主题选择器 UI + localStorage 迁移

### 2.1 ThemeSettings.vue
- checkbox → `n-radio-group` 三选一，或 `n-button-group` 带色块预览
- 三个选项：浅色 / 深色 / 护眼，每个带小圆形色块预览（accent + bg 色）
- emit `update:theme` 传递 themeName 字符串

### 2.2 AppContent.vue
- `toggleTheme(dark: boolean)` → `setTheme(name: string)`
- `update:is-dark` prop → `update:theme`
- localStorage key `'xdev-theme'` 值从布尔改为主题名字符串
- 派发 `xdev-theme-change` 事件传递 `{ detail: themeName }`

### 2.3 SettingsPage.vue
- `:is-dark` prop → `:theme` prop
- `@update:is-dark` → `@update:theme`
- 传递给 ThemeSettings 的 prop/emit 对应更新

## Phase 3: 硬编码颜色清理

### 3.1 组件硬编码颜色
搜索 Vue 组件中的 `#xxxxxx`、`rgb()`、`rgba()`，替换为 `var(--xdev-*)`：
- `CommandPalette.vue`（1 处）
- `InstallProgressDialog.vue`（1 处）
- `ConfirmAction.vue`（1 处）
- 全局搜索确认无遗漏

### 3.2 naive-ui 组件内联样式
搜索 Vue 模板中的 `:style="{ color: '#...' }"` 或 `style="color: #..."`，替换为 CSS 变量。

## Phase 4: 构建验证

- `npm run build` 通过
- `npm run test:unit` 通过（navigation spec 等无断言失败）
- `wails build` 通过
- 视觉确认三种主题切换效果（需用户实测）

## 风险缓解

| 风险 | 缓解 |
|---|---|
| naive-ui 护眼模式 token 遗漏 | 在 naive-theme.js 中逐一覆盖所有关键组件 token |
| 旧 localStorage 布尔值 | loadTheme() 迁移逻辑：`true`→dark, `false`→light, 无值→默认 dark |
| AppShell 测试断言 isDark | 更新测试匹配新的 theme prop |
| 设计意图一致性 | 单一 designer lane 主导全部配色和 UI |

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-013-001`
