# ADR-0002: Wails v2 作为 GUI 框架

> **状态**：Accepted
> **日期**：2026-07-25

## 背景

XDev 需要提供 GUI 操作模式（除 CLI/TUI 外）。候选框架包括：
- Electron：体积大（~100MB），需要 Node.js 运行时
- Fyne：纯 Go，但 UI 生态有限
- Wails v2：Go 后端 + Web 前端，单二进制打包

## 决策

采用 Wails v2 作为 GUI 框架：
- Go 后端通过 `main_bindings.go`（build tag `bindings`）提供 API
- 前端使用 Vite + Vue.js，通过 `//go:embed all:dist` 嵌入二进制
- GUI 变体通过 `go build -tags bindings` 编译，产出 `xdev-gui`
- CLI 变体通过默认 `go build` 编译，不含 Wails 依赖

## 后果

- 正面：单二进制，无外部运行时依赖
- 正面：前端可复用 Web 生态（Vue.js、Tailwind）
- 正面：CLI 和 GUI 共享 internal/ 包，代码复用率高
- 负面：Wails v2 对非 Windows 平台的 webview 依赖可能导致兼容性问题
- 负面：GUI 变体编译需要 Node.js + npm 前端构建步骤

---

## Approval History

| approval_id | decision | date |
|---|---|---|
| 2026-07-26-adr-0002-001 | Accepted | 2026-07-26 |
