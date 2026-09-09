# Ticket 02: Naive UI 六调色板 + App/AppContent/ThemeSettings 接线

## Status: done

- `naive-theme.js`：6 套调色板；dark 家族 darkTheme、light 家族 lightTheme；Button.textColorPrimary 跟随 on-accent；focus/输入框配色对齐。
- `App.vue` / `AppContent.vue`：主题白名单改用 themes.js（消灭三处硬编码）。
- `ThemeSettings.vue`：重设计为主题卡（n-radio-group + data-action 契约保留；色板预览 + 选中 accent 环）。
- i18n en/zh：`theme.midnight` / `theme.nord` / `theme.ember`。
