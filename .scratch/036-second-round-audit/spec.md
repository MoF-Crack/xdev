# Spec 036 — 第二轮三轮审查问题修复（Spec 035 实现后复查）

Status: done（实施完成 + 两轮循环测试全绿零回归）
Date: 2026-08-24
来源：Spec 035 全部 11 项修复实施完成后的第二轮三轮审查（架构分析 → CodeReview/Bug 发掘 → 安全审计 + 功能测试）。

## 审查方法与基线

- 审查对象：HEAD 6c32696（单二进制合并）+ 全部未提交改动（一键更新/PhaseInstalling + Spec 035 修复 F1-F11 + e2e mock 修复）。diff 全文 756+/49-，基准 `.scratch/spec036-review-baseline.diff`。
- 第 1 轮（架构）：单二进制多态入口（main.go F1 与 root.go 的 MaybeExec 双路径互斥性）、批量更新数据流（useBatchUpdates → waitForCompletion → Wails 事件）、TUI 状态机（installState/updatesView/updateQueue/updateIdx 四元组非法状态枚举）、ResolveKind 导出方向、e2e mock 一致性。
- 第 2 轮（CodeReview）：逐文件审查 diff 涉及的 28 个文件；重点等待器三闭包变量全读写路径、批量循环分支完备性、TUI 键序/渲染、i18n 键、shim 新测试。
- 第 3 轮（安全 + 功能）：新增外部面盘点（零）、既有加固复核（runXdevCommand 白名单/TOFU/IsSafeExternalURL 等未触碰）、bindings 构建验证（独立 main_bindings.go，F1 无影响）；功能基线实跑全绿：`go build ./...` ✅、`go vet`（5 包）✅、`go test ./... -short -count=1` 29 包 ✅、`npx vitest run` 29 文件 181 用例 ✅、`npx vite build` ✅、e2e 18/18 ✅。

## 问题清单

本轮**未发现 P0/P1**——符合预期（代码已过 4 轮方案审计 + 实现 + 30 个新守卫测试）。发现 3 项 P2 + 2 项 P3：

| # | 定级 | 问题 | 证据位置 |
|---|------|------|----------|
| N1 | P2 | TUI 更新视图激活期间 errMsg 不渲染：批量更新某项失败或 registry miss 后，View() 走 updatesView 分支 early return，主视图 errBar 拼接被跳过——用户只看到列表「没反应」，错误提示要 Esc 退回主视图才可见 | `internal/tui/app.go` View() updatesView 分支（约 :307-315）与主视图 errBar 拼接（约 :364-367）的顺序；errMsg 写入点 `app.go:265-268`（InstallResultMsg Err）与 `updates.go:81-90`（registry miss）|
| N2 | P2 | GUI 批量更新进行中，更新列表每行的单项 Install 按钮未禁用：用户点击 → doInstall 弹 unverified 确认 → startInstallConfirmed → installInternal 因 `active.value=true` 拒绝（started=false、无 error）→ 批量计 failed 并 `setShowProgress(false)` 误关当前项进度框（另一真实安装仍在进行） | `frontend/src/pages/DashboardPage.vue` 行按钮无 `:disabled`；`useInstall.js` installInternal 开头 active 守卫；`useBatchUpdates.js` started=false 分支 setShowProgress(false)；进度框 `AppContent.vue:40` v-model:show |
| N3 | P2 | TUI `updatesViewString` 的「更新中: N/M」行为死代码：F4 修复后该行唯一可见前提（updateQueue!=nil && installState==nil）不可达——队列非 nil 的所有窗口都伴随 installState!=nil（被 viewInstallOverlay 替换渲染），installState 清空的每个路径都同时清队列 | `internal/tui/updates.go:133-135`；installState 全部清空点：app.go InstallResultMsg（成功耗尽/失败 F3 清）、handleInstallOverlayKey 取消（F3 清）、startToolUpdate registry miss（F3 清）|
| N4 | P3 | 批量更新每项完成瞬间进度框闪烁关闭再开：onComplete 回调（handleInstallComplete）同步置 `showProgress.value=false`，批量循环下一项再 `setShowProgress(true)`——两态切换之间有一帧空白 | `frontend/src/AppContent.vue:1000-1004`（handleInstallComplete 首行）；`useBatchUpdates.js` completed 分支后下一项 setShowProgress(true) |
| N5 | P3 | 批量中途取消时双 toast：进度框 Cancel → cancelDownload 包装弹 `download_cancelled`，composable 汇总再弹 `install_all_cancelled`——同一动作两条提示 | `frontend/src/AppContent.vue:1019-1021`；`useBatchUpdates.js` cancelled 分支 message.warning |

## 已核实驳回的疑点（记录在案，不修）

| 疑点 | 驳回证据 |
|---|---|
| F1 修复影响 bindings 构建 | bindings 用独立 `main_bindings.go`（//go:build bindings），与 main.go 互斥；`go build -tags bindings ./...` 实测 exit=0 |
| F1 对计划任务/非交互调用者零参行为变化 | basename 为 xdev/xdev-*/xdev-gui* 时 MaybeExec 在 Lookup 前短路（shim.go:64-74），零参行为与修复前一致 |
| confirmUnverified 的 onClose/onNegativeClick 双触发 | Promise resolve 幂等，二次调用 no-op；Naive UI 关闭路径不会悬挂 |
| i18n 多占位符（{done}/{failed}/{skipped}）机制支持 | `i18n/index.ts:31-34` 逐键 replace，任意数量占位符均可替换 |
| InstallProgressDialog 新增 Installing 相位缺 i18n 键 | `installing_installing` en/zh 均存在（zh.ts:220、en.ts:236）|
| useInstall dispose 后再 install 悬挂 | AppContent 是根组件，卸载=应用退出；竞态窗口极小（OBS 记录，不修）|
| TUI 'r' 刷新后 updateQueue 与 updates 脱钩 | 队列是按键时刻快照，语义正确（继续装快照剩余项）|
| repair() 不重置 cancelRequested | repair 与 install 共用同一 installWithOptions 下载链路（dlMgr 任务），cancel 语义一致；批量不调用 repair |
| 批量+单项事件串扰 | belongsToCurrentTask 按 taskId（tool@version）判定，批量串行无并发项 |
| mock GetBootstrapState 与后端字段漂移 | 后端 BootstrapState{Categories,Stats,Favorites}（app_tool_query.go:75-79）与 mock 字段一致；mock 数据与 GetDashboardStats/GetFavorites 各自 mock 有重复维护成本（OBS，可后续抽公共 fixture）|

## 目标

1. 修复 N1-N3（P2），消除 Spec 035 修复引入/暴露的交互与死代码问题。
2. 修复 N4/N5（P3）批量更新体验细节。
3. 每项修复配守卫测试；全量 `go test ./... -short` + vitest + vite build + e2e 保持全绿（基线见上）。

## 非目标

- 不重构批量更新 composable 结构（Spec 035 刚收敛）。
- 不处理 e2e mock 数据重复维护（OBS 级，另行记录）。
- 不动 HANDOFF 已记录的遗留项（Go 1.26.6 升级、Ed25519 生产嵌入）。
