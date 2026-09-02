# Solution — Spec 035 三轮分析问题修复（最终版）

> 状态：**FINAL（审计循环收敛：v1→v2→v3→v4 四轮，发现 7+11+4+0 个问题，全部吸收后第 4 轮收敛审计 0 问题）**
> 审计轨迹：第 1 轮独立子代理（7 ISSUE：F2 高危重设计、F5 直赋路径、F3 第三处、F4/F7 微调、测试缺失、F2b 误报）；第 2 轮独立子代理（11 ISSUE：PreviewInstall catch 矛盾、showProgress 不复位、F4 叠加宣称不成立、事件竞态×2、伪码错误×2、超时不 emit、形态对齐、测试基建、spec 漂移）；第 3 轮自查（4 ISSUE：result.result 死代码、idle 未落地、F11 升格、CHANGELOG）；第 4 轮收敛核验（0 问题）。

## 问题清单（11 项）

| # | 定级 | 问题 | 证据 |
|---|------|------|------|
| F1 | P0 | 零参数 shim 绕过 MaybeExec 误启 GUI | main.go:45-47 vs root.go:50；实测证实 |
| F2 | P0 | 一键更新全部对 unverified 版本空转 + 假成功 | AppContent.vue:289-307, :942-951 |
| F3 | P1 | TUI 更新队列残留（Err/取消/registry 未命中三处不清） | app.go:265-268, :485-494; updates.go:81-85 |
| F4 | P1 | TUI 更新视图遮蔽安装进度（键序 + View 序） | app.go:216-224, :302-305 |
| F5 | P2 | PhaseInstalling percent=-1 TUI 显示 -1%（store 与直赋两路都漏） | installer.go:471-477; tui/install.go:137,146; app.go:237 |
| F6 | P2 | findAsset partial-match 可选中 xdev-gui 归档 | update.go:318-323 |
| F7 | P2 | parseSHA256SUMS 接受非标准单空格分隔 | update.go:494-498 |
| F8 | P2 | runXdevCommand 优先 LookPath（PATH 注入面） | app_updates.go:310-316 |
| F9 | P2 | TUI help 未收录 [x] 键 | help.go:15-28 |
| F10 | P2 | GUI/installer kind 判定不同源（EffectiveKind vs resolveKind） | app_install.go:318 vs installer.go:982-994 |
| F11 | P2 | 下载轮询 1h 超时后不 emit 任何事件（前端等待永挂，单装/批量同病） | app_install.go:400-403 |

## F1（P0）零参数 shim 误启 GUI

`main.go` 零参数分支在 `runGUI()` 前先做 shim 判定（错误处理与 root.go:51-58 同构；import 增 `path/filepath` + `internal/shim`）：

```go
if len(os.Args) <= 1 {
    if handled, err := shim.MaybeExec(filepath.Base(os.Args[0])); handled {
        if err != nil {
            if exitErr, ok := err.(*shim.ExitCodeError); ok {
                os.Exit(exitErr.Code)
            }
            fmt.Fprintln(os.Stderr, "shim:", err)
            os.Exit(1)
        }
        os.Exit(0)
    }
    runGUI()
    return
}
```

已核实（两轮审计 PASS）：xdev/xdev-*/xdev-gui*/wailsbindings 在 Lookup 前短路零副作用（shim.go:64-74）；零参与带参两条 MaybeExec 路径互斥（early return）；main→shim 无 import cycle；bindings 入口不受影响；`xdev doctor`/`self` 等自引用带参调用走 root.go 路径不受影响。

## F2（P0）一键更新全部（v4：分支重写）

### 设计原则
1. FR-018 逐项确认：unverified 每项单独弹确认，拒绝→跳过该项继续；PreviewInstall 失败（旧构建）同样弹确认，绝不静默跳过。
2. 事件驱动等待（waitForCompletion），不忙等。
3. 取消 = 取消当前项并中止整批。
4. 真实汇总（成功/失败/跳过），取消另有文案；删除 install_all_done。
5. 批量形态：默认形态，仅「形态特定且唯一形态」自动选定（对齐单装）；不弹形态选择器。

### 新增 `frontend/src/composables/useBatchUpdates.js`（依赖全注入，可测）

```js
export function useBatchUpdates({
  previewInstall, installConfirmed, waitForCompletion,
  dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
}) {
  function confirmUnverified(tool, version) {
    return new Promise((resolve) => {
      dialog.warning({
        title: t('install.unverified_title') || 'Unverified Version',
        content: t('install.unverified_content', { tool, version }),
        positiveText: t('btn.install'), negativeText: t('btn.skip'),
        closable: true,
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
      });
    });
  }

  async function installAllUpdates() {
    const list = [...availableUpdates.value];
    if (!list.length) return;
    const ok = await new Promise((resolve) => {
      dialog.warning({ /* 既有批量确认，参数不变 */ });
    });
    if (!ok) return;
    let done = 0, failed = 0, skipped = 0, cancelled = false;
    try {
      for (const u of list) {
        let preview = null;
        try { preview = await previewInstall(u.tool, u.latestVersion); } catch { preview = null; }
        const unverified = preview ? !!preview.unverified : true; // 旧构建无 PreviewInstall → 按 unverified 对待
        if (unverified && !(await confirmUnverified(u.tool, u.latestVersion))) { skipped++; continue; }

        let formFilter = '';
        if (preview?.hasFormSpecificArchives && (preview.availableForms || []).length === 1) {
          formFilter = preview.availableForms[0];
        }

        setShowProgress(true);
        const started = await installConfirmed(u.tool, u.latestVersion, formFilter);
        if (!started) { failed++; setShowProgress(false); continue; }

        // waitForCompletion 返回：'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'
        const result = await waitForCompletion();
        if (result === 'cancelled' || result === 'disposed') { cancelled = true; break; }
        if (result === 'completed') { done++; await loadUpdates(); }
        else { failed++; setShowProgress(false); }   // 'error' | 'idle'（idle=事件全丢，保守计失败）
      }
    } finally {
      setShowProgress(false); // 安全网：任何退出路径都复位
    }
    if (cancelled) message.warning(t('updates.install_all_cancelled', { done, failed, skipped }));
    else message.info(t('updates.install_all_summary', { done, failed, skipped }));
  }
  return { installAllUpdates };
}
```

AppContent.vue：删除原 `installAllUpdates` 函数，装配 composable；`@install-all-updates` 指向其返回值；`setShowProgress: (v) => { showProgress.value = v }`。解构风格与 AppContent.vue:202 一致（useInstall 返回对象新增 waitForCompletion 后一并解构）。

### `useInstall.js` 改动

```js
let completionWaiter = null;
let lastResult = null;          // 事件先于 waiter 注册的竞态兜底
let cancelRequested = false;    // 取消竞态标记

function notifyCompletion(result) {
  lastResult = result;
  const w = completionWaiter; completionWaiter = null;
  if (w) w(result);
}
// installInternal 开头：cancelRequested = false（每项重置）
// onProgress 的 phase==='Completed' 分支：onComplete?.(data) 后追加 notifyCompletion('completed')
// onError：开头 `if (cancelRequested) { phase.value='Cancelled'; active.value=false;
//            paused.value=false; notifyCompletion('cancelled'); return; }`（吞「下载已取消」toast，
//            belongsToCurrentTask 守卫仍在最前）；否则原逻辑 + notifyCompletion('error')
// cancel()：`await CancelDownload` 之前置 cancelRequested=true；catch 内复位 false
// dispose()：unsubscribe 前 notifyCompletion('disposed')

function waitForCompletion() {
  if (!active.value) {          // 事件已先到（如已装快路径同步 emit Completed）
    const r = lastResult; lastResult = null;
    return Promise.resolve(r ?? 'idle');
  }
  lastResult = null;
  return new Promise((resolve) => { completionWaiter = resolve; });
}
// 返回对象新增 waitForCompletion
```

双重 notify 幂等性（已核验）：cancel() 与 onError(cancelRequested) 都可能 notify 'cancelled'——单槽保证第二个 no-op（waiter 已空）；cancel 前置守卫 `!active` 拦截「已完成后再取消」；CancelDownload throw 时 cancelRequested 复位、不 notify（等待后续真实事件）。快速路径（已装快路径同步 emit Completed，app_install.go:185-191）事件先到：onProgress 置 active=false + notify → waiter 未注册仅存 lastResult → waitForCompletion 早退返回 'completed'。正确。

### i18n（en/zh 同步）
新增：`btn.skip`（Skip / 跳过）、`updates.install_all_summary`（"Updates finished: {done} succeeded, {failed} failed, {skipped} skipped" / "更新结束：成功 {done}，失败 {failed}，跳过 {skipped}"）、`updates.install_all_cancelled`（"Update batch cancelled: {done} succeeded, {failed} failed, {skipped} skipped" / "批量更新已取消：成功 {done}，失败 {failed}，跳过 {skipped}"）。删除：`updates.install_all_done`（唯一引用 AppContent.vue:309 随旧函数移除）。占位符机制与 `install_all_confirm` 的 {n} 相同（i18n/index.ts:28-37 逐键 replace），已核验。

### 审计驳回项（记录）
- F2b（第 1 轮）：`useSniffSession.installUpdate` 单项无空转（非阻塞对话框即确认 UI）——第 2 轮复核驳回成立，不改。

## F3（P1）TUI 更新队列残留

三处清理（两轮审计 PASS）：
1. `app.go` InstallResultMsg Err 分支（:265-268）：return 前清 `updateQueue`/`updateIdx`。
2. `app.go` `handleInstallOverlayKey`（:485-494）：取消时清。
3. `updates.go` `startToolUpdate` registry 未命中（:81-85）：清（含 updateIdx）。

## F4（P1）TUI 更新视图遮蔽（v4：overlay 替换 + 内嵌批量行）

1. 键序：installState 判定移到 updatesView 之前（Esc 先取消安装，取消后再 Esc 退视图；与 F3 取消清队列联动）。
2. View()：updatesView 分支内 installState 非空时 `return m.viewInstallOverlay(m.updatesViewString())`——**替换渲染**（既有 viewInstallOverlay 忽略 base 参数，install.go:129-163；主视图 overlay 同行为，不宣称叠加）。
3. 批量上下文内嵌 overlay（值接收者只读访问 updateQueue/updateIdx；updateQueue==nil 时行隐藏，普通单装 overlay 输出不变）：
```go
// viewInstallOverlay 内 "Installing %s@%s" 行之后：
if m.updateQueue != nil {
    b.WriteString(StyleDim.Render(fmt.Sprintf("  Batch %d/%d", m.updateIdx+1, len(m.updateQueue))) + "\n")
}
```

## F5（P2）PhaseInstalling percent=-1

1. `progressState.store`：`percent < 0` 时**只更新 phase**（percent/speed/eta 全保持——与 GUI「整体保持只换文案」一致；常见时序下载完 percent=100 → Installing 显示 100%，与 GUI Percent:100 对齐）。
2. `app.go` InstallProgressMsg：`if msg.Percent >= 0 { m.installState.Percent = msg.Percent }`（防御层：源头被 1 挡住后真实链路不可达）。
3. `viewInstallOverlay`：渲染局部 `percent := s.Percent; if percent < 0 { percent = 0 }`（纯 clamp）。

## F6（P2）删 findAsset partial-match（update.go:318-323）

两轮核实：CI 全平台产物与精确名逐字一致；AppImage 命名不含 goos/goarch 成对子串不受影响；deb/rpm 误命中正是要防的。仅保留精确匹配。

## F7（P2）删 parseSHA256SUMS 单空格分支（update.go:494-498）

仅认标准双空格；同步将 `TestParseSHA256SUMS_SingleSpace`（update_test.go:201-210）改为负向断言（单空格行不解析成功）。

## F8（P2）runXdevCommand 自身优先（app_updates.go:310-316）

`os.Executable()` 优先，失败回退 `exec.LookPath("xdev")`。生产零调用（仅测试），无现存影响。

## F9（P2）help 补 `{"x", "Available updates (sniff)"}`（插在 `r` 之后）

键位无冲突（两轮核实）。updates.go:136 页脚提示视图内键（[a]/[r]/[Esc]），与 help 主模式键表分工不同，无需同步。

## F10（P2）ResolveKind 导出

`resolveKind` → `ResolveKind`（nil-guard：`if a == nil { return manifest.InstallKindArchive }`）；installer 内 5 处调用点（:472/485/1002/1088/1377）机械同步；`app_install.go:318` 改用。无 import cycle（gui→installer 已存在）。

## F11（P2）下载轮询超时不 emit（app_install.go:400-403）

maxPolls 耗尽分支补 emit（与同函数 StatusFailed/Cancelled 分支同格式，taskID 在作用域内）：
```go
wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
    "taskId": taskID, "tool": toolName, "version": version,
    "error": "安装等待超时（1 小时）",  // 与同函数既有中文 errMsg 风格一致（:385/:388）
})
```
单装路径（进度框等 Completed 永不来）与批量路径（waitForCompletion 永挂）同受益。

## 测试策略（守卫测试）

| 修复 | 测试 | 位置（已核实存在/有先例） |
|---|---|---|
| F1 | MaybeExec 零参数别名分派 / xdev 名零副作用 | `internal/shim/shim_test.go`（:341-377 已有 TestMaybeExec_* 基建） |
| F2 | useBatchUpdates 单测（分支覆盖：确认→装、拒绝→skip、PreviewInstall 抛错→仍弹确认、唯一形态自动选定、started=false→failed、completed→done、error/idle→failed、cancelled→break+warning 文案、disposed→break、finally 复位 showProgress） | 新 `frontend/test/composables/useBatchUpdates.spec.js`；dialog mock 先例 usePackageManagers.spec.js:20（`vi.fn((opts) => opts)` + 手动调 `onPositiveClick()`） |
| F2 | waitForCompletion 四路 resolve + lastResult 竞态（快路径事件先到）+ cancelRequested 吞错 | 扩展 `frontend/test/composables/useInstall.spec.js`（listeners 手动 emit 基建 :46 已有） |
| F3 | Err 清队列 / 取消键清队列 / registry 未命中清队列 | `internal/tui/tui_test.go`（同包构造 Model 直发 Msg） |
| F4 | 键序（updatesView+installState 时 Esc 走取消）；View 含 "Batch 2/5"；普通单装无 Batch 行 | 同上 |
| F5 | InstallProgressMsg{Percent:-1} 不改 installState.Percent；View 无 "-1%" | 同上 |
| F6 | 负向：`xdev-gui-windows-amd64-v1.0.zip` 不被选中 | `internal/update/update_test.go` |
| F7 | 单空格行解析失败（负向改造） | 同上 |
| F10 | ResolveKind(nil) 不 panic + rustup URL 启发式 | `internal/installer/kind_routing_test.go` |
| F11 | （后端 goroutine 难单测；以代码走查 + 前端 idle 分支保守计失败兜底） | — |

AppContent.vue 不新增挂载测试（App.test.js 均 stub 之，无基建）——批量逻辑全在 composable 由单测覆盖，AppContent 仅 ~10 行装配，靠既有全量 + 手动冒烟。

回归门：`go build ./...` + `go vet`（改动包）+ `go test ./... -short -count=1` + `npx vitest run` + `npx vite build` 全绿；F1 手动冒烟（别名零参数分发 + xdev 零参数起 GUI）；F2 手动冒烟（批量装 2 个嗅探版本 + 中途取消）。

## 交付顺序（任务拆分依据）

1. **F1**（main.go，独立）
2. **F6+F7**（update.go 同文件 + 测试）
3. **F8**（app_updates.go）
4. **F10**（installer.go 导出 → gui 引用；先于 F2 便后续共审 app_install.go）
5. **F11**（app_install.go 超时 emit）
6. **F5**（tui/install.go + tui/app.go）
7. **F3 → F4**（F4 交互连贯依赖 F3 取消清队列；tui 包内顺序）
8. **F2**（useInstall.js 等待器 → useBatchUpdates.js → AppContent 装配 → i18n；F11 先行使批量不依赖「Cancel 逃生」）
9. **F9**（help.go，随 F3/F4 一起提交亦可）
10. **CHANGELOG.md** `[Unreleased]` 记录 Spec 035（CHANGELOG.md:6 已有该节）

同批发布约束：F2 前端与 F11 后端同批（否则批量挂死依赖手动 Cancel）；其余无跨批依赖。

## 风险与回滚

- 结构性改动仅 F2 composable 提取（新文件 + AppContent 装配替换）；useInstall 等待器为纯增量（cancelRequested 仅分流取消场景的 onError）。
- 其余均为单点小改，逐文件可独立 revert。
- 无数据格式/接口变更；manifest/签名/下载链路不动（F6/F7 是收窄不是放宽）。
