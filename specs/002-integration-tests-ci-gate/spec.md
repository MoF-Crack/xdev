# Feature Specification: integration tests CI 门禁

> **文档类型**：Feature Spec
> **状态**：Accepted
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义 integration tests CI 门禁的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `002-integration-tests-ci-gate`
**Created**: 2026-07-16
**Status**: Accepted
**Input**: Constitution `1.0.0-rc.3` 当前已知执行缺口第 297 行

---

## Why

Constitution `1.0.0-rc.3` "当前已知执行缺口" 第 297 行明确：

> "默认 CI 未执行带 `integration` build tag 的集成测试。"

当前 CI 执行 `go test ./...`，该命令默认跳过带 `integration` build tag 的测试文件。这些集成测试涉及跨模块集成、文件系统操作、下载流程验证和外部工具交互——正是 Constitution 原则 II（测试隔离）、原则 V（跨平台）和原则 VII（可复现构建）关注的核心路径。

长期跳过集成测试的风险：
- 集成路径在无人察觉的情况下静默损坏；
- 单元测试通过但模块间交互失败在本地开发中难以发现；
- 跨模块重构缺少安全网；
- 发布前缺少集成回归证据。

受影响方：
- 所有修改跨模块代码（`internal/downloader/`、`internal/integration/`、`internal/signer/`）的开发者；
- 依赖 CI 提供集成质量信号的代码审查者；
- 发布流程缺少集成验证基线。

不关闭此缺口：Constitution 的测试隔离、跨平台和可复现性要求缺少 CI 层面的强制执行，集成路径的回归风险持续累积。

## 目标

在 CI 中建立 integration tests 门禁：确保每次 push/PR 执行带 `integration` build tag 的测试套件，任一测试失败时阻塞 CI，使集成路径的回归在合并前被检测。

## 范围

### 包含

- CI workflow 中新增专用 job 或 step 执行 `go test -tags=integration ./...`
- 集成测试失败时的 CI 失败信号和失败报告
- 集成测试执行超时的上限设置
- 集成测试 job 的资源限制（避免影响 CI 整体性能）

### 非目标

- 修改现有集成测试代码或增加新的集成测试用例
- 将集成测试作为 release gate（发布门禁由 Spec 003、004 分别覆盖）
- 修改 `go test` 的默认行为或其他 build tag
- 将集成测试结果作为 Spec 合规性证据（由 P5 治理自动化覆盖）
- 处理需要外部服务或网络的集成测试环境搭建

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Push 时 CI 自动运行 integration tests (Priority: P1)

开发者推送代码后，CI 自动执行带 `integration` build tag 的测试套件。如果所有集成测试通过，CI 标记为成功；如果有任何集成测试失败，CI 标记为失败。

**Why this priority**: 这是缺口的核心——将集成测试纳入 CI 执行范围，是后续所有场景的前提。

**Independent Test**: 推送包含集成测试通过和失败的代码变更，观察 CI 是否正确反映结果。

**Acceptance Scenarios**:

1. **Given** 代码推送触发 CI 运行，**When** CI 执行 integration tests job，**Then** 所有带 `integration` tag 的测试被执行
2. **Given** 所有集成测试通过，**When** CI integration tests job 完成，**Then** job 状态为 success
3. **Given** 至少一个集成测试失败，**When** CI integration tests job 完成，**Then** job 状态为 failure

---

### User Story 2 - integration test 失败时阻塞 PR 合并 (Priority: P2)

当集成测试失败时，PR 无法合并（受 GitHub branch protection 规则或 CI 状态检查约束）。开发者必须修复集成测试或在代码审查中提供合理解释后，CI 重新通过才能合并。

**Why this priority**: CI 运行测试但允许失败代码合并则失去门禁意义；但需 branch protection 配合，属于配置层面。

**Independent Test**: 提交一个故意失败的集成测试，检查 PR 页面的 merge 按钮是否被 CI 失败状态禁用。

**Acceptance Scenarios**:

1. **Given** PR 触发 CI 且 integration tests 失败，**When** 审查者尝试合并 PR，**Then** 合并被 CI 失败状态阻止
2. **Given** 开发者修复集成测试后重新推送，**When** CI 重新运行且全部通过，**Then** PR 合并阻止解除

---

### User Story 3 - 本地可复现 CI integration test 结果 (Priority: P3)

开发者可以在本地环境使用与 CI 相同的命令和参数运行集成测试，获得一致的结果，便于调试失败的集成测试。

**Why this priority**: 可复现性提升开发效率，但不影响 CI 门禁本身的有效性。

**Independent Test**: 在本地运行 `go test -tags=integration ./...`，对比本地结果与 CI 结果的一致性。

**Acceptance Scenarios**:

1. **Given** CI 中 integration test 失败，**When** 开发者在本地运行相同命令 `go test -tags=integration ./...`，**Then** 本地也失败且输出与 CI 一致
2. **Given** CI 中 integration test 通过，**When** 开发者在本地运行相同命令，**Then** 本地也通过

---

### 边界条件

- 当集成测试执行时间超过预设超时上限（如 30 分钟）时，CI 应如何标记（timeout failure vs 允许继续）？
- 当某些集成测试依赖外部网络资源而 CI runner 无网络访问时，这些测试应如何处理（跳过 vs 失败）？
- 当集成测试需要特定平台环境（如 Windows 专有路径）而 CI runner 在其他平台运行时，应如何标记（skip vs expected failure）？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：CI workflow 必须包含专用的 job 或 step，执行 `go test -tags=integration ./...` 命令
- **FR-002**：任一集成测试失败时，对应的 CI job 必须标记为 failure（非零退出码）
- **FR-003**：集成测试 job 必须设置执行超时（建议上限 30 分钟），超时后标记为 failure
- **FR-004**：集成测试 job 的失败输出必须包含失败测试的名称、文件和行号
- **FR-005**：本地开发者必须能够使用与 CI 相同的命令复现集成测试结果

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 集成测试 job 的执行时间不应影响 CI 整体反馈速度；建议集成测试 job 与快速检查 job 并行运行
- **NFR-002**：安全 — 集成测试不得在 CI 环境中访问或修改真实凭据或生产数据
- **NFR-003**：兼容性 — 集成测试 job 不得修改已有 CI job 的触发条件或输出
- **NFR-004**：跨平台 — 集成测试 job 必须覆盖 Windows、Linux、macOS 三平台（可并行执行）
- **NFR-005**：可维护性 — 集成测试 job 的定义必须与已有 CI workflow 结构一致，遵循相同的命名和触发约定

### 关键实体

- **build tag**：Go 源码中的 `//go:build integration` 编译指令，标记需要集成环境的测试文件
- **CI job**：CI workflow 中的独立执行单元，具有独立的运行环境、触发条件和超时设置

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：CI 运行日志中可确认 `-tags=integration` 参数被使用，且执行了带 `integration` tag 的测试包
- **SC-002**：PR 中引入集成测试失败时，CI 状态为 failure 且 PR 合并被阻塞
- **SC-003**：集成测试全部通过时，CI 总耗时增加不超过集成测试独立执行时间
- **SC-004**：集成测试失败日志可追溯至具体测试函数和断言行

---

## 假设与依赖

- 假设仓库中存在带 `//go:build integration` tag 的测试文件（`internal/integration/` 等）
- 假设 CI runner 具备运行集成测试所需的系统资源（磁盘、内存）
- 假设 CI runner 的操作系统支持集成测试中涉及的文件系统操作
- 依赖 GitHub Actions 的 `ubuntu-latest`、`windows-latest`、`macos-latest` runner 可用
- 假设集成测试不依赖外部网络服务（如需网络，需额外环境配置）

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 集成测试可能访问真实文件系统 | 中 | 约束集成测试使用临时目录；Constitution 原则 II 要求隔离 |
| 兼容性 | 新增 CI job 可能增加 CI 队列等待时间 | 低 | 与快速检查 job 并行执行 |
| 跨平台 | 平台特定测试可能在非目标平台失败 | 中 | 每个平台 runner 只执行其适用的集成测试 |
| 持久化 | 集成测试可能遗留临时文件 | 低 | CI runner 为临时环境，每次运行后自动清理 |
| 签名/发布 | 无 | 低 | 不适用 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Not Applicable | 不涉及信任边界或密钥变更 |
| 原则 II：测试必须隔离宿主状态 | Compliant | 集成测试必须遵循隔离原则；CI 门禁强制执行 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 直接响应 Constitution 识别为未完成的执行缺口 |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需通过双重审查门禁 |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | 集成测试 job 覆盖三平台 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不改变已有 CI job 行为，仅新增 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Compliant | 集成测试门禁增强构建可复现性的验证 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否
- 是否影响已存在的 CI workflow？是（新增 job，不修改已有 job）

### 跨平台影响

- 集成测试 job 需要在 Windows、Linux、macOS 三平台提供一致的运行环境
- 平台特定的集成测试（如 Windows Junction、Linux Symlink）需在对应平台执行
- 跨平台的 `go test -tags=integration` 命令语法一致

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？否
- 是否引入新的信任边界？否

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-002-integration-tests-ci-gate-001` |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-25 | `0.2.0` | `2026-07-25-spec-002-001` CI integration-tests job 已实现，含三平台 matrix + 30min timeout |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 缺口第 297 行创建 Spec 初稿 | — |
