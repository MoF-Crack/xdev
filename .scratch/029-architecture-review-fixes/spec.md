# Spec 029 — 架构审查修复方案

Status: phase-b-complete (B5 deferred)
Source: `C:\Users\xinyan\AppData\Local\Temp\architecture-review-2026-08-20.html`

## 目标

把审查报告里的 **已核实 bug** 和 **加载路径问题** 先落地；架构 deepening（host-scan / App facade / Install(spec) / hardlink shim）写成后续阶段，不在本轮拆包。

## Phase A — 立刻修（本轮）

| ID | 问题 | 方案 |
|---|---|---|
| A1 | `UpdatePackageManager("pnpm")` 用 PM 名查 catalog/registry | 解析 `pmDef.toolName`（yarn/pnpm → node）。附属工具（detectCmd ≠ toolName）不走 InstallTool，返回明确错误。`HasUpdate` 只对「自身可 xdev 安装」的 PM（detectCmd == toolName）置位 |
| A2 | `localReady` 仅 15s 后 close | `loadLocalCache` 成功后立即 `signalLocalReady`；`refreshLocalCache` 启动即跑（不再 15s 延迟），仍受 shutdown 取消 |
| A3 | `resolveShimDir` / `toolScanCacheFile` 调 `config.Load("")` | 删除这两处全局 Load。探测走 `a.getCfg().Core.ShimDir`；`detectLocalToolPath(name, shims, shimDir)` |
| A4 | Windows shim 用 size 判断已最新 | 同 size 再比 mtime；mtime 也相同才跳过。不同则 rename+.copy |
| A5 | 文档谎言 | `app_tool_query.go` 注释去掉不存在的 SearchTools/GetToolDetail/RefreshToolVersions；`docs/architecture.md` 版本/Go/AppContent 行数/internal/ai 与现状对齐 |

## Phase B — deepening

| ID | 状态 | 出口 |
|---|---|---|
| B1 | **落地** | `internal/hostscan`：`Query` → `[]Hit`；`Scan`/`ScanPATH` 返回 PATH 上全部实例（跳过 shimDir）。GUI `collectPMInstances` / `detectLocalToolPath` / `LookPath` 委托它。测试用 `PathList` 注入假树 |
| B2 | **落地（不增绑定）** | App 仍是 Wails 宽 facade（不改 JS 绑定面）；实现按 catalog / install-session / host-scan / prefs 分组。`CONTEXT.md` 记下术语 |
| B3 | **落地** | `InstallSpec` 增加 Kind/Method；`App.install` 分发 tool / package-manager / sub-tool。Wails `InstallTool*` / `InstallPackageManager` / `InstallSubTool` / `RepairInstall` 转发 |
| B4 | **落地** | Windows `CreateHardLink`，失败再 copy；SameFile 则跳过 |
| B5 | **未做（需确认）** | AppContent 拆 view / one-api 移仓。不删除 `one-api/` |

## 非目标

- 不在本轮引入新 Wails 绑定或重写绑定面
- 不改 ADR-0002（Wails）/ ADR-0003（Windows 主平台）
- 不删除 `one-api/`（B5，需单独确认）
