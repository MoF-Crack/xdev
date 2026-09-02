# Test Plan — Spec 036 第二轮审查问题修复

> 状态：FINAL（经 4 轮自查审计定稿；本轮审查中的可用性/可行性已逐项核验）。

## 一、单元/组件测试用例（守卫测试）

### TC-N1（ticket 01，`internal/tui/tui_test.go` 扩展）

| ID | 前置 | 步骤 | 断言 |
|---|---|---|---|
| TC-N1-01 | `newTUITestModel` + `updatesView=true` + `errMsg="boom"` | `View()` | 输出含 `Error: boom`（`StyleError` 渲染） |
| TC-N1-02 | 同上但 `errMsg=""` | `View()` | 输出不含 `Error:` |
| TC-N1-03 | errMsg 非空 + updates 为空（暂无可用更新 + 错误） | `View()` | 两者提示行并存（"暂无…" 与 "Error: boom"） |

N3（死代码删除）无专属用例：既有 TC-F4-02（Spec 035，overlay `Batch N/M` 行）覆盖批量进度展示；删除后 `updatesViewString` 不再含「更新中」由代码走查确认。

### TC-N2（ticket 02，`useBatchUpdates.spec.js` 扩展，注入 active ref）

| ID | 场景 | 断言 |
|---|---|---|
| TC-N2-01 | `active.value=true` 调 `installAllUpdates()` | dialog 零调用；`message.warning(install.busy:…)` 被调；`installConfirmed` 零调用 |
| TC-N2-02 | 2 项 + waitImpl='completed' + installImpl 返回 false（IPC 错误路径） | failed=2；setShowProgress 序列中每项后无 false（finally 最终 false 除外） |
| TC-N2-03 | 第 1 项 completed 后置 `active.value=true`（模拟用户插入单项安装） | 第 2 项 `installConfirmed` 零调用；`message.warning(install_all_cancelled:…)`；done=1 |

### TC-N4/N5（ticket 03，`useBatchUpdates.spec.js` 扩展，注入 batchActive ref）

| ID | 场景 | 断言 |
|---|---|---|
| TC-N4-01 | 2 项 completed | `batchActive` 序列：`[true, false]`（循环中 true / finally false） |
| TC-N4-02 | 2 项 completed | `setShowProgress` 序列无 false→true 相邻（防闪烁）——即末次 false 之前不应出现 false 再 true |
| TC-N5-01 | 第 1 项后 waitImpl='cancelled' | `message.warning(install_all_cancelled)` 被调；单装 toast（download_cancelled）属 AppContent 层，由代码走查 + 手动冒烟 MS-N5 覆盖 |

AppContent 收敛点守卫（startInstallConfirmed/startRepairConfirmed）与 cancelDownload 的 batchActive 守卫无 mount 基建（App.test.js 均 stub）——靠既有全量 + 手动冒烟。

## 二、回归门（ticket 04，全绿才可提交）

1. `go build ./...` + `go vet ./internal/tui`
2. `go test ./... -short -count=1`（基线 29 包）
3. `cd frontend && npx vitest run`（基线 29 文件 181 用例 + 新增/改动）
4. `cd frontend && npx vite build`
5. `cd frontend && npx playwright test e2e/app.spec.js`（基线 18/18）

## 三、手动冒烟（验收演示）

| ID | 场景 | 步骤 | 通过标准 |
|---|---|---|---|
| MS-N2 | 安装进行中点单项安装 | GUI 单装某工具进行中 → 点列表另项 Install → 确认 | 弹 busy 提示（install.busy），不静默；进度框不被关 |
| MS-N4 | 批量完成项无闪烁 | 嗅探≥2 更新 → 全部更新 → 观察 | 每项完成进度框不闪关再开；列表刷新；汇总文案 |
| MS-N5 | 批量取消单文案 | 同上第 2 项 Cancel | 仅 install_all_cancelled 文案；无 download_cancelled |
| MS-TUI-N1 | TUI 更新视图错误提示 | `xdev tui` → x → a（registry miss 或失败）→ 观察 | updates 视图内见 Error 行；无需退视图 |

## 四、验收标准（DoD，全 ticket）

1. 该 ticket Files 列表内文件（且仅这些）被修改（`git status` 核对）。
2. 该 ticket 全部 TC 用例存在且绿（新增守卫测试与实现同 commit）。
3. 回归门第 1-5 项全绿。
4. 手动冒烟（涉交互的 ticket：01/02/03）按 MS 表执行并记录结果。
5. 基线对照：go test 29 包、vitest 文件/用例数 ≥ 基线；新增 i18n 键 en/zh 同步。
