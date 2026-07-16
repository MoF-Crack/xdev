# XDev 治理批准状态硬门禁实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 记录 `rc.3` 未授权修订事件与用户后续批准，并建立覆盖全部治理资产的批准状态差异硬门禁。

**Architecture:** 使用 `.specify/approvals/*.json` 作为机器可读批准账本，目标治理文档的 `Approval History` 作为就地记录。PowerShell 5.1 验证器读取 Git 暂存区或指定基线差异，发现受保护字段写入时，要求同一差异中新建唯一且匹配的批准记录；版本化 pre-commit hook 复用该验证器。

**Tech Stack:** PowerShell 5.1、Git、JSON、Markdown。

## Global Constraints

- 基线提交为 `a8b271bafd9e567c2dfee9c2b3953cf4464a8fcb`。
- 不改写、amend、reset 或 rebase `059da207f3cbd028a7e1f95537914238fdf00ddb`。
- Constitution 保持 `1.0.0-rc.3` 候选版和 `Ratified: Pending`，不得升级为 `1.0.0`。
- 本次批准不追溯合法化 `059da20` 中未经授权的操作。
- 批准者固定为 `xinyan <zuijiuhjd@foxmail.com>`。
- 每次受保护状态写入必须同时新增结构化 JSON 和目标文档 `Approval History` 记录。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 不修改 `.github/workflows/ci.yaml`；governance CI 接入保留给 P5。

---

## 文件结构

```text
.specify/approvals/2026-07-16-constitution-rc3-001.json
docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md
scripts/docs/validate-approvals.ps1
scripts/docs/tests/validate-approvals.tests.ps1
scripts/git-hooks/pre-commit
scripts/git-hooks/pre-commit.ps1
scripts/install-git-hooks.ps1
.specify/memory/constitution.md
```

- `validate-approvals.ps1`：唯一批准状态验证实现。
- `validate-approvals.tests.ps1`：临时 Git 仓库中的行为测试。
- `pre-commit`：Git 可执行入口，转发到 PowerShell。
- `pre-commit.ps1`：定位仓库并以 `-Staged` 调用验证器。
- `install-git-hooks.ps1`：显式设置本仓库 `core.hooksPath`。
- 批准 JSON：本次 `rc.3` 的机器可读批准事实。
- 事件文档：保留既往违规事实与纠正措施。

---

### Task 1: 以临时 Git 仓库建立批准门禁失败测试

**Files:**
- Create: `scripts/docs/tests/validate-approvals.tests.ps1`
- Test target: `scripts/docs/validate-approvals.ps1`

**Interfaces:**
- Consumes: PowerShell 5.1、可用 Git executable。
- Produces: `Invoke-ApprovalGateCase` 测试辅助函数，以及覆盖所有治理资产的可重复 RED/GREEN 测试。

- [ ] **Step 1: 创建临时仓库测试框架**

测试脚本使用 `$env:TEMP/xdev-approval-gate-tests-<guid>`，每个 case 单独初始化 Git，设置本地测试身份并创建基线提交。核心辅助函数：

```powershell
[CmdletBinding()]
param(
    [string]$Validator = (Join-Path $PSScriptRoot '..\validate-approvals.ps1')
)

$ErrorActionPreference = 'Stop'
$script:Passed = 0
$script:Failed = 0
$script:Git = (Get-Command git).Source

function Invoke-Git {
    param([string]$Repo, [string[]]$Arguments)
    & $script:Git -C $Repo @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function New-TestRepo {
    $repo = Join-Path $env:TEMP ("xdev-approval-gate-tests-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $repo -Force | Out-Null
    Invoke-Git $repo @('init', '--initial-branch=main')
    Invoke-Git $repo @('config', 'user.name', 'Approval Gate Test')
    Invoke-Git $repo @('config', 'user.email', 'approval-gate@example.invalid')
    return $repo
}

function Write-Utf8NoBom {
    param([string]$Path, [string]$Content)
    $parent = Split-Path -Parent $Path
    if ($parent) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
    [System.IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding($false)))
}

function Invoke-Validator {
    param([string]$Repo)
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Validator -RepositoryRoot $Repo -Staged 2>&1
    return [PSCustomObject]@{ ExitCode = $LASTEXITCODE; Output = ($input | Out-String) }
}
```

实现时修正 `Invoke-Validator`，必须先将命令输出捕获到局部变量再构造结果，避免使用流水线自动变量：

```powershell
$output = @(& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Validator -RepositoryRoot $Repo -Staged 2>&1)
$exitCode = $LASTEXITCODE
[PSCustomObject]@{ ExitCode = $exitCode; Output = ($output -join "`n") }
```

- [ ] **Step 2: 编写无批准记录时阻断 Constitution 版本变更的测试**

基线文件和变更使用完整 Markdown：

```powershell
$baseline = @'
# Test Constitution
| 版本 | `1.0.0-rc.2` |
| 状态 | 候选版，尚未正式批准 |
## Approval History
| 阶段 | 状态 | 批准 ID |
|---|---|---|
'@
$changed = $baseline.Replace('1.0.0-rc.2', '1.0.0-rc.3')
```

暂存变更后断言退出码非零，输出包含：

```text
APPROVAL_GATE_BLOCKED
.specify/memory/constitution.md
```

- [ ] **Step 3: 编写批准 JSON 与文档记录必须同步的测试**

覆盖以下失败 case：

```text
JSON 存在但文档无 approval_id
文档 ID 与 JSON approval_id 不同
target_path 不匹配
target_version 不匹配
target_state 不匹配
approved_by 不匹配
authorization_text 为空
复用基线中已有批准 JSON
```

合法 case 在同一 staged diff 中新增 JSON，并在目标文档追加：

```markdown
| Constitution 候选内容批准 | Approved Candidate | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0-rc.3` | `2026-07-16-constitution-rc3-001` | 仅批准当前候选内容；不追溯授权 `059da20`，不构成 `1.0.0` 正式批准 |
```

合法 case 断言退出码为 0，输出包含 `APPROVAL_GATE_PASSED`。

- [ ] **Step 4: 为每类治理资产创建 RED/GREEN 状态测试**

参数化以下路径与变更。每个基线和变更文档都必须包含 `> **版本**：` 元数据。第一轮不添加批准记录，全部必须失败：

```powershell
$cases = @(
    @{ Path = 'docs/adr/0001-test.md'; From = '> **状态**：Proposed'; To = '> **状态**：Accepted' },
    @{ Path = 'docs/security/exceptions/test.md'; From = '> **状态**：Draft'; To = '> **状态**：Active' },
    @{ Path = 'specs/001-test/spec.md'; From = '> **状态**：Draft'; To = '> **状态**：Approved' },
    @{ Path = 'specs/001-test/plan.md'; From = '> **状态**：Planned'; To = '> **状态**：Ready' },
    @{ Path = 'specs/001-test/acceptance.md'; From = '> **状态**：Verified'; To = '> **状态**：Accepted' },
    @{ Path = 'specs/archive/2026/001-test/archive-metadata.md'; From = '> **状态**：Accepted'; To = '> **状态**：Archived' }
)
```

第二轮为每个 case 同步新增匹配批准 JSON 和文档 `Approval History` 行，全部必须通过。状态型资产的记录使用：

```json
{
  "schema_version": 1,
  "approval_id": "<case-specific-id>",
  "asset_type": "<derived-asset-type>",
  "target_path": "<case-path>",
  "decision": "approve-state-transition",
  "target_version": "1.0.0",
  "target_state": "<case-target-state>",
  "scope": "批准当前测试资产的目标状态写入",
  "approved_by": "xinyan <zuijiuhjd@foxmail.com>",
  "approved_at": "2026-07-16",
  "authorization_text": "批准测试状态写入",
  "recorded_at": "2026-07-16",
  "recorded_by": "Approval Gate Test",
  "supersedes": []
}
```

每个目标文档追加包含相同 `<case-specific-id>`、目标版本和目标状态的 `Approval History` 行。测试断言每类资产的 GREEN case 退出码为 0，不得只用 Constitution 的通过结果推断其他路径规则正确。

- [ ] **Step 5: 运行测试确认 RED**

Run:

```powershell
.\scripts\docs\tests\validate-approvals.tests.ps1
```

Expected: FAIL，因为 `scripts/docs/validate-approvals.ps1` 尚不存在；失败必须明确指向缺失验证器，而不是测试脚本语法错误。

---

### Task 2: 实现 Git 差异批准验证器

**Files:**
- Create: `scripts/docs/validate-approvals.ps1`
- Modify: `scripts/docs/tests/validate-approvals.tests.ps1`（仅修复 RED 阶段发现的测试框架错误）

**Interfaces:**
- Consumes: `-RepositoryRoot <path>`、`-Staged` 或 `-BaseRef <git-ref>`。
- Produces: exit `0` + `APPROVAL_GATE_PASSED`，或 exit `1` + `APPROVAL_GATE_BLOCKED: <reason>`。

- [ ] **Step 1: 定义参数和 Git 命令边界**

```powershell
[CmdletBinding(DefaultParameterSetName = 'Staged')]
param(
    [string]$RepositoryRoot = (Get-Location).Path,
    [Parameter(ParameterSetName = 'Staged')]
    [switch]$Staged,
    [Parameter(Mandatory = $true, ParameterSetName = 'BaseRef')]
    [string]$BaseRef
)

$ErrorActionPreference = 'Stop'
$git = (Get-Command git).Source

function Invoke-GitCapture {
    param([string[]]$Arguments)
    $output = @(& $git -C $RepositoryRoot @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed: $($output -join "`n")"
    }
    return $output
}
```

暂存区模式使用 `git diff --cached --unified=0 --no-ext-diff`；基线模式使用 `git diff <BaseRef> --unified=0 --no-ext-diff`。

- [ ] **Step 2: 识别受保护路径和字段变化**

路径规则：

```powershell
function Get-AssetType {
    param([string]$Path)
    if ($Path -eq '.specify/memory/constitution.md') { return 'constitution' }
    if ($Path -match '^docs/adr/[^/]+\.md$' -and $Path -notmatch '/README\.md$') { return 'adr' }
    if ($Path -match '^docs/security/exceptions/[^/]+\.md$' -and $Path -notmatch '/README\.md$') { return 'security-exception' }
    if ($Path -match '^specs/[^/]+/spec\.md$') { return 'spec' }
    if ($Path -match '^specs/[^/]+/plan\.md$') { return 'plan' }
    if ($Path -match '^specs/[^/]+/(acceptance|review-record)\.md$') { return 'acceptance' }
    if ($Path -match '^specs/archive/.+/(archive-metadata|spec)\.md$') { return 'archive' }
    return $null
}
```

受保护行正则：

```powershell
$protectedLine = '(?i)(\*\*(状态|版本|正式批准日期|批准者)\*\*|^\|\s*(状态|版本|正式批准日期|批准者)\s*\||\bRatified\s*:|\bVersion\s*:|Approval History|Approved Candidate|\b(Accepted|Rejected|Superseded|Active|Extended|Closed|Approved|Ready|Verified|Archived)\b)'
```

只检查 diff 中以单个 `+` 或 `-` 开始的内容行，跳过 `+++`/`---` 文件头。受保护路径中任一受保护行变化均要求批准，宁可阻断不确定变更，不静默放行。

- [ ] **Step 3: 读取同一差异中新建的批准 JSON**

通过 `git diff --diff-filter=A --name-only` 只接受本次差异新增的 `.specify/approvals/*.json`。每份记录用 `ConvertFrom-Json` 解析并验证字段：

```text
schema_version = 1
approval_id
asset_type
target_path
decision
target_version
target_state
scope
approved_by
approved_at
authorization_text
recorded_at
recorded_by
supersedes
```

`approved_by` 必须精确等于 `xinyan <zuijiuhjd@foxmail.com>`。所有字符串必填字段 `Trim()` 后不得为空，`approval_id` 必须与文件名一致，所有 approval ID 必须唯一。所有受保护治理文档必须提供版本元数据，因此 Constitution 和状态型资产的 `target_version` 都不得为空。

- [ ] **Step 4: 匹配目标变化、批准记录和文档内记录**

对每个发生受保护变化的路径：

1. 找到 `target_path` 完全一致的新增批准 JSON；
2. 要求恰好一份，0 或多于 1 均失败；
3. 要求 `asset_type` 与路径推导结果一致；
4. 从变更后的工作树/索引内容读取目标文档；
5. 要求文档包含 approval ID；
6. 所有资产要求目标文档版本与 `target_version` 一致；状态型资产还要求目标状态与 `target_state` 一致；
7. 同一个 approval ID 不得匹配多个目标路径。

暂存区读取目标内容使用：

```powershell
$content = (Invoke-GitCapture @('show', ":$Path")) -join "`n"
```

基线模式读取工作树文件，但必须验证文件位于 `$RepositoryRoot` 下。

- [ ] **Step 5: 统一成功与失败输出**

```powershell
function Stop-ApprovalGate {
    param([string]$Reason)
    [Console]::Error.WriteLine("APPROVAL_GATE_BLOCKED: $Reason")
    exit 1
}
```

无受保护变化或全部匹配时输出：

```text
APPROVAL_GATE_PASSED: <count> protected change(s) validated
```

- [ ] **Step 6: 运行测试确认 GREEN**

Run:

```powershell
.\scripts\docs\tests\validate-approvals.tests.ps1
```

Expected: 所有 case 通过，输出总通过数且退出码为 0。

- [ ] **Step 7: 运行静态和空差异验证**

Run:

```powershell
[void][scriptblock]::Create((Get-Content -LiteralPath '.\scripts\docs\validate-approvals.ps1' -Raw))
[void][scriptblock]::Create((Get-Content -LiteralPath '.\scripts\docs\tests\validate-approvals.tests.ps1' -Raw))
.\scripts\docs\validate-approvals.ps1 -BaseRef HEAD
```

Expected: 两个脚本可解析；空差异输出 `APPROVAL_GATE_PASSED: 0 protected change(s) validated`。

---

### Task 3: 添加版本化 pre-commit 执行入口

**Files:**
- Create: `scripts/git-hooks/pre-commit`
- Create: `scripts/git-hooks/pre-commit.ps1`
- Create: `scripts/install-git-hooks.ps1`
- Modify: `scripts/docs/tests/validate-approvals.tests.ps1`

**Interfaces:**
- Consumes: Git hook invocation，无参数。
- Produces: 暂存区批准门禁退出码；安装器将本地 `core.hooksPath` 设置为 `scripts/git-hooks`。

- [ ] **Step 1: 先增加 hook 转发测试**

测试在临时仓库中复制 validator 和 hook，暂存未批准 Constitution 版本变更，执行 `scripts/git-hooks/pre-commit.ps1`，断言非零和 `APPROVAL_GATE_BLOCKED`。合法双重记录 case 断言为 0。

- [ ] **Step 2: 创建跨 Git for Windows 入口**

`scripts/git-hooks/pre-commit`：

```sh
#!/bin/sh
exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$(dirname "$0")/pre-commit.ps1"
```

`scripts/git-hooks/pre-commit.ps1`：

```powershell
$ErrorActionPreference = 'Stop'
$repoRoot = (& git rev-parse --show-toplevel).Trim()
if ($LASTEXITCODE -ne 0 -or -not $repoRoot) {
    [Console]::Error.WriteLine('APPROVAL_GATE_BLOCKED: cannot resolve repository root')
    exit 1
}
$validator = Join-Path $repoRoot 'scripts\docs\validate-approvals.ps1'
& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $validator -RepositoryRoot $repoRoot -Staged
exit $LASTEXITCODE
```

- [ ] **Step 3: 创建显式安装脚本**

`scripts/install-git-hooks.ps1`：

```powershell
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = (& git rev-parse --show-toplevel).Trim()
if ($LASTEXITCODE -ne 0 -or -not $repoRoot) { throw 'Not inside a Git repository' }
& git -C $repoRoot config --local core.hooksPath scripts/git-hooks
if ($LASTEXITCODE -ne 0) { throw 'Failed to configure core.hooksPath' }
$configured = (& git -C $repoRoot config --local --get core.hooksPath).Trim()
if ($configured -ne 'scripts/git-hooks') { throw "Unexpected core.hooksPath: $configured" }
Write-Output 'Configured core.hooksPath=scripts/git-hooks'
```

- [ ] **Step 4: 运行 hook 测试和安装器**

Run:

```powershell
.\scripts\docs\tests\validate-approvals.tests.ps1
.\scripts\install-git-hooks.ps1
git config --local --get core.hooksPath
```

Expected: 测试通过；配置输出两次均为 `scripts/git-hooks`。

---

### Task 4: 同步写入治理事件和 `rc.3` 批准事实

**Files:**
- Create: `docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md`
- Create: `.specify/approvals/2026-07-16-constitution-rc3-001.json`
- Modify: `.specify/memory/constitution.md`

**Interfaces:**
- Consumes: 用户原始指令“1.创建勘误记录。2.同意rc.3. 3.添加批准状态硬性门禁，且每次批准都需要在写入时进行记录。”以及“同意实施”。
- Produces: 不可歧义的事件记录和 `rc.3` 双重批准记录。

- [ ] **Step 1: 创建治理事件记录**

文档必须原样包含以下事实声明：

```markdown
# 治理事件：未经事前批准修订 Constitution `rc.3`

> **事件日期**：2026-07-16
> **发现日期**：2026-07-16
> **相关提交**：`059da207f3cbd028a7e1f95537914238fdf00ddb`
> **状态**：Corrective Action In Progress

## 事件结论

`059da20` 在没有用户事前明确批准的情况下，将 Constitution 候选版从 `1.0.0-rc.2` 修订为 `1.0.0-rc.3` 并提交。该操作超出了“继续执行 Git 初始导入”的授权范围，属于治理变更授权边界违规。

2026-07-16 用户随后批准 `1.0.0-rc.3` 当前候选内容。该批准从记录时刻起确认候选内容可作为后续工作的候选基线，但不追溯授权、不覆盖、不消除 `059da20` 中未经授权的操作事实，也不构成 Constitution `1.0.0` 正式批准。
```

其余章节必须记录：操作时间 `2026-07-16 20:05:30 +08:00`、未伪造批准记录、影响、根因、四个失效门禁、纠正措施、验证证据和关联批准 ID。

- [ ] **Step 2: 创建结构化批准 JSON**

使用设计文档第 5.2 节的完整 JSON，`authorization_text` 使用用户批准 `rc.3` 的原始文本：

```text
1.创建勘误记录。2.同意rc.3. 3.添加批准状态硬性门禁，且每次批准都需要在写入时进行记录。
```

`scope` 必须明确不追溯授权 `059da20` 且不构成 `1.0.0` 正式批准。

- [ ] **Step 3: 在 Constitution 同步追加批准记录**

只修改 `Approval History`，追加：

```markdown
| Constitution `rc.3` 候选内容批准 | Approved Candidate | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0-rc.3` | `2026-07-16-constitution-rc3-001` | 批准当前候选内容作为后续治理工作的候选基线；不追溯授权 `059da20`，不构成 `1.0.0` 正式批准 |
```

不得修改顶部状态、正式批准日期或底部 `Ratified: Pending`。

- [ ] **Step 4: 暂存完整纠正差异并运行硬门禁**

只暂存本计划创建或修改的路径：

```powershell
$paths = @(
    'scripts/docs/validate-approvals.ps1',
    'scripts/docs/tests/validate-approvals.tests.ps1',
    'scripts/git-hooks/pre-commit',
    'scripts/git-hooks/pre-commit.ps1',
    'scripts/install-git-hooks.ps1',
    'docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md',
    '.specify/approvals/2026-07-16-constitution-rc3-001.json',
    '.specify/memory/constitution.md'
)
git add -- $paths
.\scripts\docs\validate-approvals.ps1 -Staged
```

Expected: `APPROVAL_GATE_PASSED: 1 protected change(s) validated`。

- [ ] **Step 5: 验证 Constitution 没有被正式批准**

```powershell
$constitution = Get-Content -LiteralPath '.specify\memory\constitution.md' -Raw
if ($constitution -notmatch 'Ratified: Pending') { throw 'Constitution must remain pending' }
if ($constitution -match '正式批准日期\s*\|\s*2026-07-16') { throw '1.0.0 ratification was not authorized' }
if ($constitution -notmatch '2026-07-16-constitution-rc3-001') { throw 'Approval ID missing from Constitution' }
```

- [ ] **Step 6: 执行全量聚焦验证**

```powershell
.\scripts\docs\tests\validate-approvals.tests.ps1
.\scripts\docs\validate-approvals.ps1 -Staged
git diff --cached --check
git diff --cached --stat
git diff --cached --name-only
git check-ignore -v -- keys/priv.key
git diff --cached --name-only -- keys/priv.key
```

Expected: 测试和门禁通过；whitespace 检查无输出；私钥被忽略且暂存查询无输出。

- [ ] **Step 7: 执行双重独立审查**

Spec 合规审查逐条核对批准设计第 10、11 节。工程质量审查检查 PowerShell 5.1 兼容性、路径安全、Git diff 解析、fail-closed 行为、临时仓库清理和 hook 可绕过风险说明。任一审查失败必须修复并重新运行对应审查。

- [ ] **Step 8: 请求用户批准最终纠正差异**

展示：

```text
事件记录路径
批准 ID
Constitution 保持 Ratified: Pending 的证据
测试通过/失败 case 数
暂存文件白名单
双重审查结论
```

用户明确批准最终差异前不得提交。

- [ ] **Step 9: 创建独立纠正提交**

用户批准后执行：

```powershell
git commit -m "governance: enforce recorded approvals"
git log --oneline -3
git status --short --branch
```

Expected: hook 自动再次运行批准门禁，提交成功，工作区干净。

---

## 最终验收

- [ ] `059da20` 的对象 ID 和内容未改变。
- [ ] 事件记录明确后续批准不追溯合法化既往操作。
- [ ] `rc.3` JSON 和 Constitution 使用相同 approval ID。
- [ ] Constitution 仍为候选版且 `Ratified: Pending`。
- [ ] 全部治理资产类别具有 RED/GREEN 测试。
- [ ] 旧批准记录不能复用。
- [ ] 本地 pre-commit 已配置并调用唯一验证器。
- [ ] 没有修改 CI、P1 路线图或其他未经批准的治理状态。
- [ ] `keys/priv.key` 未跟踪、未暂存、未进入新提交。
