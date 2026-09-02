# Solution — Spec 036 第二轮审查问题修复（v3）

> 状态：**FINAL v3.1**（3 轮自查审计收敛：3+1+2 ISSUE 全部吸收；第 4 轮核验 0 新问题）。
> 审计轨迹：第 1 轮（子代理失败转自查）：3 ISSUE（A-1 UpdateBadge 入口遗漏 / A-2 handleInstallCancel 漏守卫 / A-3 Esc 提示误导）→ v2。
> 第 2 轮（自查）：1 ISSUE（B-1 抽屉/版本管理器安装入口经 doInstall→startInstallConfirmed 同病，prop/包装式守卫不完备）→ v3 改守卫收敛点。
> 第 3 轮（自查）：2 ISSUE（B-2 进度框有 Run in Background 后门——N2 并发场景真实可达，且需逐项预检防批量逐项撞守卫弹 N 条警告 / B-3 Esc 键与 mask 点击在 active 时已被 requestClose 守卫挡住，确认无需处理）→ v3.1。
> 第 4 轮（核验）：逐项复核 N1-N5 修改点与测试可行性，0 新问题。**收敛**。

## 问题清单（5 项）

| # | 定级 | 问题 |
|---|------|------|
| N1 | P2 | TUI updatesView 激活期间 errMsg 不渲染 |
| N2 | P2 | 安装进行中全部二级安装入口未拦截（DashboardPage 行 / UpdateBadge 弹层 / 抽屉 / 版本管理器 / 修复）→ active 冲突静默失败 + 批量误关进度框 |
| N3 | P2 | TUI updatesViewString「更新中: N/M」死代码 |
| N4 | P3 | 批量完成项进度框闪烁（handleInstallComplete 与 handleInstallCancel 都置 showProgress=false） |
| N5 | P3 | 批量取消双 toast |

## N1 + N3（P2）：TUI 更新视图渲染收口

### N1 errMsg 渲染
updatesViewString 末尾内嵌 errMsg 行（**不带** "(press Esc to dismiss)" 提示——updates 视图 Esc 语义是退视图而非清 errMsg，提示会误导）：
```go
// updatesViewString 在页脚 [a]/[r]/[Esc] 行之前：
if m.errMsg != "" {
    sb.WriteString("\n" + StyleError.Render(fmt.Sprintf("  Error: %s", m.errMsg)) + "\n")
}
```
复用既有 `StyleError`（styles.go:186）。errMsg 清空时机不变（RefreshMsg :292 重置；退视图后主视图 errBar 再以 Esc 清）。

### N3 死代码删除
删除 `updates.go:133-135`（`if m.updateQueue != nil` 块）——F4 后该行唯一可见前提（updateQueue!=nil && installState==nil）不可达，批量进度由 overlay 的 `Batch N/M` 行单一承担。

## N2（P2）：安装互斥守卫（v3：守卫收敛点，全入口覆盖）

**设计演进**：v1 按钮禁用（漏 UpdateBadge）→ v2 handler 包装（漏抽屉/版本管理器）→ v3 **守卫 startInstallConfirmed / startRepairConfirmed 收敛点**——所有安装/修复路径（行按钮、弹层、抽屉、版本管理器、形态选择器、修复、unverified 确认）都经这两个函数，一处守卫全覆盖且给用户明确反馈。

```js
// AppContent.vue startInstallConfirmed（:956）与 startRepairConfirmed（:979）开头：
if (installing.value) {
  message.warning(t('install.busy'));
  return;
}
```

安全性论证：批量适配器（:295）也调 startInstallConfirmed，但批量循环严格串行——waitForCompletion 在 active=false 时才 resolve，适配器调用时 active 必为 false，守卫不误伤 ✅。

可达性确认（B-2）：进度框是模态（mask+focus trap）但有「Run in Background」后门（InstallProgressDialog.vue:18/:49）——用户可在安装进行中收起对话框操作界面，N2 并发场景真实可达，非纯理论。

配套（三层）：
1. **useBatchUpdates 入口守卫**（接收 useInstall 的 active ref）：安装进行中点「全部更新」直接提示不进循环：
```js
async function installAllUpdates() {
  if (active.value) { message.warning(t('install.busy')); return; }
  // ... 既有循环
}
```
2. **逐项预检（B-2）**：批量循环每项 installConfirmed 前检查——后台运行后用户启动了单项安装时，批量让位退出（而非剩余项逐个撞守卫弹 N 条警告）：
```js
for (const u of list) {
  if (active.value) { cancelled = true; break; }  // 用户插入安装，批量让位
  // ... 既有逐项逻辑
}
```
3. **started=false 不关进度框**：useBatchUpdates 的 installConfirmed started=false 分支删 `setShowProgress(false)`——进度框属正在进行的另一安装；批量循环退出时 finally 统一复位。

i18n 新增 `install.busy`（en: "An installation is already in progress"；zh: "已有安装进行中"）。

## N4（P3）：批量完成项进度框闪烁

AppContent 新增 `batchActive` ref 传入 useBatchUpdates（循环开始置 true、finally 置 false）。**两个回调**（v2 已修 A-2）的 showProgress=false 行加守卫：
```js
async function handleInstallComplete(data) {
  if (!batchActive.value) showProgress.value = false;   // 批量中复位置于 composable 退出时
  message.success(t('install.success', { tool: data.tool, version: data.version }));
  await refreshInstallViews(data.tool);
}
async function handleInstallCancel(data) {
  await refreshInstallViews(data.tool);
  await nextTick();
  installTrigger.value = document.querySelector(`[data-action="detail"][data-tool="${CSS.escape(data.tool)}"]`) || installTrigger.value;
  if (!batchActive.value) showProgress.value = false;
}
```
useBatchUpdates：循环开始 `batchActive.value = true`；`finally { batchActive.value = false; setShowProgress(false); }`。
单装路径（batchActive=false）行为逐字节不变 ✅。

## N5（P3）：批量取消双 toast

```js
async function cancelDownload() {
  if (await runCancel() && !batchActive.value) message.warning(t('download_cancelled'));
}
```
单装路径不变 ✅。

## 守卫测试

| 修复 | 测试 | 位置 |
|---|---|---|
| N1 | updatesView+errMsg 渲染含 "Error:"；无 errMsg 时不含 | `internal/tui/tui_test.go`（newTUITestModel 基建） |
| N3 | dead code 删除靠既有 TC-F4-02（Batch 行）覆盖 + 代码走查 | — |
| N2 | useBatchUpdates spec：active=true 时 installAllUpdates 早退 + warning；installConfirmed started=false 时 setShowProgress 不被调 | `frontend/test/composables/useBatchUpdates.spec.js`（注入 active ref） |
| N2 | AppContent 守卫（startInstallConfirmed/startRepairConfirmed）：无 mount 基建，代码走查 + 手动冒烟 MS-N2 | — |
| N4 | useBatchUpdates spec：batchActive 循环中 true / finally false；setShowProgress 序列无 false→true 相邻抖动 | 同上 |
| N5 | cancelDownload 守卫：代码走查 + 手动冒烟 MS-N5 | — |

## 交付顺序

1. **N1+N3**（internal/tui/updates.go，单文件）
2. **N2+N4+N5**（useBatchUpdates.js + AppContent.vue + i18n en/zh，同批；batchActive 前置于 N5）
3. **回归门**（CHANGELOG + go/vitest/vite/e2e 全量 + 冒烟）

## 风险与回滚

- N1/N3 单文件小改，逐条可 revert。
- N2 收敛点守卫：批量适配器串行论证保证不误伤；started=false 分支由 finally 兜底。
- N4/N5 batchActive 纯增量开关，单装路径不变。
- 无数据格式/接口/签名变更。
