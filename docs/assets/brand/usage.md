# F2 流弧单 X 品牌资产

F2 的流弧 x 同时是字标首字母。完整字标为 **一个流弧 x + dev**，不要再拼接 `XDev`、`xdev` 或额外的 x。

## 资产

| 用途 | 资产 | 约束 |
| --- | --- | --- |
| 完整字标 | [xdev-wordmark.svg](<xdev-wordmark.svg>)、[PNG](<xdev-wordmark.png>) | 269:91；透明底；与批准的单 X 字标几何完全一致 |
| 纯符号 | [xdev-symbol.svg](<xdev-symbol.svg>) | 128:128；透明底；不加圆角底板、描边或节点 |
| 应用图标 | [xdev-app-icon.svg](<xdev-app-icon.svg>)、[512px PNG](<xdev-app-icon.png>) | 固定 `#5655CD` 圆角底、白色图形、四角透明；不随主题变化 |
| 前端静态字标 | [logo.svg](<../../../frontend/src/assets/logo.svg>) | 与文档字标同源同几何 |
| 前端小图标 | [logo-small.svg](<../../../frontend/src/assets/logo-small.svg>) | 与应用图标相同，不是缩小版字标 |
| 浏览器图标 | [favicon.svg](<../../../frontend/public/favicon.svg>) | 固定应用图标，HTML 使用 `/favicon.svg` |
| 桌面图标 | [appicon.png](<../../../build/appicon.png>)、[icon.ico](<../../../build/windows/icon.ico>) | PNG 为 512×512 RGBA；ICO 包含 16/24/32/48/64/128/256 七个 RGBA 尺寸 |

所有轮廓均为 SVG path，无字体、网络、渐变、滤镜或外部资源依赖。共享轮廓与原始 lockup transform 位于 [f2.js](<../../../frontend/src/brand/f2.js>)。

## Vue 组件

[XDevLogo.vue](<../../../frontend/src/components/XDevLogo.vue>) 的默认 variant 为 `symbol`，支持 `wordmark`。

```vue
<!-- 独立标识：role="img"，aria-label="XDev" -->
<XDevLogo variant="wordmark" />

<!-- 紧凑侧栏：默认仅显示流弧 x，同样有可访问名 -->
<XDevLogo />

<!-- 旁边已有明确的可见品牌名，避免读屏重复朗读 -->
<XDevLogo decorative />
```

`decorative` 维持原有契约：`aria-hidden="true"`，不输出 `role` 和 `aria-label`；非装饰模式输出 `role="img"`、`aria-label="XDev"`，不输出 `aria-hidden`。所有模式 `focusable="false"`，不增加 Tab 停靠点。

- 符号：`var(--xdev-accent, #5655CD)`。
- 组件内 dev：`var(--xdev-text-primary, currentColor)`，不继承符号 accent。
- 静态 SVG 被内联时可使用相同 CSS token；通过 `<img>` 加载时不继承页面 token，字标默认使用批准的浅色版本 `#5655CD` / `#252C44`。深色界面优先用 Vue 组件，不对应用图标重新着色。
- 组件沿用父容器的宽高，SVG 默认保持比例。字标容器按 269:91 设置，例如高 24px、宽约 71px；不要沿用原符号的方形容器宽度。

## 由集成方修改的调用点

本品牌任务不修改布局、主题、HTML、版本或构建脚本。

1. [AppTitleBar.vue](<../../../frontend/src/layouts/AppTitleBar.vue>)：完整身份区使用 `variant="wordmark"`，移除旁边独立的 `XDev` 文本，并为字标设置横向尺寸。
2. [AppSidebar.vue](<../../../frontend/src/layouts/AppSidebar.vue>)：展开时使用完整 wordmark 且移除独立 `strong` 品牌文本；紧凑时保留独立 symbol。没有相邻可见品牌名时不要设为 decorative。
3. [index.html](<../../../frontend/index.html>)：favicon 链接指向 `/favicon.svg`。
4. [theme.css](<../../../frontend/src/styles/theme.css>)：主题 accent/text token 的取值由主题任务统一维护；品牌组件不新增第二套主题系统。
5. 后续若文档使用完整字标，直接引用本目录字标，不再额外拼接文字；若打包流程从另一处重建或覆盖图标，应由集成方确认最终输入仍是本次 PNG/ICO。

## 验证

[验证记录](<verification.json>) 保存本次输入/输出 SHA-256、尺寸与验证方法。

- [XDevLogo.spec.js](<../../../frontend/test/XDevLogo.spec.js>)：15 项，覆盖默认符号、单 X 字标、精确轮廓与 transform、两个 variant 的可访问名/装饰模式、变体切换、颜色分工、静态 SVG、PNG 尺寸及 ICO 七个目录项。
- 连同 [AppShell.spec.js](<../../../frontend/test/AppShell.spec.js>)：23/23 通过。
- 以现有 Playwright + 已安装 Chromium 渲染批准源与产物，Pillow RGBA 逐像素差异为零：应用图标、静态字标、实际编译的 Vue wordmark 均一致。
- 浏览器 computed style 实测：符号与 dev 分别跟随 accent/text；移除 token 后符号回落固定靛紫，dev 回落父元素 currentColor。
- 未安装依赖、未提交或 push。
