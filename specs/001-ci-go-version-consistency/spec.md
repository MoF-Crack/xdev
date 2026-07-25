# Feature Specification: CI Go 工具链版本一致性

> **文档类型**：Feature Spec
> **状态**：Accepted
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义 CI Go 工具链版本一致性检查的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `001-ci-go-version-consistency`
**Created**: 2026-07-16
**Status**: Accepted
**Input**: Constitution `1.0.0-rc.3` 原则 VII，第 92 行：CI Go 工具链版本必须与 `go.mod` 的 `go` 指令一致

---

## Why

Constitution `1.0.0-rc.3` 原则 VII 明确规定：

> "CI 的 Go 工具链版本必须与 `go.mod` 的 `go` 指令一致；不一致属于可复现性缺陷。"

当前 CI 工作流（`.github/workflows/ci.yaml`）硬编码 `go-version: "1.23"`，与 `go.mod` 的 `go 1.23` 于 rc.3 修订时手动同步一致。但缺少自动化验证机制确保未来 patch 或 minor 更新时两侧保持同步。

风险：
- CI 版本滞后于 `go.mod` → CI 构建行为与 `go.mod` 声明不一致，可能隐藏语言特性兼容问题；
- CI 版本超前于 `go.mod` → CI 可能使用 `go.mod` 未声明的语言特性，本地开发者无法复现；
- 人工同步容易遗漏，属于 Constitution 明确禁止的可复现性缺陷。

受影响方：
- 所有提交代码并通过 CI 验证的开发者；
- 依赖 CI 构建产物进行测试和发布的流程；
- 发布审计和可复现性要求。

不关闭此缺口：在 `go.mod` 语言版本升级后，CI 行为与本地开发可能长期不一致，违反 Constitution 原则 VII。

## 目标

建立自动化机制，确保 CI 的 Go 工具链版本始终与 `go.mod` 的 `go` 指令一致。当两者不一致时，CI 必须明确失败并输出差异信息，阻止构建继续。

## 范围

### 包含

- CI workflow 中 Go 版本的自动解析与比对逻辑
- `go.mod` 中 `go` 指令值的提取
- 版本不一致时的 CI 失败输出（含期望值 vs 实际值）
- 本地开发环境可用的版本一致性检查命令或脚本

### 非目标

- Go 版本升级本身（版本选择属于独立决策）
- 修改 `go.sum` 或 vendor 策略
- 其他语言工具链（Node.js、Python 等）的版本一致性
- 替换 `setup-go` action 或改变 Go 安装方式
- 将 CI Go 版本设定为浮动（如 `1.x`）而非固定

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - CI 自动检测 Go 版本漂移 (Priority: P1)

当 `go.mod` 的 `go` 指令被升级（如 `go 1.23` → `go 1.24`），但 CI workflow 中 `go-version` 字段未被同步更新时，CI 应在构建早期阶段检测到不一致并明确失败。

**Why this priority**: CI 版本漂移是 Constitution 明确禁止的可复现性缺陷，属于阻塞级问题，必须最先处理。

**Independent Test**: 修改 `go.mod` 的 `go` 指令但不修改 CI workflow，推送后 CI 必须失败并输出期望版本 vs 实际版本。

**Acceptance Scenarios**:

1. **Given** `go.mod` 中 `go 1.24`，CI workflow 中 `go-version: "1.23"`，**When** CI 运行 Go 版本一致性检查步骤，**Then** CI 该步骤失败，输出 `go.mod requires go 1.24 but CI uses 1.23`
2. **Given** `go.mod` 中 `go 1.23`，CI workflow 中 `go-version: "1.23"`，**When** CI 运行 Go 版本一致性检查步骤，**Then** CI 该步骤通过，构建继续
3. **Given** `go.mod` 中 `go` 指令使用 patch 版本（如 `go 1.23.0`），CI workflow 中 `go-version: "1.23"`，**When** CI 运行版本一致性检查，**Then** 检查按 minor 版本比对通过（允许 CI patch 版本高于声明）

---

### User Story 2 - 本地开发验证 CI 版本一致性 (Priority: P2)

开发者在本地修改 `go.mod` 的 `go` 指令后，可以在提交前运行本地命令验证 CI 版本与 `go.mod` 是否一致，避免推送后 CI 失败。

**Why this priority**: 本地验证可减少 CI 资源浪费和开发者等待时间，但不如 CI 门禁本身关键。

**Independent Test**: 运行本地验证命令，在版本一致时返回零退出码，不一致时返回非零并输出差异。

**Acceptance Scenarios**:

1. **Given** 本地开发环境已签出代码，**When** 开发者运行本地版本一致性检查命令，**Then** 命令输出当前 `go.mod` 期望版本和 CI 声明版本
2. **Given** 版本一致，**When** 运行本地检查命令，**Then** 返回零退出码

---

### User Story 3 - CI 提供清晰的修复指引 (Priority: P3)

当版本不一致导致 CI 失败时，CI 日志应包含明确的修复指引，告知开发者如何同步版本。

**Why this priority**: 提升开发者体验，但不影响缺陷检测本身。

**Independent Test**: 触发版本不一致的 CI 失败，读取日志确认包含具体操作指引。

**Acceptance Scenarios**:

1. **Given** CI 版本检查失败，**When** 开发者查看 CI 日志，**Then** 日志中包含与 `go.mod` 版本同步的推荐操作（如 "update go-version in .github/workflows/ci.yaml to match go.mod"）
2. **Given** CI 版本检查失败，**When** 检查失败输出格式，**Then** 错误信息包含期望版本、实际版本和差异文件路径

---

### 边界条件

- 当 `go.mod` 文件缺失或格式损坏时，CI 版本检查应明确失败并报告文件解析错误？
- 当 CI workflow 中存在多个 job 使用不同 Go 版本时，检查应覆盖所有 job 还是仅检查主构建 job？
- 当 `go.mod` 使用 `toolchain` 指令时，CI 应以 `toolchain` 还是 `go` 指令为权威版本？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：CI 必须从 `go.mod` 提取 `go` 指令值作为权威 Go 语言版本（不依赖 `toolchain` 指令）
- **FR-002**：CI 必须将所有使用 Go 的 job 中声明的 `go-version` 与权威版本比对
- **FR-003**：不一致时 CI 必须明确失败（非零退出码）并输出：期望版本、实际版本、不一致的 job 名称和 CI 配置文件路径
- **FR-004**：本地必须提供等效的命令或脚本，使开发者可在推送前验证版本一致性
- **FR-005**：版本比对规则为 minor 级别匹配（如 `go 1.23` 匹配 `1.23.0`、`1.23.4`），不要求 patch 版本精确相等

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 版本一致性检查必须在 CI 构建步骤开始前完成，耗时 <5 秒
- **NFR-002**：安全 — 不涉及信任边界或密钥变更
- **NFR-003**：兼容性 — 必须支持现有的 `go.mod` 格式（Go 1.21+），不修改 `go.mod` 内容
- **NFR-004**：跨平台 — 版本检查逻辑必须在 Windows、Linux、macOS 的 CI runner 上一致运行
- **NFR-005**：可维护性 — 检查逻辑不得硬编码版本号，必须从 `go.mod` 动态读取

### 关键实体

- **版本声明**：`go.mod` 中的 `go <major>.<minor>` 指令，表示模块开发所需的最低 Go 语言版本
- **CI 版本配置**：CI workflow 中 `go-version` 字段，表示 CI 使用的 Go 工具链版本

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：修改 `go.mod` 的 `go` 指令而不更新 CI workflow 后，推送触发的 CI 运行必须失败
- **SC-002**：`go.mod` 与 CI workflow 版本一致时，版本检查步骤零误报通过
- **SC-003**：本地版本检查命令可在 <5 秒内完成并输出结果
- **SC-004**：CI 版本检查失败日志中包含修复指引的比例达到 100%

---

## 假设与依赖

- 假设 CI 环境已安装 `git` 和 `go` 工具链
- 假设 `go.mod` 位于仓库根目录且格式符合 Go 规范
- 假设 CI workflow 文件路径为 `.github/workflows/ci.yaml`
- 依赖 `go mod` 子命令或文本解析正确提取 `go` 指令值
- 依赖 GitHub Actions 的 `setup-go` action 或等效机制声明 Go 版本

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 无 | 低 | 不适用 |
| 兼容性 | 新增 CI 步骤可能增加构建时间 | 低 | 检查逻辑极轻量，<5 秒 |
| 跨平台 | CI runner OS 差异可能影响 go.mod 解析 | 低 | 使用 `go mod edit -json` 或跨平台文本解析 |
| 持久化 | 无 | 低 | 不适用 |
| 签名/发布 | 无 | 低 | 不适用 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Not Applicable | 不涉及信任边界或密钥变更 |
| 原则 II：测试必须隔离宿主状态 | Not Applicable | 不涉及测试隔离 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 直接响应 Constitution 原则 VII 的要求 |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需通过双重审查门禁 |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | 版本检查逻辑需在 Windows、Linux、macOS CI 一致运行 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不改变已有 CI 行为，仅增加检查步骤 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Needs Review | 本 Spec 直接执行该原则：CI Go 版本一致性是可复现构建的前提 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否
- 是否影响已存在的 CI workflow？是（新增检查步骤，但不改变已有 job 的行为）

### 跨平台影响

- 版本检查脚本需兼容 Windows PowerShell 5.1 和 Linux/macOS bash
- `go.mod` 解析在不同操作系统上行为一致（Go 工具链跨平台）
- GitHub Actions runner 在所有平台上使用相同的 `setup-go` action

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？否
- 是否引入新的信任边界？否

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-25 | `0.2.0` | `2026-07-25-spec-001-001` CI Go 版本一致性检查脚本和 CI step 已实现 |
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-001-ci-go-version-consistency-001` |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 原则 VII 第 92 行创建 Spec 初稿 | — |
