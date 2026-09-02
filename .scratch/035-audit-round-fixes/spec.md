# Spec 035 — 三轮分析问题修复（架构 P0 / 更新链路 Bug / 安全 P2）

Status: done
Date: 2026-08-24
来源：三轮分析（架构 → Code Review/Bug → 安全 + 功能测试），2026-08-23/24

## 背景与问题清单

三轮分析在未提交改动（单二进制 + 一键更新 + PhaseInstalling）及关联模块中发现以下问题：

| # | 定级 | 问题 | 证据位置 |
|---|------|------|----------|
| F1 | P0 | 零参数 shim 调用绕过 `shim.MaybeExec`，误启 GUI（`python`/`node` 零参数进 REPL 的工具全中招；同时是攻击面扩大问题） | `main.go:45-47`（零参数分支在 `cli.NewRootCommand` 内的 `root.go:50` MaybeExec 之前直接 runGUI）；实测：`node.exe --version` 正确分发、`node.exe` 零参数无分发痕迹直入 GUI 路径 |
| F2 | P0 | GUI「一键更新全部」对嗅探（unverified）版本空转 + 假成功：`installAllUpdates` 循环 `await doInstall(...)`，而 `doInstall` 对 `preview.unverified` 弹非阻塞对话框后立即 return | `frontend/src/AppContent.vue:289-307`（循环）、`:942-951`（unverified 非阻塞 return）、`:202`（`installing` 仅在 `installInternal` 内置位） |
| F3 | P1 | TUI 更新队列残留：`InstallResultMsg.Err != nil` 与安装取消均不清 `updateQueue`，后续任意普通安装成功会意外续跑「全部更新」队列 | `internal/tui/app.go:259-277`（Err 分支缺清理）、`handleInstallOverlayKey`（取消不清队列） |
| F4 | P1 | TUI 更新视图遮蔽安装进度：`updatesView` 键处理与 View() 渲染均优先于 `installState`，按 `a` 后无进度反馈且 Esc 被吞 | `internal/tui/app.go:216-224`（键序）、`:302-305`（View 序） |
| F5 | P2 | `PhaseInstalling percent=-1` TUI 未处理：进度条清零 + 显示 `-1%` | `internal/installer/installer.go:471-477` 发 -1；`internal/tui/install.go:137`（`filled=int(-1/100*30)=0`）、`:146`（`%.0f%%` → `-1%`） |
| F6 | P2 | 自更新 `findAsset` partial-match 回退可选中非预期归档（如 `xdev-gui-windows-amd64-v1.0.zip`） | `internal/update/update.go:318-323` |
| F7 | P2 | `parseSHA256SUMS` 接受单空格分隔（非 `sha256sum` 标准双空格），宽容解析在安全敏感路径不必要 | `internal/update/update.go:494-498`；CI 产物为标准 `sha256sum *` 双空格（`.github/workflows/ci.yaml:436`） |
| F8 | P2 | `runXdevCommand` 优先 `exec.LookPath("xdev")`，PATH 上同名恶意 exe 会被 GUI 执行（参数虽受白名单约束） | `internal/gui/app_updates.go:310-315` |
| F9 | P2 | TUI help 未收录 `[x] Updates` 键 | `internal/tui/help.go` 键表 |
| F10 | P2 | GUI 阶段判定用 `EffectiveKind()`、installer 用 `resolveKind()`（多 rustup URL 启发式），两处逻辑不同源，未来易漂移 | `internal/gui/app_install.go:318` vs `internal/installer/installer.go:982-994` |
| F11 | P2 | 下载轮询 1h 超时（maxPolls 耗尽）后不 emit 任何事件——前端进度框/批量等待永挂，仅能手动 Cancel 逃生（单装/批量同病；第 2 轮审计发现，升格为独立问题） | `internal/gui/app_install.go:400-403` |

已核实撤销的疑点（记录在案，不修）：
- ~~macOS 自更新找不到 SHA256SUMS~~：主发布 job 产出 darwin tar 并生成覆盖全部产物的 `SHA256SUMS`（ci.yaml:238-248, 436），`verifySHA256` 能命中。
- ~~`xdev upgrade --all` 与 `updates` 版本源不一致（P1）~~：`LoadCatalogCached` 指纹含磁盘 manifest mtime（manifest.go:1037-1042），嗅探合并后新进程缓存失效重读磁盘、拿到新 `Latest`。仅同进程长驻 GUI 的边缘场景有理论窗口（GUI 内已有 `reloadCatalog` 处理），降为「验证记录」，不入本 Spec。
- ~~`useSniffSession.installUpdate` 单项安装同根因空转（第 1 轮审计 F2b，第 2 轮复核驳回）~~：单次调用 `doInstall` 时非阻塞 unverified 对话框即该项的确认 UI，用户点击后链路完整；仅 `loadUpdates()` 时机略早无危害。不改。

## 设计决策（F2）

- **逐项确认**：批量确认对话框不作为 unverified 版本的 `confirmed=true` 依据（FR-018）；每项单独弹确认，拒绝则跳过该项继续。PreviewInstall 调用失败（旧构建）同样按 unverified 弹确认，不静默跳过。
- **取消语义**：进度框 Cancel = 取消当前项并中止整批。
- **形态选择**：批量一律默认形态（formFilter=''），仅当 PreviewInstall 报告「形态特定且唯一形态」时自动选定该形态（与单装逻辑对齐）；不弹形态选择对话框。
- **汇总文案**：结束报「成功 N / 失败 M / 跳过 K」（取消中止另有文案）；删除无条件 `install_all_done`。
- **i18n 增删**：新增 `btn.skip`、`updates.install_all_summary`、`updates.install_all_cancelled`（en/zh 同步）；删除 `updates.install_all_done`。
- **可测性**：批量循环提取为 `useBatchUpdates` composable（依赖注入），AppContent 仅装配。

## 目标

1. 修复 F1-F4（P0/P1），消除功能与安全回归。
2. 修复 F5-F11（P2），按最小改动原则收口。
3. 每项修复配守卫测试；全量 `go test ./... -short` + vitest + vite build 保持全绿。

## 非目标

- 不重构 TUI 状态机（仅最小修复队列/视图遮蔽问题）。
- 不改 `upgrade` 命令的版本源逻辑（已核实无 P1 级问题）。
- 不处理 HANDOFF 已记录的遗留项（Ed25519 生产嵌入、Go 1.26.6 升级）。
