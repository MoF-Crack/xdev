# Feature Specification: 发布签名硬门禁

> **文档类型**：Feature Spec
> **状态**：Accepted
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义发布签名硬门禁的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `003-release-signing-hard-gate`
**Created**: 2026-07-16
**Status**: Accepted
**Input**: Constitution `1.0.0-rc.3` 当前已知执行缺口第 298 行；安全例外 `docs/security/exceptions/manifest-signature-verification.md`

---

## Why

Constitution `1.0.0-rc.3` "当前已知执行缺口" 第 298 行：

> "发布作业在签名密钥或公钥缺失时可能继续执行，尚未形成签名硬门禁。"

Constitution 原则 I 要求"正式发布必须启用 manifest 签名验证"，且"信任判断必须发生在执行和提取之前，不能在副作用产生后补做验证"。分层 Manifest 信任模型要求内置 manifest 使用 Ed25519 签名验证。

当前事实（已在 `rc.3` 中记录）：
- `internal/signer/verify.go` 中 `rootPublicKeyB64` 源码默认值为空；
- `.github/workflows/ci.yaml` 仅在 secret 存在时签名和注入公钥；
- 签名私钥或公钥缺失时，CI 不阻塞，可能产出未签名或签名验证被跳过的发布产物。

该缺口已登记为安全例外 `docs/security/exceptions/manifest-signature-verification.md`（状态 Draft），目标版本为首个无预发布标识的 SemVer 正式版本，截止日期 2026-09-01。

受影响方：
- 所有依赖 manifest 签名验证的下载和安装用户；
- 发布流程和发布审计；
- 安全合规性和 trust-on-first-use 机制的有效性。

不关闭此缺口：正式发布可能绕过签名验证，Constitution 的信任边界要求无法执行；安全例外到期后发布流程无合规路径。

## 目标

建立发布签名硬门禁：签名私钥（`XDEV_SIGNING_PRIVATE_KEY`）或根公钥（`XDEV_SIGNING_PUBLIC_KEY`）缺失时，CI release job 必须明确失败并阻止发布产物生成，不得静默降级为未签名发布。

## 范围

### 包含

- CI release job 中签名私钥存在性的前置检查
- CI release job 中根公钥注入验证
- 密钥缺失时的非零退出码失败
- 失败日志中明确标识缺失的 secret 变量名
- 对应的负向测试（确认缺失密钥时 CI 正确失败）

### 非目标

- 签名算法变更（保持 Ed25519）
- manifest 格式变更
- 对已发布历史产物的追溯签名
- 修改 `internal/signer/` 中的签名验证逻辑
- 生成或轮换签名密钥对
- 关闭 `docs/security/exceptions/manifest-signature-verification.md` 安全例外（由用户批准）

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 发布时签名私钥缺失导致 CI 失败 (Priority: P1)

当 CI release job 执行时，如果 `XDEV_SIGNING_PRIVATE_KEY` secret 不存在或为空，CI 必须在签名步骤之前立即失败，不得继续执行后续的构建和发布步骤。

**Why this priority**: 签名私钥是签名验证链的起点；私钥缺失时产出的版本无法被签名验证，属于最高风险的安全缺口。

**Independent Test**: 在测试 CI 环境中删除 `XDEV_SIGNING_PRIVATE_KEY` secret，触发 release workflow，验证 CI 失败且未生成发布产物。

**Acceptance Scenarios**:

1. **Given** CI release workflow 被触发，`XDEV_SIGNING_PRIVATE_KEY` secret 不存在，**When** CI 执行到签名前置检查步骤，**Then** CI 该步骤失败（非零退出码），停止后续所有构建步骤
2. **Given** CI release workflow 被触发，`XDEV_SIGNING_PRIVATE_KEY` secret 存在且非空，**When** CI 执行签名前置检查，**Then** 检查通过，继续执行
3. **Given** 签名私钥缺失导致 CI 失败，**When** 查看 CI 日志，**Then** 日志中明确输出缺失的 secret 名称 `XDEV_SIGNING_PRIVATE_KEY`

---

### User Story 2 - 发布时公钥缺失导致 CI 失败 (Priority: P2)

当 CI release job 构建发布二进制时，如果 `XDEV_SIGNING_PUBLIC_KEY` secret 不存在或为空，CI 必须在 ldflags 注入步骤之前立即失败，防止产出无法验证签名的二进制。

**Why this priority**: 公钥缺失时二进制在运行时跳过签名验证，相当于签名保护完全失效。优先级略低于私钥检查因为公钥缺失不影响签名生成本身，但影响运行时验证。

**Independent Test**: 在测试 CI 环境中删除 `XDEV_SIGNING_PUBLIC_KEY` secret 但保留私钥，触发 release workflow，验证 CI 失败。

**Acceptance Scenarios**:

1. **Given** CI release workflow 执行到构建步骤，`XDEV_SIGNING_PUBLIC_KEY` secret 不存在，**When** CI 执行公钥注入前置检查，**Then** CI 该步骤失败，不继续构建二进制
2. **Given** 公钥缺失导致 CI 失败，**When** 查看 CI 日志，**Then** 日志中明确输出缺失的 secret 名称 `XDEV_SIGNING_PUBLIC_KEY`
3. **Given** 私钥存在但公钥缺失，**When** CI 检查到公钥缺失，**Then** CI 不应尝试用私钥签名后继续（半成品不安全）

---

### User Story 3 - 签名验证链完整时发布成功 (Priority: P3)

当签名私钥和公钥均存在且有效时，CI release job 正常执行签名、公钥注入和发布流程，发布产物具备完整的签名验证链路。

**Why this priority**: 这是正常流程的验证，确保硬门禁不引入假阳性阻塞。

**Independent Test**: 在 CI 环境中正确配置所有签名 secret，触发 release workflow，验证发布成功。

**Acceptance Scenarios**:

1. **Given** `XDEV_SIGNING_PRIVATE_KEY` 和 `XDEV_SIGNING_PUBLIC_KEY` 均已正确配置，**When** CI release workflow 完整执行，**Then** 所有步骤通过，发布产物生成
2. **Given** 发布成功，**When** 使用正确公钥验证发布二进制的 manifest 签名，**Then** 签名验证通过

---

### 边界条件

- 当 secret 变量存在但值为空字符串时，CI 应将其视为缺失并失败（而非视为"已配置"）？
- 当私钥格式无效（非 Ed25519 64 字节种子）时，CI 应在签名前检测并失败，而非在签名时崩溃？
- 当发布流程需要在多个 job 中使用签名密钥时，前置检查应在每个使用密钥的 job 中独立执行？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：CI release job 必须在签名步骤之前检查 `XDEV_SIGNING_PRIVATE_KEY` secret 存在且非空；缺失或为空时立即失败（非零退出码）
- **FR-002**：CI release job 必须在公钥注入（ldflags）步骤之前检查 `XDEV_SIGNING_PUBLIC_KEY` secret 存在且非空；缺失或为空时立即失败
- **FR-003**：任一密钥检查失败时，CI 必须停止所有后续步骤，不得产出任何发布产物
- **FR-004**：密钥检查失败日志必须明确标识缺失的 secret 变量名（如 "Missing required secret: XDEV_SIGNING_PRIVATE_KEY"）
- **FR-005**：必须提供负向测试用例：在缺失密钥的 CI 环境中验证 CI 按预期失败

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 密钥存在性检查必须在 <1 秒内完成
- **NFR-002**：安全 — 密钥检查步骤不得将密钥内容输出到日志、错误消息或构建产物中
- **NFR-003**：兼容性 — 不得修改非 release workflow 的 CI 行为（push/PR 构建不受影响）
- **NFR-004**：跨平台 — 密钥检查逻辑必须在所有平台的 GitHub Actions runner 上一致执行
- **NFR-005**：可维护性 — 密钥检查逻辑必须集中定义，避免在多个 job 中重复分散

### 关键实体

- **签名私钥** (`XDEV_SIGNING_PRIVATE_KEY`)：Ed25519 64 字节种子，用于对 manifest 生成 Ed25519 签名
- **根公钥** (`XDEV_SIGNING_PUBLIC_KEY`)：Ed25519 公钥，通过 ldflags 注入发布二进制，运行时验证 manifest 签名
- **发布产物**：CI release job 输出的跨平台二进制文件及其伴随的签名文件

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：在测试 CI 环境中删除 `XDEV_SIGNING_PRIVATE_KEY` secret 后触发 release workflow，CI 必须返回非零退出码且无发布产物生成
- **SC-002**：在测试 CI 环境中删除 `XDEV_SIGNING_PUBLIC_KEY` secret 后触发 release workflow，CI 必须返回非零退出码
- **SC-003**：两个 secret 均正确配置时，release workflow 正常完成且发布产物通过签名验证
- **SC-004**：失败日志中包含具体缺失 secret 名称的比例达到 100%

---

## 假设与依赖

- 假设签名私钥通过 GitHub Actions secrets 机制安全存储，不在仓库中明文出现
- 假设 CI release workflow 已存在且包含签名和 ldflags 注入步骤
- 假设 `internal/signer/` 的签名逻辑（`SignManifest`、`WriteSignatureFile`）已可用
- 依赖 GitHub Actions secrets 在 workflow 运行时可安全读取
- 假设签名密钥和公钥是匹配的 Ed25519 密钥对

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 密钥检查失败但错误处理不当导致密钥泄露到日志 | 高 | 严禁将 secret 值输出到日志；仅输出变量名 |
| 兼容性 | 硬门禁可能阻塞紧急 hotfix 发布 | 中 | 紧急发布需通过安全例外流程临时豁免（记录并审批） |
| 跨平台 | GitHub Actions secrets 在不同 runner OS 上行为一致 | 低 | GitHub 保证跨平台一致性 |
| 持久化 | 无 | 低 | 不适用 |
| 签名/发布 | 门禁逻辑本身的 bug 可能错误放行 | 高 | 必须包含负向测试覆盖；代码审查重点关注边界条件 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Needs Review | 本 Spec 直接执行该原则——将签名验证从"可选"变为"硬门禁"；关联安全例外 |
| 原则 II：测试必须隔离宿主状态 | Not Applicable | 不涉及测试隔离 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 直接响应 Constitution 识别的执行缺口 |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需通过双重审查门禁；安全类变更需要 checklist |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | 发布签名门禁需在三平台 release job 中一致执行 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | 不改变已有签名逻辑，仅增加前置门禁 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Compliant | 签名硬门禁确保发布产物可验证，增强可复现性 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否
- 是否修改 manifest 或 lockfile 格式？否
- 是否影响已有目录结构？否
- 是否影响已存在的 CI workflow？是（在 release job 中新增前置检查步骤）

### 跨平台影响

- 签名门禁逻辑在各平台 CI runner 上行为一致（依赖 GitHub Actions secrets 和环境变量）
- 发布产物 ldflags 注入在各平台上语法相同（Go 的 `-ldflags` 跨平台一致）

### 安全影响

- 是否涉及认证/授权变更？是（将签名验证从可选变为硬门禁）
- 是否涉及敏感数据存储或传输？是（处理签名私钥，但仅检查存在性不输出内容）
- 是否引入新的信任边界？否（强化现有信任边界）
- 关联安全例外：`docs/security/exceptions/manifest-signature-verification.md`

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-003-release-signing-hard-gate-001` |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-25 | `0.2.0` | `2026-07-25-spec-003-001` 整体 Spec 的目标、范围、需求和验收标准；实现已在 release job 中加入 "Verify signing secrets" 硬门禁 step |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 缺口第 298 行和安全例外创建 Spec 初稿 | — |
