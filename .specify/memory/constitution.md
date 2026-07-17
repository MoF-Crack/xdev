# XDev Constitution

## 文档状态

| 字段 | 值 |
|---|---|
| 项目 | XDev |
| 版本 | `1.0.0-rc.3` |
| 状态 | 候选版，尚未正式批准 |
| 候选日期 | 2026-07-15 |
| 正式批准日期 | 待定 |
| 批准者 | `xinyan <zuijiuhjd@foxmail.com>` |

本候选版用于整理现有文档、校验工程事实并建立后续 Spec-Driven Development（SDD）与 LOOP Engineering 的约束基线。升级到 `1.0.0` 前，必须完成文档体系整理、交叉一致性检查和用户最终批准。

## 不可协商原则

### I. 安全与信任边界

- 所有下载产物必须在使用前执行 SHA-256 完整性校验。
- 内置 manifest 必须使用受信公钥验证 Ed25519 签名；第三方 manifest 必须通过明确的 TOFU（Trust On First Use）或更强机制建立信任。
- 必须拒绝空哈希、全零哈希、校验失败、HTTPS 降级和未经明确授权的可执行文件或脚本安装路径。
- 私有签名密钥、访问令牌、密码及其他凭据不得进入版本库、构建日志、错误输出或测试固件。
- 正式发布必须启用 manifest 签名验证。任何暂时偏离只能依据已批准且尚未失效的安全例外执行。
- 安全例外不得被描述为永久修复，也不得用于扩大未签名内容或弱校验路径的适用范围。

**依据**：`docs/download-source-policy.md`、`docs/signing-guide.md`、`docs/security-audit-verification.md`、`internal/downloader/`、`internal/signer/`。

### II. 测试必须隔离宿主状态

- 自动化测试不得修改真实用户注册表、用户或系统环境变量、Shell 配置、全局工具目录或其他持久化宿主状态。
- 测试必须使用临时目录、内存替身、依赖注入、沙箱或明确的无持久化写入模式。
- 测试结束后的回滚不能替代隔离，因为中断、崩溃和超时可能导致永久污染。
- 无法隔离的测试不得进入默认测试套件；必须先改造生产接口或提供专用隔离环境。
- 一旦检测到宿主状态变化，测试必须失败，并将其视为阻塞性缺陷。

**依据**：`docs/test-plan.md`、`docs/progress-and-defects-analysis.md`、`internal/integration/`。

### III. Constitution 与 Spec 是权威约束源

- 本 Constitution 是全项目工程原则和治理规则的最高权威来源。
- 活跃功能目录中的 `spec.md` 是该功能目标、范围、用户场景、需求和验收标准的唯一任务级权威来源。
- `plan.md` 只能说明如何满足 Spec；`tasks.md` 只能拆分执行步骤；二者不得增加、删除或改变需求语义。
- 实现与 Spec 冲突时必须暂停实现并以 Spec 为准，不能通过降低验收标准或反向修改目标让现有实现通过。
- Agent 可以提出 Spec 修订建议，但不得自行批准或静默修改需求。
- Spec 发生语义变化时，必须记录原因、影响范围和受影响任务；相关实现重新进入 LOOP，并重新执行 Spec 合规审查和工程质量审查。
- Constitution 与 Spec 冲突时，以 Constitution 为准。业务目标确需突破 Constitution 时，必须先完成 Constitution 修订流程。

### IV. 逐任务双重独立审查

每个实现任务必须依次通过两个独立门禁：

1. **Spec 合规审查**：逐条检查实现是否满足需求、验收场景和范围约束，识别遗漏、超范围实现、行为偏移和未经批准的需求解释。
2. **工程质量审查**：检查代码质量、安全性、架构约束、测试证据、兼容性、宿主状态隔离和跨平台影响。

审查规则：

- 两个审查者均不得是该任务的实现者。
- Spec 存在矛盾、歧义或缺口时，必须暂停任务并先修订 Spec，不能由实现者自行选择解释。
- 任一门禁不通过，任务都必须返回实现 LOOP。
- 修复后必须重新执行失败门禁；如果修复涉及 Spec 语义、核心架构、安全边界或验收范围变化，则两个门禁都必须重新执行。
- Orchestrator 负责调度、证据收集和门禁执行，但不得代替独立审查者降低标准。
- 审查通过不等于用户批准，也不能替代本 Constitution 规定的阶段批准。

### V. Windows 主平台与跨平台分层验证

- Windows 是当前主平台，相关行为和聚焦测试属于任务级强制门禁。
- 所有变更必须在当前可用平台运行最小且有意义的聚焦验证。
- 平台无关代码至少必须证明 Windows 行为正确，并提供 Linux、macOS 的编译检查、测试或 CI 证据。
- 涉及 Linux 或 macOS 专有实现时，任务阶段必须提供对应平台测试、CI 或可审查的替代证据，不能以“理论上兼容”放行。
- Linux、macOS 的全面验证可以集中到发布门禁，但平台相关任务仍须在任务阶段提供对应证据。
- 正式发布前必须通过 Windows、Linux、macOS CI。缺少任一平台证据时，只能声明已验证的平台，不能声明“跨平台完成”。

**依据**：`.github/workflows/ci.yaml`、`docs/architecture.md`、`docs/test-plan.md`。

### VI. 向后兼容与可恢复迁移

- 配置文件、manifest、lockfile、CLI 命令与参数、默认行为及已安装目录结构默认保持向后兼容。
- Agent 不得以“简化”“重构”或“当前为 alpha”为由移除已有兼容行为。
- 破坏性变更必须提供明确版本边界、兼容性影响说明、数据与配置迁移方案、失败回滚方案及用户明确批准。
- 无法自动迁移时，必须提供可验证的人工迁移步骤。
- 读取旧格式和写入新格式应分别设计，避免直接覆盖导致不可恢复的数据丢失。
- CLI 默认行为不得静默改变；必要变化应优先通过新增显式参数引入。
- 涉及当前架构强约束的破坏性变更，还必须满足 ADR 和 Constitution 修订要求。

### VII. 依赖、生成资产与构建必须可复现

- `go.mod`、`go.sum`、`frontend/package.json` 和 `frontend/package-lock.json` 是依赖事实来源。
- `vendor/`、`node_modules/`、构建输出、测试运行产物、下载缓存和可重复生成的审计报告不得进入版本库。
- 运行和验证所必需且无法由普通开发者安全恢复的资产必须版本化，包括 manifest 签名、公钥、Wails bindings 和视觉回归测试基线。
- 构建与测试命令必须可从版本化输入重现；本地未记录状态不得成为成功构建的必要条件。
- CI 的 Go 工具链版本必须与 `go.mod` 的 `go` 指令一致；不一致属于可复现性缺陷。
- 引入新依赖必须说明必要性、许可证和维护影响，并遵循 YAGNI 原则。是否满足 YAGNI 由工程质量审查者判断并记录理由。

## 当前架构强约束

以下约束不是永久不可变，但普通功能实现和 Agent 自主 LOOP 不得绕过或事实性改变它们。

### A. 单一入口 Shim Dispatch

- 所有受管工具入口统一通过 xdev shim 分发。
- Shim 通过入口名称识别目标工具，并解析到当前激活版本。
- 普通功能不得引入并行分发机制或直接绕过 shim。
- 安装、激活、切换和卸载后必须正确更新或失效相关索引与缓存。

### B. 标准 Plugin Interface 与 Manifest 驱动扩展

- 新工具默认通过标准 Plugin Interface 和 manifest 接入。
- 通用安装流程保持工具无关；工具特例只能通过明确的插件扩展点实现。
- 不得将工具专用解析、路径或安装逻辑散落到通用安装、下载和激活流程。
- Manifest 必须明确描述版本、平台、架构、来源、哈希、安装类型和必要参数。

### C. Junction/Symlink 激活模型

- 受管版本遵循 `current/<tool>` 指向 `install/<tool>/<version>` 的激活模型。
- Windows 使用 Junction，Unix-like 平台使用 Symlink 或等价的受控链接机制。
- 激活与版本切换必须具备事务性；失败时恢复先前目标和可用状态。
- 不得通过直接覆盖版本目录或破坏旧版本数据实现切换。

### D. 分层 Manifest 信任模型

- 内置 manifest 使用 Ed25519 签名验证。
- 第三方 manifest 使用 TOFU 或更强的明确授权机制。
- 所有下载产物强制执行 SHA-256 校验。
- 全零哈希、HTTPS 降级、签名失败和未经授权的 executable/script 路由必须拒绝。
- 信任判断必须发生在执行和提取之前，不能在副作用产生后补做验证。

### 架构强约束的变更门禁

改变上述任一约束必须同时具备：

1. `docs/adr/NNNN-<decision>.md`，记录背景、候选方案、权衡、兼容性、迁移和回滚计划；
2. Constitution 修订及对应 SemVer 版本升级；
3. 对受影响 Spec、Plan、Tasks 和测试策略的更新；
4. `xinyan <zuijiuhjd@foxmail.com>` 的明确批准。

批准前，Agent 不得通过代码实现事实性改变核心架构。

## SDD 与 LOOP Engineering 生命周期

活跃功能必须遵循以下生命周期：

```text
specify
→ 用户批准 Spec
→ clarify（存在歧义时强制）
→ plan
→ Constitution 合规审查
→ 用户批准 Plan
→ tasks
→ analyze
→ implement
→ 逐任务双重独立审查 LOOP
→ converge
→ 最终验收
→ 用户批准验收
→ 用户批准归档
```

### 生命周期门禁

- `clarify` 发现未解决歧义时，不得进入 `plan`。
- `analyze` 发现 Constitution、Spec、Plan、Tasks 不一致时，不得实施。
- `converge` 不能代替逐任务审查，只负责最终查漏和追加剩余工作。
- 涉及安全、向后兼容、跨平台、持久化状态或当前架构强约束时，必须生成并通过 checklist。
- 普通任务可由 Orchestrator 建议是否生成 checklist，但不得跳过 Spec、Plan、Tasks、Analyze 或双重独立审查。

### 阶段批准权

以下状态跃迁只能由用户批准：

| 状态跃迁 | 批准内容 |
|---|---|
| `Draft → Approved` | Spec 的目标、范围、需求和验收标准 |
| `Planned → Ready` | Plan 的技术方案、约束满足方式和验证策略 |
| `Verified → Accepted` | 最终实现和验收证据 |
| `Accepted → Archived` | 归档状态、遗留问题和历史证据 |

批准者统一记录为 `xinyan <zuijiuhjd@foxmail.com>`。

- Agent 只能在用户明确批准后写入批准记录。
- “继续”“执行吧”等指令只批准当前明确展示的阶段，不自动批准后续阶段。
- 审查通过、测试通过或 Orchestrator 判定完成均不能代替用户批准。
- 语义变更后，原批准不再覆盖变更部分，必须更新版本并重新批准。
- 拼写、格式和链接修复不触发重新批准，但必须记录在变更历史中。

每份活跃 Spec 应包含：

```markdown
## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
```

## 安全例外治理

安全例外存放于：

```text
docs/security/exceptions/<exception-id>.md
```

每份安全例外必须记录：

- 偏离的 Constitution 条款；
- 风险、影响范围和禁止扩大的边界；
- 临时补偿控制和修复计划；
- 责任人、批准者和逐次发布批准记录；
- 目标版本、截止日期和自动失效条件；
- 关闭证据或续期重新评估记录。

### Manifest 签名验证临时例外

当前 manifest 签名验证未完全启用的状态必须登记为安全例外，规则如下：

- 目标版本：首个符合 SemVer 且不包含 `alpha`、`beta`、`rc` 等预发布标识的版本；
- 截止日期：2026-09-01；
- 自动失效：目标版本和截止日期任一先到即失效；
- 逐次复核：例外有效期内，每次发布仍需 `xinyan <zuijiuhjd@foxmail.com>` 明确批准并记录；
- 失效行为：签名验证未完成则阻止正式发布；
- 续期要求：重新评估风险、设置新版本和日期，并重新获得批准，不能自动延续旧批准；
- 事实标记：例外关闭前，任何文档不得将该问题标记为“已修复”。

本 Constitution 只定义例外约束；具体风险、补偿控制和批准历史必须写入独立安全例外文件。

## 文档与规格治理

### 规范源

- 新功能统一使用项目根目录下的 `specs/<编号-功能名>/`。
- 活跃功能通常包含 `spec.md`、`plan.md`、`tasks.md`，并按需要包含 `research.md`、`data-model.md`、`quickstart.md`、`contracts/` 和 checklist。
- Constitution、ADR、安全例外、Spec、Plan、Tasks 和 checklist 使用简体中文。
- 函数名、类型名、CLI 命令、路径、配置键、日志原文和 Git 提交信息保留英文。
- 不强制维护中英文双语副本，避免内容漂移。

### 历史规格归档

- 完成的历史规格归档到 `specs/archive/<完成年份>/<编号-功能名>/`。
- 迁移自旧系统且没有原始 Spec Kit 编号的规格使用 `000-` 前缀。
- `.trae/specs/enhance-gui-ux-and-fixes/` 的目标归档路径为 `specs/archive/2026/000-enhance-gui-ux-and-fixes/`。
- 归档规格必须记录原始路径、原始工具、创建和完成时间、迁移日期、状态、实现与验证证据及已知遗留问题。
- `.specify/feature.json` 不得指向 `specs/archive/`。

归档后的 `spec.md`、`plan.md`、`tasks.md`、`checklist.md` 和原始验证材料不可改写。历史错误、失效链接或状态偏差只能追加到：

```text
specs/archive/<年份>/<归档规格>/errata.md
```

每条勘误必须包含日期、原文位置、问题说明、正确事实和证据引用。新需求、行为变化或补充实现必须创建新的正式 feature spec，并反向链接原归档规格。

### 旧计划和 Backlog

- `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` 冻结为历史实施记录；未完成的集成验收和最终验证提炼为新的正式 feature spec。
- `docs/pending-work-plan.md` 中具有独立目标、边界和验收标准的事项分别转换为 feature spec，不整体迁移。
- `IMPROVEMENT-PLAN.md` 保留为需求 backlog，只负责候选需求、优先级和状态，不能直接驱动实现。
- Backlog 条目只有经过 `specify → clarify（按需）→ plan → tasks → analyze` 后，才能进入 OMO 执行和 LOOP。
- `.trae/documents/xdev-improvement-roadmap.md` 属于路线图，后续迁移为 `docs/roadmap.md`，不作为功能 Spec。

## 治理与修订

### 治理资产目录

- ADR：`docs/adr/NNNN-<decision>.md`
- 安全例外：`docs/security/exceptions/<exception-id>.md`
- 功能规格：`specs/<编号-功能名>/`
- 历史规格：`specs/archive/<完成年份>/<编号-功能名>/`

ADR 记录长期架构决策，安全例外记录临时风险偏离。二者都不能直接覆盖 Constitution。

### Constitution 版本规则

本 Constitution 使用语义化版本：

- `MAJOR`：删除原则、放宽不可协商约束、改变治理模型或批准权限；
- `MINOR`：新增原则、新增强制门禁或扩展约束覆盖范围；
- `PATCH`：澄清措辞、修复矛盾、补充不改变语义的说明和引用；
- 候选版使用 `1.0.0-rc.N`；
- `1.0.0-rc.N → 1.0.0` 表示用户正式批准并生效。
- 候选内容发生修订时必须递增 `rc.N`；候选版之间不继承正式批准状态。

每次修订必须记录版本、日期、变更原因、影响范围和批准者。核心架构变更还必须遵循 ADR 门禁。

### 修订对在途工作的影响

- Constitution 的 `MAJOR` 或 `MINOR` 修订生效时，所有未归档功能必须按新版本重新执行 Constitution 合规审查。
- `PATCH` 修订若只澄清措辞可保留现有批准；如果实际改变命令、路径、配置值、验证方式或约束解释，则按语义变更处理并重新审查。
- ADR 影响已批准 Plan 或已发布能力时，必须更新受影响 Spec/Plan，或创建新的 feature spec 并反向链接原规格。
- Plan 的用户批准仅覆盖批准时的技术方案。实现期间即使 Spec 语义不变，只要技术方案、架构边界或验证策略发生实质变化，也必须更新 Plan 版本并重新执行 Constitution 合规审查和 `Planned → Ready` 批准。
- 已归档规格不可回写；后续治理变化通过新 feature spec 或 `errata.md` 建立关联。

### 当前已知执行缺口

候选版确认以下约束尚未完全由自动化工具执行，不能在正式批准前隐瞒：

- 默认 CI 未执行带 `integration` build tag 的集成测试；
- 发布作业在签名密钥或公钥缺失时可能继续执行，尚未形成签名硬门禁；
- 测试隔离依赖 `--no-path-fix` 等参数，尚缺少统一的宿主状态快照检测；
- 双重审查和批准历史尚未由模板或自动检查强制。

这些缺口必须在文档体系设计中建立责任归属、修复 Spec 或显式例外；候选版不得据此宣称工程已完全合规。

### 正式批准条件

`1.0.0-rc.3` 升级为 `1.0.0` 前必须完成：

1. 与现有架构、安全、测试、操作和 CI 文档交叉检查；
2. 处理或明确标记文档之间的事实冲突；
3. 建立 ADR 和安全例外目录及模板；
4. 完成规范文档体系设计和历史文档迁移方案；
5. 清除全部占位符、矛盾和不可验证表述；
6. 由 `xinyan <zuijiuhjd@foxmail.com>` 最终审阅并明确批准。

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Constitution 结构与规则决策 | Candidate Draft Authorized | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | 初始结构、原则、架构约束、生命周期、审批、例外和文档治理决策；不代表正式批准 |
| Constitution 独立复核修订 | Candidate Revised | — | 2026-07-15 | `1.0.0-rc.2` | 补充术语、审查独立性、在途重置、当前执行缺口并纠正过时证据；等待用户审阅 |
| CI 工具链事实同步 | Candidate Revised | — | 2026-07-16 | `1.0.0-rc.3` | CI Go 版本已与 `go.mod` 统一为 `1.23`；正式批准状态不变 |
| Constitution `rc.3` 候选内容批准 | Approved Candidate | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0-rc.3` | `2026-07-16-constitution-rc3-001` | 批准当前候选内容作为后续治理工作的候选基线；不追溯授权 `059da20`，不构成 `1.0.0` 正式批准 |
| Constitution 正式批准 | Pending | — | — | `1.0.0` | 待文档体系整理和最终审阅后批准 |

**Version**: `1.0.0-rc.3` | **Candidate Date**: 2026-07-15 | **Ratified**: Pending | **Last Amended**: 2026-07-16
