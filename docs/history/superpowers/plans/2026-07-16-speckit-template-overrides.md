# P2A 实施计划：Spec Kit 中文模板覆盖

> **文档类型**：实施计划
> **关联路线图**：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md` P2A（第 41 行、第 117-124 行）
> **关联设计规格**：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1` 第 10 节
> **前置子项目**：P1 治理骨架
> **状态**：Planned
> **文档版本**：`1.0.0`
> **创建日期**：2026-07-16
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **批准范围**：仅授权在当前计划定义的交付物和步骤范围内执行 P2A；任何超出本计划范围的动作必须另行批准。
> **延期授权声明**：本计划不授权执行 P4A、P4B、P5、P6、P7 或任何未在本计划文件所有权清单中列出的操作。

---

## 1. 目标

在 `.specify/templates/overrides/` 下创建 **4 份简体中文覆盖模板**，使 XDev 的 `/speckit.specify`、`/speckit.plan`、`/speckit.tasks` 和 `/speckit.checklist` 命令生成符合 XDev 文档体系设计规格的中文文档骨架。

实施后：
- `Resolve-Template`（`common.ps1`）自动优先返回 `templates/overrides/` 下的覆盖模板；
- 所有生成的新 Feature Spec、Plan、Tasks、Checklist 均包含设计规格（第 10 节）定义的必备章节（元数据、批准记录、Constitution 检查、双重审查证据等）；
- 核心模板 `.specify/templates/*.md` 保持原样，不做任何修改。

---

## 2. 架构概述

### 2.1 模板解析链路

```text
/speckit.specify → create-new-feature.ps1 → Resolve-Template('spec-template')
                                                      │
                                          Priority 1: .specify/templates/overrides/spec-template.md  ← 本计划创建
                                          Priority 4: .specify/templates/spec-template.md            ← 不修改
```

```text
/speckit.plan    → setup-plan.ps1          → Resolve-Template('plan-template')
/speckit.tasks   → setup-tasks.ps1         → Resolve-Template('tasks-template')
/speckit.checklist → checklist 生成脚本    → Resolve-Template('checklist-template')
```

`Resolve-Template` 函数（`common.ps1:320-381`）查找逻辑：
1. **Priority 1**：`.specify/templates/overrides/<name>.md` — 项目覆盖（本计划创建）
2. Priority 2：`.specify/presets/<preset-id>/templates/` — 已安装预设
3. Priority 3：`.specify/extensions/<ext-id>/templates/` — 扩展模板
4. **Priority 4**：`.specify/templates/<name>.md` — 核心模板（不回退修改）

### 2.2 覆盖模板与核心模板的关系

| 模板名 | 核心用途 | 覆盖后用途 |
|---|---|---|
| `spec-template.md` | `specify` 命令的 Spec 骨架 | 中文 Spec 骨架，增加元数据、批准记录、Constitution 影响 |
| `plan-template.md` | `plan` 命令的 Plan 骨架 | 中文 Plan 骨架，增加 Constitution 逐条检查、安全例外判断 |
| `tasks-template.md` | `tasks` 命令的任务列表骨架 | 中文任务骨架，增加双重审查列、FR/SC 引用、LOOP 状态 |
| `checklist-template.md` | `checklist` 命令的检查清单骨架 | 中文检查清单骨架，按变更类型分类（安全/持久化/兼容性/跨平台/签名） |

覆盖模板**替代**核心模板（`Resolve-Template` 在找到 overrides 后立即返回，不回退到核心模板），因此覆盖模板必须包含核心模板的关键指令注释，同时追加 XDev 特有章节。

---

## 3. 全局约束

以下约束必须贯穿所有 Task：

| # | 约束 | 来源 |
|---|---|---|
| C1 | **简体中文**：模板文档正文、标题、注释使用简体中文；代码标识符、路径、命令、配置键、日志原文和 Git 提交信息保留英文 | 路线图第 15 行 |
| C2 | **不修改核心模板**：`.specify/templates/spec-template.md`、`plan-template.md`、`tasks-template.md`、`checklist-template.md`、`constitution-template.md` 一律不碰 | 路线图第 16 行 |
| C3 | **只创建 overrides 目录**：写入范围严格限定在 `.specify/templates/overrides/` 目录下的 4 个 `.md` 文件 | 路线图第 119-124 行 |
| C4 | **Constitution 候选上下文**：`.specify/memory/constitution.md` 当前为 `1.0.0-rc.3`（候选版），覆盖模板中的 Constitution 引用必须表述为"候选"而非"已批准" | 路线图第 13 行、设计规格第 7.0 节 |
| C5 | **不执行 `git add .`**：暂存文件时必须精确指定路径 | 路线图第 22 行 |
| C6 | **不授权 P4+**：任何超出 4 个覆盖模板的创建、修改、删除、移动均不在本计划授权范围内；如果需要额外操作，必须由用户另行批准 | 路线图第 175-186 行 |
| C7 | **代码/路径保留英文**：模板中的 `[BRACKET_PLACEHOLDER]` 键名、文件路径、命令示例和终端输出块保留英文原文 | 设计规格第 17 节 AC-002 |

---

## 4. 文件结构

### 4.1 目标交付物（P2A 路径白名单）

```
.specify/templates/overrides/
├── spec-template.md          # 创建 — 中文 Feature Spec 模板
├── plan-template.md          # 创建 — 中文 Implementation Plan 模板
├── tasks-template.md         # 创建 — 中文 Task List 模板
└── checklist-template.md     # 创建 — 中文 Checklist 模板
```

### 4.2 不修改的文件

```
.specify/templates/spec-template.md          # 英国核心模板 — 保持原样
.specify/templates/plan-template.md          # 英国核心模板 — 保持原样
.specify/templates/tasks-template.md         # 英国核心模板 — 保持原样
.specify/templates/checklist-template.md     # 英国核心模板 — 保持原样
.specify/templates/constitution-template.md  # 英国核心模板 — 保持原样
```

### 4.3 相关但不修改的文件

```
.specify/scripts/powershell/common.ps1       # Resolve-Template 函数 — 已优先读取 overrides
.specify/scripts/powershell/create-new-feature.ps1  # 消费 spec-template
.specify/scripts/powershell/setup-plan.ps1          # 消费 plan-template
.specify/scripts/powershell/setup-tasks.ps1         # 消费 tasks-template
.specify/memory/constitution.md                     # 1.0.0-rc.3 候选条文章引用
```

---

## 5. 逐任务分解

---

### Task 1：前置验证

**目标**：确认上游 P1 已完成、overrides 目录不存在（或可安全创建）、Spec Kit 脚本可正常定位核心模板。

**文件范围**：只读检查，不写入任何文件。

#### 步骤

1. 检查 P1 交付物是否存在：
   - 逐条验证 P1 路径清单（路线图第 102-115 行）所列文件均已存在。
   - 若任一路径缺失，**阻塞**，报告缺失项后退出，等待 P1 验收。
2. 检查目标目录状态：
   - 如果 `.specify/templates/overrides/` 目录不存在，后续 Task 2 步骤 1 中创建。
   - 如果目录已存在且包含任何 `.md` 文件，列出已有文件并**要求用户决策**（是否覆盖、跳过或重命名）。
3. 验证 `Resolve-Template` 当前工作正常：
   - 执行 `powershell -Command "& { . .\.specify\scripts\powershell\common.ps1; Resolve-Template -TemplateName 'spec-template' -RepoRoot $PWD }"` 确认返回核心模板路径。
   - 执行 `powershell -Command "& { . .\.specify\scripts\powershell\common.ps1; Resolve-Template -TemplateName 'plan-template' -RepoRoot $PWD }"` 确认返回核心模板路径。
   - 执行 `powershell -Command "& { . .\.specify\scripts\powershell\common.ps1; Resolve-Template -TemplateName 'tasks-template' -RepoRoot $PWD }"` 确认返回核心模板路径。
4. 验证 `common.ps1` 源码中 overrides 路径指向：
   - 确认 `common.ps1` 第 328-329 行存在 `$override = Join-Path $base "overrides/$TemplateName.md"`。
5. 若以上任一检查失败，**报告原因并阻塞**，不得进入 Task 2。

#### 验证命令

```powershell
# 检查 P1 交付物列表（逐条）
$p1Files = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/security/exceptions/README.md',
    'docs/security/exceptions/manifest-signature-verification.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md'
)
foreach ($f in $p1Files) {
    if (Test-Path -LiteralPath $f) { Write-Host "OK: $f" } else { Write-Host "MISSING: $f" }
}

# 检查 overrides 目录是否已有文件
if (Test-Path -LiteralPath '.specify\templates\overrides') {
    Write-Host 'WARNING: overrides directory already exists'
    Get-ChildItem -LiteralPath '.specify\templates\overrides' -Filter '*.md' | ForEach-Object { Write-Host "  EXISTING: $($_.Name)" }
} else {
    Write-Host 'OK: overrides directory does not exist, will create in Task 2'
}

# 验证 Resolve-Template 当前返回核心模板
. .\.specify\scripts\powershell\common.ps1
Write-Host 'spec-template resolves to:' (Resolve-Template -TemplateName 'spec-template' -RepoRoot $PWD)
Write-Host 'plan-template resolves to:' (Resolve-Template -TemplateName 'plan-template' -RepoRoot $PWD)
Write-Host 'tasks-template resolves to:' (Resolve-Template -TemplateName 'tasks-template' -RepoRoot $PWD)
```

**门禁**：全部 P1 文件存在；overrides 目录不存在或用户明确允许覆盖；Resolve-Template 三个路径均指向 `.specify/templates/*-template.md`。

---

### Task 2：创建 4 份覆盖模板

**目标**：在 `.specify/templates/overrides/` 下创建 4 个简体中文 Markdown 模板文件。

**文件范围**：仅写入以下 4 个文件：
- `.specify/templates/overrides/spec-template.md`
- `.specify/templates/overrides/plan-template.md`
- `.specify/templates/overrides/tasks-template.md`
- `.specify/templates/overrides/checklist-template.md`

**接口约定**：
- 所有模板**必须**保留核心模板中 `/speckit.*` 命令用于识别占位符的关键词（如 `$ARGUMENTS`、`[###-feature-name]`、`[P]` 标记、`US1/US2` 标签等），确保 Spec Kit 脚本能正确执行字符串替换和解析。
- 所有模板**必须**继承核心模板中的执行指令注释（以 `<!--` 开头、`-->` 结尾的 HTML 注释块），确保 Spec Kit Agent 能理解模板的填充规则。
- XDev 特有的章节（元数据块、批准记录、Constitution 检查、双重审查等）**追加**在核心结构之上，不与核心模板的 Spec Kit 指令冲突。

#### Task 2.1：创建 `.specify/templates/overrides/` 目录

如果目录不存在，创建 `.specify/templates/overrides/` 目录。

#### Task 2.2：创建 `spec-template.md`

**设计依据**：设计规格第 10.1 节。

```markdown
# Feature Specification: [FEATURE NAME]

> **文档类型**：Feature Spec
> **状态**：Draft
> **文档版本**：0.1.0
> **创建日期**：[DATE]
> **最后更新**：[DATE]
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义当前功能的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `[###-feature-name]`
**Created**: [DATE]
**Status**: Draft
**Input**: User description: "$ARGUMENTS"

---

## Why

<!--
  必须在此处说明为什么需要此功能，而非仅仅描述功能做什么。
  回答：当前存在什么问题？谁受影响？不做会有什么后果？
-->

[说明此功能的业务或技术动机]

## 目标

<!--
  用一两句话概括此功能的核心目标。目标必须是可验证的结果，而非实现方式。
-->

[核心目标]

## 范围

### 包含

- [明确包含在本次交付中的内容]

### 非目标

<!--
  显式列出本次不实现的内容，防止范围蔓延。
  这些项目可以留作后续 Feature Spec。
-->

- [明确排除的内容]

---

## User Scenarios & Testing *(mandatory)*

<!--
  IMPORTANT: User stories should be PRIORITIZED as user journeys ordered by importance.
  Each user story/journey must be INDEPENDENTLY TESTABLE - meaning if you implement just ONE of them,
  you should still have a viable MVP (Minimum Viable Product) that delivers value.

  Assign priorities (P1, P2, P3, etc.) to each story, where P1 is the most critical.
  Think of each story as a standalone slice of functionality that can be:
  - Developed independently
  - Tested independently
  - Deployed independently
  - Demonstrated to users independently
-->

### User Story 1 - [Brief Title] (Priority: P1)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently - e.g., "Can be fully tested by [specific action] and delivers [specific value]"]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]
2. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

### User Story 2 - [Brief Title] (Priority: P2)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

### User Story 3 - [Brief Title] (Priority: P3)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

[Add more user stories as needed, each with an assigned priority]

### 边界条件

<!--
  必须列出边界条件和异常场景。不得以 TODO、TBD 或
  [NEEDS CLARIFICATION] 填充 — 如果暂时无法确定，
  在 Plan 阶段之前必须解决。
-->

- 当 [边界条件] 时，系统应 [行为]？
- 当 [错误场景] 时，系统应 [处理方式]？

---

## Requirements *(mandatory)*

<!--
  ACTION REQUIRED: The content in this section represents placeholders.
  Fill them out with the right functional requirements.
-->

### 功能需求 (Functional Requirements)

- **FR-001**：系统必须 [具体能力描述]
- **FR-002**：系统必须 [具体能力描述]
- **FR-003**：用户必须能够 [关键交互]
- **FR-004**：系统必须 [数据要求]
- **FR-005**：系统必须 [行为要求]

*示例：标记不确定需求*

- **FR-006**：系统必须通过 [NEEDS CLARIFICATION: 认证方式未指定 — email/password、SSO、OAuth？] 认证用户
- **FR-007**：系统必须保留用户数据 [NEEDS CLARIFICATION: 保留期限未指定]

### 非功能需求 (Non-Functional Requirements)

<!--
  根据设计规格，以下维度必须逐条评估：
-->

- **NFR-001**：性能 — [响应时间、吞吐量等]
- **NFR-002**：安全 — [认证、授权、数据保护等]
- **NFR-003**：兼容性 — [对已有功能、CLI 接口、配置和文件的兼容要求]
- **NFR-004**：跨平台 — [Windows/Linux/macOS 行为一致性要求]
- **NFR-005**：可维护性 — [日志、调试、文档要求]

### 关键实体 *(如果功能涉及数据)*

- **[实体 1]**：[含义及关键属性，不含实现细节]
- **[实体 2]**：[含义及与其他实体的关系]

---

## Success Criteria *(mandatory)*

<!--
  ACTION REQUIRED: Define measurable success criteria.
  These must be technology-agnostic and measurable.
-->

### 可度量成果

- **SC-001**：[可度量指标，如 "用户可在 2 分钟内完成账户创建"]
- **SC-002**：[可度量指标，如 "系统支持 1000 并发用户无明显降级"]
- **SC-003**：[用户满意度指标，如 "90% 用户首次尝试即成功完成主要任务"]
- **SC-004**：[业务指标，如 "减少 [X] 相关工单 50%"]

---

## 假设与依赖

<!--
  列出所有假设条件，当未明确指定时基于合理默认值填充。
-->

- [关于目标用户的假设]
- [关于范围边界的假设]
- [关于数据/环境的假设]
- [对现有系统/服务的依赖]

---

## 风险分类

<!--
  识别与此功能相关的主要风险并标注等级：
  - 安全风险：是否涉及信任边界、密钥、认证或授权变更？
  - 兼容性风险：是否影响已有 CLI 接口、manifest、lockfile 或目录结构？
  - 跨平台风险：是否依赖平台特定 API 或路径？
  - 持久化风险：是否修改用户配置、数据文件或数据库 schema？
  - 签名/发布风险：是否影响发布产物生成或签名流程？
-->

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | | | |
| 兼容性 | | | |
| 跨平台 | | | |
| 持久化 | | | |
| 签名/发布 | | | |

---

## Constitution、兼容性、跨平台与安全影响

<!--
  本节对应设计规格第 10.1 节中对 Spec 模板的要求。
  在 Constitution 正式批准前（当前为 1.0.0-rc.3 候选），
  本节的判定结果仅作为审查建议，不作为强制性阻塞条件。
-->

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 1：[名称] | Compliant / Not Applicable / Needs Review | |
| 原则 2：[名称] | | |
| ... | | |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？[是/否，若是则说明]
- 是否修改 manifest 或 lockfile 格式？[是/否]
- 是否影响已有目录结构？[是/否]

### 跨平台影响

- [Windows/Linux/macOS 差异化行为说明]
- [平台特定依赖或限制]

### 安全影响

- 是否涉及认证/授权变更？[是/否]
- 是否涉及敏感数据存储或传输？[是/否]
- 是否引入新的信任边界？[是/否]

---

## 批准记录 (Approval History)

<!--
  Orchestrator 必须等待用户在对话中明确批准后才能写入批准记录。
  Agent 的审查结论不能替代用户批准。
  语义变化会使原批准对变化部分失效。
-->

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Pending | — | — | — | 整体 Spec 的目标、范围、需求和验收标准 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | [DATE] | Initial Draft | 依据 $ARGUMENTS 生成 Feature Spec 初稿 | — |
```

#### Task 2.3：创建 `plan-template.md`

**设计依据**：设计规格第 10.2 节。

```markdown
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
```

#### Task 2.4：创建 `tasks-template.md`

**设计依据**：设计规格第 10.3 节。

```markdown
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
```

#### Task 2.5：创建 `checklist-template.md`

**设计依据**：设计规格第 10.4 节。

```markdown
# [CHECKLIST TYPE] Checklist: [FEATURE NAME]

> **文档类型**：Checklist
> **关联 Spec**：`specs/[###-feature-name]/spec.md`
> **关联 Plan**：`specs/[###-feature-name]/plan.md`
> **创建日期**：[DATE]
> **最后更新**：[DATE]
> **触发条件**：涉及安全、持久化、兼容性、跨平台、架构约束或发布签名的变更

**Purpose**: [Brief description of what this checklist covers]
**Created**: [DATE]
**Feature**: [Link to spec.md or relevant documentation]

**Note**: This checklist is generated by the `/speckit.checklist` command based on feature context and requirements.

<!--
  ============================================================================
  IMPORTANT: The checklist items below are SAMPLE ITEMS for illustration only.

  The /speckit.checklist command MUST replace these with actual items based on:
  - User's specific checklist request
  - Feature requirements from spec.md
  - Technical context from plan.md
  - Implementation details from tasks.md

  DO NOT keep these sample items in the generated checklist file.

  XDev 特有要求：
  - 以下变更类型必须生成 checklist：
    1. 安全和信任边界
    2. 持久化配置或用户数据
    3. CLI、manifest、lockfile 或目录兼容性
    4. Windows、Linux、macOS 平台行为
    5. 当前架构强约束
    6. 正式发布和签名流程
  - Checklist 是审查输入，不能替代双重审查。
  ============================================================================
-->

---

## 1. 安全与信任边界

> **触发**：涉及认证、授权、密钥、签名、加密或信任边界变更时必填。

- [ ] CHK001 所有用户输入均已验证和清理
- [ ] CHK002 敏感数据（密钥、token、密码）不在日志或错误消息中泄露
- [ ] CHK003 认证/授权变更已通过安全审查
- [ ] CHK004 信任边界未意外扩大
- [ ] CHK005 第三方依赖的安全公告已检查（无已知高危漏洞）

---

## 2. 持久化配置与用户数据

> **触发**：涉及配置文件、数据库 schema、本地存储或用户数据格式变更时必填。

- [ ] CHK006 配置 schema 变更向后兼容（旧配置可正常读取）
- [ ] CHK007 数据迁移脚本已编写且通过 dry-run 测试
- [ ] CHK008 迁移失败时回滚路径已定义和测试
- [ ] CHK009 用户数据文件在异常退出后保持一致性
- [ ] CHK010 已有用户数据的升级路径已验证

---

## 3. CLI、Manifest、Lockfile 与目录兼容性

> **触发**：涉及 CLI 参数、manifest 格式、lockfile、目录结构或路径变更时必填。

- [ ] CHK011 已有 CLI 参数未被移除（deprecated 参数保留并输出警告）
- [ ] CHK012 新增 CLI 参数不与已有参数冲突
- [ ] CHK013 manifest/lockfile 格式变更后旧版本可被识别（至少输出明确错误）
- [ ] CHK014 目录结构变更已在 README 和运维文档中更新
- [ ] CHK015 路径引用（代码内、文档内、脚本内）已全部更新

---

## 4. 跨平台行为 (Windows / Linux / macOS)

> **触发**：涉及平台特定 API、路径、进程管理或文件系统时必填。

- [ ] CHK016 Windows (PowerShell 5.1) 上核心路径已验证
- [ ] CHK017 Linux (amd64) 上核心路径已验证
- [ ] CHK018 macOS 12+ 上核心路径已验证
- [ ] CHK019 路径分隔符使用跨平台兼容方式（`Join-Path` / `filepath.Join`）
- [ ] CHK020 平台特定依赖已声明且有 fallback
- [ ] CHK021 文件权限和行尾符在各平台一致

---

## 5. 当前架构强约束

> **触发**：涉及 Constitution（候选 rc.3）中定义为"当前架构强约束"的项时必填。

- [ ] CHK022 改动未违反当前架构强约束（如语言版本、核心依赖、构建系统）
- [ ] CHK023 如需放宽架构约束，对应 ADR 已创建并获得接受
- [ ] CHK024 CI pipeline 仍可正常执行（构建、测试、lint 均通过）

---

## 6. 正式发布与签名流程

> **触发**：涉及发布产物、签名、版本号或分发渠道变更时必填。

- [ ] CHK025 版本号遵循语义化版本规范（MAJOR.MINOR.PATCH）
- [ ] CHK026 发布产物的构建环境与 CI 环境一致
- [ ] CHK027 所有目标平台的发布产物已生成且通过 smoke test
- [ ] CHK028 签名流程已验证（如果适用）
- [ ] CHK029 发布说明包含此版本的变更摘要和已知问题

---

## 附加检查项

<!-- 根据功能特性添加自定义分类 -->

- [ ] CHK030 [自定义检查项]
- [ ] CHK031 [自定义检查项]

---

## 最终验证

- [ ] CHK099 所有 FR-* 功能需求均有对应验证
- [ ] CHK100 所有 SC-* 成功标准均有通过证据
- [ ] CHK101 两类审查（Spec 合规 + 工程质量）记录均在 `reviews/` 目录下

---

## 注意事项

- 完成项标记: `[x]`
- 在行内添加注释或发现
- 链接到相关资源或文档
- 项目按分类顺序编号，便于引用
- 此 checklist 是审查输入，不能替代独立的双重审查流程

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | [DATE] | Initial Draft | 依据 spec.md 和 plan.md 生成 checklist 初稿 | — |
```

---

### Task 3：验证

**目标**：确认 4 份覆盖模板已创建、`Resolve-Template` 正确解析、核心模板未被修改、模板内容符合约束。

**文件范围**：只读检查，不修改任何文件。

#### 步骤

1. **检查覆盖模板存在性**：
   - 逐一验证 4 个文件存在且非空：
     - `.specify/templates/overrides/spec-template.md`
     - `.specify/templates/overrides/plan-template.md`
     - `.specify/templates/overrides/tasks-template.md`
     - `.specify/templates/overrides/checklist-template.md`
   - 若缺失或为 0 字节，**阻塞**。

2. **验证 Resolve-Template 返回覆盖模板**：
   - 对 `spec-template`、`plan-template`、`tasks-template`、`checklist-template` 各执行一次 `Resolve-Template`；
   - 确认每个返回值均指向 `.specify/templates/overrides/` 下的对应文件（而非核心模板路径）。

3. **验证核心模板未被修改**：
   - 对比核心模板文件（5 个）与 `HEAD` 的 diff；
   - 若任何核心模板有未预期修改，**阻塞**并报告。

4. **验证模板内容约束**（手动审查）：
   - 检查每个覆盖模板的正文、标题、注释均为简体中文；
   - 检查代码块、路径、命令、配置键在模板示例中保留英文；
   - 检查每个覆盖模板保留了核心模板的关键 Spec Kit 指令（`$ARGUMENTS`、`[###-feature-name]`、`[P]`、`US1/US2`、`<!-- ... -->` HTML 注释块等）。

5. **验证不包含未授权修改**：
   - 确认 `.specify/templates/overrides/` 目录下仅包含 4 个文件（不多不少）；
   - 确认仓库中无其他新增或修改文件。

#### 验证命令

```powershell
# 1. 检查覆盖模板存在且非空
$overrides = @(
    '.specify/templates/overrides/spec-template.md',
    '.specify/templates/overrides/plan-template.md',
    '.specify/templates/overrides/tasks-template.md',
    '.specify/templates/overrides/checklist-template.md'
)
foreach ($f in $overrides) {
    if (Test-Path -LiteralPath $f) {
        $size = (Get-Item -LiteralPath $f).Length
        Write-Host "OK: $f ($size bytes)"
    } else {
        Write-Host "MISSING: $f"
    }
}

# 2. 验证 Resolve-Template 返回覆盖模板
. .\.specify\scripts\powershell\common.ps1
Write-Host 'spec-template resolves to:' (Resolve-Template -TemplateName 'spec-template' -RepoRoot $PWD)
Write-Host 'plan-template resolves to:' (Resolve-Template -TemplateName 'plan-template' -RepoRoot $PWD)
Write-Host 'tasks-template resolves to:' (Resolve-Template -TemplateName 'tasks-template' -RepoRoot $PWD)
Write-Host 'checklist-template resolves to:' (Resolve-Template -TemplateName 'checklist-template' -RepoRoot $PWD)
# 每个输出应包含 ...templates\overrides\... 路径

# 3. 验证核心模板未被修改
git diff --name-only HEAD -- .specify/templates/spec-template.md .specify/templates/plan-template.md .specify/templates/tasks-template.md .specify/templates/checklist-template.md .specify/templates/constitution-template.md
# 应无输出（即无修改）

# 4. 验证 overrides 目录下仅包含 4 个文件
$count = (Get-ChildItem -LiteralPath '.specify\templates\overrides' -Filter '*.md').Count
Write-Host "Files in overrides/: $count (expected: 4)"
if ($count -ne 4) { Write-Host 'ERROR: unexpected file count in overrides/' }

# 5. 验证仓库中无其他未预期修改
git status --short
# overrides 目录下的 4 个新文件应在 Untracked files 中
# 无其他修改（Modified/Deleted）文件
```

**门禁**：4 个覆盖模板均存在且非空；`Resolve-Template` 对 4 个模板名均返回 `overrides/` 路径；核心模板无修改；overrides 目录下仅有 4 个文件；仓库中无非预期修改。

---

### Task 4：审查与提交

**目标**：执行 P2A 子项目的统一审查流程（路线图第 187-200 行），生成差异包，由用户批准后提交。

**文件范围**：仅暂存 4 个覆盖模板文件。

#### 步骤

1. **记录实施前 commit**：
   - 执行 `git rev-parse HEAD` 获取当前 commit hash，记录为实施前基线。
2. **执行 Spec 合规审查**：
   - 由独立审查者（非实施者）逐条检查：
     - 文件是否落在 P2A 路径白名单内（仅 `overrides/` 下 4 个文件）；
     - 模板内容是否覆盖设计规格第 10.1-10.4 节的要求；
     - 是否保留了 Spec Kit 的关键占位符和指令注释；
     - 是否满足全局约束 C1-C7。
   - 阻塞问题必须修复后复审；非阻塞问题记录但不阻塞提交。
3. **执行工程质量审查**：
   - 由不同于 Spec 审查者的独立审查者检查：
     - Markdown 语法正确性；
     - 模板示例的一致性（如 FR/SC 编号规则一致）；
     - 中英文混合边界的正确性（代码块、命令、路径保留英文）；
     - 无硬编码的特定项目路径或数值（模板应是通用的）。
   - 阻塞问题必须修复后复审；非阻塞问题记录但不阻塞提交。
4. **生成差异包**：
   - 执行 `git diff --stat HEAD` 展示变更概要；
   - 执行 `git diff HEAD -- .specify/templates/overrides/` 展示具体变更内容。
5. **用户批准**：
   - 将差异包和两类审查结论提交给用户；
   - **等待用户明确批准后才能暂存和提交**。
6. **精确暂存**（用户批准后）：
   - 执行以下命令（**不得使用 `git add .` 或 `git add -A`**）：
   ```powershell
   git add '.specify/templates/overrides/spec-template.md'
   git add '.specify/templates/overrides/plan-template.md'
   git add '.specify/templates/overrides/tasks-template.md'
   git add '.specify/templates/overrides/checklist-template.md'
   ```
7. **创建提交**：
   - 提交信息使用英文：
   ```text
   feat: add Chinese spec template overrides for P2A

   Created 4 Chinese override templates under .specify/templates/overrides/:
   - spec-template.md (XDev Feature Spec template with metadata and approval records)
   - plan-template.md (XDev Implementation Plan template with Constitution checklist)
   - tasks-template.md (XDev Task List template with dual-review columns)
   - checklist-template.md (XDev Checklist template with risk-category sections)

   Ref: P2A of docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md
   ```

#### 验证命令

```powershell
# 记录实施前基线
$baseline = git rev-parse HEAD
Write-Host "Baseline commit: $baseline"

# 生成差异包（在 overrides 目录创建后、暂存前）
git diff --stat HEAD
git diff HEAD -- .specify/templates/overrides/

# 精确暂存（用户批准后执行）
git add '.specify/templates/overrides/spec-template.md'
git add '.specify/templates/overrides/plan-template.md'
git add '.specify/templates/overrides/tasks-template.md'
git add '.specify/templates/overrides/checklist-template.md'

# 确认暂存区仅包含 4 个目标文件
git diff --cached --name-only
# 应输出 4 行，每行指向 overrides/ 下的对应文件

# 创建提交（用户批准后执行）
git commit -m 'feat: add Chinese spec template overrides for P2A

Created 4 Chinese override templates under .specify/templates/overrides/:
- spec-template.md (XDev Feature Spec template with metadata and approval records)
- plan-template.md (XDev Implementation Plan template with Constitution checklist)
- tasks-template.md (XDev Task List template with dual-review columns)
- checklist-template.md (XDev Checklist template with risk-category sections)

Ref: P2A of docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md'
```

**门禁**：两类审查均通过（无阻塞问题）；用户明确批准；暂存区仅包含 4 个 `overrides/` 文件；提交信息符合规范。

---

## 6. 验收标准

| # | 标准 | 验证方式 |
|---|---|---|
| AC-P2A-01 | `.specify/templates/overrides/` 目录下存在 4 个 `.md` 文件，文件名精确匹配路线图 P2A 路径清单 | Task 3 步骤 1 |
| AC-P2A-02 | `Resolve-Template` 对 `spec-template`、`plan-template`、`tasks-template`、`checklist-template` 均返回 `overrides/` 路径 | Task 3 步骤 2 |
| AC-P2A-03 | 5 个核心模板（`.specify/templates/*.md`）内容与 HEAD 一致，未被修改 | Task 3 步骤 3 |
| AC-P2A-04 | 所有覆盖模板正文、标题、注释使用简体中文；代码标识符、路径、命令、配置键保留英文 | Task 3 步骤 4 |
| AC-P2A-05 | 所有覆盖模板保留了 Spec Kit 所需的关键指令占位符（`$ARGUMENTS`、`[###-feature-name]`、`[P]`、`US1/US2` 标签、`<!-- ... -->` 指令注释块） | Task 3 步骤 4 |
| AC-P2A-06 | overrides 目录下不包含路线图 P2A 清单之外的任何文件 | Task 3 步骤 5 |
| AC-P2A-07 | 仓库中除 `overrides/` 下 4 个新文件外，无其他修改 | `git status` 验证 |
| AC-P2A-08 | 两类独立审查（Spec 合规 + 工程质量）均已通过，无阻塞问题 | Task 4 步骤 2-3 |
| AC-P2A-09 | 用户明确批准 P2A 提交 | Task 4 步骤 5 |
| AC-P2A-10 | 提交仅包含 4 个 overrides 文件，提交信息符合规范 | Task 4 步骤 7 |

---

## 7. 明确延后与不授权事项

以下操作**不在本计划授权范围内**，不得在 P2A 执行期间顺手完成：

- 创建或修改 `.specify/templates/overrides/adr-template.md`（属于 P2B）；
- 创建或修改 `.specify/templates/overrides/security-exception-template.md`（属于 P2B）；
- 创建或修改 `.specify/templates/overrides/archive-metadata-template.md`（属于 P2B）；
- 创建或修改 `.specify/templates/overrides/review-record-template.md`（属于 P2B）；
- 修改 `.specify/templates/` 中的任何核心模板文件；
- 修改 `.specify/scripts/powershell/common.ps1` 或任何 Spec Kit 脚本；
- 修改 `.specify/memory/constitution.md`；
- 修改 `.github/workflows/` 下的任何文件；
- 执行任何与旧文档移动、元数据添加、链接修复相关的操作（属于 P4A/P4B）；
- 创建 `scripts/docs/validate-*.ps1` 或任何验证脚本（属于 P3/P5）；
- 创建 Feature Spec（属于 P6）；
- 将 Constitution 从 `rc.3` 升级为正式版（属于 P7）；
- 修改 CI pipeline 配置或切换治理 Job 为阻塞模式；
- 使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd` 命令。

如需执行上述任何操作，必须由用户在新的子项目实施计划中另行批准。

---

## 8. 审批记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P2A 实施计划批准 | Pending | — | — | `1.0.0` | 授权按本计划在 P2A 路径白名单内写入 4 个覆盖模板 |

---

## 9. 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `1.0.0` | 2026-07-16 | Initial | 依据路线图 P2A 和设计规格第 10 节编写 P2A 实施计划 | — |
