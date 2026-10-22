# Ticket 01: 主题注册表 + theme.css 六主题 token 重写

## Status: done

- 新建 `frontend/src/theme/themes.js`：THEMES 注册表（value/labelKey/family/swatch 预览色）+ isKnownTheme。
- 重写 `frontend/src/styles/theme.css`：6 主题块 + :root=graphite 默认；保留全部既有 token 名；新增 `--xdev-bg-hover` 与 warning-bg/border/text 全主题补齐；radius 6/10；中性 shadow；`.xdev-eyebrow` 工具类；`.xdev-btn--primary` 实心化；删 `--xdev-preview-*`。

## Comments

- 验证：theme.test.js 新契约通过（见 Ticket 05）。
