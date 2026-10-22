# Ticket 04: 测试契约更新 + 全量验证门

## Status: done

- `theme.test.js`：语义 token 契约扩至 6 主题；accent/bodyColor/on-accent 断言更新；
  新增 themes.js ↔ theme.css ↔ naive-theme.js 一致性守卫；legacy neon 守卫保留；
  新增 --xdev-bg-hover / warning 三 token 入契约。
- 验证门：`npx vitest run` 全绿 → `npx vite build` → e2e `--update-snapshots`
  重拍 6 张基线 → `npx playwright test` 全绿。
