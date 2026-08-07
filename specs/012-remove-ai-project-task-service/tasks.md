# Tasks 012: 移除 AI 助手、项目、任务、服务四个板块

> **依据**：`specs/012-remove-ai-project-task-service/plan.md`

## Phase 1: CLI 层清理（串行，必须先完成）

- [ ] T1.1 删除 CLI 命令文件：`ai_cmd.go`、`service.go`、`task.go`、`lock.go`、`lock_test.go`、`install_lockfile_test.go`
- [ ] T1.2 `doctor.go`：移除 `internal/ai` import、`--ai` flag、AI diagnosis 代码块
- [ ] T1.3 `install.go`：移除 `internal/lockfile`、`internal/project` import 和所有调用
- [ ] T1.4 `local.go`：移除 `internal/project` import 和调用
- [ ] T1.5 `hook.go`：移除 `internal/project` import 和 `resolveProjectEnv` 函数
- [ ] T1.6 `root.go`：移除 ai/service/task/lock 子命令注册
- [ ] T1.7 `go build ./internal/cli/...` 验证通过

## Phase 2: 后端领域包 + GUI binding + config（与 Phase 3 并行）

- [ ] T2.1 删除 5 个目录：`internal/ai/`、`internal/project/`、`internal/lockfile/`、`internal/task/`、`internal/service/`
- [ ] T2.2 `internal/gui/app_project.go`：删除 28 个 binding 方法 + 辅助类型/函数
- [ ] T2.3 `internal/gui/app.go`：移除 skillMu/skillCancels/skillTombstones/aiKeystore 字段和初始化
- [ ] T2.4 删除 `internal/gui/app_ai_skill_test.go`
- [ ] T2.5 `internal/config/config.go`：移除 AIConfig/ProviderConfig/ModelParams/Capabilities/AI 字段/legacy migration
- [ ] T2.6 `internal/config/config_test.go`：移除 AI 配置测试
- [ ] T2.7 `go build ./...` + `go test ./internal/gui/... ./internal/config/...` 验证通过

## Phase 3: 前端清理（与 Phase 2 并行）

- [ ] T3.1 `navigation.js`：移除 4 个导航项和未使用 icon import
- [ ] T3.2 `AppContent.vue`：移除 4 个 import、v-else-if 分支、ref/函数/onMenuSelect 分支
- [ ] T3.3 删除 `AIAssistantPage.vue`、`ProjectPage.vue`、`TasksPage.vue`、`ServicesPage.vue`
- [ ] T3.4 `SettingsPage.vue`：移除 AI Provider 配置区域
- [ ] T3.5 `en.ts`、`zh.ts`：移除相关 i18n key
- [ ] T3.6 删除 4 个页面 spec 文件
- [ ] T3.7 更新 `navigation.spec.js`、`mocks/wails.js`、`smoke.mjs`、`e2e/app.spec.js`
- [ ] T3.8 `npm run build` + `npm run test:unit` + `node smoke.mjs` 验证通过

## Phase 4: 最终验证

- [ ] T4.1 `wails build` 通过
- [ ] T4.2 确认生成 bindings 无已移除方法
- [ ] T4.3 确认 5 个目录已删除
- [ ] T4.4 交付用户验收