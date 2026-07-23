# Feature Specification: 测试宿主状态快照

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-23
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义测试宿主状态快照检测机制的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `005-test-host-state-snapshot`
**Created**: 2026-07-16
**Status**: Approved
**Version**: `0.2.0`
**Input**: Constitution `1.0.0` 原则 II；P6 已知缺口 `005-test-host-state-snapshot`

---

## Why

Constitution `1.0.0` 原则 II 规定：

> "自动化测试不得修改真实用户注册表、用户或系统环境变量、Shell 配置、全局工具目录或其他持久化宿主状态。"
>
> "测试结束后的回滚不能替代隔离，因为中断、崩溃和超时可能导致永久污染。"
>
> "一旦检测到宿主状态变化，测试必须失败，并将其视为阻塞性缺陷。"

当前已知执行缺口指出：

> 测试隔离依赖 `--no-path-fix` 等参数，尚缺少统一的宿主状态快照检测。

当前事实：

- `internal/cli` 通过 `--no-path-fix` 设置 `Installer.SetNoEnvWrite(true)`，可跳过 PATH、用户环境变量和 `WM_SETTINGCHANGE`。
- 集成测试默认注入 `--no-path-fix`，但缺少统一、可证明的“测试前后宿主状态未变”检测。
- 历史缺陷记录显示：`use`/`install` 曾污染宿主 `GOROOT` 等用户级环境变量；回滚不能替代隔离。

受影响方：

- 本地开发者（宿主环境被污染后工具链失效）；
- CI runner（残留状态影响后续 job）；
- 测试可信度（通过不等于无副作用）。

不关闭此缺口：Constitution 原则 II 缺少可验证的执行手段。

## 目标

建立统一的测试宿主状态快照机制：在测试运行前捕获关键宿主状态基线，测试后比对差异；检测到任何持久化修改时测试失败，并将宿主状态修改视为阻塞性缺陷。

## 范围

### 包含

- 宿主状态快照采集范围：
  - Windows：用户环境变量（`HKCU\Environment`）、用户软件注册表白名单键、用户级 Shell 配置、全局工具目录白名单；
  - Linux/macOS：进程可观察的用户环境变量、用户级 Shell 配置、全局工具目录白名单；
  - 明确排除：系统级 `HKLM`、系统 `PATH` 以外的不可写系统区、密钥/令牌路径。
- 测试前后快照采集与差异比对；
- 差异失败信号与结构化诊断；
- 默认测试套件与 CI 测试 job 中的强制启用；
- 跨平台适配（Windows 主平台，Linux/macOS 分层验证）。

### 非目标

- 替换现有第一道防线（`--no-path-fix` / `noEnvWrite` 保留）；
- 侵入式修改第三方测试框架；
- 自动修复宿主状态修改；
- 对非测试场景强制快照检测；
- 快照性能优化优先于正确性；
- 在本 Spec 中实现产品侧 `--no-env-write` 语义拆分（可在实现计划中作为配套改进，但不改变本 Spec 验收边界）。

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 自动化测试运行前后宿主状态无差异 (Priority: P1)

开发者或 CI 运行测试套件时，快照机制在测试前后分别采集宿主状态基线。若测试后状态与测试前一致（在定义检测范围内），快照检测通过。

**Why this priority**: 核心价值是证明隔离有效，且不产生假阳性。

**Independent Test**: 运行已知完全隔离的测试套件，验证快照检测通过。

**Acceptance Scenarios**:

1. **Given** 测试套件使用临时目录与 `--no-path-fix` 且不写宿主持久状态，**When** 快照检测在测试后比对，**Then** 无差异，检测通过
2. **Given** 快照检测通过，**When** 查看测试输出，**Then** 输出包含 `host state snapshot: no changes detected` 或等价确认信息

---

### User Story 2 - 发现宿主状态变更时测试失败并输出差异 (Priority: P1)

当测试错误修改宿主状态（写注册表、改用户环境变量、在全局目录创建文件），快照检测失败并输出差异详情。

**Why this priority**: 违规反馈与通过场景同等重要。

**Independent Test**: 创建故意修改用户环境变量/注册表的探针测试，验证检测失败。

**Acceptance Scenarios**:

1. **Given** 测试写入 `HKCU\Environment` 中的白名单变量（如 `GOROOT`），**When** 快照比对，**Then** 检测失败
2. **Given** 快照检测失败，**When** 查看失败输出，**Then** 输出包含：(a) 变更类型；(b) 路径或键名；(c) 变更前后值
3. **Given** 测试在用户全局工具目录白名单路径创建文件，**When** 快照比对，**Then** 检测失败并报告文件系统差异

---

### User Story 3 - CI 中强制执行宿主状态隔离检查 (Priority: P2)

CI 测试 job 启用宿主状态快照检测；检测失败时 job 失败。

**Why this priority**: 持续防止宿主泄漏进入主干。

**Independent Test**: 在 CI 或本地模拟 CI 入口运行含泄漏探针的套件，验证非零退出。

**Acceptance Scenarios**:

1. **Given** CI 测试 job 启用快照检测，**When** 检测到宿主状态修改，**Then** job failure
2. **Given** 干净隔离测试，**When** 快照检测运行，**Then** job 通过

---

### 边界条件（已澄清）

| 边界 | 决定 |
|---|---|
| 容器/受限沙箱 CI runner | **仍运行**快照检测。隔离环境不能豁免；检测证明“本次测试未改当前宿主可见状态”。 |
| 快照自身写入 | 快照文件只允许写到测试临时目录（`t.TempDir()` / 进程专属 temp），并在比对前将该路径加入排除列表，避免自干扰。 |
| 合法临时文件 | 默认忽略系统临时目录（Windows `%TEMP%` / Unix `$TMPDIR`）中由测试创建且测试后清理的文件；只跟踪配置的持久路径白名单。 |
| 并行测试套件 | 首版以**进程级套件边界**（`TestMain` / 独立测试进程）采集快照；不保证同一进程内 `t.Parallel()` 子测试细粒度隔离。并行套件必须使用独立进程或串行快照边界。 |
| 系统级注册表 `HKLM` | **不采集**（需要提升权限且非测试应写区域）。若未来需要，单独修订 Spec。 |
| 进程环境 vs 用户环境 | 必须检测**用户持久环境**（Windows 注册表用户环境）；进程内 `os.Setenv` 的临时修改若不落盘可不报，除非落盘到用户环境。 |

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：测试框架必须支持在测试套件执行前采集宿主状态快照，覆盖范围至少包括：Windows 用户环境变量与用户软件注册表白名单、跨平台用户环境变量、指定文件系统路径和全局工具目录白名单
- **FR-002**：测试框架必须在测试套件执行后重新采集宿主状态快照，并与测试前基线进行差异比对
- **FR-003**：检测到宿主状态差异时，测试必须返回失败并输出：(a) 差异类别；(b) 变更路径或键名；(c) 变更前后的值
- **FR-004**：CI 中的默认测试 job 必须启用宿主状态快照检测，检测失败时 CI job 标记为 failure
- **FR-005**：快照检测机制必须支持跨平台运行（Windows 注册表用户区 + 文件系统 / Linux + macOS 文件系统 + 环境变量）
- **FR-006**：快照检测默认启用；允许通过显式环境变量 `XDEV_HOST_SNAPSHOT=0` 关闭，但默认测试与 CI 不得设置该关闭项
- **FR-007**：采集范围必须可配置（允许添加或排除路径/键），默认配置必须覆盖 xdev 已知污染面（`GOROOT`/`GOPATH`/`JAVA_HOME`/`NODE_HOME` 等用户环境变量，以及用户级 xdev 相关路径）

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 快照采集和比对的额外耗时不应超过被测套件墙钟时间的 5%
- **NFR-002**：安全 — 快照不得持久保存密钥/令牌内容；默认排除 `keys/`、`.ssh`、凭据文件路径
- **NFR-003**：兼容性 — 不得要求改写既有业务测试体；通过 `TestMain` 或测试辅助包接入
- **NFR-004**：跨平台 — Windows 主平台完整实现；Linux/macOS 提供编译与核心路径测试证据
- **NFR-005**：可维护性 — 默认白名单与排除列表集中配置，便于扩展

### 关键实体

- **宿主状态快照**：时间点上的宿主状态集合（环境变量、注册表用户区键值、路径存在性/内容 hash）
- **差异报告**：前后快照比对结果；每条含类型、路径/键、前后值

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：故意写入用户环境变量或用户注册表白名单键的探针测试必须失败并输出变更详情
- **SC-002**：完全隔离的现有集成测试套件在启用快照后零假阳性通过
- **SC-003**：CI 默认测试路径启用快照后，任何宿主状态泄漏导致 job 失败
- **SC-004**：快照采集和比对额外耗时 ≤ 被测套件墙钟时间的 5%

---

## 假设与依赖

- 测试使用 Go 标准 `testing` 包
- Windows 可程序化读取 `HKCU\Environment` 与用户软件键
- CI runner 每次 job 相对独立，但仍运行快照检测
- `--no-path-fix` / `noEnvWrite` 继续作为第一道隔离防线
- 依赖 `TestMain` 或共享 test helper 触发套件级快照

---

## 风险分类

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 安全 | 快照可能捕获敏感路径信息 | 中 | 排除密钥路径；快照仅存临时目录 |
| 兼容性 | 额外文件操作干扰测试 | 低 | 快照写入测试 temp 并排除自身路径 |
| 跨平台 | 注册表 API 非 Windows 不可用 | 中 | build tags / 运行时分支 |
| 假阳性 | 系统并发改动用户环境 | 中 | 缩小到 xdev 相关白名单键与路径 |
| 并行 | 同进程并行子测试竞争 | 中 | 首版套件/进程级边界，文档化限制 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（1.0.0） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Not Applicable | 不改变下载/签名信任边界 |
| 原则 II：测试必须隔离宿主状态 | Compliant | 本 Spec 直接提供原则 II 的可验证执行手段 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 直接响应原则 II 执行缺口 |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需双重审查 |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | Windows 完整，Linux/macOS 分层证据 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不改变既有 CLI 默认行为语义；仅增加检测层 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Compliant | 不引入不可复现依赖；快照产物不入库 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否（`--no-path-fix` 保持；可选后续拆分 `--no-env-write` 不在本 Spec 强制范围内）
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否
- 是否影响已存在的测试？仅增加套件级检测包装，不要求改写业务断言

### 跨平台影响

- Windows：用户环境变量、用户注册表白名单、用户级路径
- Linux/macOS：用户环境变量、用户级配置与工具路径
- 平台特定逻辑通过 build tags 或运行时 OS 检测隔离

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？有限（路径元数据）；排除密钥内容
- 是否引入新的信任边界？否

---

## 批准记录 (Approval History)

| Stage | Status | Approver | Date | Version | Scope |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-005-test-host-state-snapshot-001` |
| Spec 澄清 | Draft | — | 2026-07-23 | `0.2.0` | Constitution 1.0.0 对齐与边界澄清 |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-23 | `0.2.0` | `2026-07-23-spec-005-approved-001`；用户原文「统一spec005 approve」；批准目标、范围、需求与验收标准 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 缺口与原则 II 创建 Spec 初稿 | — |
| 0.2.0 | 2026-07-23 | Clarify + Approve | 关联 Constitution 升级为 `1.0.0`；澄清边界；补充 FR-006/FR-007；Draft→Approved | `xinyan <zuijiuhjd@foxmail.com>` |
