# XDev 项目治理模板实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

**目标：** 在 `.specify/templates/overrides/` 下创建四份项目治理模板——ADR、安全例外、归档元数据和审查记录——为后续 ADR 起草、安全例外申请、规格归档和双重审查记录提供统一的文档结构和审批证据格式。

**架构：** 本阶段只创建 Markdown 模板文件。模板使用 Constitution 要求的完整元数据、Approval History 和变更历史结构；所有模板使用简体中文，代码标识、路径和命令保留英文。不覆盖或修改 Spec Kit 核心模板，不消费或修改 `.specify/approvals/` 批准账本，不安装或运行任何自动化验证脚本。

**技术栈：** Markdown、Git、Windows PowerShell 5.1。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.3`，状态为候选版（`Ratified: Pending`），尚未正式批准。`Approval History` 中 `rc.3` 批准记录（`2026-07-16-constitution-rc3-001`）仅授权候选内容作为后续治理工作的候选基线，不构成 `1.0.0` 正式批准。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`，第 10 节"项目级模板覆盖"定义治理模板的结构要求。
- 关联路线图：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`，P2B 交付物和文件所有权定义在第 127-133 行。
- 前置依赖 P1 治理骨架必须已完成；`.specify/templates/overrides/` 目录可能尚不存在，本计划负责创建。
- 所有模板使用简体中文；代码标识、命令、路径、配置键、状态名和 Git 提交信息保留英文。
- 不直接修改 `.specify/templates/` 中的 Spec Kit 核心模板；项目覆盖只写入 `.specify/templates/overrides/`。
- 不修改 `.github/workflows/ci.yaml`、`.specify/approvals/`、`docs/governance-incidents/` 或任何现有治理文档。
- 不消费或依赖尚未创建的 P2A 交付物（`spec-template.md` 等）；ADR 模板仅包含 ADR 特定字段，不过载 Spec 审查生命周期的完整表格。
- 模板初始状态均为 `Candidate` 或 `Draft`，不得声称治理体系已生效或 Constitution 已正式批准。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 本计划必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准进入执行；批准本计划只授权创建四份模板，不授权起草具体 ADR、激活安全例外、归档规格或执行后续 P4+ 元数据治理与历史迁移。

---

## 文件结构

本计划创建：

```text
.specify/templates/overrides/adr-template.md
.specify/templates/overrides/security-exception-template.md
.specify/templates/overrides/archive-metadata-template.md
.specify/templates/overrides/review-record-template.md
```

如果 `.specify/templates/overrides/` 目录不存在，创建该目录。四份模板写入同一目录，路径所有权与路线图 P2B 白名单完全一致。

## 接口契约

### 输入

- `.specify/memory/constitution.md` `1.0.0-rc.3`：治理原则、批准规则、安全例外治理、归档规则、审查要求和元数据规范。
- `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`：ADRs（第 4.1 节）、安全例外（第 4.1 节）、归档元数据（第 15 节）、审查记录（第 11 节）和 Approval History（第 8.2 节）的结构要求。
- P1 已创建目录索引：`docs/adr/README.md`、`docs/security/exceptions/README.md`、`specs/archive/README.md`。
- P1 已创建首份安全例外草稿：`docs/security/exceptions/manifest-signature-verification.md`（作为 `security-exception-template.md` 内容的参考，但不直接复制）。

### 输出

- ADR 模板：`Proposed / Accepted / Rejected / Superseded` 四态字段、决策背景、候选方案、影响评估、审批与变更历史。
- 安全例外模板：`Draft / Active / Expired / Closed / Revoked` 五态字段、偏离条款、风险矩阵、补偿控制、逐次发布批准表、自动失效条件和审批历史。
- 归档元数据模板：来源追踪、完成状态、实现证据、遗留问题、审批与变更历史。
- 审查记录模板：审查对象、关联版本、逐条判定、阻塞/非阻塞问题清单、修复与复审历史、最终结论和审查者身份。

四份模板不得包含未解决占位符（`TODO`、`TBD`、`PLACEHOLDER`、`[NEEDS CLARIFICATION]`）、空表格单元格或默认填充值。所有表格结构完整，占位注释使用 `<!-- -->` 格式且不可见。

## Task 1：前置验证与基线记录

**文件：**

- 读取：`.specify/memory/constitution.md`
- 读取：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`
- 读取：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`
- 读取：`docs/adr/README.md`
- 读取：`docs/security/exceptions/README.md`
- 读取：`docs/security/exceptions/manifest-signature-verification.md`
- 读取：`specs/archive/README.md`
- 不修改任何文件

**接口：**

- 消费：P1 治理骨架交付物、Constitution 候选版和文档体系设计。
- 产出：可供后续任务引用的基线 commit ID 和门禁通过证据。

- [ ] **Step 1：加载用户级 Git 环境**

```powershell
$env:Path = [Environment]::GetEnvironmentVariable('Path', 'User') + ';' +
    [Environment]::GetEnvironmentVariable('Path', 'Machine')
$env:GIT_CONFIG_GLOBAL = [Environment]::GetEnvironmentVariable('GIT_CONFIG_GLOBAL', 'User')
$env:GIT_TEMPLATE_DIR = [Environment]::GetEnvironmentVariable('GIT_TEMPLATE_DIR', 'User')
```

- [ ] **Step 2：验证 Git 程序与分支**

```powershell
$gitPath = (Get-Command git).Source
if ($gitPath -ne 'D:\Env\Tools\Git\cmd\git.exe') {
    throw "Git 路径不符合要求：$gitPath"
}

$branch = git branch --show-current
if ($branch -ne 'main') {
    throw "当前分支不是 main：$branch"
}
```

预期：命令成功，无异常。

- [ ] **Step 3：验证仓库和私钥状态**

```powershell
$head = git rev-parse --verify HEAD 2>$null
if ($LASTEXITCODE -ne 0 -or -not $head) {
    throw '仓库尚无 HEAD。请先完成 Git 初始导入。'
}

$remotes = @(git remote)
if ($remotes.Count -gt 0) {
    throw "本计划不允许已配置远程仓库：$($remotes -join ', ')"
}

$ignored = git check-ignore -v -- keys/priv.key
if ($LASTEXITCODE -ne 0 -or -not $ignored) {
    throw 'keys/priv.key 未被 .gitignore 排除'
}

$trackedPrivateKey = @(git ls-files -- keys/priv.key)
if ($trackedPrivateKey.Count -gt 0) {
    throw 'keys/priv.key 已被 Git 跟踪'
}
```

预期：HEAD 存在，无远程仓库，私钥被忽略且未跟踪。

- [ ] **Step 4：验证 P1 治理骨架已交付**

```powershell
$p1Files = @(
    'docs/adr/README.md',
    'docs/security/exceptions/README.md',
    'specs/archive/README.md'
)

$p1Missing = @($p1Files | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($p1Missing.Count -gt 0) {
    $p1Missing
    throw 'P1 治理骨架尚未完成，缺少以下文件。P2B 依赖 P1 交付物。'
}
```

预期：无缺失文件，P1 交付物完整。

- [ ] **Step 5：检查目标路径不存在**

```powershell
$targetDir = '.specify/templates/overrides/'
$targetFiles = @(
    "$targetDir/adr-template.md",
    "$targetDir/security-exception-template.md",
    "$targetDir/archive-metadata-template.md",
    "$targetDir/review-record-template.md"
)

$existing = @($targetFiles | Where-Object { Test-Path -LiteralPath $_ })
if ($existing.Count -gt 0) {
    $existing
    throw '目标模板文件已存在，必须重新审计而不是覆盖'
}
```

预期：无输出，四份模板文件均不存在。

- [ ] **Step 6：记录实施基线**

```powershell
$baseCommit = git rev-parse HEAD
$baseStatus = git status --short

Write-Output "BASE_COMMIT=$baseCommit"
Write-Output 'BASE_STATUS:'
$baseStatus
```

预期：记录完整 commit ID。若工作区存在与 `.specify/templates/overrides/` 重叠的未提交修改，停止执行。

- [ ] **Step 7：确认用户已批准执行本计划**

检查当前对话或版本化批准记录中是否存在 `xinyan <zuijiuhjd@foxmail.com>` 对"执行 `2026-07-16-governance-templates.md`"的明确批准。没有明确批准时必须停止，不能把 P1 验收、路线图编写授权或"开始下一阶段"指令解释为 P2B 实施授权。

## Task 2：创建四份项目治理模板

**文件：**

- 创建：`.specify/templates/overrides/adr-template.md`
- 创建：`.specify/templates/overrides/security-exception-template.md`
- 创建：`.specify/templates/overrides/archive-metadata-template.md`
- 创建：`.specify/templates/overrides/review-record-template.md`

**接口：**

- 消费：Constitution 治理规则、设计文档模板规范和 P1 目录索引。
- 产出：可被人工流程和后续治理脚本引用的四份 Markdown 模板。

---

### Step 1：创建目录 `.specify/templates/overrides/`

```powershell
New-Item -ItemType Directory -Path '.specify/templates/overrides' -Force | Out-Null
```

预期：目录创建成功。

---

### Step 2：创建 `adr-template.md`

```markdown
# ADR-NNNN：<决策标题>

> **文档类型**：ADR
> **状态**：Proposed
> **文档版本**：`0.1.0`
> **创建日期**：YYYY-MM-DD
> **最后更新**：YYYY-MM-DD
> **ADR 编号**：NNNN
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **关联 Constitution**：`.specify/memory/constitution.md` `1.0.0-rc.3`
> **权威范围**：本 ADR 记录的架构决策、候选方案、影响和回滚计划
> **替代文档**：无

## 背景

<!-- 描述触发本决策的上下文、约束条件和需要解决的问题。 -->

## 决策

<!-- 清晰陈述做出的架构决策。 -->

## 候选方案

<!-- 列出评估过的替代方案及其优劣。 -->

## 影响与后果

<!-- 分析决策对架构、代码、测试、运维和治理的影响。 -->

## 兼容性、迁移与回滚

<!-- 说明兼容性影响、迁移步骤和回滚计划。 -->

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|

<!-- 填写示例：
| ADR 提案批准 | Accepted | `xinyan <zuijiuhjd@foxmail.com>` | YYYY-MM-DD | `0.1.0` | 批准本 ADR 以指导后续设计和实现 |
-->

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
```

---

### Step 3：创建 `security-exception-template.md`

```markdown
# <例外标题>

> **文档类型**：安全例外
> **状态**：Draft
> **文档版本**：`0.1.0`
> **创建日期**：YYYY-MM-DD
> **最后更新**：YYYY-MM-DD
> **责任人**：`xinyan <zuijiuhjd@foxmail.com>`
> **批准者**：待用户明确批准
> **关联 Constitution**：`.specify/memory/constitution.md` `1.0.0-rc.3`
> **权威范围**：<例外 ID 所覆盖的临时安全偏离>
> **替代文档**：无

## 偏离条款

<!-- 列出本例外暂时偏离的 Constitution 或 Policy 具体条款。 -->

## 当前事实

<!-- 客观描述当前代码、配置、工作流中的技术事实，不添加主观判断。 -->

## 风险

<!-- 分析偏离带来的安全风险和实际影响。 -->

## 影响范围

<!-- 列出受影响的组件、流程、文档和发布工件。 -->

## 禁止扩大边界

<!-- 明确禁止在例外期间新增或放宽的安全行为。 -->

## 临时补偿控制

<!-- 描述在正式修复前采取的临时安全措施。 -->

## 修复计划

<!-- 概述修复方案和所需步骤。不授权在此模板中直接实施代码变更。 -->

## 有效期与自动失效

- 目标版本：<!-- 填写符合 SemVer 的目标版本 -->
- 截止日期：YYYY-MM-DD
- 自动失效：目标版本与截止日期任一先到即失效
- 失效后行为：<!-- 例外失效后应采取的行动 -->
- 续期：必须重新评估风险、设置新目标版本和日期，并重新获得用户批准，不能继承旧批准

## 逐次发布批准记录

| 发布版本 | 日期 | 签名文件确认 | 私钥可用 | 公钥注入 | 批准者 | 结论 |
|---|---|---|---|---|---|---|

<!-- 填写示例：
| v0.1.0 | YYYY-MM-DD | 已确认 | 已确认 | 已确认 | `xinyan <zuijiuhjd@foxmail.com>` | 批准发布 |
初始 Draft 状态：当前无批准记录。`Draft` 状态不能用于发布。
-->

## 关闭条件

<!-- 列举例外关闭必须满足的条件。 -->

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|

<!-- 填写示例：
| 例外草案创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | YYYY-MM-DD | `0.1.0` | 创建例外草案，记录风险和控制；不授权发布 |
| 例外激活 | Active | `xinyan <zuijiuhjd@foxmail.com>` | YYYY-MM-DD | `0.1.0` | 批准例外生效，允许在补偿控制下发布 |
初始 Draft 状态：当前没有批准记录。草案创建事实记录在"变更历史"中；`Draft` 状态不能用于发布。
-->

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
```

---

### Step 4：创建 `archive-metadata-template.md`

```markdown
# 归档元数据：<编号-功能名>

> **文档类型**：归档元数据
> **状态**：Snapshot
> **文档版本**：`0.1.0`
> **创建日期**：YYYY-MM-DD
> **最后更新**：YYYY-MM-DD
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **权威范围**：本规格的归档事实、来源、状态和遗留问题记录
> **替代文档**：无

## 来源信息

| 字段 | 值 |
|---|---|
| 原始路径 | <!-- 迁移前在仓库中的路径 --> |
| 原始工具 | <!-- 创建该规格时使用的工具（如 Spec Kit、Trae） --> |
| 原始编号 | <!-- 旧系统中的编号或标识 --> |

## 时间信息

| 字段 | 值 |
|---|---|
| 规格创建时间 | YYYY-MM-DD |
| 完成时间 | YYYY-MM-DD |
| 迁移日期 | YYYY-MM-DD |

## 完成状态

<!-- 描述实现完成情况：已通过验收 / 部分完成 / 被替代。 -->

## 实现与验证证据

<!-- 列出实现结果、测试证据、审查记录和验收证据。 -->

## 已知遗留问题

<!-- 记录归档时尚未解决的问题、技术债务或后续改进建议。 -->

## 勘误

<!-- 发现历史错误、失效链接或状态偏差时，追加到此区域。每条勘误包含日期、原文位置、问题说明、正确事实和证据引用。
满足以下条件时本区域留空：未发现需要勘误的错误。
-->

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|

<!-- 填写示例：
| 归档确认 | Archived | `xinyan <zuijiuhjd@foxmail.com>` | YYYY-MM-DD | `0.1.0` | 批准归档并确认元数据完整 |
-->

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
```

---

### Step 5：创建 `review-record-template.md`

```markdown
# 审查记录：<任务编号>-<审查类型>

> **文档类型**：审查记录
> **状态**：Draft
> **文档版本**：`0.1.0`
> **创建日期**：YYYY-MM-DD
> **最后更新**：YYYY-MM-DD
> **关联功能**：`specs/<编号-功能名>/`
> **审查者**：<!-- 审查者身份，不得是任务实现者 -->
> **权威范围**：本记录的审查结论、发现问题和修复验证证据

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | <!-- 如 T001 --> |
| 审查类型 | <!-- Spec 合规审查 / 工程质量审查 --> |
| 审查范围 | <!-- 描述审查覆盖的 diff、文件和关注点 --> |
| 关联 Spec 版本 | <!-- 如 `0.2.0` --> |
| 关联 Plan 版本 | <!-- 如 `0.1.0` --> |

## 逐条判定

<!-- 逐条列出审查点及其判定。

填写示例（Spec 合规审查）：
| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001 实现对应 | 通过 | `src/handler.go:45-67` 实现了需求描述的行为 |
| 未超范围实现 | 通过 | diff 中无超出 Spec 范围的新增功能 |

填写示例（工程质量审查）：
| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| 测试证据 | 通过 | `go test ./...` 通过，覆盖率为 82% |
| 主机状态隔离 | 通过 | 测试使用临时目录，未修改注册表或环境变量 |
| 跨平台兼容 | 通过 | Windows 聚焦测试通过，Linux/macOS 编译通过 |
-->

## 验证证据

<!-- 列出运行验证命令、输出摘要或引用 CI 日志。

填写示例：
```text
> go test -count=1 ./internal/...
ok      internal/downloader    1.234s
```
-->

## 阻塞问题

<!-- 列出必须修复后才能通过审查的阻塞问题。无阻塞时间时明确写"无"。 -->

| 编号 | 问题描述 | 严重程度 | 建议修复方式 |
|---|---|---|---|

## 非阻塞问题

<!-- 列出建议修复但不阻塞审查通过的问题。无非阻塞问题时明确写"无"。 -->

| 编号 | 问题描述 | 严重程度 | 建议修复方式 |
|---|---|---|---|

## 修复与复审历史

| 版本 | 日期 | 修复内容 | 复审结论 | 审查者 |
|---|---|---|---|---|

<!-- 无修复和复审历史时写"无"。 -->

## 最终结论

<!-- 审查最终结论：通过 / 不通过 / 有条件通过（附条件说明）。

Agent 的审查结论不能代替用户批准。 -->
- **审查结论**：<!-- 通过 / 不通过 / 有条件通过 -->
- **结论日期**：YYYY-MM-DD
- **审查者**：<!-- 审查者身份 -->

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|

<!-- 填写示例：
| 审查完成确认 | Confirmed | `xinyan <zuijiuhjd@foxmail.com>` | YYYY-MM-DD | `0.1.0` | 确认审查结论有效 |
-->

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
```

---

### Step 6：验证模板文件完整性

```powershell
$targetDir = '.specify\templates\overrides'
$targetFiles = @(
    "$targetDir\adr-template.md",
    "$targetDir\security-exception-template.md",
    "$targetDir\archive-metadata-template.md",
    "$targetDir\review-record-template.md"
)

foreach ($file in $targetFiles) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "缺少文件：$file"
    }
    $content = Get-Content -LiteralPath $file -Raw
    if ([string]::IsNullOrWhiteSpace($content)) {
        throw "空文件：$file"
    }
}
```

预期：命令无输出、无异常。

## Task 3：聚焦验证

**文件：**

- 验证：本计划创建的全部 4 个 Markdown 模板文件
- 不创建新文件

**接口：**

- 消费：Task 2 的全部交付物。
- 产出：模板结构、元数据、占位符和关键字段验证证据。

- [ ] **Step 1：扫描占位符和空表格单元格**

```powershell
$targetDir = '.specify\templates\overrides'
$targetFiles = @(
    "$targetDir\adr-template.md",
    "$targetDir\security-exception-template.md",
    "$targetDir\archive-metadata-template.md",
    "$targetDir\review-record-template.md"
)

$problems = @()
foreach ($file in $targetFiles) {
    $content = Get-Content -LiteralPath $file -Raw

    # 禁止可见占位符
    if ($content -match '\b(TODO|TBD|PLACEHOLDER|NEEDS CLARIFICATION)\b') {
        $problems += "$file：包含可见占位符"
    }
}

if ($problems.Count -gt 0) {
    $problems
    throw '模板内容验证失败：可见占位符'
}
```

预期：无输出。注意：`<!-- -->` 中的注释（包括 `YYY-MM-DD` 标记）是合法模板注释，不视为占位符。

- [ ] **Step 2：验证通用元数据强制字段**

每份模板必须包含以下必需元数据行：

```powershell
$requiredMetadata = @(
    '**文档类型**',
    '**状态**',
    '**文档版本**',
    '**创建日期**',
    '**最后更新**',
    '**所有者**',
    '**权威范围**'
)

foreach ($file in $targetFiles) {
    $content = Get-Content -LiteralPath $file -Raw
    foreach ($field in $requiredMetadata) {
        if ($content -notmatch [regex]::Escape($field)) {
            throw "$file 缺少必需元数据字段：$field"
        }
    }
}
```

预期：命令无输出、无异常。

- [ ] **Step 3：验证 Approval History 和变更历史表存在**

```powershell
foreach ($file in $targetFiles) {
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -notmatch 'Approval History') {
        throw "$file 缺少 Approval History 部分"
    }
    if ($content -notmatch '变更历史') {
        throw "$file 缺少变更历史部分"
    }
    # 验证表头存在
    if ($content -notmatch '\|\s*阶段\s*\|.*\|\s*批准范围\s*\|') {
        throw "$file Approval History 表头格式不正确"
    }
    if ($content -notmatch '\|\s*版本\s*\|.*\|\s*批准者\s*\|') {
        throw "$file 变更历史表头格式不正确"
    }
}
```

预期：命令无输出、无异常。

- [ ] **Step 4：验证各模板类型特有字段**

```powershell
$typeSpecificChecks = @{
    '.specify\templates\overrides\adr-template.md' = @(
        '**ADR 编号**',
        '## 背景',
        '## 决策',
        '## 候选方案',
        '**替代文档**'
    )
    '.specify\templates\overrides\security-exception-template.md' = @(
        '## 偏离条款',
        '## 风险',
        '## 禁止扩大边界',
        '## 临时补偿控制',
        '## 有效期与自动失效',
        '## 逐次发布批准记录',
        '## 关闭条件'
    )
    '.specify\templates\overrides\archive-metadata-template.md' = @(
        '## 来源信息',
        '## 完成状态',
        '## 实现与验证证据',
        '## 已知遗留问题',
        '## 勘误'
    )
    '.specify\templates\overrides\review-record-template.md' = @(
        '## 审查对象',
        '## 逐条判定',
        '## 验证证据',
        '## 阻塞问题',
        '## 非阻塞问题',
        '## 修复与复审历史',
        '## 最终结论'
    )
}

foreach ($entry in $typeSpecificChecks.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    foreach ($field in $entry.Value) {
        if ($content -notmatch [regex]::Escape($field)) {
            throw "$($entry.Key) 缺少类型特有字段：$field"
        }
    }
}
```

预期：命令无输出、无异常。

- [ ] **Step 5：验证安全例外模板不包含伪造激活证据**

```powershell
$exceptionTemplate = Get-Content -LiteralPath '.specify\templates\overrides\security-exception-template.md' -Raw

if ($exceptionTemplate -match '\*\*状态\*\*：Active') {
    throw '安全例外模板不得将初始状态设为 Active'
}
if ($exceptionTemplate -notmatch '`Draft` 状态不能用于发布') {
    throw '安全例外模板必须说明 Draft 不能用于发布'
}
if ($exceptionTemplate -notmatch '当前无批准记录') {
    throw '安全例外模板必须说明草案状态下无批准记录'
}
```

预期：命令无输出、无异常。

- [ ] **Step 6：验证 ADR 模板的合法状态列表**

```powershell
$adrTemplate = Get-Content -LiteralPath '.specify\templates\overrides\adr-template.md' -Raw
if ($adrTemplate -notmatch '\*\*状态\*\*：Proposed') {
    throw 'ADR 模板初始状态必须为 Proposed'
}
```

预期：命令无输出、无异常。

- [ ] **Step 7：检查 Markdown 行尾空白**

```powershell
$whitespaceProblems = @()
foreach ($file in $targetFiles) {
    $lineNumber = 0
    foreach ($line in (Get-Content -LiteralPath $file)) {
        $lineNumber++
        if ($line -match '[ \t]+$') {
            $whitespaceProblems += "${file}:${lineNumber}"
        }
    }
}
if ($whitespaceProblems.Count -gt 0) {
    $whitespaceProblems
    throw '模板文件存在行尾空白'
}
```

预期：无输出。

## Task 4：双重独立审查与提交

**文件：**

- 审查：本计划创建的全部文件
- 提交：仅本计划文件白名单

**接口：**

- 消费：完整模板差异和验证证据。
- 产出：两个独立审查结论、用户验收和一个本地 Git 提交。

- [ ] **Step 1：重新声明白名单并生成可审查的暂存差异**

```powershell
$targetDir = '.specify/templates/overrides'
$expected = @(
    "$targetDir/adr-template.md",
    "$targetDir/security-exception-template.md",
    "$targetDir/archive-metadata-template.md",
    "$targetDir/review-record-template.md"
)

if ($expected.Count -ne 4) {
    throw "模板白名单数量错误：$($expected.Count)"
}

$baseCommit = git rev-parse HEAD
git add -- $expected
git status --short
git diff --cached --check
git diff --cached --stat
git diff --cached -- $expected
```

保存 `$baseCommit`，并将 `git diff --cached -- $expected` 输出作为两个审查者的差异范围。审查返工后重新执行本步骤更新暂存差异。

- [ ] **Step 2：执行 Spec 合规审查**

使用全新的独立 Agent 会话执行审查，不复用实现者会话。审查者必须独立于实现者，读取：

```text
.specify/memory/constitution.md
docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md
docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md
docs/superpowers/plans/2026-07-16-governance-templates.md
本计划产生的差异
```

必须逐项判定：

- 四份模板是否全部符合 P2B 路径所有权（`.specify/templates/overrides/`）；
- 是否直接修改了 `.specify/templates/` 核心模板；
- 每份模板是否包含 Constitution 第 8.2 节要求的 Approval History 表；
- 每份模板是否包含文档体系设计第 6 节定义的通用元数据（文档类型、状态、文档版本、创建日期、最后更新、所有者、权威范围）；
- 安全例外模板是否保持 Draft 初始状态，不模拟 Active 或发布批准；
- 模板是否错误声称治理体系已生效或 Constitution 已正式批准；
- 归档元数据模板是否区分了不可变区域和勘误追加空间。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 3：执行工程质量审查**

使用另一个全新的独立 Agent 会话执行审查，不复用实现者或 Spec 合规审查者会话。第二审查者必须不同于实现者和 Spec 合规审查者，检查：

- 模板结构是否一致，四份模板的元数据区块、Approval History 和变更历史格式是否对齐；
- 中文使用是否规范，代码标识、命令、路径是否保留英文；
- 是否存在敏感数据、不可验证声明或过度规定实现细节；
- 合法状态列表是否与 Constitution rc.3、设计文档和 P1 目录索引一致；
- 模板注释是否清晰说明填写规则，同时不引入执行歧义；
- 是否存在对尚未创建的 P4+、P5 或 P6 交付物的硬依赖。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 4：请求用户验收**

向用户提供：

- 创建文件清单（四份模板）；
- 聚焦验证结果（Task 3 全部通过）；
- Spec 合规审查结论；
- 工程质量审查结论；
- 明确说明四份模板均为初始候选状态，Constitution 仍为 `1.0.0-rc.3` `Ratified: Pending`。

只有用户明确回复批准 P2B 验收或含义同等明确的指令后才能继续提交。泛化的"继续"不得解释为验收批准。

- [ ] **Step 5：按白名单暂存**

```powershell
git add -- $expected

if ($expected.Count -ne 4) {
    throw "模板白名单数量错误：$($expected.Count)"
}

$staged = @(git diff --cached --name-only)
$unexpected = @($staged | Where-Object { $_ -notin $expected })
if ($unexpected.Count -gt 0) {
    $unexpected
    throw '暂存区包含本计划范围外文件'
}

git diff --cached --check
git diff --cached --stat
```

预期：暂存区只包含四份治理模板文件，且行尾空白检查通过。

- [ ] **Step 6：创建提交**

```powershell
git commit -m "docs: add project governance templates (ADR, security exception, archive metadata, review record)"
git log --oneline -1
```

预期：提交成功。

- [ ] **Step 7：提交后检查**

```powershell
git status --short --branch
git show --stat --oneline HEAD
git ls-files -- keys/priv.key
```

预期：

- 提交包含且仅包含四份治理模板；
- `keys/priv.key` 查询无输出；
- 不存在本计划产生的未提交文件。

## 验收标准

- **AC-001**：四份模板文件全部存在于 `.specify/templates/overrides/`。
- **AC-002**：每份模板包含完整通用元数据（文档类型、状态、文档版本、创建日期、最后更新、所有者、权威范围）。
- **AC-003**：每份模板包含 Constitution 要求的 Approval History 和变更历史表，表头格式正确。
- **AC-004**：ADR 模板包含决策背景、候选方案、影响评估和回滚计划。
- **AC-005**：安全例外模板包含偏离条款、风险、补偿控制、双因子自动失效和逐次发布批准记录表，初始状态为 Draft 且声明 Draft 不能用于发布。
- **AC-006**：归档元数据模板包含来源追踪、完成状态、实现证据、遗留问题和勘误追加空间。
- **AC-007**：审查记录模板包含审查对象、关联版本、逐条判定、阻塞/非阻塞问题、修复复审历史和最终结论，并明确审查者不得是任务实现者。
- **AC-008**：四份模板不含可见占位符（`TODO`、`TBD`、`PLACEHOLDER`、`NEEDS CLARIFICATION`）。
- **AC-009**：不修改 `.specify/templates/` 核心模板、`.github/workflows/ci.yaml` 和任何现有治理文档。
- **AC-010**：`keys/priv.key` 未被跟踪或暂存，不进入提交。
- **AC-011**：两个独立审查门禁和用户验收均通过后才创建提交。
- **AC-012**：Constitution 保持 `1.0.0-rc.3` `Ratified: Pending`，模板不声称治理自动化已生效。

## 失败恢复

### 提交前

如果暂存了错误文件：

```powershell
git restore --staged -- <path>
```

如果新文件内容错误，使用 `apply_patch` 修正；不要删除或还原用户的并发修改。

### 提交后

如果提交后发现非敏感遗漏，创建后续修复提交，不自动 amend。

如果私钥或其他凭据进入提交，立即停止，不配置 remote、不推送，并启动独立密钥事件响应方案。

## 不授权事项

本计划只授权创建四份治理模板。以下工作必须通过独立的后续计划或用户明确指令执行，P2B 不附带授权：

- **P4A 元数据治理**：为旧文档添加状态、来源和替代关系元数据。
- **P4B 历史文档迁移**：移动 `.trae` 规格、旧审计、旧实施计划到归档目录。
- **P5 治理自动化**：创建 `validate-governance.ps1` 等验证脚本、CI governance job 或 pre-commit hook。
- **P6 已知缺口 Spec 化**：创建七个 Feature Spec。
- **P7 Constitution 正式批准**：将 Constitution 升级为 `1.0.0`。
- **起草具体 ADR、激活安全例外、归档任何规格或为任何任务生成审查记录**。
- **修改 `.specify/approvals/` 或消费批准账本**。
- **安装 Git hooks、改变 `core.hooksPath` 或运行 `validate-approvals.ps1`**。

验收 P2B 后，后续并行子项目可启动执行（P2A 尚未完成的模板覆盖同理）：

- P4A 依赖 P2B 和 P3；
- P5 依赖 P2A、P2B 和 P3。

## 后续计划

P2B 完成后，以下子项目可以继续推进：

- `2026-07-15-speckit-template-overrides.md`（P2A）：中文 Spec、Plan、Tasks、Checklist 模板覆盖。
- `2026-07-15-document-link-validation.md`（P3）：Markdown 路径和锚点验证脚本。
- `2026-07-15-legacy-document-metadata.md`（P4A）：旧文档元数据治理（依赖 P2B 和 P3）。

本计划不授权执行上述后续工作。
