# Plan 012: 移除 AI 助手、项目、任务、服务四个板块

> **状态**：待用户批准
> **依据**：`specs/012-remove-ai-project-task-service/spec.md`

## 执行策略

三阶段串行，每阶段结束验证编译。前端和后端可并行，但后端领域包删除必须在 CLI 引用清理之后。

```
Phase 1: CLI 层清理（移除子命令 + 清理 import）
    ↓
Phase 2: 后端领域包删除 + GUI binding 删除 + config 清理（并行）
    ↓
Phase 3: 前端清理（并行于 Phase 2 后半段）
    ↓
Phase 4: wails build + 全量验证
```

## Phase 1: CLI 层清理

### 1.1 删除 CLI 命令文件
- 删除 `internal/cli/ai_cmd.go`
- 删除 `internal/cli/service.go`
- 删除 `internal/cli/task.go`
- 删除 `internal/cli/lock.go`（lock generate/install 子命令）
- 删除 `internal/cli/lock_test.go`
- 删除 `internal/cli/install_lockfile_test.go`

### 1.2 修改引用了即将删除包的 CLI 文件
- `internal/cli/doctor.go`：移除 `internal/ai` import、`--ai` flag、AI diagnosis 代码块（line 30, 41, 186-219, 232）
- `internal/cli/install.go`：移除 `internal/lockfile`、`internal/project` import 和所有调用（line 42-43, 117, 176-180, 422, 469-484）
- `internal/cli/local.go`：移除 `internal/project` import 和调用（line 9, 44, 62, 77-86）
- `internal/cli/hook.go`：移除 `internal/project` import 和调用（line 36, 58, 124, resolveProjectEnv 函数）

### 1.3 修改 root.go 子命令注册
- `internal/cli/root.go`：移除 `ai`、`service`、`task`、`lock` 子命令注册行

### 1.4 验证
- `go build ./internal/cli/...` 通过（此时领域包还在，但 CLI 不再引用）

## Phase 2: 后端领域包 + GUI binding + config

### 2.1 删除领域包目录
- 删除 `internal/ai/`（含 `internal/ai/providers/`）
- 删除 `internal/project/`
- 删除 `internal/lockfile/`
- 删除 `internal/task/`
- 删除 `internal/service/`

### 2.2 GUI binding 清理
- `internal/gui/app_project.go`：删除 28 个方法（19 AI + 6 Project/Tasks + 3 Services）和所有辅助类型/函数
- `internal/gui/app.go`：移除 `skillMu`、`skillCancels`、`skillTombstones`、`aiKeystore` 字段和初始化
- 删除 `internal/gui/app_ai_skill_test.go`

### 2.3 config 清理
- `internal/config/config.go`：移除 `AIConfig`、`ProviderConfig`、`ModelParams`、`Capabilities`、`AI` 字段、legacy AI migration 代码
- `internal/config/config_test.go`：移除 AI 配置相关测试

### 2.4 验证
- `go build ./...` 通过
- `go test ./internal/gui/... ./internal/config/...` 通过

## Phase 3: 前端清理

### 3.1 导航 + AppContent
- `navigation.js`：移除 4 个导航项和未使用 icon import
- `AppContent.vue`：移除 4 个 import、4 个 v-else-if 分支、相关 ref/函数/onMenuSelect 分支

### 3.2 删除页面组件
- 删除 `AIAssistantPage.vue`、`ProjectPage.vue`、`TasksPage.vue`、`ServicesPage.vue`

### 3.3 SettingsPage
- 移除 AI Provider 配置区域（state、函数、模板分支、binding 调用）

### 3.4 i18n
- `en.ts`、`zh.ts`：移除 `nav.ai/project/tasks/services` 和 `ai.*/project.*/services.*` key

### 3.5 测试清理
- 删除 4 个页面 spec 文件
- 更新 `navigation.spec.js`：移除 4 个 staticPageKeys
- 更新 `mocks/wails.js`：移除相关 mock 和 call tracker
- 更新 `smoke.mjs`：移除 ProjectPage/TasksPage/ServicesPage marker
- 更新 `e2e/app.spec.js`：移除 project/tasks/services/AI 测试流程

### 3.6 验证
- `npm run build` 通过
- `npm run test:unit` 通过
- `node frontend/test/smoke.mjs` 通过

## Phase 4: 最终验证

- `wails build` 通过（自动重新生成 bindings）
- 确认生成 bindings 无 28 个已移除方法
- 确认 5 个目录已删除
- 确认 `xdev --help` 无 AI/project/task/service 子命令（若 CLI 可构建运行）

## 并行分配

Phase 1 完成后，Phase 2 和 Phase 3 可并行：
- @fixer A：Phase 2（后端领域包 + GUI binding + config）
- @fixer B：Phase 3（前端清理）

两者写入范围无重叠（A 写 Go，B 写 Vue/TS/JS）。

## 风险缓解

| 风险 | 缓解 |
|---|---|
| install.go 中 lockfile/project 调用深度 | Phase 1.2 逐文件清理后立即编译验证 |
| hook.go 的 resolveProjectEnv 被其他函数调用 | 先搜索调用点，内联或移除 |
| config.AI 被其他非 AI 代码引用 | 搜索 `cfg.AI` 全部引用，确认仅 AI 板块使用 |
| backward_compat.go 等未发现的引用 | Phase 1 编译会暴露，逐步修复 |

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-012-001`
