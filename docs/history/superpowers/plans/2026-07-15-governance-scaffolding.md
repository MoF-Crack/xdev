# XDev 治理骨架实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

**目标：** 创建 XDev 四层文档体系所需的治理目录、索引和首份 manifest 签名验证安全例外，为后续模板、迁移和治理自动化提供稳定目录契约。

**架构：** 本阶段只新增 Markdown 文件，不移动或改写任何现有文档。每个目录使用 `README.md` 声明职责、合法内容和禁止事项；安全例外将 Constitution 中的临时规则固化为可追踪文档，但保持 `Draft`，等待用户单独批准后才可转为 `Active`。

**技术栈：** Markdown、Git、Windows PowerShell 5.1、GitHub Spec Kit 0.12.15。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.2`，状态为候选版，尚未正式批准。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`。
- 本计划必须在 `docs/history/implementation-plans/git-initial-import-plan.md` 定义的 Git 初始导入完成后执行。
- 当前分支必须为 `main`，必须存在可用 `HEAD`，且不得配置远程仓库或执行推送。
- 所有新文档使用简体中文；命令、路径、配置键、状态名和代码标识保留英文。
- 不修改 `.specify/templates/`、`.github/workflows/ci.yaml`、现有安全审计、旧计划和 `.trae/` 文件。
- 不创建完整 `signing-policy.md`；其内容需要后续独立治理计划和用户决策。
- 安全例外初始状态必须为 `Draft`，不得由 Agent 自行改为 `Active`。
- 例外失效条件固定为：首个无预发布标识的 SemVer 正式版本或 2026-09-01，以先到者为准。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 本计划必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准进入执行；“开始下一阶段”只授权编写计划，不等同于批准执行 P1。

---

## 文件结构

本计划创建：

```text
docs/README.md
docs/adr/README.md
docs/security/README.md
docs/security/policy/README.md
docs/security/procedures/README.md
docs/security/exceptions/README.md
docs/security/exceptions/manifest-signature-verification.md
docs/audits/README.md
docs/history/README.md
docs/history/implementation-plans/README.md
specs/README.md
specs/archive/README.md
```

不创建空目录；每个目录均由索引文件进入版本库。

## 接口契约

### 输入

- `.specify/memory/constitution.md`：治理原则、例外失效条件和批准规则。
- `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`：目录架构和文档职责。
- `docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`：子项目边界和文件所有权。
- `docs/audits/2026-07-08-security-audit-verification.md`、`internal/signer/verify.go`、`.github/workflows/ci.yaml`：签名验证当前事实。

### 输出

- 后续计划可以依赖的固定治理目录。
- 一份初始状态为 `Draft` 的 manifest 签名验证安全例外。
- 清晰区分政策、操作流程、安全例外、审计、历史计划、活跃规格和归档规格的目录索引。

## Task 1：执行前门禁和基线记录

**文件：**

- 读取：`.specify/memory/constitution.md`
- 读取：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`
- 读取：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`
- 不修改文件

**接口：**

- 消费：Git 初始导入结果和当前仓库状态。
- 产出：可供后续任务引用的基线 commit ID 和门禁证据。

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

- [ ] **Step 3：验证 Git 初始导入已经完成**

```powershell
$head = git rev-parse --verify HEAD 2>$null
if ($LASTEXITCODE -ne 0 -or -not $head) {
    throw '仓库尚无 HEAD。请先执行 docs/history/implementation-plans/git-initial-import-plan.md。'
}

$commitCount = [int](git rev-list --count HEAD)
if ($commitCount -lt 4) {
    throw "初始导入提交不足 4 个：$commitCount"
}
```

预期：`$commitCount` 至少为 4。

- [ ] **Step 4：验证远程仓库和私钥状态**

```powershell
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

预期：无远程仓库，私钥被忽略且未跟踪。

- [ ] **Step 5：检查目标路径不存在**

```powershell
$targets = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/security/exceptions/README.md',
    'docs/security/exceptions/manifest-signature-verification.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md'
)

$existing = @($targets | Where-Object { Test-Path -LiteralPath $_ })
if ($existing.Count -gt 0) {
    $existing
    throw '目标文件已存在，必须重新审计而不是覆盖'
}
```

预期：无输出。

- [ ] **Step 6：记录实施基线**

```powershell
$baseCommit = git rev-parse HEAD
$baseStatus = git status --short

Write-Output "BASE_COMMIT=$baseCommit"
Write-Output 'BASE_STATUS:'
$baseStatus
```

预期：记录完整 commit ID。若工作区存在与本计划目标路径重叠的修改，停止执行。

- [ ] **Step 7：确认用户已批准执行本计划**

检查当前对话或版本化批准记录中是否存在 `xinyan <zuijiuhjd@foxmail.com>` 对“执行 `2026-07-15-governance-scaffolding.md`”的明确批准。没有明确批准时必须停止，不能把设计确认或计划编写授权解释为实施授权。

## Task 2：创建文档入口与治理目录索引

**文件：**

- 创建：`docs/README.md`
- 创建：`docs/adr/README.md`
- 创建：`docs/audits/README.md`
- 创建：`docs/history/README.md`
- 创建：`docs/history/implementation-plans/README.md`

**接口：**

- 消费：四层文档架构。
- 产出：文档入口、ADR、审计和历史记录目录契约。

- [ ] **Step 1：创建 `docs/README.md`**

```markdown
# XDev 文档索引

> **文档类型**：长期参考文档
> **状态**：Living
> **权威范围**：XDev 文档分类、入口和职责边界

## 文档分层

| 层级 | 目录 | 职责 |
|---|---|---|
| 治理约束 | `.specify/memory/constitution.md`、`docs/adr/`、`docs/security/exceptions/` | 定义必须遵守的全局规则、架构决策和临时安全偏离 |
| 活跃规格 | `specs/<编号-功能名>/` | 定义当前功能的目标、计划、任务、审查和验收 |
| 长期参考 | `docs/` | 解释当前系统的架构、命令、操作、测试和路线图 |
| 历史记录 | `specs/archive/`、`docs/history/`、`docs/audits/` | 保存已完成规格、旧计划和时间点审计 |

## 权威边界

- Constitution 正式批准后是最高工程治理约束。
- 用户批准的活跃 `spec.md` 是对应功能的任务级需求权威源。
- 长期参考文档必须随代码事实更新，但不能覆盖 Constitution 或已批准 Spec。
- Backlog、Roadmap、历史计划和审计不能直接驱动实现。

## 主要入口

- 架构：`docs/architecture.md`
- 技术参考：`docs/technical-reference.md`
- 操作指南：`docs/operations.md`
- 测试策略：`docs/test-plan.md`
- ADR：`docs/adr/`
- 安全治理：`docs/security/`
- 时间点审计：`docs/audits/`
- 历史实施记录：`docs/history/`
- 活跃功能规格：`specs/`

## 语言规则

Constitution、ADR、安全例外和 Feature Spec 使用简体中文。函数名、类型名、CLI 命令、路径、配置键、日志原文和 Git 提交信息保留英文。

## 当前状态

文档体系处于候选治理实施阶段。现有文件将在后续计划中分批添加元数据和迁移；本索引不代表迁移已经完成。
```

- [ ] **Step 2：创建 `docs/adr/README.md`**

```markdown
# Architecture Decision Records

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 长期架构决策的记录规则

## 用途

ADR 记录具有长期影响的架构决策、候选方案、权衡、兼容性、迁移和回滚计划。

改变 Constitution 中的当前架构强约束时，必须先创建 ADR，并同时修订 Constitution、更新受影响 Spec/Plan、获得 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。

## 文件命名

```text
docs/adr/NNNN-<decision>.md
```

- `NNNN` 使用四位递增编号，从 `0001` 开始。
- `<decision>` 使用小写 kebab-case。
- 被替代的 ADR 不删除，状态改为 `Superseded` 并链接替代 ADR。

## 合法状态

```text
Proposed
Accepted
Rejected
Superseded
```

## 批准规则

- Agent 可以起草 ADR，但不能自行将状态改为 `Accepted`。
- `Accepted` 必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。
- ADR 与 Ratified Constitution 冲突时不生效。
```

- [ ] **Step 3：创建 `docs/audits/README.md`**

```markdown
# XDev 审计与时间点报告

> **文档类型**：历史索引
> **状态**：Living
> **权威范围**：安全、质量和工程审计的归档规则

## 用途

本目录保存基于特定日期、代码基线和工具版本生成的审计报告。审计报告描述时间点事实，不永久代表当前状态。

## 文件命名

```text
docs/audits/YYYY-MM-DD-<audit-name>.md
```

## 必需元数据

- 状态：`Snapshot` 或 `Superseded`；
- 审计日期；
- 代码基线 commit；
- 审计范围与方法；
- 工具版本；
- 替代或纠偏文档。

## 不可变规则

- 不改写原始审计结论以匹配新代码。
- 后续事实通过顶部纠偏说明、勘误或新的审计报告记录。
- 审计与当前代码冲突时，以当前代码、测试和配置事实判断参考文档是否过时。
```

- [ ] **Step 4：创建 `docs/history/README.md`**

```markdown
# XDev 历史文档

> **文档类型**：历史索引
> **状态**：Living
> **权威范围**：旧计划、一次性执行记录和已替代文档的保存规则

## 用途

本目录保存不再作为活跃规范源、但仍具有追溯价值的文档。

## 规则

- 历史文档不得直接驱动 Agent 实现。
- 未完成需求必须提炼为新的 `specs/<编号-功能名>/spec.md`。
- 旧文档保留原始内容，并增加来源、冻结日期和替代文档信息。
- 发现历史错误时追加勘误，不通过重写伪造原始状态。

## 子目录

- `implementation-plans/`：已经执行、被替代或冻结的一次性实施计划。
```

- [ ] **Step 5：创建 `docs/history/implementation-plans/README.md`**

```markdown
# 历史实施计划

> **文档类型**：历史索引
> **状态**：Living
> **权威范围**：已执行、被替代或冻结的实施计划归档规则

## 归档条件

实施计划满足以下任一条件时可迁入本目录：

- 对应实施已经完成并通过验收；
- 计划被新的 Spec/Plan 明确替代；
- 计划被用户明确冻结，不再继续执行。

## 归档要求

- 保留原始计划内容；
- 添加原始路径、状态、冻结日期和替代文档；
- 未完成工作必须创建新的 Feature Spec，不能继续修改已冻结计划；
- 迁移后必须更新仓库内所有有效引用。
```

- [ ] **Step 6：验证新索引内容**

```powershell
$files = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md'
)

foreach ($file in $files) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "缺少文件：$file"
    }
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -match '\b(TODO|TBD|PLACEHOLDER|NEEDS CLARIFICATION)\b') {
        throw "文件包含占位符：$file"
    }
}
```

预期：命令无输出、无异常。

## Task 3：创建安全治理索引

**文件：**

- 创建：`docs/security/README.md`
- 创建：`docs/security/policy/README.md`
- 创建：`docs/security/procedures/README.md`
- 创建：`docs/security/exceptions/README.md`

**接口：**

- 消费：Constitution 的安全与信任边界和安全例外治理规则。
- 产出：安全政策、操作流程和临时例外之间的职责边界。

- [ ] **Step 1：创建 `docs/security/README.md`**

```markdown
# XDev 安全治理

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 安全政策、操作流程和临时例外的分类入口

## 目录

| 目录 | 职责 |
|---|---|
| `policy/` | 长期安全规则和发布约束 |
| `procedures/` | 密钥管理、签名、恢复等操作步骤 |
| `exceptions/` | 有期限、可撤销、需批准的临时安全偏离 |
| `../audits/` | 基于特定代码基线的时间点安全审计 |

## 权威关系

- Ratified Constitution 的安全原则优先于本目录所有文档。
- Policy 定义长期规则，Procedure 说明如何执行，Exception 只允许临时偏离。
- 审计报告不能自动创建、续期或批准安全例外。
- 安全例外不能将风险描述为永久修复。

## 当前迁移状态

现有 `docs/security/policy/download-source-policy.md`、`docs/security/procedures/signing-guide.md`、`docs/security/procedures/download-cache-recovery.md` 和安全审计将在后续迁移计划中处理。本阶段不移动这些文件。
```

- [ ] **Step 2：创建 `docs/security/policy/README.md`**

```markdown
# 安全政策

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 长期安全政策的存放和批准规则

## 规则

- Policy 描述必须长期成立的安全约束，不包含一次性操作步骤。
- Policy 不得放宽 Ratified Constitution。
- 新建或语义修改 Policy 必须经过独立安全审查和用户批准。
- 临时偏离 Policy 必须创建 `docs/security/exceptions/<exception-id>.md`。

## 计划迁入

- `download-source-policy.md`：下载来源、传输、重定向、校验和发布门禁。
- `signing-policy.md`：manifest 签名、根公钥、密钥角色和发布约束。该文件将在后续独立计划中创建。
```

- [ ] **Step 3：创建 `docs/security/procedures/README.md`**

```markdown
# 安全操作流程

> **文档类型**：长期参考索引
> **状态**：Living
> **权威范围**：执行既有安全政策的操作步骤

## 规则

- Procedure 说明如何完成密钥生成、签名、验证、恢复和发布检查。
- Procedure 不能改变 Policy 或 Constitution 的安全要求。
- 命令、路径和配置值必须经过实际验证。
- 涉及私钥的示例只能使用占位符或临时测试密钥，不能记录真实凭据。

## 计划迁入

- `signing-guide.md`；
- `download-cache-recovery.md`。
```

- [ ] **Step 4：创建 `docs/security/exceptions/README.md`**

```markdown
# 安全例外

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：临时安全偏离的格式、批准和失效规则

## 合法状态

```text
Draft
Active
Expired
Closed
Revoked
```

## 必需内容

每份安全例外必须记录：

- 偏离的 Constitution 或 Policy 条款；
- 风险和影响范围；
- 禁止扩大的边界；
- 补偿控制和修复计划；
- 责任人、批准者和逐次发布批准记录；
- 目标版本、截止日期和自动失效条件；
- 关闭证据或续期重新评估记录。

## 批准规则

- Agent 只能创建 `Draft`，不能自行激活、续期或关闭例外。
- `Active`、续期和关闭必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。
- 到期例外自动变为 `Expired`，不得继续用于发布。
```

- [ ] **Step 5：验证安全治理索引**

```powershell
$files = @(
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/security/exceptions/README.md'
)

foreach ($file in $files) {
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -notmatch '\*\*状态\*\*') {
        throw "缺少状态元数据：$file"
    }
    if ($content -match '\b(TODO|TBD|PLACEHOLDER|NEEDS CLARIFICATION)\b') {
        throw "文件包含占位符：$file"
    }
}
```

预期：命令无输出、无异常。

## Task 4：创建活跃与归档规格索引

**文件：**

- 创建：`specs/README.md`
- 创建：`specs/archive/README.md`

**接口：**

- 消费：Spec Kit 目录规则和 Constitution 生命周期。
- 产出：活跃规格与归档规格的不可混用边界。

- [ ] **Step 1：创建 `specs/README.md`**

```markdown
# XDev Feature Specs

> **文档类型**：规范索引
> **状态**：Living
> **权威范围**：活跃 Feature Spec 的目录、生命周期和批准规则

## 活跃规格目录

```text
specs/<编号-功能名>/
```

- 编号使用三位或更多数字，如 `001-`、`002-`；
- 功能名使用小写 kebab-case；
- 活跃目录由 Spec Kit 创建和定位；
- `specs/archive/` 不属于活跃功能目录。

## 标准文件

```text
spec.md
plan.md
tasks.md
checklist.md
research.md
data-model.md
quickstart.md
contracts/
reviews/
```

只有 `spec.md` 是任务级需求权威源。Plan 和 Tasks 不得改变 Spec 语义。

## 生命周期

```text
specify
→ 用户批准 Spec
→ clarify（按需）
→ plan
→ Constitution 合规审查
→ 用户批准 Plan
→ tasks
→ analyze
→ implement
→ 双重独立审查 LOOP
→ converge
→ 用户批准验收
→ 用户批准归档
```

## 禁止事项

- Backlog、Roadmap、旧计划和归档规格不能直接进入 implement。
- Agent 不得代填用户批准。
- 未解决占位符、未批准例外或未接受 ADR 不得进入实施。
- `.specify/feature.json` 不得指向 `specs/archive/`。
```

- [ ] **Step 2：创建 `specs/archive/README.md`**

```markdown
# XDev 规格归档

> **文档类型**：历史索引
> **状态**：Living
> **权威范围**：完成规格的目录、不可变和勘误规则

## 目录

```text
specs/archive/<完成年份>/<编号-功能名>/
```

迁移自旧系统且没有原始 Spec Kit 编号的规格使用 `000-` 前缀。

## 归档前置条件

- 实现状态为 `Accepted`；
- 用户明确批准 `Accepted → Archived`；
- 归档元数据记录来源、完成时间、迁移日期、实现证据和遗留问题；
- 活跃功能指针不再指向该目录。

## 不可变规则

归档后的 `spec.md`、`plan.md`、`tasks.md`、`checklist.md` 和原始验证材料不可改写。

发现历史错误、失效链接或状态偏差时，只能追加：

```text
errata.md
```

每条勘误必须包含日期、原文位置、问题说明、正确事实和证据引用。新需求必须创建新的活跃 Feature Spec，并反向链接归档规格。
```

- [ ] **Step 3：验证规格索引边界**

```powershell
$activeIndex = Get-Content -LiteralPath 'specs/README.md' -Raw
$archiveIndex = Get-Content -LiteralPath 'specs/archive/README.md' -Raw

if ($activeIndex -notmatch 'specs/<编号-功能名>/') {
    throw '活跃规格目录规则缺失'
}
if ($archiveIndex -notmatch 'specs/archive/<完成年份>/<编号-功能名>/') {
    throw '归档目录规则缺失'
}
if ($archiveIndex -notmatch '不可改写') {
    throw '归档不可变规则缺失'
}
```

预期：命令无输出、无异常。

## Task 5：创建 manifest 签名验证安全例外草案

**文件：**

- 创建：`docs/security/exceptions/manifest-signature-verification.md`
- 读取：`internal/signer/verify.go`
- 读取：`.github/workflows/ci.yaml`
- 读取：`manifests/*.yaml.sig`

**接口：**

- 消费：Constitution 的安全例外规则和当前代码事实。
- 产出：一份可由用户审查、但尚未激活的安全例外。

- [ ] **Step 1：复核当前签名事实**

```powershell
$rootKeyLine = Select-String -Path 'internal\signer\verify.go' -Pattern 'var rootPublicKeyB64 = ""'
if (-not $rootKeyLine) {
    throw '根公钥状态已变化，必须重新编写例外事实基础'
}

$signatureCount = @(Get-ChildItem -LiteralPath 'manifests' -Filter '*.yaml.sig' -File).Count
if ($signatureCount -lt 1) {
    throw '未找到 manifest 签名文件，风险范围与本计划假设不一致'
}

$ci = Get-Content -LiteralPath '.github\workflows\ci.yaml' -Raw
if ($ci -notmatch 'XDEV_SIGNING_PRIVATE_KEY' -or $ci -notmatch 'XDEV_SIGNING_PUBLIC_KEY') {
    throw 'CI 签名变量结构已变化，必须重新审计'
}
```

预期：根公钥源码默认值为空，存在 `.yaml.sig` 文件，CI 引用私钥和公钥变量。

- [ ] **Step 2：创建安全例外草案**

```markdown
# Manifest 签名验证临时安全例外

> **文档类型**：安全例外
> **状态**：Draft
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-15
> **最后更新**：2026-07-15
> **责任人**：`xinyan <zuijiuhjd@foxmail.com>`
> **批准者**：待用户明确批准
> **关联 Constitution**：`.specify/memory/constitution.md` `1.0.0-rc.2`
> **权威范围**：manifest 签名验证尚未形成正式发布硬门禁期间的临时风险治理

## 偏离条款

本例外暂时偏离 Constitution 的以下结果要求：

- “正式发布必须启用 manifest 签名验证”；
- 分层 Manifest 信任模型要求内置 manifest 使用受信根公钥验证签名。

Constitution 已将“CI 在签名缺失或公钥未注入时明确失败”记录为当前缺失的执行门禁。本例外同时跟踪该门禁缺口，但不把尚未实现的门禁描述为现行技术事实。

## 当前事实

- `internal/signer/verify.go` 中 `rootPublicKeyB64` 的源码默认值为空；空值或占位值 `REPLACE_WITH_REAL_PUBLIC_KEY` 都会关闭 manifest 签名验证。
- 仓库包含 `manifests/*.yaml.sig`，但签名文件存在不能证明发布二进制已经注入对应根公钥。
- `.github/workflows/ci.yaml` 仅在相关 secret 存在时签名或注入公钥；缺失 secret 时尚无明确的发布失败硬门禁。
- 因此当前状态不能被描述为“manifest 签名验证已完全修复”。

## 风险

- 正式发布产物可能在未注入根公钥时生成，导致内置 manifest 签名验证被跳过。
- 签名文件、公钥和发布二进制可能不属于同一受验证发布链。
- 文档或发布说明可能错误地把“存在签名文件”解释为“运行时验证已启用”。

## 影响范围

- manifest 下载前的签名验证；
- release workflow；
- 发布二进制的 `-ldflags` 公钥注入；
- 安全文档和发布声明。

## 禁止扩大边界

- 不得新增允许正式发布跳过签名验证的路径。
- 不得将 TOFU 降级扩展到内置 manifest 的正式发布行为。
- 不得将本例外作为关闭 SHA-256、HTTPS 或下载前验证的依据。
- 不得在例外关闭前把问题标记为“已修复”。

## 临时补偿控制

- 每次发布前人工确认签名文件存在、发布环境提供签名私钥和根公钥。
- 每次发布必须由 `xinyan <zuijiuhjd@foxmail.com>` 单独批准并记录。
- 发布说明必须明确签名验证状态，不得使用超出证据的安全声明。
- 保持 SHA-256、HTTPS 和 HTTPS 降级阻断等现有控制启用。

## 修复计划

创建独立 Feature Spec，至少覆盖：

1. CI 在签名私钥缺失时阻止正式发布；
2. CI 在发布二进制未注入根公钥时阻止正式发布；
3. 验证签名文件、公钥和发布产物属于同一发布链；
4. 添加可自动验证的负向测试；
5. 修正过时安全文档和发布声明。

本例外不授权直接实施上述代码变更。

## 有效期与自动失效

- 目标版本：首个符合 SemVer 且不包含 `alpha`、`beta`、`rc` 等预发布标识的版本；
- 截止日期：2026-09-01；
- 自动失效：目标版本与截止日期任一先到即失效；
- 失效后行为：签名验证修复未完成时，禁止正式发布；
- 续期：必须重新评估风险、设置新目标版本和日期，并重新获得用户批准，不能继承旧批准。

## 逐次发布批准记录

| 发布版本 | 日期 | 签名文件确认 | 私钥可用 | 公钥注入 | 批准者 | 结论 |
|---|---|---|---|---|---|---|

当前无批准记录。`Draft` 状态不能用于发布。

## 关闭条件

- 发布 workflow 在签名私钥或公钥缺失时明确失败；
- 发布产物的根公钥注入得到自动验证；
- 对应安全测试和三平台发布证据通过；
- 过时安全声明已经纠偏；
- 用户批准关闭例外。

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|

当前没有批准记录。草案创建事实记录在“变更历史”中；`Draft` 状态不能用于发布。

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `0.1.0` | 2026-07-15 | Initial Draft | 根据 Constitution 候选版和当前代码事实创建例外草案 | — |
```

- [ ] **Step 3：验证草案没有伪造批准**

```powershell
$exception = Get-Content -LiteralPath 'docs\security\exceptions\manifest-signature-verification.md' -Raw

if ($exception -notmatch '\*\*状态\*\*：Draft') {
    throw '安全例外初始状态必须为 Draft'
}
if ($exception -match '\*\*状态\*\*：Active') {
    throw 'Agent 不得自行激活安全例外'
}
if ($exception -notmatch '2026-09-01') {
    throw '安全例外截止日期缺失'
}
if ($exception -notmatch '不包含.*alpha.*beta.*rc') {
    throw '正式版本失效条件缺失'
}
if ($exception -notmatch '当前无批准记录') {
    throw '草案必须明确没有发布批准记录'
}
```

预期：命令无输出、无异常。

## Task 6：执行治理骨架验证

**文件：**

- 验证：本计划创建的全部 12 个 Markdown 文件
- 不创建新文件

**接口：**

- 消费：Tasks 2-5 的全部交付物。
- 产出：目录完整性、元数据、占位符、路径引用和安全边界验证证据。

- [ ] **Step 1：验证文件清单完整**

```powershell
$expected = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/security/exceptions/README.md',
    'docs/security/exceptions/manifest-signature-verification.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md'
)

$missing = @($expected | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    $missing
    throw '治理骨架文件不完整'
}
```

预期：无缺失文件。

- [ ] **Step 2：扫描占位符和空文件**

```powershell
$problems = @()
foreach ($file in $expected) {
    $content = Get-Content -LiteralPath $file -Raw
    if ([string]::IsNullOrWhiteSpace($content)) {
        $problems += "$file：空文件"
    }
    if ($content -match '\b(TODO|TBD|PLACEHOLDER|NEEDS CLARIFICATION)\b') {
        $problems += "$file：包含占位符"
    }
}
if ($problems.Count -gt 0) {
    $problems
    throw '治理骨架内容验证失败'
}
```

预期：无问题。

- [ ] **Step 3：验证目录职责关键语句**

```powershell
$requirements = @{
    'docs/README.md' = '文档分层'
    'docs/adr/README.md' = 'NNNN-<decision>.md'
    'docs/audits/README.md' = 'Snapshot'
    'docs/history/README.md' = '不得直接驱动 Agent 实现'
    'docs/history/implementation-plans/README.md' = '归档条件'
    'docs/security/README.md' = '权威关系'
    'docs/security/policy/README.md' = 'Policy'
    'docs/security/procedures/README.md' = 'Procedure'
    'docs/security/exceptions/README.md' = 'Expired'
    'docs/security/exceptions/manifest-signature-verification.md' = '当前无批准记录'
    'specs/README.md' = 'specs/<编号-功能名>/'
    'specs/archive/README.md' = '不可改写'
}

foreach ($entry in $requirements.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    if ($content -notmatch [regex]::Escape($entry.Value)) {
        throw "$($entry.Key) 缺少关键规则：$($entry.Value)"
    }
}
```

预期：命令无输出、无异常。

- [ ] **Step 4：复查私钥和禁止路径**

```powershell
$trackedPrivateKey = @(git ls-files -- keys/priv.key)
$stagedPrivateKey = @(git diff --cached --name-only -- keys/priv.key)
if ($trackedPrivateKey.Count -gt 0 -or $stagedPrivateKey.Count -gt 0) {
    throw '私钥进入 Git 跟踪或暂存区'
}
```

预期：命令无输出、无异常。

- [ ] **Step 5：检查 Markdown 行尾空白**

```powershell
$whitespaceProblems = @()
foreach ($file in $expected) {
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
    throw 'Markdown 文件存在行尾空白'
}
```

预期：无输出。

## Task 7：双重独立审查与提交

**文件：**

- 审查：本计划创建的全部文件
- 提交：仅本计划文件白名单

**接口：**

- 消费：完整治理骨架差异和验证证据。
- 产出：两个独立审查结论、用户验收和一个本地 Git 提交。

- [ ] **Step 1：重新声明白名单并生成可审查的暂存差异**

```powershell
$expected = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/security/exceptions/README.md',
    'docs/security/exceptions/manifest-signature-verification.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md'
)
if ($expected.Count -ne 12) {
    throw "治理骨架白名单数量错误：$($expected.Count)"
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
docs/superpowers/plans/2026-07-15-governance-scaffolding.md
本计划产生的差异
```

必须逐项判定：

- 12 个文件是否全部符合路径所有权；
- 安全例外是否保持 `Draft`；
- 是否记录固定失效条件和逐次发布批准；
- 是否错误声称治理体系或签名问题已经生效/修复；
- 是否移动或改写了不在范围内的旧文档。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 3：执行工程质量审查**

使用另一个全新的独立 Agent 会话执行审查，不复用实现者或 Spec 合规审查者会话。第二审查者必须不同于实现者和 Spec 合规审查者，检查：

- 文档职责是否重叠或冲突；
- 路径、状态名和批准规则是否一致；
- PowerShell 验证命令是否兼容 Windows PowerShell 5.1；
- 安全例外的风险和补偿控制是否可审计；
- 是否存在敏感数据、不可验证声明或过度设计。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 4：请求用户验收**

向用户提供：

- 创建文件清单；
- 聚焦验证结果；
- Spec 合规审查结论；
- 工程质量审查结论；
- 安全例外仍为 `Draft` 的明确说明。

只有用户明确回复批准 P1 验收或含义同等明确的指令后才能继续提交。泛化的“继续”不得解释为验收批准。

- [ ] **Step 5：按白名单暂存**

```powershell
git add -- $expected

if ($expected.Count -ne 12) {
    throw "治理骨架白名单数量错误：$($expected.Count)"
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

预期：暂存区只包含 12 个治理骨架文件。

- [ ] **Step 6：创建提交**

```powershell
git commit -m "docs: establish governance documentation structure"
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

- 提交包含且仅包含治理骨架文件；
- `keys/priv.key` 查询无输出；
- 不存在本计划产生的未提交文件。

## 验收标准

- **AC-001**：12 个治理骨架文件全部存在。
- **AC-002**：每个目录索引明确职责、合法内容和禁止事项。
- **AC-003**：活跃规格和归档规格具有不可混用的路径边界。
- **AC-004**：安全 Policy、Procedure、Exception 和 Audit 职责分离。
- **AC-005**：manifest 签名安全例外状态为 `Draft`，没有伪造激活或发布批准。
- **AC-006**：例外包含 2026-09-01 和首个无预发布标识 SemVer 正式版本的双重失效条件。
- **AC-007**：例外明确记录当前根公钥默认值、签名文件与发布硬门禁之间的事实差异。
- **AC-008**：所有文件无占位符、空内容和空白错误。
- **AC-009**：`keys/priv.key` 未被跟踪或暂存。
- **AC-010**：两个独立审查门禁和用户验收均通过后才创建提交。

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

## 后续计划

P1 完成后可并行编写和执行：

- `2026-07-15-speckit-template-overrides.md`；
- `2026-07-15-governance-templates.md`；
- `2026-07-15-document-link-validation.md`。

本计划不授权执行上述后续工作。
