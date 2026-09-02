# Spec 035 三轮审查报告（审查结果总记录）

> 用途：**会话移交**——新会话从此文档获得全部审查结论，无需重新分析。
> 配套文档（同目录）：`spec.md`（问题清单+F2 设计决策）、`solution.md`（最终修复方案）、`issues/01-09-*.md`(任务卡)、`test-plan.md`(测试用例+验收标准)。
> 日期：2026-08-24　审查对象：单二进制改造（HEAD=6c32696 已提交）+ 一键更新/PhaseInstalling 未提交 diff + 关联模块。

---

## 一、功能测试基线（全部实跑）

| 项目 | 结果 |
|---|---|
| `go build ./...` | ✅ 通过 |
| `go vet`（tui/installer/cli/update/gui） | ✅ 通过 |
| `go test ./... -short -count=1` | ✅ **29 包全绿** |
| `npx vitest run`（frontend/test） | ✅ **28 文件 / 161 用例全绿** |
| `npx vite build` | ✅ 8.25s 通过 |

实施修复后的回归门以此基线对照（只增不减），见 test-plan.md 第二节。

## 二、第 1 轮：架构分析

**总体评价**：单二进制多态入口（无参→GUI / `tui`→TUI / `gui`→进程内 GUI / 其余→CLI）设计自洽；分层清晰（cli 不 import gui、toolmeta 元数据收口、InstallSpec 三 kind 统一入 `App.install`）。未提交 diff 的方向正确。

### A-1（P0）零参数 shim 调用绕过分发，误启 GUI【F1】
- **机制**：`main.go:45-47` 在 `len(os.Args)<=1` 时直接 `runGUI()`；而 shim 分发在 `cli.NewRootCommand`（`internal/cli/root.go:50` 调 `shim.MaybeExec`）。零参数路径根本不进入命令树。
- **实验证据**：测试构建副本改名 `node.exe` + fake XMDEV_HOME：
  - `node.exe --version` → 输出 `shim: run ...\current\node\node.exe`（分发正常）
  - `node.exe`（零参数）→ 无任何分发痕迹，直入 GUI 启动路径
- **影响面**：`python`/`node` 等零参数进 REPL 的工具 shim 全部中招；同时是攻击面扩大问题（不可信 PATH 下同名 shim 零参数调用启动完整 GUI）。旧版（双二进制 main.go 经 Cobra）不存在此问题——属 commit 6c32696 单二进制改造引入的回归。
- 附带发现：root.go Long help「Running xdev without arguments launches the interactive TUI」已过时（现为 GUI）；root RunE 的 TUI 分支仅在纯 flag 场景可达（如 `xdev --verbose`）。

## 三、第 2 轮：Code Review + Bug 发掘

### B-1（P0→F2）GUI「一键更新全部」空转 + 假成功
`frontend/src/AppContent.vue:289-307` 循环 `await doInstall(u.tool, u.latestVersion)`；而 `doInstall` 对 `preview.unverified` 版本（嗅探版本即更新列表主体）弹**非阻塞** Naive UI 对话框后立即 return（AppContent.vue:942-951）。后果：循环瞬间跑完、对话框堆叠、无条件 `message.success('全部更新完成')`；即使非 unverified 路径，`installing` 守卫也使后续项被静默跳过。配套 `useSniffSession.js:63-66 installUpdate` 单项路径经核实**不受影响**（见驳回项 R-3）。

### B-2（P1→撤销降级）upgrade --all 与 updates 版本源不一致
初判 P1；核验后撤销：`LoadCatalogCached` 指纹包含磁盘 manifest 目录 mtime（manifest.go:1037-1042），嗅探合并后新进程缓存失效重读磁盘拿到新 `Latest`；且 `updates` 列表本身过滤了 merge 失败条目（result.Error!=""），新进程 `upgrade --all` 同源有效。仅同进程长驻 GUI 有理论窗口（GUI 已有 reloadCatalog 处理）。**不改代码，记录在案。**

### B-3~B-7（详见 spec.md 问题表 F3-F10）
- **F3（P1）TUI 更新队列残留**：`app.go:265-268` Err 分支不清 `updateQueue`、`handleInstallOverlayKey` 取消不清、`startToolUpdate` registry 未命中（updates.go:81-85）不清——残留队列会在后续任意普通安装成功时意外续跑。
- **F4（P1）TUI 更新视图遮蔽安装进度**：键序（app.go:216-224 updatesView 先于 installState）与渲染序（View :302-305 早 return）双重遮蔽；按 `a` 后无进度反馈且 Esc 被吞。
- **F5（P2）percent=-1 渲染泄漏**：installer.go:471-477 发 `PhaseInstalling,-1`；TUI 数据流两条路都漏（store install.go:72-74 + 直赋 app.go:237）→ 显示 `-1%` 且进度条清零。前端 useInstall.js:24 有守卫，TUI 没有。
- **F6（P2）findAsset partial-match**：update.go:318-323 可选中 `xdev-gui-windows-amd64-v*.zip`（两轮核实 CI 命名后确认风险真实：extract 提取根级 xdev.exe 会成功）。
- **F7（P2）parseSHA256SUMS 单空格分支**：update.go:494-498，CI 实际产标准双空格（ci.yaml:436 `sha256sum *`），宽容解析不必要。
- **F8（P2）runXdevCommand LookPath 优先**：app_updates.go:310-316，PATH 注入可执行同名 exe（生产当前零调用，前瞻性风险）。
- **F9/F10（P2）**：help 缺 `[x]` 键位（help.go:15-28）；GUI 用 `EffectiveKind()` 而 installer 用 `resolveKind()`（含 rustup URL 启发式）两处不同源（app_install.go:318 vs installer.go:982-994）。

### 审计发现的额外存量 bug【F11，P2】
下载轮询 1h 超时分支（app_install.go:400-403）只 Cancel+RemoveTask **不 emit 任何事件**——前端进度框等待 Completed 永不来，仅能手动 Cancel 逃生；批量场景放大为等待器永挂。单装同样受害。

## 四、第 3 轮：安全审计

### 加固现状确认（前几轮修复保持有效，无需动作）
- 自更新链路：强制 HTTPS + `SecureRedirectCheck` 禁降级重定向；SHA256SUMS 缺失/下载失败/条目缺失一律拒绝更新（H-11 fail-closed）；zip-slip 防护（仅根级 `xdev` 成员）+ 200MiB 上限。
- stale 替换：自指保护 + 前后 SHA-256 校验（stale.go）。
- 隔离/远程执行白名单、runXdevCommand 子命令闭集、Lua load 封堵等第 021/034 轮修复均在位。

### 本轮新增发现
- F1 同时是安全问题（见上）。
- **F8/F6/F7** 属安全敏感路径收窄（自更新资产精确匹配、checksums 格式收紧、CLI 子进程自身优先）。

### 残留风险（记录，不在本 Spec 处理）
1. 运行时 Go 1.26.5 stdlib govulncheck 报告（GO-2026-6218 等），需升级 1.26.6。
2. 生产 release 的 Ed25519 根公钥嵌入依赖 CI secrets；本地构建嵌入与否取决于 `keys/pub.key` 存在性（HANDOFF 已跟踪）。
3. Windows hardlink shim 在自更新 rename 后仍指向旧 inode，靠后续 shim 维护刷新（EnsureSelfInstalled 只刷 homeDir 副本）——潜在滞后，非漏洞。

## 五、审查过程中被驳回的疑点（防止后续会话重复调查）

| 疑点 | 驳回证据 |
|---|---|
| macOS 自更新找不到 SHA256SUMS | 主发布 job 产出 darwin tar 进 dist 并生成覆盖全部产物的 `SHA256SUMS`（ci.yaml:238-248、430-441）；`SHA256SUMS-darwin` 仅 DMG 补充，不影响 CLI 自更新。 |
| `xdev upgrade --all` 读不到嗅探版本（原 P1） | LoadCatalogCached 指纹含磁盘 manifest mtime（manifest.go:1037-1042）→ 嗅探后新进程缓存失效重读；updates 列表过滤 merge 失败条目，两者同源。降为验证记录。 |
| useSniffSession.installUpdate 单项安装同 F2 空转（第 1 轮审计误报，第 2 轮复核驳回） | 单次调用 doInstall 时非阻塞 unverified 对话框即该项确认 UI，点击后 startInstallConfirmed 链路完整；loadUpdates 时机略早无危害（完成后 handleInstallComplete→refreshInstallViews 再刷）。 |

## 六、修复方案与任务指针（实施阶段用）

- 方案定稿（4 轮审计循环收敛 0 问题）：`.scratch/035-audit-round-fixes/solution.md`
- 任务卡（按序）：`issues/01-f1-zero-arg-shim.md` → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09；硬约束：**04、07 必须先于/同批于 08**
- 测试用例与 DoD：`.scratch/035-audit-round-fixes/test-plan.md`
- 审计轨迹详情保留在 solution.md 头部摘要。

## 七、环境与注意事项（新会话必读）

1. **Go 工具链**：`set GOROOT=E:\XDev\install\go\1.26.5\go` 并把其 `\bin` 加入 PATH 前。**注意不要把 `E:\XDev\install\go\1.26.5` 本身当 GOROOT（空目录外壳）**——本轮两个子代理因此错误地报告"无法编译"。
2. 构建：`go build ./...`；前端：`cd frontend && npx vitest run && npx vite build`；e2e：`npx playwright test e2e/app.spec.js`（基线 18/18，可选）。
3. 工作树当前有未提交改动（单二进制 + 一键更新/PhaseInstalling，13 文件 + internal/tui/updates.go）——Spec 035 的修复将叠加其上，任务卡以当前工作树行号为准。
4. 文档变更约定：HANDOFF.md 会话协议、.scratch issue-tracker 单文件 per ticket、STATUS 行标记。
