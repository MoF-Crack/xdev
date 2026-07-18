---
description: "Task list template for feature implementation — XDev 中文覆盖版"
---

# Tasks: [FEATURE NAME]

> **文档类型**：Task List
> **关联 Plan**：`specs/[###-feature-name]/plan.md`
> **关联 Spec**：`specs/[###-feature-name]/spec.md`
> **任务状态流**：Pending → In Progress → Spec Review → Quality Review → Verified（或 Rework / Blocked）

**Input**: Design documents from `/specs/[###-feature-name]/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: 以下示例包含测试任务。测试是可选的 — 仅在 Feature Spec 显式要求时包含。

**Organization**: 任务按用户故事分组，使每个故事可独立实施和测试。

---

## 格式: `[ID] [P?] [Story] 描述`

- **[P]**：可并行执行（不同文件、无依赖）
- **[Story]**：任务所属用户故事（e.g., US1, US2, US3）
- 描述中包含精确文件路径

## 路径约定

- **单项目**：`src/`、`tests/` 位于仓库根目录
- **Web 应用**：`backend/src/`、`frontend/src/`
- **移动端**：`api/src/`、`ios/src/` 或 `android/src/`
- 以下路径假定为单项目结构 — 根据 plan.md 结构调整

<!--
  ============================================================================
  IMPORTANT: The tasks below are SAMPLE TASKS for illustration purposes only.

  The /speckit.tasks command MUST replace these with actual tasks based on:
  - User stories from spec.md (with their priorities P1, P2, P3...)
  - Feature requirements from plan.md
  - Entities from data-model.md
  - Endpoints from contracts/

  Tasks MUST be organized by user story so each story can be:
  - Implemented independently
  - Tested independently
  - Delivered as an MVP increment

  DO NOT keep these sample tasks in the generated tasks.md file.

  XDev 特有要求：
  - 每个任务必须包含 FR/SC 引用、双重审查人员和聚焦验证命令。
  - 进入 Verified 必须有两类独立审查记录和验证输出。
  - 禁止只使用 [x] 作为完成证据。
  ============================================================================
-->

---

## Phase 1: Setup (共享基础设施)

**Purpose**: 项目初始化和基础结构搭建

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T001 | 按实施计划创建项目结构 | — | plan.md | 目录结构 | `src/`, `tests/` | 无 | | | | `Test-Path src; Test-Path tests` | Pending |
| T002 | 初始化项目依赖 | — | plan.md | 依赖清单 | `go.mod`, `package.json` 等 | T001 | | | | `go mod tidy; go build ./...` | Pending |
| T003 | [P] 配置格式化和 linting 工具 | — | plan.md | 配置文件 | `.golangci.yml`, `.eslintrc` 等 | T001 | | | | `golangci-lint run` | Pending |

---

## Phase 2: Foundational (阻塞性前置)

**Purpose**: 所有用户故事前必须完成的核心基础设施

**⚠️ CRITICAL**: 本阶段完成前，任何用户故事工作不得开始

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T004 | 搭建数据库 schema 和迁移框架 | FR-XXX | data-model.md | migration 文件 | `migrations/` | T001 | | | | `migrate up` | Pending |
| T005 | [P] 实现认证/授权框架 | FR-XXX | spec.md 安全需求 | auth 模块 | `src/auth/` | T001 | | | | `go test ./src/auth/...` | Pending |
| T006 | [P] 搭建 API 路由和中间件 | — | contracts/ | 路由注册 | `src/api/router.go` | T001 | | | | `curl localhost:8080/health` | Pending |
| T007 | 创建所有故事共享的基础实体 | — | data-model.md | model 文件 | `src/models/` | T004 | | | | `go test ./src/models/...` | Pending |
| T008 | 配置错误处理和日志基础设施 | — | plan.md | 日志模块 | `src/log/` | T001 | | | | 触发错误并验证日志输出 | Pending |
| T009 | 配置环境管理 | — | plan.md | 环境配置 | `.env.example`, `config/` | T001 | | | | 验证各环境配置加载 | Pending |

**Checkpoint**: 基础设施就绪 — 用户故事实施可以并行开始

---

## Phase 3: User Story 1 - [标题] (Priority: P1) 🎯 MVP

**Goal**: [简要描述此故事交付的内容]

**Independent Test**: [如何独立验证此故事]

### 测试 for User Story 1 (OPTIONAL — 仅在 Spec 要求时包含) ⚠️

> **NOTE: 先写测试，确保测试 FAIL，然后再实现**

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T010 | [P] [US1] Contract test for [endpoint] | SC-XXX | contracts/ | test 文件 | `tests/contract/test_[name].go` | T006 | | | | `go test -run TestContract` | Pending |
| T011 | [P] [US1] Integration test for [user journey] | SC-XXX | spec.md US1 | test 文件 | `tests/integration/test_[name].go` | T007 | | | | `go test -run TestIntegration` | Pending |

### 实现 for User Story 1

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T012 | [P] [US1] 创建 [Entity1] 模型 | FR-XXX | data-model.md | model 文件 | `src/models/[entity1].go` | T007 | | | | `go test ./src/models/` | Pending |
| T013 | [P] [US1] 创建 [Entity2] 模型 | FR-XXX | data-model.md | model 文件 | `src/models/[entity2].go` | T007 | | | | `go test ./src/models/` | Pending |
| T014 | [US1] 实现 [Service] | FR-XXX | T012, T013 | service 文件 | `src/services/[service].go` | T012, T013 | | | | `go test ./src/services/` | Pending |
| T015 | [US1] 实现 [endpoint/feature] | FR-XXX | T014 | handler 文件 | `src/[location]/[file].go` | T014 | | | | `curl` 或集成测试 | Pending |
| T016 | [US1] 添加验证和错误处理 | FR-XXX | T015 | 更新 handler | `src/[location]/[file].go` | T015 | | | | 边界条件测试 | Pending |
| T017 | [US1] 添加日志记录 | — | T015 | 日志调用 | `src/[location]/[file].go` | T015 | | | | 检查日志输出 | Pending |

**Checkpoint**: User Story 1 应完全可用并可独立测试

---

## Phase 4: User Story 2 - [标题] (Priority: P2)

**Goal**: [简要描述此故事交付的内容]

**Independent Test**: [如何独立验证此故事]

### 测试 for User Story 2 (OPTIONAL — 仅在 Spec 要求时包含) ⚠️

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T018 | [P] [US2] Contract test for [endpoint] | SC-XXX | contracts/ | test 文件 | `tests/contract/test_[name].go` | T006 | | | | `go test -run TestContract` | Pending |
| T019 | [P] [US2] Integration test for [user journey] | SC-XXX | spec.md US2 | test 文件 | `tests/integration/test_[name].go` | T007 | | | | `go test -run TestIntegration` | Pending |

### 实现 for User Story 2

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T020 | [P] [US2] 创建 [Entity] 模型 | FR-XXX | data-model.md | model 文件 | `src/models/[entity].go` | T007 | | | | `go test ./src/models/` | Pending |
| T021 | [US2] 实现 [Service] | FR-XXX | T020 | service 文件 | `src/services/[service].go` | T020 | | | | `go test ./src/services/` | Pending |
| T022 | [US2] 实现 [endpoint/feature] | FR-XXX | T021 | handler 文件 | `src/[location]/[file].go` | T021 | | | | `curl` 或集成测试 | Pending |
| T023 | [US2] 与 User Story 1 集成（如需要） | — | T015, T022 | 集成代码 | 相关文件 | T015, T022 | | | | 端到端测试 | Pending |

**Checkpoint**: User Story 1 和 2 均独立可用

---

## Phase 5: User Story 3 - [标题] (Priority: P3)

**Goal**: [简要描述此故事交付的内容]

**Independent Test**: [如何独立验证此故事]

### 测试 for User Story 3 (OPTIONAL — 仅在 Spec 要求时包含) ⚠️

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T024 | [P] [US3] Contract test for [endpoint] | SC-XXX | contracts/ | test 文件 | `tests/contract/test_[name].go` | T006 | | | | `go test -run TestContract` | Pending |
| T025 | [P] [US3] Integration test for [user journey] | SC-XXX | spec.md US3 | test 文件 | `tests/integration/test_[name].go` | T007 | | | | `go test -run TestIntegration` | Pending |

### 实现 for User Story 3

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| T026 | [P] [US3] 创建 [Entity] 模型 | FR-XXX | data-model.md | model 文件 | `src/models/[entity].go` | T007 | | | | `go test ./src/models/` | Pending |
| T027 | [US3] 实现 [Service] | FR-XXX | T026 | service 文件 | `src/services/[service].go` | T026 | | | | `go test ./src/services/` | Pending |
| T028 | [US3] 实现 [endpoint/feature] | FR-XXX | T027 | handler 文件 | `src/[location]/[file].go` | T027 | | | | `curl` 或集成测试 | Pending |

**Checkpoint**: 所有用户故事均独立可用

---

[Add more user story phases as needed, following the same pattern]

---

## Phase N: Polish & 跨切面关注点

**Purpose**: 影响多个用户故事的改进

| ID | 任务描述 | FR/SC | 输入 | 输出 | 文件范围 | 前置依赖 | 实现者 | Spec 审查者 | Quality 审查者 | 验证命令 | 状态 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| TXXX | [P] 文档更新 | — | 全部 story | 文档 | `docs/` | 全部 story | | | | 对比文档与代码事实 | Pending |
| TXXX | 代码清理与重构 | — | 全部 story | 改进代码 | 全局 | 全部 story | | | | `golangci-lint run` | Pending |
| TXXX | 跨 story 性能优化 | SC-XXX | 全部 story | 优化代码 | 全局 | 全部 story | | | | benchmark 对比 | Pending |
| TXXX | [P] 补充单元测试（如要求） | — | 全部 story | test 文件 | `tests/unit/` | 全部 story | | | | `go test ./...` | Pending |
| TXXX | 安全加固 | NFR-XXX | 全部 story | 加固代码 | 全局 | 全部 story | | | | 安全审计检查表 | Pending |
| TXXX | 运行 quickstart.md 验证 | — | quickstart.md | 验证报告 | — | 全部 story | | | | 按 quickstart 步骤执行 | Pending |

---

## 依赖与执行顺序

### Phase 依赖

- **Setup (Phase 1)**: 无依赖 — 可立即开始
- **Foundational (Phase 2)**: 依赖 Setup 完成 — **阻塞所有用户故事**
- **User Stories (Phase 3+)**: 全部依赖 Foundational 完成
  - 用户故事可并行（如果有多个实现者）
  - 或按优先级顺序串行（P1 → P2 → P3）
- **Polish (Final Phase)**: 依赖所有目标用户故事完成

### 用户故事依赖

- **User Story 1 (P1)**: Foundational (Phase 2) 后可开始 — 不依赖其他故事
- **User Story 2 (P2)**: Foundational (Phase 2) 后可开始 — 可能与 US1 集成但应独立可测
- **User Story 3 (P3)**: Foundational (Phase 2) 后可开始 — 可能与 US1/US2 集成但应独立可测

### 每个用户故事内部

- 测试（如果包含）必须先写且 FAIL，再实现
- Models → Services → Endpoints
- 核心实现 → 集成
- 一个故事完成后再进入下一优先级

### 并行机会

- Setup 中所有标记 [P] 的任务可并行
- Foundational 中所有标记 [P] 的任务可并行（在 Phase 2 内）
- Foundational 完成后，所有用户故事可并行开始（如果有多人）
- 一个故事内所有标记 [P] 的测试可并行
- 一个故事内所有标记 [P] 的模型可并行
- 不同用户故事可由不同实现者并行

---

## 并行示例: User Story 1

```bash
# 同时启动 User Story 1 的所有测试（如果要求测试）：
Task: "Contract test for [endpoint] in tests/contract/test_[name].go"
Task: "Integration test for [user journey] in tests/integration/test_[name].go"

# 同时启动 User Story 1 的所有模型：
Task: "创建 [Entity1] 模型 in src/models/[entity1].go"
Task: "创建 [Entity2] 模型 in src/models/[entity2].go"
```

---

## 实施策略

### MVP First (仅 User Story 1)

1. 完成 Phase 1: Setup
2. 完成 Phase 2: Foundational (CRITICAL — 阻塞所有故事)
3. 完成 Phase 3: User Story 1
4. **停止并验证**: 独立测试 User Story 1
5. 如果就绪，部署/演示

### 增量交付

1. Setup + Foundational → 基础设施就绪
2. 添加 User Story 1 → 独立测试 → 部署/演示 (MVP!)
3. 添加 User Story 2 → 独立测试 → 部署/演示
4. 添加 User Story 3 → 独立测试 → 部署/演示
5. 每个故事在不破坏已有故事的基础上增加价值

### 并行团队策略

如果有多个实现者：

1. 团队一起完成 Setup + Foundational
2. Foundational 完成后：
   - 实现者 A: User Story 1
   - 实现者 B: User Story 2
   - 实现者 C: User Story 3
3. 各故事独立完成并集成

---

## 任务状态定义

| 状态 | 含义 | 进入下一状态条件 |
|---|---|---|
| **Pending** | 等待开始 | 前置依赖全部完成 |
| **In Progress** | 正在实施 | 代码实现完成，自测通过 |
| **Spec Review** | Spec 合规审查中 | 审查者逐条检查 FR/SC 引用和无占位符 |
| **Quality Review** | 工程质量审查中 | 审查者检查代码质量、测试覆盖、边界条件 |
| **Rework** | 需返工 | 修复后重新进入审查 |
| **Verified** | 已验收 | 两类审查均已通过且记录在 `reviews/` 目录 |
| **Blocked** | 外部阻塞 | 阻塞原因已记录，解除后恢复 |

**关键规则**：
- 进入 `Verified` 必须**同时**存在 Spec 审查记录和 Quality 审查记录
- 两种审查必须由不同主体承担，且不得由任务实现者承担
- 禁止只使用 `[x]` 作为完成证据

---

## 注意事项

- [P] 任务 = 不同文件、无依赖
- [Story] 标签使任务可追溯到特定用户故事
- 每个用户故事应独立可完成和可测试
- 先验证测试 FAIL，再实现
- 每个任务或逻辑组后提交
- 在任何 checkpoint 停止以独立验证故事
- 避免：模糊任务、同文件冲突、破坏独立性的跨故事依赖

---

## 审查证据目录

<!--
  每个 Verified 任务必须在 `reviews/` 目录下产生两类审查记录：
-->

```text
specs/[###-feature]/reviews/
├── T001-spec-review.md      # Spec 合规审查（FR/SC 引用完整性、无占位符）
├── T001-quality-review.md   # 工程质量审查（代码质量、测试覆盖、边界条件）
├── T002-spec-review.md
├── T002-quality-review.md
├── ...
└── final-acceptance.md      # 最终验收（全部任务 Verified 后由用户批准）
```

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | [DATE] | Initial Draft | 依据 spec.md 和 plan.md 生成任务列表初稿 | — |
