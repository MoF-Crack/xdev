# Spec 013: UI 主题重构——三套配色方案

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-08
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：三套 UI 主题重构的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 背景与动机

当前 xdev GUI 只有深色/浅色两种主题，且配色风格偏向通用模板，缺乏辨识度。用户要求至少三种界面配色（浅色、深色、护眼），并对整个 UI 风格进行重构。

## 当前架构

```
App.vue → <n-config-provider :theme :theme-overrides>
  ├── isDark ref（布尔值）→ darkTheme / lightTheme 二选一
  ├── localStorage('xdev-theme') 持久化
  └── xdev-theme-change 事件驱动切换

theme.css → :root(dark默认) + [data-theme="light"] 覆盖
naive-theme.js → getNaiveThemeOverrides(mode) 映射到 naive-ui token
ThemeSettings.vue → checkbox 开关（仅深色/浅色）
```

**问题**：
1. `isDark` 是布尔值，无法扩展到三种以上主题
2. `data-theme` 属性只支持 `"dark"` / `"light"` 两个值
3. naive-ui 只用 `darkTheme`/`lightTheme` 两个预设，无法自定义第三套
4. ThemeSettings 只有一个 checkbox，无法选择三种主题
5. 当前配色缺乏设计意图——深色用 indigo accent，浅色用同色系，视觉风格平淡

## 范围

### 重构内容

| 层级 | 文件 | 变更 |
|---|---|---|
| 主题模型 | `App.vue`、`AppContent.vue` | `isDark: boolean` → `themeName: string`（'light'/'dark'/'eye-care'） |
| CSS 变量 | `styles/theme.css` | 三套 `[data-theme="..."]` 配色方案 + 重构设计 token |
| Naive UI 映射 | `theme/naive-theme.js` | 支持三套 themeOverrides；护眼模式用 `lightTheme` 基底 + 自定义覆盖 |
| 主题选择器 | `ThemeSettings.vue` | checkbox → 三选一按钮组/下拉 |
| 全局样式 | 各页面组件中的硬编码颜色 | 搜索并替换为 CSS 变量引用 |

### 不在范围

- 页面布局结构（导航、工具栏、内容区布局不变）
- naive-ui 组件替换（仍用 naive-ui）
- 字体/间距/圆角 token（保留现有值，仅改颜色）
- 后端任何代码

## 三套配色方案设计

### 方案 1：浅色（Light）

清新明亮的日间风格，适合明亮环境。

| Token | 色值 | 用途 |
|---|---|---|
| bg-app | `#f8f9fa` | 应用背景 |
| bg-panel | `#ffffff` | 面板/侧边栏 |
| bg-card | `#ffffff` | 卡片 |
| bg-elevated | `#f1f3f5` | 悬浮元素 |
| accent | `#3b82f6` | 强调色（蓝色） |
| text-primary | `#1a1a2e` | 主文本 |
| text-secondary | `#495057` | 次文本 |
| border-subtle | `rgba(0,0,0,0.08)` | 细边框 |

### 方案 2：深色（Dark）

沉稳的夜间风格，低眩光高对比。

| Token | 色值 | 用途 |
|---|---|---|
| bg-app | `#0d1117` | 应用背景（GitHub Dark 风格） |
| bg-panel | `#161b22` | 面板/侧边栏 |
| bg-card | `#1c2128` | 卡片 |
| bg-elevated | `#22272e` | 悬浮元素 |
| accent | `#58a6ff` | 强调色（亮蓝） |
| text-primary | `#e6edf3` | 主文本 |
| text-secondary | `#8b949e` | 次文本 |
| border-subtle | `rgba(110,118,129,0.2)` | 细边框 |

### 方案 3：护眼（Eye-Care）

柔和的暖色调，降低视觉疲劳，适合长时间使用。

| Token | 色值 | 用途 |
|---|---|---|
| bg-app | `#f5f1e8` | 应用背景（米黄/暖白） |
| bg-panel | `#faf7f0` | 面板/侧边栏 |
| bg-card | `#fdfbf5` | 卡片 |
| bg-elevated | `#f0ebe0` | 悬浮元素 |
| accent | `#5b8c5a` | 强调色（柔和绿） |
| text-primary | `#3d3528` | 主文本（暖棕） |
| text-secondary | `#6b5d4f` | 次文本 |
| border-subtle | `rgba(61,53,40,0.12)` | 细边框 |

### 语义色（三套共用逻辑，色值各异）

| 语义 | Light | Dark | Eye-Care |
|---|---|---|---|
| success | `#16a34a` | `#3fb950` | `#5b8c5a` |
| warning | `#d97706` | `#d29922` | `#b8860b` |
| error | `#dc2626` | `#f85149` | `#c45a3a` |
| info | `#2563eb` | `#58a6ff` | `#5b8c9a` |

## 实现设计

### 主题模型变更

```typescript
// App.vue
type ThemeName = 'light' | 'dark' | 'eye-care';

const themeName = ref<ThemeName>(loadTheme());
const currentTheme = computed(() => {
  // dark → darkTheme; light/eye-care → lightTheme（护眼用 lightTheme 基底 + 覆盖）
  return themeName.value === 'dark' ? darkTheme : lightTheme;
});
const currentThemeOverrides = computed(() => getNaiveThemeOverrides(themeName.value));

// applyTheme 设 data-theme="light"|"dark"|"eye-care"
```

### CSS 变量结构

```css
/* theme.css */
:root { /* dark 默认值，保持向后兼容 */ }

[data-theme="light"] { /* 浅色覆盖 */ }
[data-theme="dark"] { /* 深色覆盖（显式声明，不依赖 :root fallback） */ }
[data-theme="eye-care"] { /* 护眼覆盖 */ }
```

### 主题选择器

ThemeSettings.vue 从 checkbox 改为三选一按钮组（`n-radio-group` 或 `n-button-group`），显示三个选项：浅色 / 深色 / 护眼，每个选项带小色块预览。

### 硬编码颜色清理

搜索所有 Vue 组件中的硬编码颜色值（`#xxxxxx`、`rgb(...)`、`rgba(...)`），替换为 `var(--xdev-*)` 引用。重点排查：
- `AIAssistantPage.vue`（已删除）
- `DashboardPage.vue`、`SettingsPage.vue`、`CustomToolsPage.vue`（已删除）
- `ToolDetailDrawer.vue`、`InstallProgressDialog.vue`
- `AppShell.vue`、`AppContent.vue`
- 各 `components/` 下的子组件

## 验收标准

1. `npm run build` 通过
2. `npm run test:unit` 通过
3. `wails build` 通过
4. GUI 设置页可选择三种主题：浅色、深色、护眼
5. 切换主题后全部界面（导航、工具栏、卡片、表格、按钮、输入框、对话框、toast）配色一致变化
6. 三种主题各自的语义色（success/warning/error）正确显示
7. 主题选择持久化（重启后保持上次选择）
8. 无硬编码颜色残留（搜索 Vue 文件中的 `#` 开头的颜色值）

## 风险

- **naive-ui 护眼模式**：护眼模式基于 `lightTheme` + 自定义覆盖，部分组件可能有未覆盖的 token 导致配色不一致。需逐一测试关键组件。
- **硬编码颜色遗漏**：大量组件可能有内联样式或 scoped CSS 中的硬编码颜色，搜索可能不全。构建后视觉检查可补充。
- **向后兼容**：已有用户的 `localStorage('xdev-theme')` 存的是布尔值 `true`/`false`，需迁移为新的主题名字符串。

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-013-approved-001` 三套 UI 主题重构的目标、范围和验收 |
