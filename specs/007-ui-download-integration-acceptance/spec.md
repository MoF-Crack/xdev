# Feature Specification: UI/下载源计划剩余集成验收

> **文档类型**：Feature Spec
> **状态**：Accepted
> **文档版本**：0.2.0
> **创建日期**：2026-07-16
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义 UI/下载源历史计划剩余集成验收事项的目标、范围、需求和验收标准
> **替代文档**：`docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md`（历史冻结）、`docs/pending-work-plan.md`（待拆分）

**Feature Branch**: `007-ui-download-integration-acceptance`
**Created**: 2026-07-16
**Status**: Accepted
**Input**: Constitution `1.0.0-rc.3` 第 255 行；`docs/pending-work-plan.md`

---

## Why

Constitution `1.0.0-rc.3` 第 255 行将 `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` 冻结为历史实施记录，并明确：

> "未完成的集成验收和最终验证提炼为新的正式 feature spec。"

XDev UI 重设计与下载源治理已完成 Task 1-18 的核心实施，但 `docs/pending-work-plan.md` 列出了该计划 Task 13-17 的 **5 项后续工作**仍待处理：

| # | 待处理项 | 优先级 | 依赖 |
|---|---|---|---|
| 1 | Windows full SHA audit（~29GB 下载）—— 替换 32 条 Android manifest 占位 hash 和解决 5 组同 URL 不同 SHA 冲突 | 高 | 无 |
| 2 | `.yaml.sig` 重签 —— SHA 修改后 manifest 签名失效，需重新签名 | 高 | 依赖 #1 |
| 3 | 不可修复工具文档化（Haskell、Kotlin、Ninja、UV 无可用镜像） | 中 | 无 |
| 4 | TRANSIENT 错误重试 —— 16 条 TRANSIENT 下载错误需重试验证或建立 CI 豁免 | 中 | 无 |
| 5 | `toolsHashFullCheck` vet 告警修复 —— 2 行 `fmt.Println` 冗余换行 | 低 | 无 |

这些事项缺少正式 Spec 驱动的跟踪和验收标准，目前仅以工作计划形式存在，不符合 Constitution 原则 III 要求的"活跃功能必须通过正式 Spec 驱动实现"。

受影响方：
- 下载 manifest 的完整性和可验证性（32 个占位 hash 无法通过 SHA-256 校验）；
- manifest 签名验证链的完整性（SHA 修改后签名失效）；
- TRANSIENT 错误的用户体验（下载失败时无自动重试）；
- CI 审计门禁的正确性（占位 hash 无法被审计检测）。

不关闭此缺口：历史计划中遗留的技术债务持续累积，manifest 完整性和签名验证链存在已知缺陷，不符合 Constitution 原则 I 的 SHA-256 完整性校验要求。

## 目标

将 UI/下载源历史计划中未完成的 5 项集成验收事项固化为正式 Feature Spec，为每项工作定义明确的验收标准、优先级和完成条件，使其可独立追踪、审查和实施。

## 范围

### 包含

- Windows full SHA audit 的需求定义：替换 32 条 Android manifest 占位 hash，解决 12 个同 URL 不同 SHA 冲突，修正 Ninja SHA 错误
- `.yaml.sig` 重签的需求定义：基于 SHA audit 结果重新生成 manifest 签名文件
- 不可修复工具文档化的需求定义：为 Haskell、Kotlin、Ninja、UV 创建明确的文档记录
- TRANSIENT 错误重试的需求定义：建立 16 条 TRANSIENT 错误的重试验证或 known-transient 豁免机制
- `toolsHashFullCheck` vet 告警修复的需求定义：修复 2 行 `fmt.Println` 冗余换行

### 非目标

- 直接实施这 5 项工作（由后续 Plan/Tasks 负责）
- 新增超出 `docs/pending-work-plan.md` 范围的需求
- 修改下载器的核心下载逻辑
- 修改 manifest 的格式或结构
- 处理 pending-work-plan.md 中未列出的其他遗留问题

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Windows full SHA audit 完成并替换占位 hash (Priority: P1)

运维人员能够运行完整的 Windows SHA audit，下载所有约 29GB 的工具归档文件，计算实际 SHA-256 hash，并回写到 manifest YAML 文件中，替换 32 条占位 hash 并解决 5 组同 URL 不同 SHA 冲突。

**Why this priority**: SHA 完整性是 Constitution 原则 I 的核心要求——占位 hash 无法通过 SHA-256 校验，直接影响下载安全。

**Independent Test**: 对小批工具运行 linkaudit full 模式，验证 SHA 值被正确计算并回写到 manifest。

**Acceptance Scenarios**:

1. **Given** 运行小批 SHA audit（kotlin、erlang、git、gradle、mingw、ninja、android build-tools/cmdline-tools/platform），**When** audit 完成，**Then** 所有 12 个同 URL 不同 SHA 冲突被统一为正确 SHA，Android 占位 hash 被替换为真实 SHA
2. **Given** 运行中批 SHA audit（android-ndk），**When** audit 完成，**Then** 9 条 NDK 占位 hash 被替换为真实 SHA
3. **Given** 运行大批 SHA audit（其余 ~150 条工具条目），**When** audit 完成，**Then** 已正确验证的 SHA 保持不变，发现的不一致项记录在 `build/manifest-changes-unsigned.md`

---

### User Story 2 - .yaml.sig 重签 (Priority: P2)

SHA audit 完成后，所有修改了 sha256 值的 manifest YAML 文件的 `.yaml.sig` 签名失效。需要基于新的 manifest 内容重新生成 Ed25519 签名文件。

**Why this priority**: 签名文件必须与 manifest SHA 值匹配才能通过验证；但依赖 SHA audit 先完成。

**Independent Test**: 修改一个 manifest 的 sha256 值后重新签名，使用公钥验证签名是否匹配。

**Acceptance Scenarios**:

1. **Given** manifest `android-build-tools.yaml` 的 SHA 已被修正，**When** 运行签名命令，**Then** 生成新的 `android-build-tools.yaml.sig` 且签名验证通过
2. **Given** 所有被修改的 manifest 已重新签名，**When** 运行 `go run ./cmd/xdev-keygen -verify` 对所有 manifest，**Then** 所有签名验证通过

---

### User Story 3 - TRANSIENT 错误自动重试机制生效 (Priority: P3)

对 16 条已知 TRANSIENT 错误（Windows official 11 条 + darwin/arm64 5 条），实施自动重试或建立 known-transient CI 豁免机制，避免因网络瞬断导致的 CI 假阳性失败。

**Why this priority**: 提升 CI 稳定性和开发者体验，但不涉及安全或数据完整性。

**Independent Test**: 在下载器中配置重试参数，对模拟的 TRANSIENT 失败场景验证重试逻辑。

**Acceptance Scenarios**:

1. **Given** 下载请求首次返回 TRANSIENT 错误，**When** 重试逻辑启用且配置为最多 3 次重试、间隔 30 秒，**Then** 第 2 次或第 3 次重试成功下载
2. **Given** 所有重试均失败，**When** 达到最大重试次数，**Then** 下载器返回明确的 TRANSIENT 失败错误
3. **Given** known-transient 工具在 CI audit 中再次出现 TRANSIENT 错误，**When** 工具在 known-transient 豁免列表中，**Then** CI 不因此阻断

---

### User Story 4 - 不可修复工具文档化 (Priority: P4)

为 Haskell/GHC、Kotlin、Ninja、UV 四个无可信第二镜像源的工具创建明确的文档记录，说明当前状态、限制和用户应对方案。

**Why this priority**: 文档化是透明性要求，不影响运行时行为。

**Independent Test**: 检查文档是否存在且内容覆盖所有四个工具的状态说明。

**Acceptance Scenarios**:

1. **Given** 完成不可修复工具文档化，**When** 查看 `docs/security/policy/download-source-policy.md`，**Then** 包含 "official-only, no mirror available" 章节，列出 4 个工具及其调查结论
2. **Given** 文档已完成，**When** 用户查询 Kotlin 的下载源状态，**Then** 文档明确说明无可用镜像、仅支持 GitHub Release 官方源

---

### User Story 5 - toolsHashFullCheck vet 告警修复 (Priority: P5)

修复 `toolsHashFullCheck/main.go` 中的 2 行 `fmt.Println` 冗余换行 vet 告警，使 `go vet ./...` 全量通过。

**Why this priority**: 修复简单（2 行改动，5 分钟），但优先级最低因为它是最容易的独立修复。

**Independent Test**: 运行 `go vet ./...` 验证零告警。

**Acceptance Scenarios**:

1. **Given** 修复 `main.go:96` 和 `:103` 的 `fmt.Println` 冗余换行，**When** 运行 `go vet ./...`，**Then** 无任何告警，返回零退出码

---

### 边界条件

- 当 SHA audit 因网络或磁盘限制无法完成全部 ~29GB 下载时，应如何降级处理（只完成小批、标记其余为未验证）？
- 当签名私钥不应作为生产密钥使用时，`.yaml.sig` 重签应使用现有密钥、生成新密钥对还是不重签？
- 当 TRANSIENT 错误的根因是 CDN 永久不可达（而非网络瞬断）时，应如何处理（标记为 URL_DEAD vs known-transient）？

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：Android manifest 中的 32 条占位 hash（值 `a04cfd774f...`）必须全部替换为通过 full download audit 验证的真实 SHA-256 值
- **FR-002**：5 组同 URL 不同 SHA 冲突必须通过实际下载验证后统一为正确的 SHA-256 值
- **FR-003**：所有被修改了 SHA 的 manifest YAML 文件必须重新生成对应的 `.yaml.sig` 签名文件
- **FR-004**：TRANSIENT 错误下载必须支持自动重试机制（可配置重试次数和间隔），或建立 known-transient CI 豁免列表
- **FR-005**：Haskell/GHC、Kotlin、Ninja、UV 四个不可修复工具必须在安全政策文档中记录其下载源限制和用户应对方案
- **FR-006**：`toolsHashFullCheck/main.go` 中的 2 行 vet 告警必须修复，`go vet ./...` 全量通过

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — SHA audit 的总下载量约 29GB，应支持分批执行且每批完成后自动清理临时文件
- **NFR-002**：安全 — SHA audit 的下载产物必须在使用后清理（`-cleanup` flag），不得在 CI runner 或本地环境中长期留存
- **NFR-003**：兼容性 — manifest SHA 修改后，已有依赖 manifest 的下载逻辑不得被破坏
- **NFR-004**：跨平台 — SHA audit 本身是跨平台活动（验证同一归档文件在不同平台下载的 SHA 一致性）
- **NFR-005**：可维护性 — 所有 SHA 变更必须记录在 `build/manifest-changes-unsigned.md` 中

### 关键实体

- **Manifest YAML**：`manifests/<tool>.yaml`，定义工具的版本、平台、下载 URL 和 SHA-256 hash
- **占位 hash**：值为 `a04cfd774f522d7944520b01a09b3e0fbf4069e9a806cd0c620179b987340029` 的 32 条条目，分布在 4 个 Android manifest 中
- **`.yaml.sig`**：Ed25519 签名文件，签名 payload 包含工具名、sha256_map 和时间戳

---

## Success Criteria *(mandatory)*

### 可度量成果

- **SC-001**：SHA audit 完成后，32 条占位 hash 中 0 条残留（全部替换为真实 SHA-256 值）
- **SC-002**：`.yaml.sig` 重签后，所有被修改的 manifest 签名验证通过（`go run ./cmd/xdev-keygen -verify` 零失败）
- **SC-003**：TRANSIENT 错误重试或豁免机制生效后，CI audit job 不再因已知 TRANSIENT 错误阻断
- **SC-004**：不可修复工具文档化后，`docs/security/policy/download-source-policy.md` 包含 4 个工具的完整状态说明
- **SC-005**：`go vet ./...` 全量通过，零告警

---

## 假设与依赖

- 假设存在约 40GB 可用磁盘空间用于 SHA audit 的临时下载
- 假设网络环境可访问所有 official 下载 URL（GitHub、Google、Microsoft CDN 等）
- 假设 `keys/priv.key` 和 `keys/pub.key` 存在于项目树中且可用于重签
- 假设 `cmd/linkaudit` 的 full 模式可用且正确
- 依赖 `docs/pending-work-plan.md` 中定义的精确工作范围（32 条占位 hash、5 组冲突等）

---

## 风险分类

| 风险类别 | 风险描述 | 严重度（高/中/低） | 缓解措施 |
|---|---|---|---|
| 安全 | 占位 hash 使下载完整性校验失效 | 高 | 通过 full SHA audit 替换为真实 SHA |
| 兼容性 | manifest SHA 修改后签名失效 | 中 | 同步重签 `.yaml.sig` |
| 跨平台 | SHA audit 需要三平台产物但可能仅在 Windows 执行 | 中 | 同 URL 不同平台已按相同 SHA 统一（通过小批验证） |
| 持久化 | SHA audit 临时文件可能占用大量磁盘 | 中 | 使用 `-cleanup` flag 自动清理 |
| 签名/发布 | `.yaml.sig` 重签可能影响发布产物的签名验证 | 高 | 重签后全面验证签名链路；记录变更 |

---

## Constitution、兼容性、跨平台与安全影响

### Constitution 影响评估

| Constitution 原则（候选 rc.3） | 影响判定 | 说明 |
|---|---|---|
| 原则 I：安全与信任边界 | Needs Review | SHA audit 和签名重签直接关乎 SHA-256 完整性校验和 manifest 签名验证 |
| 原则 II：测试必须隔离宿主状态 | Not Applicable | SHA audit 不是测试活动 |
| 原则 III：Constitution 与 Spec 是权威约束源 | Compliant | 本 Spec 将冻结的历史计划提炼为正式 Spec |
| 原则 IV：逐任务双重独立审查 | Compliant | 实施时需通过双重审查门禁 |
| 原则 V：Windows 主平台与跨平台分层验证 | Compliant | SHA audit 以 Windows 为主平台执行 |
| 原则 VI：向后兼容与可恢复迁移 | Compliant | SHA 修正不影响下载器兼容性；旧 manifest 被新版本替代 |
| 原则 VII：依赖、生成资产与构建必须可复现 | Compliant | 正确的 SHA-256 是可复现构建的前提 |

### 兼容性影响

- 是否修改已有 CLI 参数或行为？否（仅修改 manifest 数据文件）
- 是否修改 manifest 或 lockfile 格式？否（仅修改 sha256 字段值）
- 是否影响已有目录结构？否
- 是否影响下载器运行时行为？否（manifest 被 embed 到二进制，正确 SHA 不影响下载逻辑）

### 跨平台影响

- SHA audit 需要下载跨平台归档文件（Windows、Linux、macOS 的下载 URL 已在 manifest 中定义）
- 同 URL 不同 SHA 冲突的解决需要实际下载对应的平台归档文件
- 签名重签是平台无关操作（Ed25519 签名逻辑跨平台一致）

### 安全影响

- 是否涉及认证/授权变更？否
- 是否涉及敏感数据存储或传输？是（签名私钥用于重签操作，需确保不在日志和 CI 输出中泄露）
- 是否引入新的信任边界？否（强化现有 SHA-256 + Ed25519 信任链）
- 关联：本 Spec 的执行可能影响 `docs/security/exceptions/manifest-signature-verification.md` 的关闭条件

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `0.1.0` | `2026-07-16-p6-007-ui-download-integration-acceptance-001` |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-25 | `0.2.0` | `2026-07-25-spec-007-001` search 命令无匹配退出码修复已完成；其余 4 项技术债(SHA audit/重签/不可修复工具文档/TRANSIENT 重试)实现 deferred |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-16 | Initial Draft | 依据 Constitution rc.3 第 255 行和 pending-work-plan.md 创建 Spec 初稿 | — |
