# XDev 规范文档体系设计

> **文档类型**：治理设计
> **状态**：Candidate
> **文档版本**：`1.0.0-rc.1`
> **创建日期**：2026-07-15
> **最后更新**：2026-07-15
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **权威范围**：XDev 的规范文档分类、状态、目录、迁移、模板和自动化门禁设计
> **关联 Constitution**：`.specify/memory/constitution.md` `1.0.0-rc.2`
> **实施状态**：尚未实施
> **批准边界**：四个设计部分已由用户确认，可用于编写实施计划；在 Constitution 正式批准和本设计最终验收前，不表示治理体系已生效

## 1. 目标

建立一套可被人和 Agent 共同执行的规范文档体系，使 XDev 的 Constitution、Feature Spec、技术方案、任务、审查证据、长期参考文档与历史记录具有明确的职责边界和权威顺序。

该体系必须解决以下问题：

- `.specify/`、`.trae/specs/`、`docs/superpowers/plans/` 和散落在 `docs/` 中的计划同时充当规范源；
- 历史审计结论与当前代码事实冲突；
- Backlog、Roadmap 和实施计划可以绕过 Spec 直接驱动 Agent；
- Spec Kit 模板尚未体现 XDev 的批准、双重审查和安全例外规则；
- OMO LOOP 缺少可持久化、可机器检查的门禁证据；
- Constitution 中的工程原则尚未落实为模板、审查记录和 CI 检查。

## 2. 非目标

本设计不直接完成以下工作：

- 不在本设计提交中移动现有文档；
- 不直接修改 `.github/workflows/ci.yaml`；
- 不直接修复 manifest 签名、测试隔离或三平台构建缺口；
- 不创建远程仓库或推送 Git 历史；
- 不将历史文档内容重写为当前事实；
- 不用文档结构替代代码、测试和发布验证。

上述工作将在本设计批准后，通过独立实施计划和 Feature Spec 分阶段完成。

## 3. 设计原则

### 3.1 单一权威范围

每份文档必须声明自己能决定什么。不同文档可以相互引用，但不能同时争夺同一事实的最终解释权。

### 3.2 约束与说明分离

- 治理文档定义必须遵守的规则；
- Feature Spec 定义当前功能要实现的目标；
- Plan 定义实现方式；
- 参考文档解释当前系统；
- 历史文档保留过去事实，但不继续驱动实现。

### 3.3 历史不可伪造

历史规格和时间点审计不得通过后续编辑改写其原始结论。发现错误时通过 `errata.md`、纠偏说明或替代关系保留证据链。

### 3.4 门禁必须持久化

用户批准、Spec 合规审查、工程质量审查、安全例外和最终验收不能只存在于对话上下文中，必须写入版本化文档。

### 3.5 自动化服务于治理

脚本和 CI 负责检查占位符、引用、状态和证据是否完整，但不能替代用户批准或独立审查者的判断。

## 4. 四层文档架构

```text
第 1 层：治理约束
├── .specify/memory/constitution.md
├── docs/adr/
└── docs/security/exceptions/

第 2 层：活跃规格
└── specs/<编号-功能名>/
    ├── spec.md
    ├── plan.md
    ├── tasks.md
    ├── checklist.md
    ├── research.md
    ├── data-model.md
    ├── quickstart.md
    ├── contracts/
    └── reviews/

第 3 层：长期参考文档
├── README.md
├── docs/README.md
├── docs/architecture.md
├── docs/technical-reference.md
├── docs/operations.md
├── docs/test-plan.md
├── docs/roadmap.md
└── docs/backlog.md

第 4 层：历史与时间点记录
├── specs/archive/<年份>/<编号-功能名>/
├── docs/history/
└── docs/audits/
```

### 4.1 第 1 层：治理约束

职责：定义所有功能和 Agent 都必须遵守的全局规则。

- Constitution 定义不可协商原则、当前架构强约束、批准权和生命周期。
- ADR 记录长期架构决策、替代方案、迁移和回滚。
- 安全例外记录暂时偏离、安全风险、补偿控制和自动失效条件。

治理层不得承载具体功能需求。

### 4.2 第 2 层：活跃规格

职责：定义当前功能的目标、实现方式、执行任务和审查证据。

- `spec.md` 是任务级唯一需求权威源。
- `plan.md` 只能实现 Spec，不能改变需求。
- `tasks.md` 只能拆分 Plan，不能增加需求。
- `checklist.md` 记录高风险变更的专项门禁。
- `reviews/` 保存逐任务双重审查和最终验收证据。

### 4.3 第 3 层：长期参考文档

职责：持续解释当前系统的结构、命令、运维、测试和发展方向。

- 参考文档应随当前代码更新；
- 当参考文档与代码事实冲突时，应修正文档并记录变更；
- 参考文档不能覆盖 Constitution 或已批准 Spec。

### 4.4 第 4 层：历史与时间点记录

职责：保存完成的规格、旧计划、审计和一次性实施记录。

- 历史材料默认只读；
- 不再作为活跃实现依据；
- 发现错误时通过勘误或替代关系纠偏；
- 新工作必须创建新的活跃 Feature Spec。

## 5. 文档状态模型

| 文档类型 | 合法状态 |
|---|---|
| Constitution | `Candidate`、`Ratified`、`Superseded` |
| 治理设计 | `Candidate`、`Accepted`、`Superseded` |
| ADR | `Proposed`、`Accepted`、`Rejected`、`Superseded` |
| 安全例外 | `Draft`、`Active`、`Expired`、`Closed`、`Revoked` |
| Feature Spec | `Draft`、`Approved`、`In Progress`、`Verified`、`Accepted`、`Archived` |
| Plan | `Planned`、`Ready`、`Superseded` |
| 长期参考文档 | `Living`、`Deprecated` |
| 审计或时间点报告 | `Snapshot`、`Superseded` |
| Backlog 或 Roadmap | `Living`、`Frozen` |

状态不能只修改顶部字段。每次状态变化必须同时追加变更记录；需要用户批准的状态必须追加批准记录。

## 6. 通用元数据

受治理文档应按类型包含以下元数据：

```markdown
> **文档类型**：Feature Spec
> **状态**：Draft
> **文档版本**：0.1.0
> **创建日期**：YYYY-MM-DD
> **最后更新**：YYYY-MM-DD
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：当前功能的目标、范围、需求和验收标准
> **替代文档**：无
```

字段规则：

- `文档类型` 决定其模板、状态和权威范围；
- `状态` 表示生命周期位置；
- `文档版本` 表示内容版本，不等于产品版本；
- `权威范围` 明确该文档可以决定的事实；
- `替代文档` 建立文档演进关系，旧文档不能无痕消失；
- Snapshot 必须额外记录审计日期、代码基线和工具版本。

## 7. 权威优先级

文档或事实冲突时按以下顺序处理：

```text
Ratified Constitution
→ 用户明确批准的活跃 spec.md
→ 已批准 plan.md
→ 已接受 ADR
→ 版本化代码、测试和配置事实
→ 长期参考文档
→ Backlog、Roadmap、历史计划
→ Snapshot、旧审计和归档资料
```

在 Constitution 正式批准前，上述权威序列的最高有效层级为空位；`1.0.0-rc.N` 只提供候选约束，不得被描述为已生效的最高权威。候选期内的文档整理和实施计划必须保留该限制，并由用户逐阶段批准。

解释规则：

- 代码不能覆盖已批准 Spec 的目标，但可以证明参考文档已经过时；
- Plan 与 Spec 冲突时必须修订 Plan；
- ADR 与 Constitution 冲突时 ADR 不生效；
- 旧审计与当前代码冲突时应标记为 `Superseded` 或增加纠偏说明；
- Backlog 和 Roadmap 只能提出候选需求，不能直接进入实现；
- 归档材料不能用于绕过新的治理和批准流程。

## 8. 状态与批准记录

### 8.1 变更历史

```markdown
## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
```

### 8.2 Approval History

```markdown
## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
```

规则：

- 用户在对话中明确批准后，Orchestrator 才能写入批准记录；
- Agent 的审查结论不能代替用户批准；
- 语义变化会使原批准对变化部分失效；
- 非语义格式修复可保留批准，但必须写入变更历史。

## 9. Feature Spec 生命周期

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

强制规则：

- 未解决 `[NEEDS CLARIFICATION]` 不得进入 Plan；
- `analyze` 发现 Constitution、Spec、Plan 和 Tasks 不一致时不得实施；
- 每个任务必须依次通过 Spec 合规审查和工程质量审查；
- 任一审查失败都返回实现 LOOP；
- `converge` 只负责最终查漏，不能替代逐任务审查；
- 安全、兼容性、跨平台、持久化状态或核心架构变更必须生成 checklist；
- `Draft → Approved`、`Planned → Ready`、`Verified → Accepted` 和 `Accepted → Archived` 只能由用户批准。

## 10. 项目级模板覆盖

不直接修改 Spec Kit 核心模板。在以下目录建立项目级覆盖：

```text
.specify/templates/overrides/
├── spec-template.md
├── plan-template.md
├── tasks-template.md
├── checklist-template.md
├── adr-template.md
├── security-exception-template.md
├── archive-metadata-template.md
└── review-record-template.md
```

`.specify/scripts/powershell/common.ps1` 已优先读取 `templates/overrides/`。当前 Spec Kit 脚本直接消费 `spec-template.md`、`plan-template.md` 和 `tasks-template.md`；`checklist-template.md` 的消费方式必须在实施时核对。ADR、安全例外和归档元数据模板属于项目治理模板，需由文档脚本或人工流程显式引用，不能假设 Spec Kit 会自动加载。

在项目覆盖模板落地并通过验证前，核心模板中的占位符和 `[x]` 规则仍然有效，因此本设计中的占位符、批准和任务证据门禁处于“设计完成、尚未强制”状态。

### 10.1 Spec 模板

必须包含：

- 元数据；
- Why 和目标；
- 范围与非目标；
- 用户场景；
- `FR-*` 功能需求；
- `NFR-*` 非功能需求；
- 边界条件；
- 假设与依赖；
- `SC-*` 可度量成功标准；
- 风险分类；
- Constitution、兼容性、跨平台和安全影响；
- Approval History；
- 变更历史。

达到 `Approved` 前不得保留 `TODO`、`TBD` 或 `[NEEDS CLARIFICATION]`。

### 10.2 Plan 模板

必须包含：

- 关联 Spec 和版本；
- 技术方案、模块边界和接口；
- 数据流和失败处理；
- 兼容性与迁移；
- 安全设计和测试隔离；
- 跨平台验证矩阵；
- Constitution 逐条检查；
- ADR 与安全例外判断；
- 验证方案；
- Approval History 和变更历史。

Constitution 检查的合法结果：

```text
Compliant
Not Applicable
Exception Required
ADR Required
Non-compliant
```

存在未批准的 `Exception Required`、未接受的 `ADR Required` 或任何 `Non-compliant` 时不得进入 Tasks。

### 10.3 Tasks 模板

每个任务必须包含：

- 任务编号；
- 关联 FR/SC；
- 输入与输出；
- 文件范围；
- 前置依赖；
- 实现者；
- Spec 合规审查者；
- 工程质量审查者；
- 聚焦验证命令；
- 完成证据；
- LOOP 状态。

任务状态：

```text
Pending
In Progress
Spec Review
Quality Review
Rework
Verified
Blocked
```

禁止只使用 `[x]` 作为完成证据。进入 `Verified` 必须同时存在实现结果、验证输出和两类独立审查记录。

### 10.4 Checklist 模板

以下变更必须生成 `checklist.md`：

- 安全和信任边界；
- 持久化配置或用户数据；
- CLI、manifest、lockfile 或目录兼容性；
- Windows、Linux、macOS 平台行为；
- 当前架构强约束；
- 正式发布和签名流程。

Checklist 是审查输入，不能替代双重审查。

## 11. 审查记录

每个活跃功能使用：

```text
specs/<功能>/reviews/
├── T001-spec-review.md
├── T001-quality-review.md
└── final-acceptance.md
```

审查记录必须包含：

- 审查对象和 diff 范围；
- 关联 Spec/Plan 版本；
- 逐条判定；
- 验证证据；
- 阻塞和非阻塞问题；
- 修复与复审历史；
- 最终结论；
- 审查者身份和日期。

两类审查必须由不同主体或相互隔离的 Agent 实例承担，且不得由任务实现者承担。

## 12. 自动化门禁

建议新增：

```text
scripts/docs/
├── validate-specs.ps1
├── validate-constitution.ps1
├── validate-links.ps1
└── validate-governance.ps1
```

最小检查范围：

- Constitution 和活跃 Spec 不含占位符；
- Spec 获得批准后才能进入 Plan；
- Plan 引用的 Spec 版本存在且已批准；
- Tasks 中 FR/SC 引用真实存在；
- 高风险功能存在 checklist；
- 两类审查记录齐全后任务才能为 `Verified`；
- 安全例外未过期，逐次发布批准存在；
- `.specify/feature.json` 不指向归档目录；
- 归档目录不被活跃工作流使用；
- 文档内部路径和链接有效；
- CI 工具链版本与版本化输入一致。

文档链接检查只检查 Markdown 路径、锚点和相对引用，不替代现有 `scripts/audit-links.ps1` 对软件下载源的网络与策略检查。两个工具名称和职责必须明确区分。

## 13. CI 治理 Job

新增独立 `governance` job：

```text
governance
├── validate Constitution
├── validate active Specs
├── validate approvals
├── validate exceptions
├── validate documentation links
└── validate CI/toolchain alignment
```

在 Constitution 正式批准前以报告模式运行；批准为 `1.0.0` 后改为阻塞模式。

以下已知缺口应分别建立修复 Spec：

- CI Go `1.22` 与 `go.mod` Go `1.23` 不一致；
- 默认 CI 未运行 integration tests；
- 发布缺少签名私钥和公钥硬门禁；
- 发布产物与三平台验证产物缺少严格同源关系；
- 测试隔离缺少宿主状态前后快照；
- 双重审查和批准历史尚未被自动验证。

## 14. 现有文档迁移矩阵

### 14.1 规格与计划

| 当前路径 | 目标路径 | 处理方式 |
|---|---|---|
| `specs/archive/2026/000-enhance-gui-ux-and-fixes/` | `specs/archive/2026/000-enhance-gui-ux-and-fixes/` | 原文迁移并冻结，增加归档元数据和实现证据 |
| `docs/roadmap.md` | `docs/roadmap.md` | 转为 `Living` 路线图，不作为 Feature Spec |
| `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` | `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` | 标记为历史实施记录，未完成验收另建正式 Spec |
| `docs/pending-work-plan.md` | 暂时保留，随后拆分 | 独立事项转为 Feature Spec，全部转换后冻结并归档 |
| `docs/backlog.md` | `docs/backlog.md` | 转为 `Living` backlog，不直接驱动实现 |
| `docs/history/implementation-plans/git-initial-import-plan.md` | `docs/history/implementation-plans/git-initial-import-plan.md` | Git 初始导入完成后转为历史记录 |

### 14.2 安全与审计

```text
docs/security/
├── README.md
├── policy/
│   ├── download-source-policy.md
│   └── signing-policy.md
├── procedures/
│   ├── signing-guide.md
│   └── download-cache-recovery.md
└── exceptions/
    └── manifest-signature-verification.md

docs/audits/
├── 2026-07-04-security-audit.md
└── 2026-07-08-security-audit-verification.md
```

其中 `download-source-policy.md`、`signing-guide.md`、`download-cache-recovery.md` 和两份审计为现有文件的迁移目标；`signing-policy.md`、`docs/security/README.md` 和 manifest 签名安全例外为新建治理文档。审计文件名前缀分别来自原文中的审计日期 2026-07-04 和 2026-07-08。

旧审计不改写原始结论，通过顶部纠偏说明、勘误或 `Superseded` 关系说明其时效性。

### 14.3 渐进迁移顺序

1. 创建目标目录、索引和模板；
2. 创建 manifest 签名验证安全例外；
3. 归档 `.trae` 已完成规格；
4. 为旧文档增加状态和迁移说明；
5. 搜索并更新全部引用；
6. 执行实际移动；
7. 运行路径、链接和治理检查；
8. 用户批准后冻结旧位置。

## 15. 归档规则

归档路径：

```text
specs/archive/<完成年份>/<编号-功能名>/
```

迁移自旧系统且无原编号时使用 `000-` 前缀。

归档元数据必须记录：

- 原始路径和原始工具；
- 创建时间和完成时间；
- 迁移日期；
- 完成状态；
- 实现与验证证据；
- 已知遗留问题。

归档后的原始材料不可改写。勘误使用：

```text
specs/archive/<年份>/<归档规格>/errata.md
```

后续需求必须创建新的 Feature Spec，并反向链接归档规格。

## 16. 实施阶段

### 阶段 A：治理骨架

- 创建 `docs/adr/`、`docs/security/`、`docs/audits/`、`docs/history/`、`specs/archive/`；
- 创建各类模板和目录索引；
- 创建 manifest 签名验证安全例外；
- 保持 Constitution 为候选版。

### 阶段 B：项目模板

- 创建中文 Spec、Plan、Tasks、Checklist 覆盖模板；
- 创建 ADR、安全例外、归档元数据和审查记录模板；
- 验证 Spec Kit 优先解析 overrides。

### 阶段 C：历史迁移

- 归档已完成 `.trae` Spec；
- 迁移 Roadmap、旧实施计划和安全审计；
- 更新全部路径引用；
- 添加状态和替代关系。

### 阶段 D：自动检查

- 实现四个文档验证脚本；
- 建立报告模式治理 Job；
- 为验证脚本添加测试固件和失败案例。

### 阶段 E：已知缺口 Spec 化

分别创建正式 Feature Spec：

- CI 工具链一致性；
- 集成测试 CI 门禁；
- 发布签名硬门禁；
- 三平台发布产物同源；
- 测试宿主状态快照；
- 双重审查与批准记录自动验证；
- 旧 UI/下载源计划的剩余集成验收。

### 阶段 F：Constitution 正式批准

- 执行全量文档和代码事实交叉检查；
- 确认 ADR、安全例外、模板和治理脚本存在；
- 处理所有阻塞矛盾；
- 用户审阅 `1.0.0-rc.N`；
- 明确批准后升级为 `1.0.0`；
- CI 治理 Job 由报告模式切换为阻塞模式。

## 17. 体系实施验收标准

- **AC-001**：每类文档具有唯一目录、状态和权威范围。
- **AC-002**：所有项目级模板使用简体中文，代码标识和命令保留英文。
- **AC-003**：活跃 Spec 不能带未解决占位符进入 Plan。
- **AC-004**：Plan 未通过 Constitution 检查和用户批准时不能进入 Tasks。
- **AC-005**：任务没有两类独立审查记录时不能标记 `Verified`。
- **AC-006**：安全例外过期或缺少发布批准时治理检查失败。
- **AC-007**：归档规格不会被 Spec Kit 识别为活跃功能。
- **AC-008**：历史规格迁移后保留来源和实现证据，原文不可改写。
- **AC-009**：所有移动后的文档引用有效。
- **AC-010**：Backlog、Roadmap 和历史计划不能直接进入 implement。
- **AC-011**：治理 Job 在 Constitution 正式批准前为报告模式，批准后为阻塞模式。
- **AC-012**：Constitution `1.0.0` 正式批准前不存在未记录的事实冲突和执行缺口；已记录且尚未关闭的缺口必须具有责任归属、修复 Spec 或有效例外。

这些条件用于验收后续实施结果，不是本候选设计文档自我批准的前置条件。各实施阶段还应在实施计划中定义更窄的阶段验收标准。

## 18. 风险与缓解

| 风险 | 缓解措施 |
|---|---|
| 一次性移动导致链接大面积失效 | 先建立目录和状态，再分批移动并运行链接检查 |
| 自定义模板与 Spec Kit 升级冲突 | 对 Spec/Plan/Tasks 使用 `templates/overrides/`；其他治理模板由项目脚本显式消费，不修改核心模板 |
| 文档门禁过重 | Clarify 和普通 checklist 按需，核心阶段与双重审查保持强制 |
| Agent 伪造批准或审查 | 批准者固定、审查主体隔离、记录写入 Git、脚本检查完整性 |
| 历史材料继续被误用 | 增加 `Snapshot`、`Frozen`、`Archived` 状态和替代链接 |
| 自动化检查取代人工判断 | 脚本只验证证据完整性，用户批准和审查结论仍由指定角色产生 |

## 19. Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| 四层文档架构 | Confirmed for Planning | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | 治理、活跃规格、长期参考、历史记录四层结构；仅授权进入实施计划设计 |
| 状态与权威模型 | Confirmed for Planning | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | 文档状态、元数据、权威优先级和批准记录；仅授权进入实施计划设计 |
| 迁移方案 | Confirmed for Planning | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | 规格、计划、安全、审计和长期文档渐进迁移；仅授权进入实施计划设计 |
| 模板与自动化门禁 | Confirmed for Planning | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | 模板覆盖、双重审查证据、治理脚本和 CI 报告模式；仅授权进入实施计划设计 |
| 治理设计最终接受 | Pending | — | — | `1.0.0` | 待实施结果、文档体系和 Constitution 最终审阅后批准 |

## 20. 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `1.0.0-rc.1` | 2026-07-15 | Initial Candidate | 固化已确认、可进入实施计划设计的文档体系、状态、迁移和自动化门禁候选方案 | `xinyan <zuijiuhjd@foxmail.com>` |
