# Spec 012: 移除 AI 助手、项目、任务、服务四个板块（前后端全删）

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-08
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：移除 AI/项目/任务/服务四个板块的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 背景与动机

xdev 的核心价值是开发环境版本管理——manifest 驱动的多版本安装/切换/嗅探。AI 助手、项目配置、任务运行、服务管理四个板块在实际使用中证明没有存在必要：

- **AI 助手**：健康检查等价于 `xdev doctor`，迁移发现等价于本地环境扫描，suggest/diagnose/recommend 的输出质量不足以提供超出 CLI 的实质建议，反而引入 Provider 配置复杂度和取消/IPC 可靠性问题。
- **项目**：`.xdev.toml` 配置和 lockfile 安装在 CLI 已完整覆盖，GUI 表单无额外价值。
- **任务**：项目任务运行依赖项目板块，且 GUI 中执行任务不如终端直接。
- **服务**：服务管理（redis/postgres 等）与 xdev 核心定位无关。

保留这些板块使 xdev 臃肿、增加维护成本、且功能与 CLI 重复。

## 范围

### 全部移除（前后端，包括 CLI 和 GUI）

| 板块 | 导航 key | 前端页面 | GUI binding | 后端领域包 | CLI 命令 |
|---|---|---|---|---|---|
| AI 助手 | `ai` | `AIAssistantPage.vue` | 19 个 | `internal/ai/` 整个包 | `internal/cli/ai_cmd.go` |
| 项目 | `project` | `ProjectPage.vue` | 5 个 | `internal/project/`、`internal/lockfile/` | CLI 中的 project/lockfile 子命令 |
| 任务 | `tasks` | `TasksPage.vue` | 1 个 | `internal/task/` | CLI 中的 task 运行逻辑 |
| 服务 | `services` | `ServicesPage.vue` | 3 个 | `internal/service/` | CLI 中的 service 命令 |

### 不在范围

- xdev 核心功能：manifest、installer、downloader、discovery/sniffer、env-vars、isolate、remote、favorites、dashboard、settings（镜像/代理/嗅探/缓存）
- 工具目录页（ToolCatalogPage）及其所有安装/切换/版本管理功能

## 移除清单

### 前端

**导航**（`navigation.js`）
- 移除 4 个导航项：`ai`、`project`、`tasks`、`services`
- 移除对应 icon import（`SparklesOutline`、`CodeOutline`、`PlayOutline`、`ServerOutline`，若不再被其他项使用）

**AppContent.vue**
- 移除 4 个页面 import：`AIAssistantPage`、`ProjectPage`、`TasksPage`、`ServicesPage`
- 移除 4 个 `v-else-if` 渲染分支
- 移除相关 ref state：`projectConfig`、`projectLoading`、`projectError`、`taskStates`、`lockfileInfo`、`lockfileLoading`、`lockfileInstalling`、`services`、`servicesLoading`、`servicesError`、`serviceOperating`
- 移除相关函数：`loadProjectConfig`、`setProjectTool`、`unsetProjectTool`、`runProjectTask`、`loadLockfile`、`installFromLockfile`、`loadServices`、`startService`、`stopService`
- 移除 `onMenuSelect` 中 4 个分支

**页面组件**（删除文件）
- `frontend/src/pages/AIAssistantPage.vue`
- `frontend/src/pages/ProjectPage.vue`
- `frontend/src/pages/TasksPage.vue`
- `frontend/src/pages/ServicesPage.vue`

**SettingsPage.vue**
- 移除 AI Provider 配置区域（7 个 binding 调用：`GetAIStatus`、`AIAddProvider`、`AITrustProvider`、`AIListProviderModels`、`AITestProvider`、`AISwitchProvider`、`AIRemoveProvider`）
- 移除相关 state、函数、模板分支

**i18n**（`en.ts`、`zh.ts`）
- 移除 `nav.ai`、`nav.project`、`nav.tasks`、`nav.services`
- 移除 `ai.*`（约 100 key）、`project.*`（约 18 key）、`services.*`（约 8 key）

**测试**
- 删除 `frontend/test/pages/AIAssistantPage.spec.js`
- 删除 `frontend/test/pages/ProjectPage.spec.js`
- 删除 `frontend/test/pages/TasksPage.spec.js`
- 删除 `frontend/test/pages/ServicesPage.spec.js`
- 更新 `frontend/test/navigation.spec.js`：从 `staticPageKeys` 移除 `project`、`tasks`、`services`、`ai`
- 更新 `frontend/test/mocks/wails.js`：移除 AI/Project/Tasks/Services 相关 mock 方法和 call tracker
- 更新 `frontend/test/smoke.mjs`：移除 ProjectPage/TasksPage/ServicesPage marker 和 `newProjectTool`/`newProjectVersion`
- 更新 `frontend/e2e/app.spec.js`：移除 project/tasks/services 测试流程和 AI 测试流程，保留 isolation/remote/about

### 后端 — GUI 层

**`internal/gui/app_project.go`**
- 移除 19 个 AI 方法 + 辅助类型和函数
- 移除 6 个 Project/Tasks 方法：`GetProjectConfig`、`SetProjectTool`、`UnsetProjectTool`、`RunProjectTask`、`GetLockfile`、`InstallFromLockfile`
- 移除 3 个 Services 方法：`ListServices`、`StartService`、`StopService`
- 移除辅助类型：`ProjectConfigInfo`、`TaskInfo`、`ServiceInfo`、`AIStatus`、`ProviderInfo`、`ProviderTestResult`、`SkillInfo`、`AddProviderResult`、`ModelParams`、`Capabilities`、`ProviderConfig`
- 移除辅助函数：`toConfigParams`、`fromConfigParams`、`isCloudEndpoint`、`isDestructiveAction`、`aiKeystore`、`newAssistant`、`buildAIContext`、`registerSkillContext`

**`internal/gui/app.go`**
- 移除 `skillMu`、`skillCancels`、`skillTombstones`、`aiKeystore` 字段和初始化

**`internal/gui/app_ai_skill_test.go`**
- 删除整个文件

### 后端 — 领域包（整个删除）

- `internal/ai/` — 整个目录（assistant.go、provider.go、skills.go、fallback.go、context.go、schema.go、keystore.go、providers/ 子包等）
- `internal/project/` — 整个目录
- `internal/lockfile/` — 整个目录
- `internal/task/` — 整个目录
- `internal/service/` — 整个目录

### 后端 — CLI 命令

- `internal/cli/ai_cmd.go` — 删除或移除 AI 子命令注册
- `internal/cli/` 中 project/lockfile/task/service 相关子命令注册和实现文件 — 删除
- `internal/cli/root.go` — 移除上述子命令的注册行
- `internal/cli/context.go` — 移除 AI/project/task/service 相关 context 字段（若有）

### 配置

- `internal/config/config.go` — 移除 `AIConfig`、`AI` 字段、Provider 配置结构体（若不再被其他功能使用）
- `internal/config/config_test.go` — 移除 AI 配置相关测试
- `E:\XDev\config.yaml` — 用户手动清理 AI provider 配置（不自动改）

### 生成产物（wails build 自动更新）
- `frontend/wailsjs/go/gui/App.js`、`App.d.ts`：移除 28 个 binding 声明
- `frontend/wailsjs/go/models.ts`：移除已删 struct 声明

### 文档

- `specs/010-ai-assistant-enhancement/` — 归档
- `README.md` — 移除 AI/project/task/service 功能描述
- `docs/architecture.md`、`docs/backlog.md` — 移除相关引用（若有）

## 验收标准

1. `go build ./...` 通过——无 AI/project/lockfile/task/service 包引用残留
2. `go test ./...` 通过——无相关测试失败
3. `npm run build` 通过
4. `npm run test:unit` 通过
5. `node frontend/test/smoke.mjs` 通过
6. `wails build` 通过
7. GUI 导航无 AI/项目/任务/服务四个项
8. 设置页无 AI Provider 配置区域
9. CLI `xdev --help` 无 AI/project/task/service 子命令
10. 生成 bindings 中无 28 个已移除方法
11. `internal/ai/`、`internal/project/`、`internal/lockfile/`、`internal/task/`、`internal/service/` 目录已删除

## 风险

- **跨包引用**：其他包可能 import 了 `internal/ai`、`internal/project` 等即将删除的包。需先全局搜索 import，确认并清理所有引用后才能安全删除目录。
- **config 结构体**：`config.AI` 字段可能被 GUI binding 之外的地方引用（如 CLI context）。需确认移除范围。
- **main.go / root.go**：子命令注册在 root.go 或 main.go 中，移除时需确保不破坏命令树结构。
- **工作量**：这是大规模裁剪，涉及前后端约 20+ 文件修改和 5 个目录删除。

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-012-approved-001` 移除 AI/项目/任务/服务四个板块的目标、范围和验收 |
