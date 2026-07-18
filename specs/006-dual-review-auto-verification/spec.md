# Feature Specification: 双重审查和批准记录自动验证

> **文档类型**：Feature Spec
> **状态**：Draft
> **文档版本**：0.1.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-16
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义双重审查和批准记录自动验证的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `006-dual-review-auto-verification`
**Created**: 2026-07-16
**Status**: Draft
**Input**: Constitution `1.0.0-rc.3` 当前已知执行缺口第 300 行，原则 IV

---

## Why

Constitution `1.0.0-rc.3` 原则 IV 要求：

> "每个实现任务必须依次通过两个独立门禁：Spec 合规审查和工程质量审查。两个审查者均不得是该任务的实现者。审查通过不等于用户批准。"

当前已知执行缺口第 300 行指出：

> "双重审查和批准历史尚未由模板或自动检查强制。"

当前审查流程依赖人工约定和 Orchestrator 调度，缺少自动化检查确保：
1. 每个标记为 `Verified` 的任务存在两类审查记录（Spec 合规审查 + 工程质量审查）；
2. 审查者身份与任务实现者不重复；
3. 审查记录的文件路径和格式符合项目约定（`specs/<编号-功能名>/reviews/`）。

缺少自动验证时：
- 审查可能在对话中口头完成但未持久化到版本化文档；
- 审查者独立性无法在事后追溯验证；
- 任务可能被错误标记为 `Verified` 但缺少完整的审查证据。

受影响方：
- 所有参与 SDD LOOP 的实施者和审查者；
- Orchestrator（缺少可自动检查的审查合规证据）；
- P7 Constitution 正式批准（缺少治理自动化对审查记录完整性的验证）。

不关闭此缺口：Constitution 的双重审查要求缺少可验证的执行手段，审查合规性完全依赖人工记忆和约定。

## 目标

建立自动化验证机制，检查 `specs/<编号-功能名>/reviews/` 下的审查记录完整性、审查者独立性以及格式合规性。缺失、违规或不合规时，CI 或验证脚本发出阻塞信号（非零退出码）。

## 范围

### 包含

- 审查记录目录约定（`specs/<编号-功能名>/reviews/` 下的文件命名和格式）
- 审查者独立性验证规则（审查者 ≠ 实现者）
- 审查记录完整性验证（每个 `Verified` 任务存在两类审查记录）
- CI 集成或 PowerShell 验证脚本
- 与 `tasks.md` 中任务状态和审查者字段的关联验证

### 非目标

- 审查内容质量的自动判断（由人工审查者负责）
- 取代审查流程的自动化审批
- 审查模板的创建（由 P2B 项目治理模板覆盖）
- 对历史已完成审查的追溯验证
- 在 Constitution 候选阶段将验证结果作为阻塞条件（当前仅报告）

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - CI 自动检查每个 Verified 任务有对应的两类审查记录 (Priority: P1)

当 CI 运行治理验证脚本时，脚本扫描活跃 Spec 目录下的 `reviews/` 目录，检查每个在 `tasks.md` 中标记为 `Verified` 的任务是否同时存在对应的 Spec 审查记录（`TXXX-spec-review.md`）和 Quality 审查记录（`TXXX-quality-review.md`）。

**Why this priority**: 审查记录完整性是双重审查门禁的最基础要求——记录缺失意味着审查可能从未发生。

**Independent Test**: 创建一个仅有一类审查记录的任务，运行验证脚本，验证脚本报告缺失并返回非零退出码。

**Acceptance Scenarios**:

1. **Given** `tasks.md` 中任务 T001 状态为 `Verified`，`reviews/` 目录下存在 `T001-spec-review.md` 和 `T001-quality-review.md`，**When** 验证脚本扫描该 Spec 目录，**Then** 对 T001 的检查通过
2. **Given** 任务 T002 状态为 `Verified` 但 `reviews/` 目录下仅存在 `T002-spec-review.md`，**When** 验证脚本扫描，**Then** 脚本报告 T002 缺少 Quality 审查记录，返回非零退出码
3. **Given** 任务 T003 状态为 `In Progress`（非 `Verified`），**When** 验证脚本扫描，**Then** 不对 T003 执行审查记录检查

---

### User Story 2 - 审查者与实现者重复时检测并报告 (Priority: P1)

验证脚本比对每个审查记录中的审查者身份与 `tasks.md` 中该任务的实现者字段。如果审查者与实现者为同一人，脚本报告违规。

**Why this priority**: 审查独立性是 Constitution 原则 IV 的核心要求——"两个审查者均不得是该任务的实现者"。

**Independent Test**: 在 `tasks.md` 中将某任务的实现者和 Spec 审查者设为同一值，运行验证脚本，验证脚本检测到并报告违规。

**Acceptance Scenarios**:

1. **Given** `tasks.md` 中 T001 实现者为 "Agent-A"，`T001-spec-review.md` 中审查者为 "Agent-B"，**When** 验证脚本检查独立性，**Then** 通过
2. **Given** `tasks.md` 中 T002 实现者为 "Agent-C"，`T002-quality-review.md` 中审查者也是 "Agent-C"，**When** 验证脚本检查独立性，**Then** 报告 "审查者与实现者重复"，返回非零退出码
3. **Given** 审查记录缺少审查者字段，**When** 验证脚本检查，**Then** 报告 "审查者信息缺失"

---

### User Story 3 - 审查记录缺失时 PR 阻塞 (Priority: P2)

在 CI governance workflow 中集成审查记录验证。如果验证发现任何缺失或违规，CI job 标记为 failure（报告模式）或 warning（视 Constitution 批准状态）。

**Why this priority**: CI 集成使验证自动化，但不改变当前报告模式（阻塞模式需 P7 Constitution 正式批准后启用）。

**Independent Test**: 推送一个审查记录不完整的 Spec 变更，验证 CI governance job 报告失败/warning。

**Acceptance Scenarios**:

1. **Given** PR 修改了 `tasks.md` 将某任务标记为 `Verified` 但未提交审查记录，**When** CI governance job 运行，**Then** job 报告审查记录缺失
2. **Given** 所有 Verified 任务审查记录完整且独立，**When** CI governance job 运行，**Then** job 通过

---

### 边界条件

- 当 `reviews/` 目录不存在（新 Spec 尚未进入审查阶段）时，验证脚本应跳过而非报错？
- 当审查记录文件命名不符合 `TXXX-spec-review.md` / `TXXX-quality-review.md` 约定但存在时，应如何处理？
- 当同一任务有多轮审查（Rework 后重新审查）时，验证脚本应检查最新记录还是所有记录？
- 当审查者使用真实姓名而实现者使用代号导致比对困难时，应如何标准化身份标识？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：验证脚本必须扫描活跃 Spec 目录的 `tasks.md`，提取所有状态为 `Verified` 的任务 ID
- **FR-002**：验证脚本必须检查 `reviews/` 目录下每个 `Verified` 任务是否存在对应的 Spec 审查记录（`<task-id>-spec-review.md`）和 Quality 审查记录（`<task-id>-quality-review.md`）
- **FR-003**：验证脚本必须从 `tasks.md` 提取每个任务的实现者字段，并与审查记录中的审查者身份比对；审查者与实现者重复时报告违规
- **FR-004**：缺失审查记录或独立性违规时，验证脚本必须返回非零退出码
- **FR-005**：CI governance workflow 必须调用审查记录验证脚本，并将输出纳入治理报告

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 验证脚本扫描单个 Spec 目录的耗时 <2 秒
- **NFR-002**：安全 — 不涉及密钥或信任边界变更
- **NFR-003**：兼容性 — 验证脚本必须兼容 Windows PowerShell 5.1
- **NFR-004**：跨平台 — 验证逻辑不依赖平台特定 API；路径使用正斜杠或 `Join-Path`
- **NFR-005**：可维护性 — 验证规则必须可配置（如审查记录命名模式、实现者字段名称）

### 关键实体

- **审查记录**：`reviews/TXXX-spec-review.md` 和 `reviews/TXXX-quality-review.md`，包含审查者身份、审查日期、审查结论和发现项
- **任务条目**：`tasks.md` 中的一行，包含任务 ID、状态（`Verified` / `In Progress` 等）、实现者、Spec 审查者、Quality 审查者
- **活跃 Spec 目录**：`specs/<编号-功能名>/`，包含 `tasks.md` 和 `reviews/` 子目录

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：创建一个 `Verified` 任务但缺少 Quality 审查记录的 Spec 后运行验证脚本，脚本必须返回非零退出码
- **SC-002**：审查者与实现者为同一人时，验证脚本检测到违规并返回非零退出码
- **SC-003**：所有 `Verified` 任务审查记录完整且独立时，验证脚本返回零退出码
- **SC-004**：CI governance workflow 运行时包含审查记录验证的输出

---

## 假设与依赖

- 假设 `tasks.md` 使用 P2A 中文 tasks 模板格式（包含"实现者""Spec 审查者""Quality 审查者"列）
- 假设审查记录文件命名遵循 `TXXX-spec-review.md` 和 `TXXX-quality-review.md` 约定
- 假设审查记录中包含可机器解析的审查者身份字段（如 YAML front matter 或特定格式的元数据行）
- 依赖 P5 治理自动化（`scripts/docs/validate-governance.ps1`）提供验证脚本框架
- 假设审查者身份使用一致的标识符（如 Agent 名称或用户名）

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 无 | 低 | 不适用 |
| 兼容性 | 验证脚本可能与旧格式 tasks.md 不兼容 | 低 | 仅扫描活跃 Spec 目录（统一使用 P2A 模板格式） |
| 跨平台 | PowerShell 脚本在 Linux CI runner 上可能不可用 | 中 | 提供 PowerShell Core (pwsh) 兼容版本或 fallback 到跨平台脚本 |
| 持久化 | 无 | 低 | 不适用 |
| 签名/发布 | 无 | 低 | 不适用 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Not Applicable | 不涉及信任边界 |
| 原则 II：测试必须隔离宿主状态 | Not Applicable | 不涉及测试隔离 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 直接响应 Constitution 识别的执行缺口 |
| 原则 IV：逐任务双重独立审查 | Needs Review | 本 Spec 直接执行该原则——自动化验证审查记录的完整性和独立性 |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | 验证脚本需在 Windows 主平台优先运行 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不影响已有审查流程，仅增加自动化验证层 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Not Applicable | 不涉及构建可复现性 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否（仅在已有 `reviews/` 目录下执行读取检查）
- 是否修改已有 `tasks.md` 格式？否（仅读取和解析）

### 跨平台影响

- 验证脚本主平台为 Windows PowerShell 5.1
- PowerShell Core (pwsh) 可在 Linux/macOS 上提供等效执行能力
- 路径处理使用 `Join-Path` 确保跨平台兼容

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？否
- 是否引入新的信任边界？否

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-006-dual-review-auto-verification-001` |
| Spec 批准 | Pending | — | — | — | 整体 Spec 的目标、范围、需求和验收标准 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 缺口第 300 行和原则 IV 创建 Spec 初稿 | — |
