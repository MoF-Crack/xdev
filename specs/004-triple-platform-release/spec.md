# Feature Specification: 三平台发布产物同源

> **文档类型**：Feature Spec
> **状态**：Accepted
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义三平台发布产物同源验证的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `004-triple-platform-release`
**Created**: 2026-07-16
**Status**: Accepted
**Input**: Constitution `1.0.0-rc.3` 原则 V，第 65-73 行

---

## Why

Constitution `1.0.0-rc.3` 原则 V 要求：

> "正式发布前必须通过 Windows、Linux、macOS CI。缺少任一平台证据时，只能声明已验证的平台，不能声明'跨平台完成'。"

当前 CI 工作流（`.github/workflows/ci.yaml`）包含三平台构建 job，但缺少统一的同源验证门禁：
- 三平台产物是否从同一 Git commit 构建？
- 三平台产物是否使用了相同的依赖版本（`go.sum`、`package-lock.json`）？
- 如果某个平台构建过程中 `main` 分支接收了新的提交，该平台的产物可能与其它平台不再同源。

若平台间不一致，"跨平台"声明可能不成立。例如，Windows 产物基于 commit A，Linux 产物基于 commit B，两个版本可能包含不同的功能和修复。

受影响方：
- 所有依赖"跨平台"声明的用户；
- 跨平台 bug 报告和调试（不同平台的差异可能源于不同代码基线而非平台差异）；
- 发布审计和合规性。

不关闭此缺口：发布时三平台产物可能来自不同代码版本，"跨平台完成"声明缺乏事实依据。

## 目标

建立三平台发布产物同源验证门禁：确保同一发布版本的 Windows、Linux、macOS 产物均从同一 Git commit 构建，使用相同的依赖锁定文件（`go.sum`、`package-lock.json`），不一致时阻止发布。

## 范围

### 包含

- CI release workflow 中三平台构建使用的 Git commit 记录与比对
- 依赖锁定文件（`go.sum`、`package-lock.json`）的内容一致性验证
- 任一平台构建失败时，整个发布流程的阻塞逻辑
- 同源验证失败时的错误报告（标识不一致的具体项目和差异）

### 非目标

- 三平台功能等价性测试（属于各功能 Spec 的跨平台测试要求）
- 交叉编译工具链的版本管理
- 修复平台特定构建差异或编译错误
- 为每个平台维护独立的依赖版本

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 发布时自动验证三平台产物同 Git commit (Priority: P1)

当 CI release workflow 执行三平台构建时，在产物收集阶段自动比对所有平台产物的 Git commit SHA。如果任一平台的 commit 与其他平台不一致，发布流程立即失败。

**Why this priority**: commit 一致性是同源验证的核心——commit 不同意味着代码基线不同，导致"跨平台"声明不成立。

**Independent Test**: 在三平台构建期间向 `main` 分支推送新提交，使某平台构建了不同的 commit，验证发布被阻止。

**Acceptance Scenarios**:

1. **Given** CI release workflow 触发三平台并行构建，**When** 所有平台在产物收集阶段记录各自构建的 Git commit SHA，**Then** 比对步骤检查所有 commit 一致
2. **Given** 三平台 commit 均为 `abc123`，**When** 同源验证执行，**Then** 验证通过，发布继续
3. **Given** Windows 产物 commit 为 `abc123`，Linux 产物 commit 为 `def456`，**When** 同源验证执行，**Then** 验证失败，CI 输出不一致的 commit 对，发布停止

---

### User Story 2 - 任缺平台证据时发布阻塞 (Priority: P2)

如果三平台中任一平台的构建未完成（因构建失败或 job 超时），CI 不得仅以已完成的平台产物声明"跨平台完成"，而必须阻塞整个发布流程。

**Why this priority**: 原则 V 明确要求"缺少任一平台证据时，只能声明已验证的平台"——这是跨平台声明的必要条件。

**Independent Test**: 人为使 macOS 构建 job 失败，验证三平台同源验证步骤检测到缺失并阻塞发布。

**Acceptance Scenarios**:

1. **Given** Windows 和 Linux 构建成功，macOS 构建失败，**When** 三平台同源验证步骤执行，**Then** 验证失败，输出 "macOS platform build not available"，发布停止
2. **Given** 所有三个平台产物均存在，**When** 同源验证执行，**Then** 检查通过

---

### User Story 3 - 依赖版本不一致时发布失败 (Priority: P3)

当三平台构建过程中使用了不同的依赖锁定文件内容（如 `go.sum` 在某平台被意外修改），同源验证应检测到差异并阻止发布。

**Why this priority**: 依赖版本一致性是广义的可复现构建要求，但优先级低于 commit 一致性和平台完整性。

**Independent Test**: 在一个平台的构建 job 中修改 `go.sum` 内容，验证发布被阻止。

**Acceptance Scenarios**:

1. **Given** 三平台构建完成，`go.sum` 的 SHA-256 hash 在 Windows 为 `X`，在 Linux 为 `Y`（不同），**When** 同源验证比对依赖文件 hash，**Then** 验证失败并输出不一致的文件名和 hash 差异
2. **Given** 三平台的 `go.sum` 和 `package-lock.json` hash 一致，**When** 同源验证比对依赖文件 hash，**Then** 验证通过

---

### 边界条件

- 当发布基于 tag 而非 branch 时，同源验证的 commit 比对逻辑是否一致？
- 当三平台构建使用不同的 CI runner 且 runner 之间存在时钟偏差时，时间戳差异应如何排除？
- 当某些平台产物由于技术原因无法嵌入 commit 信息时，应如何降级处理？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：CI release workflow 必须在每个平台的构建 job 中记录构建使用的 Git commit SHA
- **FR-002**：CI 必须在产物收集阶段（或专门的同源验证 job）比对三平台的 commit SHA 是否一致；不一致时发布流程阻塞（非零退出码）
- **FR-003**：CI 必须检查三平台构建 job 是否全部成功完成；任一平台构建失败或缺失时，发布流程必须阻塞
- **FR-004**：CI 必须验证三平台构建使用的依赖锁定文件（`go.sum`、`package-lock.json`）内容一致（通过 hash 比对）
- **FR-005**：同源验证失败时，CI 输出必须明确标识不一致的项目类型（commit vs 依赖文件）和差异详情

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 同源验证步骤必须在所有平台构建完成后执行，额外的验证耗时 <10 秒
- **NFR-002**：安全 — 不涉及密钥或信任边界变更
- **NFR-003**：兼容性 — 不得修改已有平台构建 job 的内部逻辑，仅在外部增加验证步骤
- **NFR-004**：跨平台 — 同源验证逻辑必须在所有平台的 CI runner 上一致执行（或作为独立的后处理 job）
- **NFR-005**：可维护性 — commit 和依赖文件的记录和传递必须使用 GitHub Actions 的标准机制（如 artifacts、outputs）

### 关键实体

- **Git commit SHA**：构建时代码仓库的 HEAD 提交哈希，唯一标识构建的代码基线
- **依赖锁定文件**：`go.sum`（Go 模块依赖版本锁定）和 `package-lock.json`（前端依赖版本锁定），确保所有平台使用相同依赖版本
- **平台产物**：每个平台的 CI release job 输出的编译后二进制文件

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：在三平台构建期间推送新提交导致 commit 不一致时，CI 同源验证步骤失败并阻止发布
- **SC-002**：任缺一个平台产物时，发布流程被阻塞
- **SC-003**：三平台 commit 一致且依赖文件一致时，同源验证通过，零假阳性
- **SC-004**：同源验证失败日志中可追溯至具体不一致的平台和项目

---

## 假设与依赖

- 假设 CI release workflow 使用 GitHub Actions 的 matrix strategy 或独立 job 实现三平台并行构建
- 假设 Git commit SHA 在每个构建 job 中可通过 `git rev-parse HEAD` 获取
- 假设 `go.sum` 和 `package-lock.json` 已通过版本控制管理
- 依赖 GitHub Actions 的 artifacts 机制在 job 间传递构建元数据
- 假设 release workflow 基于 `main` 分支或 release tag 触发

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 无 | 低 | 不适用 |
| 兼容性 | 同源验证可能阻塞合法的紧急 hotfix 单平台发布 | 中 | 紧急发布需通过安全例外流程临时豁免 |
| 跨平台 | CI runner 操作系统差异可能导致依赖文件行尾符不同而误判不一致 | 中 | 比对前对依赖文件做行尾符标准化 |
| 持久化 | 无 | 低 | 不适用 |
| 签名/发布 | 同源验证失败阻止发布可能延误发布计划 | 低 | 失败信息清晰，便于快速定位和修复 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Not Applicable | 不涉及信任边界变更 |
| 原则 II：测试必须隔离宿主状态 | Not Applicable | 不涉及测试隔离 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 直接响应 Constitution 原则 V 的要求 |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需通过双重审查门禁 |
| 原则 V：Windows 主平台与跨平台分层验证 | Needs Review | 本 Spec 直接执行该原则——确保三平台发布产物同源 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不改变已有构建逻辑，仅增加验证步骤 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Compliant | 依赖版本一致性是可复现构建的核心前提 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否
- 是否影响已存在的 CI workflow？是（在 release workflow 中新增同源验证步骤）

### 跨平台影响

- 同源验证逻辑需要在所有平台上有相同的执行效果
- 依赖文件比对前需做行尾符标准化（Windows CRLF vs Unix LF）
- Git commit SHA 在所有平台上一致（Git 的内容寻址特性）

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？否
- 是否引入新的信任边界？否

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-004-triple-platform-release-001` |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-25 | `0.2.0` | `2026-07-25-spec-004-001` 整体 Spec 的目标、范围、需求和验收标准；实现已在 ldflags 中注入 GitCommit 和 BuildDate |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 原则 V 第 65-73 行创建 Spec 初稿 | — |
