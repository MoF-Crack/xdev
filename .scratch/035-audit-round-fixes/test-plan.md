# Test Plan — Spec 035 三轮分析问题修复

> 状态：**FINAL（测试用例与验收标准经审计循环定稿：初稿 → 审计 2 问题【TC-F1 断言不可行、用例与既有覆盖重复】→ 修订 → 复审 0 问题）**

## 一、单元/组件测试用例（守卫测试）

### TC-F1（ticket 01，`internal/shim/shim_test.go` 扩展）

> MaybeExec 不感知参数数量（零参数分发的语义 = 对当前 basename 调 MaybeExec），现有 `TestMaybeExec_XdevName` / `_XdevExeName` / `_ShimName`（:343-381）已覆盖 xdev 短路与非 xdev 分发。新增仅 1 例补齐「别名命中真实目标」的正路径：

| ID | 前置 | 步骤 | 断言 |
|---|---|---|---|
| TC-F1-01 | fake home：`current/fakepy/` 下放测试二进制副本（`go test` 可用 `os.Executable()` 复制）命名为 fakepy | `t.Setenv XMDEV_HOME` + `config.ResetHomeDirForTest()` + `InvalidateShimIndex()` 后 `MaybeExec("fakepy")` | `handled=true, err==nil`（正路径分发成功；不断言 run 内部目标——与既有 :367-381 模式一致，run 的 fork/exec 已被真二进制满足） |

main 分支（零参数先判定）无法单测（main 包）——TC-F1-01 + 既有 3 用例覆盖判定入口语义 + 冒烟 MS-F1 覆盖接线。

### TC-F2a（ticket 07，`useInstall.spec.js` 扩展）

| ID | 前置 | 步骤 | 断言 |
|---|---|---|---|
| TC-F2a-01 | install 挂起中 | emit `installProgress{taskId, phase:'Completed', percent:100}` | onComplete 被调、`waitForCompletion()` resolve `'completed'` |
| TC-F2a-02 | 同上 | emit `installError{taskId, error:'x'}` | resolve `'error'`、error.value 非空 |
| TC-F2a-03 | 同上 | `cancel()`（mock CancelDownload 成功） | resolve `'cancelled'`、phase='Cancelled' |
| TC-F2a-04 | 挂起中（CancelDownload mock 挂起） | 先 `cancel()`（不 await），再 emit `installError「下载已取消」`，然后放行 CancelDownload | 单一 resolve `'cancelled'`（事件分流优先，cancel() 自身 notify no-op）、error.value 为 null、reportError 未调 |
| TC-F2a-05 | cancel 已完成（active=false） | emit `installError` | onError 守卫 return，lastResult 不变 |
| TC-F2a-06 | install 挂起中 | 先 emit `Completed`（waiter 未注册），后调 `waitForCompletion()` | 返回 `'completed'`（lastResult 兜底，非 'idle'） |
| TC-F2a-07 | effectScope | scope.stop()（dispose） | pending waiter resolve `'disposed'` |
| TC-F2a-08 | 回归 | 既有 10 用例原样跑 | 全绿 |

### TC-F2b（ticket 08，新 `useBatchUpdates.spec.js`）

依赖注入 mock：`previewInstall`、`installConfirmed`、`waitForCompletion`、`dialog.warning: vi.fn((opts)=>opts)`（先例 usePackageManagers.spec.js:20——测试内手动调 `opts.onPositiveClick()` 模拟用户点击）、`message`、`t: (k,p)=>k+JSON.stringify(p||{})`、`availableUpdates: ref([...])`、`setShowProgress: vi.fn()`、`loadUpdates: vi.fn()`。

| ID | 场景 | 断言 |
|---|---|---|
| TC-F2b-01 | 2 项 unverified，逐项确认均点安装 | 每项 1 次确认对话框；installConfirmed 调 2 次（第二次等待第一次 waitForCompletion resolve 后）；done=2；summary 文案含 done:2 |
| TC-F2b-02 | 第 1 项点跳过、第 2 项点安装 | skipped=1 done=1；installConfirmed 仅调 1 次 |
| TC-F2b-03 | previewInstall 抛错（旧构建模拟） | 仍弹确认对话框（unverified=true 路径），不静默跳过 |
| TC-F2b-04 | preview 返回 hasFormSpecificArchives + availableForms:['cli'] | installConfirmed 第 3 参 'cli'（唯一形态自动选定） |
| TC-F2b-05 | installConfirmed 返回 false（启动失败） | failed=1；setShowProgress(false) 被调；继续下一项 |
| TC-F2b-06 | waitForCompletion resolve 'error' | failed=1；setShowProgress(false)；继续 |
| TC-F2b-07 | waitForCompletion resolve 'cancelled'（第 1 项后） | break；第 2 项不再调 installConfirmed；message.warning（install_all_cancelled 文案） |
| TC-F2b-08 | waitForCompletion resolve 'idle' | failed=1（保守计失败，不误计成功） |
| TC-F2b-09 | 任意退出路径 | finally 后 setShowProgress(false) 恰好被调（末次为 false） |
| TC-F2b-10 | 批量确认对话框点取消 | installConfirmed 零调用，无 summary |

### TC-F3/F4/F9（ticket 06，`tui_test.go` 扩展，同包构造 Model 直发 Msg）

| ID | 步骤 | 断言 |
|---|---|---|
| TC-F3-01 | 置 updateQueue/updateIdx，发 `InstallResultMsg{ID:匹配, Err:...}` | updateQueue==nil、updateIdx==0 |
| TC-F3-02 | 置队列+installState（ID 匹配），按键 Esc | installState==nil 且队列已清（取消清理） |
| TC-F3-03 | 构造 registry 未命中（清空 registry 或用未知名）置队列调 startToolUpdate | errMsg 非空且队列已清 |
| TC-F4-01 | updatesView=true + installState!=nil，按 Esc | 走 handleInstallOverlayKey（installState 清空），updatesView 仍 true |
| TC-F4-02 | 同上渲染 View() | 输出含 "Batch"（批量队列非空时） |
| TC-F4-03 | 普通 installState（无队列）渲染 View() | 输出无 "Batch"（单装不变） |
| TC-F9-01 | 渲染 help overlay | 键表含 "x" 条目 |

### TC-F5（ticket 05，`tui_test.go` 扩展）

| ID | 步骤 | 断言 |
|---|---|---|
| TC-F5-01 | installState.Percent=50，发 `InstallProgressMsg{Percent:-1, Phase:Installing}` | Percent 仍 50、Phase 已更新 |
| TC-F5-02 | 强制 installState.Percent=-1 渲染 View() | 输出不含 `-1%`（clamp 生效） |
| TC-F5-03 | 发 `InstallProgressMsg{Percent:50→PhaseActivating}` 正常值 | Percent 更新为 50（分流不误伤） |

### TC-F6/F7（ticket 02，`update_test.go`）

| ID | 步骤 | 断言 |
|---|---|---|
| TC-F6-01 | release assets=[`xdev-gui-windows-amd64-v1.0.zip`, `xdev-linux-amd64-v1.0.tar.gz`]，findAsset 精确名缺失（windows） | 返回 not found（partial-match 不再命中 gui 包） |
| TC-F6-02 | 回归：精确名存在 | 命中（既有用例） |
| TC-F7-01 | parseSHA256SUMS 输入单空格行 `abc file.zip` | 解析失败（负向） |
| TC-F7-02 | 回归：双空格标准行 | 解析成功（既有用例） |

### TC-F10（ticket 03，`kind_routing_test.go`）

| ID | 步骤 | 断言 |
|---|---|---|
| TC-F10-01 | `ResolveKind(nil)` | 返回 archive 不 panic |
| TC-F10-02 | URL 含 `sh.rustup.rs` 且 InstallKind 为空 | 返回 script（启发式保留） |

### TC-F11（ticket 04）

后端 goroutine 轮询结构难单测（1h maxPolls 常量）——以代码走查验收（emit 在 Cancel/RemoveTask 前、键名一致）+ 前端 TC-F2b-08 的 idle 保守兜底。**决策记录**：不为此引入可注入 clock 重构（超出最小化原则）。

## 二、回归门（ticket 09，全绿才可提交）

1. `go build ./...`
2. `go vet ./internal/tui ./internal/installer ./internal/update ./internal/gui`
3. `go test ./... -short -count=1`（基线 29 包全绿）
4. `cd frontend && npx vitest run`（基线 28 文件 161 用例全绿 + 新增 spec）
5. `cd frontend && npx vite build`
6. e2e（`npx playwright test e2e/app.spec.js`，基线 18/18）：**决策记录**——本次改动不触及 e2e 覆盖面（dashboard 按钮仍存在、install 链路不变），跑一次确认无回归；若环境不可用（浏览器依赖）允许降级记录为「e2e 未跑，理由」，不阻塞提交。

## 三、手动冒烟（验收演示路径）

| ID | 场景 | 步骤 | 通过标准 |
|---|---|---|---|
| MS-F1 | 零参数 shim 分发 | 构建产物复制为 `python.exe`（存在已装 python 时）；分别零参数运行 `python.exe`、带参 `python.exe --version`、零参数 `xdev.exe` | python 零参数进 REPL（不弹 GUI）；带参正常；xdev 零参数起 GUI |
| MS-F2 | 批量更新全流程 | GUI 嗅探≥2 更新 → 全部更新 → 逐项确认 → 观察 | 逐项弹确认；进度框逐项推进；结束汇总「成功 N/失败 M/跳过 K」；列表刷新 |
| MS-F2-C | 批量中途取消 | 同上第 2 项安装中点进度框 Cancel | 当项取消、整批中止、warning 文案；再点全部更新可正常重跑 |
| MS-TUI | TUI 更新视图 | `xdev tui` → x → a（批量）→ Esc | overlay 显示进度+Batch N/M；Esc 取消且队列不续跑；再 Esc 退视图；help 含 [x] |

## 四、验收标准（Definition of Done，全 ticket）

1. 该 ticket Files 列表内文件（且仅这些文件）被修改（`git status` 核对，防夹带）。
2. 该 ticket 全部 TC 用例存在且绿（新增守卫测试与实现同 commit）。
3. 回归门第 1-5 项全绿（每个 ticket 完成时跑改动包级；ticket 09 跑全量）。
4. 手动冒烟（涉 GUI/TUI 交互的 ticket：01/06/08）按 MS 表执行并记录结果。
5. 基线对照：`go test` 29 包、vitest 文件/用例数 ≥ 基线（只增不减）；新增 i18n 键 en/zh 同步（TC-F2b 汇总文案断言覆盖）。
