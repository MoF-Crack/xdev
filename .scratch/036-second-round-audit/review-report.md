# Spec 036 第二轮三轮审查报告（审查结果总记录）

> 用途：**会话移交**——新会话从此文档获得全部审查结论，无需重新分析。
> 配套文档（同目录）：`spec.md`（问题清单 N1-N5 + 驳回表）、`solution.md`（修复方案 FINAL v3.1）、`issues/01-04-*.md`（任务卡）、`test-plan.md`（测试用例 + DoD）。
> 日期：2026-08-24　审查对象：HEAD 6c32696 + 全部未提交改动（单二进制 + 一键更新/PhaseInstalling + Spec 035 修复 F1-F11 + e2e mock 修复）。

---

## 审查方法说明

原计划三个并行子代理分别执行架构/CodeReview/安全三轮——**子代理基础设施在本会话持续失败**（6 次，上下文耗尽/无关闭消息）。改为**主线自查**执行同规格三轮分析（架构数据流追踪 / 逐 diff 文件 CodeReview / 安全面盘点 + 防护复核），并保留方案审计循环（4 轮：3+1+2+0 ISSUE 收敛）。结论可信度基于每条发现的 file:line 实读证据与实跑测试。

## 一、功能测试基线（全部实跑，2026-08-24 本会话）

| 项目 | 结果 |
|---|---|
| `go build ./...` | ✅ 通过（含 `-tags bindings` 验证） |
| `go vet`（tui/installer/update/gui/shim） | ✅ 通过 |
| `go test ./... -short -count=1` | ✅ **29 包全绿** |
| `npx vitest run` | ✅ **29 文件 / 181 用例全绿** |
| `npx vite build` | ✅ 9.44s 通过 |
| `npx playwright test e2e/app.spec.js` | ✅ **18/18** |

## 二、第 1 轮：架构分析

**总体评价**：Spec 035 实现与方案一致性高（F1-F11 全部按 solution.md 落地且守卫测试齐全）；单二进制多态入口、批量更新 composable 分层、TUI 状态机键序设计自洽。发现集中在**交互完整性**而非结构：

- **N1（P2）**：TUI updatesView 分支 early return 跳过主视图 errBar——批量失败/registry miss 在更新视图内无反馈（app.go View() :307-315 vs :364-367）。
- **N3（P2）**：updatesViewString「更新中: N/M」行为死代码——F4 替换渲染后其可见前提（updateQueue!=nil && installState==nil）不可达；installState 全部清空路径（InstallResultMsg 成功/失败、取消键、registry miss）都同步清队列，穷举核实。
- bindings 构建独立入口（main_bindings.go）不受 F1 影响；main.go 与 root.go 的 MaybeExec 双路径互斥（零参/带参 early return 分流）✅。

## 三、第 2 轮：Code Review + Bug 发掘

- **N2（P2）**：安装进行中全部二级安装入口未拦截。可达性经 InstallProgressDialog 的 **Run in Background 后门**（:18/:49）确认真实：用户收起进度框后点任何单项安装（DashboardPage 行按钮无 disabled / UpdateBadge 弹层 :loading 仅绑 updateInstalling map / 抽屉/版本管理器同类）→ doInstall 弹 unverified 确认 → startInstallConfirmed → installInternal active 守卫拒绝 → **静默失败**且批量计 failed 并 setShowProgress(false) **误关正在进行的安装进度框**。
- **N4（P3）**：批量每项完成时 handleInstallComplete 同步置 showProgress=false、循环下一项再 true——一帧闪烁；handleInstallCancel（:1008）同病。
- **N5（P3）**：批量取消双 toast（cancelDownload 的 download_cancelled + composable 的 install_all_cancelled）。
- useInstall 等待器三闭包变量全读写路径核验无新问题（cancelRequested 跨 repair 语义一致、lastResult 竞态兜底正确、dispose 单槽幂等）。

## 四、第 3 轮：安全审计

### 加固现状确认
- 本次全部改动**零新增外部面**（无新依赖/端点/文件写入点；useBatchUpdates 纯前端逻辑、F11 emit 进程内事件、F6/F7 是收严）。
- 既有加固未被触碰且在位：runXdevCommand 子命令白名单、TOFU sidecar、IsSafeExternalURL fail-closed、SecureRedirectCheck、zip-slip 防护、200MiB 上限、隔离/远程执行白名单。
- F1 使零参数 shim 分发与带参一致——收窄（而非扩大）了不可信 PATH 下同名 shim 的 GUI 攻击面。
- Naive UI dialog content 为文本渲染（非 v-html）——tool/version 字符串无 XSS 面。

### 残留风险（记录，不在本 Spec 处理）
1. Go 1.26.5 stdlib govulncheck 报告（GO-2026-6218 等）——需升 1.26.6（HANDOFF 既有记录）。
2. e2e mock 与真实后端字段一致性靠人工维护（GetBootstrapState mock 与后端 BootstrapState 目前一致；数据 fixture 重复维护成本 OBS）。

## 五、审查过程中被驳回的疑点（防止后续会话重复调查）

| 疑点 | 驳回证据 |
|---|---|
| F1 修复影响 bindings 构建 | 独立 main_bindings.go（//go:build bindings）；`go build -tags bindings ./...` exit=0 |
| F1 对非交互/计划任务调用者行为变化 | xdev/xdev-*/xdev-gui* 名 MaybeExec 在 Lookup 前短路（shim.go:64-74），行为与修复前一致 |
| confirmUnverified onClose/onNegativeClick 双触发 | Promise resolve 幂等，二次调用 no-op |
| i18n 多占位符机制 | i18n/index.ts:31-34 逐键 replace，任意数量占位符均可替换 |
| installing_installing i18n 键缺失 | zh.ts:220、en.ts:236 均存在 |
| useInstall dispose 后 install 悬挂 | AppContent 是根组件，卸载=退出，窗口极小（OBS） |
| TUI 'r' 刷新后队列脱钩 | 队列是按键时刻快照，语义正确 |
| repair() 不重置 cancelRequested | repair 与 install 共用 installWithOptions/dlMgr 链路，cancel 语义一致 |
| 批量+单项事件串扰 | belongsToCurrentTask 按 taskId 判定，批量串行无并发项 |
| 进度框模态可挡住并发安装入口 | **不成立**——Run in Background 后门使其可达（见 N2） |

## 六、修复方案与任务指针（实施阶段用）

- 方案定稿（4 轮审计循环 3+1+2+0 收敛）：`solution.md`（FINAL v3.1；审计轨迹含 A-1/A-2/A-3/B-1/B-2/B-3 六项 ISSUE 的吸收记录）
- 任务卡（按序）：`issues/01-n1-n3-tui-updates-view.md` → `02-n2-install-mutex-guard.md` → `03-n4-n5-batch-ux.md` → `04-changelog-regression.md`；依赖：03 blocked by 02（useBatchUpdates 签名同卡）；04 收尾
- 测试用例与 DoD：`test-plan.md`

## 七、环境与注意事项（新会话必读）

1. **GOROOT 必须带 `\go` 后缀**：`E:\XDev\install\go\1.26.5\go`；PATH 前插 `E:\XDev\install\go\1.26.5\go\bin;D:\Env\golang\bin`。
2. 构建 `go build ./...`；前端 `cd frontend && npx vitest run && npx vite build`；e2e `npx playwright test e2e/app.spec.js`（基线 18/18）。
3. 工作树含未提交改动（Spec 035 + 本轮 e2e 快照/mock 修复）——Spec 036 修复叠加其上，任务卡行号以当前工作树为准。
4. **子代理可靠性**：本会话 6 次子代理失败（上下文耗尽）——委派审查任务时输入必须收窄（文档审计可行，全库代码审查不可行）；方案审计循环可由主线自查执行。
5. 文档变更约定：HANDOFF.md 会话协议、.scratch issue-tracker 单文件 per ticket、STATUS 行标记。
