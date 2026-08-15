# Spec 028 Task 清单

> **状态**：已批准（2026-08-09）
> **前置**：Spec 028 + Plan 已获批
> **日期**：2026-08-09

## Phase A — 死递归修复（P0）

### T-A1 提取 archiveFormAgnostic 并断开递归
- **涉及**：`internal/manifest/manifest.go`
- **预期产出**：私有 helper（legacy 3-pass）+ 两个公开方法空 formFilter 走 helper + doc 同步
- **验收**：`go build ./...` 通过
- **依赖**：无

### T-A2 死递归回归测试
- **涉及**：`internal/manifest/manifest_test.go`
- **预期产出**：TC-A1~A7 测试
- **验收**：`go test ./internal/manifest/ ./internal/plugin/ ./internal/plugins/java/ ./internal/installer/ -short -count=1` 全绿
- **依赖**：T-A1

### T-A3 全量 Go 回归
- **涉及**：无（验证）
- **预期产出**：`go test ./... -short -count=1` 全绿记录
- **验收**：全绿
- **依赖**：T-A2

## Phase B — manifest 清理

### T-B1 编码守卫测试（先行落地，先红后绿）
- **涉及**：`internal/manifest/manifest_encoding_guard_test.go`（新建）
- **预期产出**：TC-B1~B4 守卫规则实现；当前 13 文件命中 → 测试先红
- **验收**：运行守卫测试并记录「红」的精确清单（13 文件命中 TC-B2/B3/B4 的明细）
- **依赖**：无（可与 Phase A 并行）

### T-B2a npm 平台包 ×4（claude-code/codex/copilot-cli/opencode）
- **涉及**：`manifests/{claude-code,codex,copilot-cli,opencode}.yaml`
- **预期产出**：注释头重写（干净中文）+ `name:` 恢复 + UTF-8 无 BOM
- **验收**：守卫测试对 4 文件转绿；TC-B5/B6 快照一致
- **依赖**：T-B1

### T-B2b JetBrains ×6（clion/goland/pycharm-community/rubymine/webstorm/jetbrains-idea-community）
- **涉及**：对应 6 个 `manifests/*.yaml`
- **预期产出**：同模板重写 + `name:` 恢复 + UTF-8 无 BOM
- **验收**：同上
- **依赖**：T-B1

### T-B2c vscode（含功能修复）
- **涉及**：`manifests/vscode.yaml`
- **预期产出**：注释头重写 + `name:` 恢复 + **`gui_binary: Code.exe` 真字段恢复**
- **验收**：TC-B4/TC-B7 通过（`go test ./internal/gui/ -run Launch` 契约）
- **依赖**：T-B1

### T-B2d zed
- **涉及**：`manifests/zed.yaml`
- **预期产出**：L2 重写 + `name:` 恢复
- **验收**：守卫测试转绿
- **依赖**：T-B1

### T-B2e cursor（删重复块）
- **涉及**：`manifests/cursor.yaml`
- **预期产出**：删除 L15 损坏重复块 + `name:` 恢复
- **验收**：守卫测试转绿；文件头与 L2-14 干净原文一致
- **依赖**：T-B1

### T-B3 语义不变性门禁
- **涉及**：`internal/manifest/manifest_cleanup_diff_test.go`（新建，或并入 T-B1 文件）
- **预期产出**：TC-B5~B7 实现
- **验收**：`go test ./internal/manifest/ -short -count=1` 全绿；`go test ./... -short -count=1` 全绿
- **依赖**：T-B2a~e

## Phase C — 前端 vitest 修复

### T-C1 localStorage polyfill
- **涉及**：`frontend/test/setup.js`
- **预期产出**：内存 localStorage 实现注入
- **验收**：`npx vitest run test/App.test.js test/SetupWizard.spec.js test/i18n.spec.js` 全绿
- **依赖**：无（可与 A/B 并行）

### T-C2 AppShell 测试补 `t` prop
- **涉及**：`frontend/test/AppShell.spec.js`
- **预期产出**：mountShell 默认注入 `t`
- **验收**：`npx vitest run test/AppShell.spec.js` 8/8
- **依赖**：无

### T-C3 EnvironmentVariablesPage 测试重写
- **涉及**：`frontend/test/pages/EnvironmentVariablesPage.spec.js`
- **预期产出**：4 个过时测试按新契约重写/迁移，其余 4 个保持通过
- **验收**：该文件全绿
- **依赖**：无

### T-C4 EnvVarEditorDialog 契约测试（新建）
- **涉及**：`frontend/test/components/EnvVarEditorDialog.spec.js`（新建）
- **预期产出**：TC-C1~C11
- **验收**：全绿
- **依赖**：无

### T-C5 FormSelectorDialog 测试更新
- **涉及**：`frontend/test/components/FormSelectorDialog.spec.js`
- **预期产出**：NRadioGroup/NRadio stub + 单选断言
- **验收**：3/3 全绿
- **依赖**：无

### T-C6 InstallProgressDialog 测试更新
- **涉及**：`frontend/test/components/InstallProgressDialog.spec.js`
- **预期产出**：i18n key 断言 + focus 校准（真 bug 则记录并显式处理）
- **验收**：4/4 全绿
- **依赖**：无

### T-C7 前端全量验证
- **涉及**：无（验证）
- **预期产出**：vitest 全绿 + vite build + e2e 18/18 记录
- **验收**：三项全过
- **依赖**：T-C1~C6

## Phase D — 收尾

### T-D1 卫生项
- **涉及**：删除 `audit.test.config.mjs`；修正 `.gitattributes` mojibake 行
- **验收**：`git status` 干净；`git check-attr whitespace -- "UNI_ENV_开发文档.md"` = unset
- **依赖**：无

### T-D2 HANDOFF 更新 + backup 提交
- **涉及**：`HANDOFF.md`、handoff-backup 分支
- **预期产出**：非阻塞项结论修正 + Spec 028 记录；≥2 个恢复点提交（Phase A 收敛 1 个 + 全流程收敛 1 个）
- **验收**：`git log --oneline handoff-backup` 含 `Spec 028` 提交
- **依赖**：T-A3、T-B3、T-C7

### T-D3 全量回归
- **涉及**：go test ./... + vitest + vite build + e2e + wails build
- **验收**：全部通过
- **依赖**：T-A3、T-B3、T-C7、T-D1

---

**审批请求**：请审批本 task 清单。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-09-task-028-001`
