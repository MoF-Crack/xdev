# Implementation Plan: [FEATURE]

> **文档类型**：Implementation Plan
> **状态**：Planned
> **文档版本**：0.1.0
> **创建日期**：[DATE]
> **最后更新**：[DATE]
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/[###-feature-name]/spec.md`
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：无

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]

**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

---

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

---

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: [e.g., Go 1.23, Python 3.11, TypeScript 5.x or NEEDS CLARIFICATION]
**Primary Dependencies**: [e.g., Wails v2, Cobra CLI, or NEEDS CLARIFICATION]
**Storage**: [if applicable, e.g., SQLite, JSON file, memory or N/A]
**Testing**: [e.g., go test, pytest, or NEEDS CLARIFICATION]
**Target Platform**: [e.g., Windows 10+, Linux (amd64), macOS 12+]
**Project Type**: [e.g., desktop-app/cli-tool/web-service]
**Performance Goals**: [domain-specific, e.g., UI 响应 <100ms, 文件扫描 <5s]
**Constraints**: [domain-specific, e.g., 无外部网络依赖, 离线可用, 内存 <200MB]
**Scale/Scope**: [domain-specific, e.g., 单用户桌面应用, 本地文件管理]

---

## 技术方案

### 模块边界与接口

<!--
  描述主要模块及其职责边界。
  每个模块应说明：职责、输入/输出接口、与其他模块的交互方式。
-->

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| [模块名] | [职责描述] | [输入] | [输出] | [依赖] |

### 数据流

<!--
  描述关键数据流路径：触发条件 → 处理链 → 最终结果。
  标注失败处理路径。
-->

```text
[触发源]
  → [处理阶段 1]
    → (成功) [处理阶段 2]
    → (失败) [错误处理路径]
  → [最终输出/持久化]
```

### 失败处理

<!--
  对每个关键路径，说明失败模式和处理策略：
  - 失败模式：可能如何失败
  - 检测方式：如何检测到失败
  - 恢复策略：回滚、重试、降级还是上报
-->

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| | | | |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？[是/否，若是则说明迁移策略]
- 是否改变已有配置文件格式？[是/否]
- 是否废弃已有接口？[是/否]

### 数据迁移

- 是否需要数据迁移？[是/否]
- 迁移策略：[自动迁移 / 手动迁移 / 不适用]

---

## 安全设计

<!--
  对于涉及安全敏感操作的方案，本节必须详细说明安全设计。
-->

- 认证/授权方式：[描述]
- 敏感数据保护：[加密存储、传输加密、内存安全等]
- 信任边界：[明确信任边界的位置]
- 输入验证：[描述对外部输入的验证策略]

---

## 测试隔离

<!--
  说明测试策略，尤其是集成测试和隔离测试的边界。
-->

- **单元测试范围**：[哪些模块需要单元测试]
- **集成测试范围**：[哪些路径需要集成测试]
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | [手动/自动] | [范围] |
| Linux (amd64) | [手动/自动] | [范围] |
| macOS 12+ | [手动/自动] | [范围] |

---

## Constitution 逐条检查

*GATE: 必须在 Phase 0 research 之前通过。Phase 1 design 之后重新检查。*

<!--
  Constitution 当前为 1.0.0-rc.3 候选版。
  检查结果使用以下合法值：
  - Compliant：完全符合
  - Not Applicable：不适用
  - Exception Required：需要安全例外（需在 Plan 中引用例外文件）
  - ADR Required：需要架构决策记录（需在 Plan 中引用 ADR）
  - Non-compliant：不符合（阻塞，不得进入 Tasks）
-->

| Constitution 原则（候选 rc.3） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| [原则 1 名称] | Compliant / Not Applicable / Exception Required / ADR Required / Non-compliant | [说明] | [例外或 ADR 路径] |
| [原则 2 名称] | | | |
| [原则 3 名称] | | | |
| [原则 4 名称] | | | |
| [原则 5 名称] | | | |
| [原则 6 名称] | | | |

### 检查结果汇总

- Compliant：_ 项
- Not Applicable：_ 项
- Exception Required：_ 项（需引用安全例外文件）
- ADR Required：_ 项（需引用 ADR 文件）
- Non-compliant：_ 项（阻塞，不得进入 Tasks）

---

## ADR 与安全例外判断

<!--
  根据 Constitution 检查结果，列出需要创建或引用的 ADR 和安全例外。
-->

### 需要创建的 ADR

| ADR 编号 | 决策主题 | 状态 | 引用路径 |
|---|---|---|---|
| ADR-XXX | [主题] | Proposed | `docs/adr/XXXX-主题.md` |

### 需要的安全例外

| 例外编号 | 例外主题 | 状态 | 引用路径 |
|---|---|---|---|
| EXC-XXX | [主题] | Draft | `docs/security/exceptions/主题.md` |

---

## 验证方案

<!--
  在实施前定义可执行的验证方案，确保 Plan 不偏离 Spec。
-->

- **Spec 合规验证**：逐条检查 FR-* 和 SC-* 是否在 Plan 中涵盖
- **Constitution 合规验证**：每项 Non-compliant 必须有用户批准的安全例外或 ADR
- **兼容性验证**：验证已有 CLI、配置和文件格式不受破坏
- **跨平台验证**：在所有目标平台上验证核心路径

---

## 项目结构

### 文档 (此功能专属)

```text
specs/[###-feature]/
├── spec.md             # Feature Spec（/speckit.specify 命令输出）
├── plan.md             # 本文件（/speckit.plan 命令输出）
├── research.md         # Phase 0 输出（/speckit.plan 命令）
├── data-model.md       # Phase 1 输出（/speckit.plan 命令）
├── quickstart.md       # Phase 1 输出（/speckit.plan 命令）
├── contracts/          # Phase 1 输出（/speckit.plan 命令）
├── tasks.md            # Phase 2 输出（/speckit.tasks 命令 — NOT created by /speckit.plan）
├── checklist.md        # 高风险变更检查清单（/speckit.checklist 命令）
└── reviews/            # 逐任务审查记录和最终验收
    ├── T001-spec-review.md
    ├── T001-quality-review.md
    └── final-acceptance.md
```

### 源代码 (仓库根目录)

<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths. The delivered plan must not include Option labels.
-->

```text
# [REMOVE IF UNUSED] Option 1: Single project (DEFAULT)
src/
├── models/
├── services/
├── cli/
└── lib/

tests/
├── contract/
├── integration/
└── unit/

# [REMOVE IF UNUSED] Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# [REMOVE IF UNUSED] Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure: feature modules, UI flows, platform tests]
```

**Structure Decision**: [Document the selected structure and reference the real directories captured above]

---

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |

---

## 批准记录 (Approval History)

<!--
  Orchestrator 必须等待用户在对话中明确批准后才能写入批准记录。
  Plan 进入 Tasks 阶段前，用户必须批准 Plan。
-->

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan 批准 | Pending | — | — | — | 技术方案、Constitution 检查、兼容/安全设计 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | [DATE] | Initial Draft | 依据 spec.md 生成实施计划初稿 | — |
