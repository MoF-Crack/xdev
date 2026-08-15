# Spec 028 Checklist

> **状态**：完成（Phase A-D 全部收敛，LOOP 出口）
> **前置**：Spec 028 + Plan + Task 已获批
> **日期**：2026-08-09

## 实施前复核基线（已完成的证据记录）

- [x] 死递归复现：`go test ./internal/manifest/ -run TestArchiveForCurrentPicksExactOS -count=1` → `fatal error: stack overflow`（100% 复现，非偶发）
- [x] 死递归定位：manifest.go L596 ↔ L612 互相委托；legacy 语义取自已收敛前 commit 673433c（3-pass）
- [x] 生产影响确认：installer.go:402 / app_install.go:167 / generic.go:114 / java.go:57
- [x] 13 个 mojibake 文件清单与损坏行、被吞字段（name ×11 + vscode gui_binary ×1）核实完毕
- [x] 签名事实核实：13 个文件均无 .sig，38 个已签名 manifest 不动
- [x] git/origin/main 无干净原文（本地新增）→ 重写策略成立
- [x] 前端全量 vitest 实测：7 文件 18 失败（App/SetupWizard/i18n = localStorage；AppShell 8 = 缺 t；EnvVars 4 + FormSelector 2 + InstallProgress 3 = 契约过时）；ToolCatalogPage 已绿
- [x] loader name-fallback 兜底机制核实（manifest.go:396/468/1082）

## Phase A — 死递归修复

### T-A1 提取 helper 断开递归
- [x] `archiveFormAgnostic` 私有 helper（exact os+arch → os+any → any-os，忽略 Form）
- [x] `ArchiveForCurrent` 委托 helper
- [x] `ArchiveForCurrentWithForm` 空 formFilter 委托 helper（不再回调）
- [x] 非空 formFilter 6-pass 逻辑一行不动
- [x] doc 注释同步（空 formFilter = legacy，忽略 Form）
- [x] **验收**：`go build ./...` 通过

### T-A2 回归测试
- [x] TC-A1 死递归直接入口回归
- [x] TC-A2 WithForm 空过滤回归
- [x] TC-A3 legacy 忽略 Form
- [x] TC-A4 legacy 回退顺序（os+any 先于 any-os）
- [x] TC-A5 WithForm 6-pass 不回归（Form 优先 + Form=="" 降级）
- [x] TC-A6 空过滤不误入 6-pass（防另一种错误去重写法）
- [x] TC-A7 生产链 smoke（Generic.ArchiveFor 无 panic）
- [x] **验收**：4 包 `go test -short -count=1` 全绿（原崩溃测试复活）

### T-A3 全量 Go 回归
- [x] `go test ./... -short -count=1` 全绿
- [x] `go test ./internal/manifest/ -run TestArchiveForCurrent -count=3` 稳定通过

## Phase B — manifest 清理

### T-B1 编码守卫测试（先红）
- [x] TC-B1 UTF-8 严格解码
- [x] TC-B2 mojibake 标记字符集（含 U+FFFD）
- [x] TC-B3 `Name == filename stem` 显式声明
- [x] TC-B4 gui 形态 + shims ⇒ gui_binary 非空（opencode 文档化例外按 stem 匹配）
- [x] 记录「红」清单（13 文件命中 B2/B3，vscode 命中 B4）

### T-B2a npm 平台包 ×4
- [x] claude-code.yaml 重写 + name 恢复
- [x] codex.yaml 重写 + name 恢复
- [x] copilot-cli.yaml 重写 + name 恢复
- [x] opencode.yaml 重写 + name 恢复
- [x] 4 文件 UTF-8 无 BOM、LF
- [x] **验收**：守卫测试 4 文件转绿 + 快照一致

### T-B2b JetBrains ×6
- [x] clion / goland / pycharm-community / rubymine / webstorm / jetbrains-idea-community 同模板重写 + name 恢复（description 中 鈥? 恢复为 —）
- [x] **验收**：守卫测试 6 文件转绿 + 快照一致

### T-B2c vscode（功能修复）
- [x] 注释头重写 + name 恢复
- [x] **恢复 `gui_binary: Code.exe` 真字段**
- [x] **验收**：TC-B4 转绿；`go test ./internal/gui/ -run Launch` 契约通过

### T-B2d zed
- [x] L2 重写 + name 恢复
- [x] **验收**：守卫测试转绿

### T-B2e cursor
- [x] 删除 L15 损坏重复块 + name 恢复
- [x] **验收**：守卫测试转绿；注释头与 L2-14 干净原文一致

### T-B3 语义不变性门禁
- [x] TC-B5 关键字段快照（13 文件全部一致）
- [x] TC-B6 全量 loader 加载无错误无告警
- [x] TC-B7 vscode GUIBinary 断言
- [x] **验收**：`go test ./internal/manifest/ -short` + `go test ./... -short` 全绿
- [x] git diff 复核：功能性字段改动仅 13×name + 1×gui_binary + 6×description(鈥?→—)

## Phase C — 前端 vitest 修复

### T-C1 localStorage polyfill
- [x] setup.js 注入内存 localStorage（getItem/setItem/removeItem/clear/key/length）
- [x] **验收**：App.test.js + SetupWizard.spec.js + i18n.spec.js 全绿

### T-C2 AppShell
- [x] mountShell 默认 `t: (key) => key`
- [x] 侧栏 aria-label 断言更新为 i18n key（nav.collapse_sidebar / nav.expand_sidebar）
- [x] matchMedia mock 改为保存/恢复模式
- [x] **验收**：AppShell.spec.js 8/8

### T-C3 EnvironmentVariablesPage
- [x] not-applicable 断言改为 status-dot 的 title/aria-label
- [x] 移除 scope-all 断言；保留 level/validate/partial
- [x] edit/browse/save 流程迁移至 C4（页面侧只测事件）
- [x] props 对齐组件实际契约（envVars/viewMode/browseDirectory/editing 移除）
- [x] **验收**：EnvironmentVariablesPage.spec.js 全绿

### T-C4 EnvVarEditorDialog（新建）
- [x] TC-C1 打开与焦点
- [x] TC-C2 编辑锁定（name disabled、level 隐藏、edit_title）
- [x] TC-C3 新建模式
- [x] TC-C4 browse 填充/追加
- [x] TC-C5 多路径增删
- [x] TC-C6 新建 save 载荷
- [x] TC-C7 编辑 save 载荷（original）
- [x] TC-C8 machine 提示
- [x] TC-C9 Escape 关闭
- [x] TC-C10 无效路径标记
- [x] TC-C11 空 name 禁用 save
- [x] **验收**：11/11 全绿

### T-C5 FormSelectorDialog
- [x] NRadioGroup/NRadio stub 替换
- [x] 单选断言（radio checked）
- [x] **验收**：3/3 全绿

### T-C6 InstallProgressDialog
- [x] phase/error 断言改 i18n key 产物（installing_downloading / install-error__message）
- [x] 复制动作断言（navigator.clipboard.writeText）
- [x] focus 行为按现实现校准（background 按钮循环）
- [x] **验收**：4/4 全绿

### T-C7 前端全量验证
- [x] `npx vitest run` 全绿（24 文件 141 测试 0 失败）
- [x] `npx vite build` 通过
- [x] `npx playwright test e2e/app.spec.js` 18/18

## Phase D — 收尾

### T-D1 卫生项
- [x] 删除 `audit.test.config.mjs`
- [x] 核查 `.gitattributes` mojibake 行（结论：文件本就干净，早前为 PS 控制台显示假象，无需修改）
- [x] **验收**：`git status` 干净；UNI_ENV 开发文档.md 行 whitespace 豁免生效

### T-D2 HANDOFF + backup
- [x] HANDOFF.md 非阻塞项三条修正 + Spec 028 完成记录 + 文件索引
- [x] Phase A 收敛 backup 提交（`4e0f658`）
- [x] 全流程收敛 backup 提交（LOOP 出口）
- [x] **验收**：`git log --oneline handoff-backup` 可见

### T-D3 全量回归
- [x] `go test ./... -short -count=1`（最终复核 EXIT=0）
- [x] `npx vitest run` + `npx vite build` + e2e
- [x] `wails build`（build/bin/xdev-gui.exe）

---

## 执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| 2026-08-09 | 规划 | 完成（spec/plan/task/checklist 产出 + 三项复核证据） | 已批准 |
| 2026-08-09 | T-A1 | 通过（archiveFormAgnostic helper 提取，死递归断开） | go build 通过 |
| 2026-08-09 | T-A2 | 通过（TC-A1~A7 落地；manifest/plugin/java/installer 四包全绿） | 原崩溃测试复活 |
| 2026-08-09 | T-A3 | 通过（go test ./... -short 全绿 EXIT=0） | |
| 2026-08-09 | T-B1 | 通过（守卫 4 项先红：13 文件命中 B2/B3 + vscode 命中 B4；opencode 例外按文件名 stem 修正） | |
| 2026-08-09 | T-B2a~e | 通过（13 文件全部重写：npm×4 + JetBrains×6 + vscode/zed/cursor；name 恢复 ×13 + vscode gui_binary 恢复；UTF-8 无 BOM + LF） | git diff 复核仅注释/name/gui_binary/description 改动 |
| 2026-08-09 | T-B3 | 通过（语义指纹快照 TC-B5 13/13 + 全量加载 TC-B6 + vscode 契约 TC-B7；go test ./... 全绿） | |
| 2026-08-09 | T-D1 | 通过（audit.test.config.mjs 已删除；.gitattributes 实测本就干净——早前 mojibake 是 PS 控制台显示假象，无需修改） | |
| 2026-08-09 | T-C1 | 通过（setup.js 内存 localStorage polyfill，App/SetupWizard/i18n 全绿） | |
| 2026-08-09 | T-C2 | 通过（AppShell 8/8：t prop + i18n aria-label + matchMedia 保存恢复） | |
| 2026-08-09 | T-C3 | 通过（EnvVars 契约重写：status-dot 属性断言 + scope-all 移除） | |
| 2026-08-09 | T-C4 | 通过（EnvVarEditorDialog.spec.js 新建，TC-C1~C11 全绿） | |
| 2026-08-09 | T-C5 | 通过（FormSelectorDialog 3/3：NRadioGroup 单选语义） | |
| 2026-08-09 | T-C6 | 通过（InstallProgressDialog 4/4：i18n key + clipboard + focus 校准） | |
| 2026-08-09 | T-C7 | 通过（vitest 24 文件 141 测试全绿；vite build；e2e 18/18） | B3 完成 |
| 2026-08-09 | T-D2 | 通过（HANDOFF 终稿 + LOOP 出口 backup 提交） | B3 完成 |
| 2026-08-09 | T-D3 | 通过（go test ./... 全绿 + vitest + vite + e2e + wails build） | B3 完成 |
