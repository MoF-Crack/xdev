# Ticket 03: 外壳与共享组件重塑（仪器面板语言）

## Status: done

- AppShell 去光晕；AppSidebar 导航项 36px/radius 4/2px accent 轨 + eyebrow 分组；
  AppTitleBar/PageToolbar 60px；SectionCard 静态化收紧；MetricCard eyebrow 标签；
  表头 eyebrow 化 + 行 hover 用 --xdev-bg-hover；ToolCard/ToolTable/ToolCatalogPage
  filters 对齐；SetupWizard 删硬编码 primary；index.html pre-mount 换新默认色。

## Comments

- 契约保留：.app-sidebar 56px 紧凑宽、.setup-card/.setup-nav、[data-view]、
  [data-action]、.metric-card 数量（4）全部不动。
