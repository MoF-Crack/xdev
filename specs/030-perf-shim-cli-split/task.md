# Spec 030 Tasks

## B1 — CLI/GUI 入口 seam

- [x] T-B1-01 `main.go` tag `!bindings && !gui`；去掉 `internal/gui` import 与 `runGUI`
- [x] T-B1-02 新增 `main_gui.go`（`gui && !bindings`）含 `runGUI`
- [x] T-B1-03 `cli/extra.go` 删除 gui import；`newGuiCmd` exec 同目录 `xdev-gui`
- [x] T-B1-04 删除 `NewGUIApp`/`RunGUI`；`build.ps1` `wails build -tags gui`
- [x] T-B1-05 守卫测试：CLI 源文件不得 import `internal/gui`

## B2 — Shim / 注册表

- [x] T-B2-01 `run_unix.go`：`unix.Exec`；`run_windows.go`：`Command` + EnrichFromRegistry
- [x] T-B2-02 `EnrichFromRegistry` 走 `GetAllUserEnvVars`
- [x] T-B2-03 测试：已有 env 被覆盖；白名单外变量不注入

## B3 — 嗅探 / Registry.All

- [x] T-B3-01 `sniffNowTool(*Tool)`；`SniffAll` 只 LoadCatalog 一次
- [x] T-B3-02 interval OnStartup 异步
- [x] T-B3-03 `Registry.All` 用 `sort.Slice`

## B4 — self-update

- [x] T-B4-01 owner `MoF-Crack`；归档文件名 + 解出二进制
- [x] T-B4-02 zip-slip 拒绝 `..`
- [x] T-B4-03 About RepoURL
