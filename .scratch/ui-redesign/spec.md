# Spec 040: GUI 重设计 — 简约科技风 + 多主题切换

## Status: done

## 需求（用户原始诉求）

> 对当前项目的UI进行重新设计，要求简约的科技风，耐看，多主题切换。

1. **简约的科技风**：克制、精密、工程感——不堆装饰，靠排版/结构/数据表达科技感。
2. **耐看**：低疲劳配色（低饱和表面、AA 对比度验证）、无持续动画噪音、长期使用不腻。
3. **多主题切换**：主题从 3 个（light/dark/eye-care）扩展为 6 个，主题切换器重设计。

## 设计方向：「仪器面板」（Instrument Panel）

XDev 是工具链的控制面板——版本槽位、shim、junction、安装遥测。设计语言取自
精密仪器：中性机加工表面、发丝线结构、丝印（silkscreen）微标签、表格化数字、
每主题一个克制的强调色。

### 自我评审（避免模板化）

- 弃用当前 GitHub 风（#0d1117/#58a6ff 蓝紫表面 + 白字蓝按钮）——太像 GitHub 克隆，无身份。
- 弃用 AI 默认三件套：奶油底+衬线+赤陶、纯黑+酸性绿、报纸 hairline 风。
- 保留的差异化决策：
  1. **暗色系主题用「亮强调色 + 深色 on-accent 文字」**（graphite/midnight/nord/ember
     四个暗主题的实心按钮是亮底深字，不是白字蓝底）——每个主题的实心控件都过 AA
     对比度，这是多数应用懒得做的工艺选择（本 Spec 的核心审美风险）。
  2. **丝印微标签系统**（.xdev-eyebrow）：11px/600/大写字距/次级色，用于指标标签、
     侧栏分组、表头——「仪器铭牌」是全应用的签名元素。
  3. **主题套件即特性**：6 主题各有命名与色板身份，设置页主题选择器重设计为
     带实时色板预览的主题卡（app/panel 底色 + accent 条 + 选中态 accent 描边）。

## 主题套件（6 个）

| key | zh / en | 家族 | app bg | accent | on-accent |
|---|---|---|---|---|---|
| `dark`（默认） | 石墨 / Graphite | 冷石墨 | #0e1116 | #5c9cff 钴蓝 | 深字 #0b1220 |
| `midnight` | 午夜 / Midnight | 深海军蓝 | #0a0e1a | #63c7e6 冰川青 | 深字 #04222e |
| `nord` | 极夜 / Nord | Polar Night | #2e3440 | #88c0d0 霜蓝（Nord 官方色） | 深字 #1f2530 |
| `ember` | 余烬 / Ember | 暖暗 | #15110c | #e6a552 琥珀 | 深字 #1d1405 |
| `light` | 日光 / Daylight | 冷白 | #f2f4f8 | #2f6fed 钴蓝 | 白字（AA 4.56:1） |
| `eye-care` | 护眼 / Eye care | 暖纸 | #f4f1e8 | #4e7d4e 苔绿 | 白字（AA 4.8:1） |

## 方案

### 1. 主题注册表（新 `src/theme/themes.js`）

单一事实来源：`THEMES` 数组（value/labelKey/家族/色板预览色）+
`isKnownTheme()`。App.vue、AppContent.vue、ThemeSettings.vue、theme.test.js
全部从注册表取值，消灭三处硬编码 `['light','dark','eye-care']` 白名单。

### 2. token 层（`src/styles/theme.css` 重写）

- 6 个主题块 + `:root`（= graphite 默认）。保留全部现有 token 名（组件零改动兼容），
  新增：`--xdev-bg-hover`（表格/行悬停中性色）、`--xdev-warning-bg/border/text`
  补齐所有主题（原先仅 :root，light/eye-care 用的是暗色回退）。
- 基础 token 调整：radius 收紧（8/12→6/10，仪器感）；shadow 去蓝光（纯中性）；
  字体栈加 "Segoe UI Variable Text"；新增 `.xdev-eyebrow` 工具类。
- `.xdev-btn--primary` 从描边改为**实心 accent + on-accent 深字/白字**，与 Naive
  实心主按钮视觉统一。
- 删除 `--xdev-preview-*` token（预览色改由 themes.js 提供）。
- `prefers-reduced-motion` 守卫保留。

### 3. Naive UI 层（`src/theme/naive-theme.js`）

6 套调色板；dark 家族基于 darkTheme、light 家族基于 lightTheme；
Button.textColorPrimary 按主题 on-accent 配套；radius 6px。

### 4. 主题选择器（`ThemeSettings.vue` 重设计）

6 个主题卡（n-radio-group 语义保留、`data-action="theme"/"theme-<value>"` 契约
不变——vitest SettingsPage.spec 与 e2e `.check()` 依赖它），每卡：色板预览条
（bg-app/bg-panel 双底色块 + accent 圆点）+ 名称 + 选中 accent 环。

### 5. 组件重塑（token + 少量结构）

- AppShell：去径向光晕背景，纯色 app 底。
- AppSidebar：导航项 36px 高、radius 4px、active = 2px accent 竖轨 + accent-soft 底；
  分组标签改 eyebrow。
- AppTitleBar/PageToolbar：toolbar 72→60px；h1 收紧字重字距。
- SectionCard：radius 10px、头/身 padding 收紧、去 hover 抬升（静态卡）。
- MetricCard：标签改 eyebrow；值 tabular（已有）；可点卡 accent 边框过渡保留。
- 表格：表头 eyebrow 化；行 hover 用 `--xdev-bg-hover`（中性，不再全表 accent-soft）。
- ToolCard/ToolTable/filters 输入框：对齐新 radius/border token。
- SetupWizard：删 `[data-theme="dark"] .button.primary` 硬编码（#1f6feb），走 accent token。
- index.html pre-mount：#0d1117/#58a6ff → 新 graphite 默认色。

### 6. i18n

新增 `theme.midnight/theme.nord/theme.ember`（en/zh 对齐，parity 守卫自动覆盖）。

## 验证门

1. `npx vitest run`：全量通过（theme.test.js 契约更新：6 主题全 token、新 accent
   断言、naive 调色板断言、themes.js 与 CSS/naive 一致性守卫）。
2. `npx vite build` 通过。
3. e2e：`npx playwright test --update-snapshots` 重拍 6 张视觉基线后
   `npx playwright test` 全绿（axe 对比度门在 dashboard/progress 上显式断言）。

## 不做（范围外）

- 不改信息架构/路由/组件契约（data-action、data-view、class 锚点全保留）。
- 不改后端/Wails 绑定。
- 不引入新字体文件（双语 UI 的耐看基础是系统字体栈 + 用法设计）。
- 不做「跟随系统」自动主题（静态 6 主题已满足需求，系统联动另立 Spec）。
