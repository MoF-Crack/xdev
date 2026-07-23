# XDev 治理自动化实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

> **历史计划状态更新（2026-07-23）：** P5 已由提交 `9a36126`
> 完成。P7 已由提交 `e91310c` 完成，Constitution `1.0.0` 和
> governance blocking mode 已进入 `HEAD`。下文的 `1.0.0-rc.3`、报告模式
> 和 `continue-on-error` 是设计时基线，不是当前事实。P7 后的治理稳定化
> 硬门禁增强已由本地提交 `640c506` 固化；恢复时应读取
> `docs/superpowers/plans/archive/2026/2026-07-18-governance-stabilization.errata.md`。

**目标：** 实现三个 PowerShell 5.1 治理验证脚本（`validate-specs.ps1`、`validate-constitution.ps1`、`validate-governance.ps1`）及其测试固件，并创建独立的 `governance.yaml` CI workflow 以报告模式运行。为 P7 Constitution 正式批准后的阻塞模式切换提供可测试的自动化基础。

**架构：** 采用 TDD 模式，先创建 governance 测试固件目录（含通过和失败两类固件），再依次实现三个验证器并在 CI 中串行编排。三个验证器职责独立、可被本地和 CI 复用。CI workflow 独立于 `.github/workflows/ci.yaml`，当前以报告模式运行（`continue-on-error: true`），P7 之后才切换为阻塞模式。

**技术栈：** Windows PowerShell 5.1、GitHub Actions、Markdown、Git。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.3`，状态为候选版（`Ratified: Pending`），尚未正式批准。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`。
- 关联路线图：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`（P5 定义见第 49 行、第 149-159 行）。
- 前置依赖 P2A（Spec Kit 模板覆盖）、P2B（项目治理模板）、P3（文档链接验证）必须已完成。
- **不得修改 `.github/workflows/ci.yaml` 或任何现有 CI 配置。**
- **Governance CI 当前以报告模式运行**（所有步骤 `continue-on-error: true`），不阻塞任何 PR 或 push。
- **P7 之前不得将 governance CI 切换为阻塞模式。**
- PowerShell 脚本必须兼容 Windows PowerShell 5.1（不依赖 PowerShell Core 7+ 语法）。
- 所有新增文件使用 UTF-8 编码（无 BOM）。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 批准者固定为 `xinyan <zuijiuhjd@foxmail.com>`。
- Agent 不得代替用户批准 Constitution、Spec、Plan、最终验收或归档。
- 本计划完成后不授权执行 P6（已知缺口 Spec 化）或 P7（Constitution 正式批准）。

---

## 文件结构

本计划创建：

```text
scripts/docs/validate-specs.ps1
scripts/docs/validate-constitution.ps1
scripts/docs/validate-governance.ps1
scripts/docs/tests/fixtures/governance/
├── valid/
│   ├── spec.md                  # 符合规范的活跃 Spec（含完整元数据和 Approval History）
│   ├── plan.md                  # 合法 Plan（含 Constitution 合规检查、Approval History）
│   ├── spec-no-plan/            # 仅有 spec.md 无 plan.md（允许的中间态）
│   │   └── spec.md
│   └── constitution-valid.md    # 无占位符、含完整元数据和 Approval History 的 1.0.0-rc.N 文档
├── invalid/
│   ├── spec-missing-meta.md     # 缺失必需元数据的 Spec
│   ├── spec-missing-approval.md # 缺失 Approval History 的 Spec
│   ├── spec-with-placeholder.md # 含 TODO/TBD/[NEEDS CLARIFICATION] 的 Spec
│   ├── plan-no-constitution-check.md  # Plan 缺失 Constitution 合规检查表
│   ├── constitution-placeholder.md    # 含未解决占位符的 Constitution
│   ├── constitution-missing-version.md # 缺失版本字段的 Constitution
│   └── constitution-wrong-state.md    # 状态字段不合法的 Constitution
└── mixed/
    └── README.md                # 说明 mixed 目录用途
.github/workflows/governance.yaml
```

### 文件职责

| 文件 | 职责 |
|---|---|
| `scripts/docs/validate-specs.ps1` | 检查活跃 Spec 目录的 `spec.md` 包含必需元数据（文档类型、状态、版本、创建日期、所有者、权威范围）和 `## Approval History` 表（至少一条记录），确保无未解决占位符 |
| `scripts/docs/validate-constitution.ps1` | 检查 `.specify/memory/constitution.md` 的占位符、版本字段、状态字段、批准状态一致性，确保 `Ratified: Pending` 字段存在 |
| `scripts/docs/validate-governance.ps1` | 综合检查：调用上述两个验证器 + `validate-approvals.ps1` + `validate-links.ps1`，汇总所有治理资产的跨文档一致性（Spec 引用的 Constitution 版本、Plan 引用的 Spec 版本有效性、安全例外过期检查、`feature.json` 不指向归档目录） |
| `scripts/docs/tests/fixtures/governance/valid/` | 正面测试固件，全部验证器应返回 PASS |
| `scripts/docs/tests/fixtures/governance/invalid/` | 负面测试固件，验证器应检测到特定违规并返回 FAIL |
| `scripts/docs/tests/fixtures/governance/mixed/` | 混合固件目录，用于测试 `validate-governance.ps1` 的跨文档综合检查 |
| `.github/workflows/governance.yaml` | 独立 CI workflow，报告模式运行所有治理验证，不阻塞 PR/push |

### 固件目录说明

**`valid/` 目录**内所有固件必须通过对应验证器的全部检查：
- `spec.md`：严格按照 P2A 覆盖模板格式，包含完整文档类型、状态 `Draft`、版本 `0.1.0`、创建日期、所有者、权威范围、## Approval History（至少一条 Pending 记录）、## 变更历史。不含 TODO/TBD/[NEEDS CLARIFICATION]。
- `plan.md`：包含关联 Spec 版本、Constitution 逐条合规检查（使用 `Compliant` / `Not Applicable` / `Exception Required` / `ADR Required` / `Non-compliant` 结果）、Approval History 和变更历史。
- `spec-no-plan/spec.md`：仅有 spec.md 无 plan.md（合法中间态，validate-specs 应通过，validate-governance 应报告 spec-plan-consistency 为 pending 但非 fatal）。
- `constitution-valid.md`：模拟 Constitution rc.N 格式，包含完整元数据（版本 `1.0.0-rc.4`、状态 `候选版，尚未正式批准`）、Ratified 字段、Approval History、无未解决占位符。

**`invalid/` 目录**内固件必须被对应验证器检测到并报告 FAIL：
- `spec-missing-meta.md`：缺失 `**文档版本**` 或 `**状态**` 字段。
- `spec-missing-approval.md`：包含元数据但缺失 `## Approval History` 节或表为空。
- `spec-with-placeholder.md`：包含 `TODO`、`TBD` 或 `[NEEDS CLARIFICATION]` 标记。
- `plan-no-constitution-check.md`：Plan 不包含 Constitution 合规检查表。
- `constitution-placeholder.md`：Constitution 文档体包含 `TBD` 或 `TODO` 占位符。
- `constitution-missing-version.md`：缺失 `**版本**` 字段或版本值不合法。
- `constitution-wrong-state.md`：状态字段值不在合法集合 {`候选版，尚未正式批准`, `Candidate`, `Ratified`, `Superseded`} 中。

**`mixed/` 目录**用于 `validate-governance.ps1` 的综合场景测试（例如 spec 引用不存在的 Constitution 版本、Plan 引用的 Spec 版本与实际不符、安全例外过期等跨文档一致性检查）。

## 接口契约

### validate-specs.ps1

**输入：**
- `-SpecDir <string[]>`：要检查的 Spec 目录路径列表，默认 `specs/` 下所有活跃 Spec 目录（排除 `specs/archive/`）。
- `-SelfTest`：切换为离线 SelfTest 模式（消费 `scripts/docs/tests/fixtures/governance/` 固件）。
- `-Report <string>`（可选）：输出 JSON 报告文件路径。

**输出：**
- 标准输出：按 Spec 目录分组的结果，列出 PASS/FAIL/WARN，以及具体问题描述。
- 退出码：`0` = 全部通过，`1` = 存在违反项，`2` = 参数/内部错误。
- JSON 报告：`[{"spec":"...","status":"PASS|FAIL","issues":["..."]}]`。

**检查范围（必须）：**
| 检查项 | 说明 |
|---|---|
| 必需元数据存在性 | `spec.md` 必须包含：`**文档类型**`、`**状态**`、`**文档版本**`、`**创建日期**`、`**最后更新**`、`**所有者**`、`**权威范围**` |
| 状态合法性 | `**状态**` 必须在合法集合内：`Draft`、`Approved`、`In Progress`、`Verified`、`Accepted`、`Archived` |
| 版本格式 | `**文档版本**` 必须符合 `X.Y.Z` 或 `X.Y.Z-rc.N` 格式 |
| Approval History | `spec.md` 必须包含 `## Approval History` 节及至少一条记录（含阶段、状态、批准者、日期、文档版本、批准范围） |
| 无未解决占位符 | `spec.md` 不得包含 `TODO`、`TBD`、`[NEEDS CLARIFICATION]`（大小写不敏感） |
| Spec 不与 Plan 脱节 | 如果同目录存在 `plan.md`，则 Spec 状态不得仍为 `Draft`（即 Plan 存在时 Spec 应至少为 `Approved`） |

**检查范围（不检查）：**
| 排除项 | 说明 |
|---|---|
| 归档目录 | `specs/archive/` 下内容不检查 |
| `.specify/feature.json` 一致性 | 由 `validate-governance.ps1` 负责 |
| FR/SC 内容语义 | 不验证需求编号连续性、可实现性或逻辑正确性 |
| 审查记录完整性 | 双重审查证据存在性由 `validate-approvals.ps1` 负责 |

### validate-constitution.ps1

**输入：**
- `-ConstitutionPath <string>`：Constitution 文件路径，默认 `.specify/memory/constitution.md`。
- `-SelfTest`：切换为离线 SelfTest 模式。
- `-Report <string>`（可选）：输出 JSON 报告文件路径。

**输出：**
- 标准输出：逐项检查结果（PASS/FAIL/WARN）。
- 退出码：`0` = 全部通过，`1` = 存在违反项，`2` = 参数/内部错误。
- JSON 报告：`{"status":"PASS|FAIL","checks":[{"check":"...","status":"PASS|FAIL","detail":"..."}]}`。

**检查范围（必须）：**
| 检查项 | 说明 |
|---|---|
| 版本字段存在且合法 | `**版本**` 字段存在，值符合 SemVer 或 `X.Y.Z-rc.N` 格式 |
| 状态字段存在且合法 | 状态字段值必须在 {`候选版，尚未正式批准`, `Candidate`, `Ratified`, `Superseded`} 内 |
| `Ratified` 字段 | 必须存在 `Ratified: Pending` 或 `**Ratified**: Pending` 字段（候选版期间，Constitution 维持 Pending） |
| 无未解决占位符 | 文档正文不得包含 `TODO`、`TBD`、`PLACEHOLDER`（大小写不敏感），允许 `Approval History` 中的 `Pending` 行 |
| Approval History 存在 | 必须包含 `## Approval History` 节及至少一条记录 |
| 批准者一致性 | Approval History 中的批准者必须为 `xinyan <zuijiuhjd@foxmail.com>` |
| 文档状态表存在 | 必须包含 `## 文档状态` 或等价的状态元数据表 |

**检查范围（不检查）：**
| 排除项 | 说明 |
|---|---|
| Constitution 原则内容语义 | 不验证原则正确性、完整性或与代码事实的一致性 |
| 批准记录真实性 | 批准记录是否经用户真实授权由 `validate-approvals.ps1` 负责 |
| 安全例外状态 | 由 `validate-governance.ps1` 负责 |

### validate-governance.ps1

**输入：**
- `-RepoRoot <string>`：仓库根目录，默认当前目录。
- `-SelfTest`：切换为离线 SelfTest 模式。
- `-Report <string>`（可选）：输出 JSON 报告文件路径。
- `-SkipExternal`（可选）：跳过外部脚本调用（仅检查本脚本内建逻辑），用于隔离测试。

**输出：**
- 标准输出：综合治理检查报告，按类别分组（Spec、Constitution、Approvals、Links、Cross-Reference、Exceptions）。
- 退出码：`0` = 全部通过，`1` = 存在违反项，`2` = 参数/内部错误。

**检查范围（必须）：**
| 检查项 | 说明 |
|---|---|
| 串联验证器 | 依次调用 `validate-specs.ps1`、`validate-constitution.ps1`、`validate-approvals.ps1`（`-BaseRef HEAD~1`）、`validate-links.ps1`，汇总结果 |
| 跨文档一致性 | 活跃 Spec 中 `关联 Constitution` 引用的版本必须与实际 `.specify/memory/constitution.md` 的 `rc.N` 匹配；Plan 引用的 Spec 版本必须与该 Spec 目录的 `spec.md` 版本一致 |
| `.specify/feature.json` 检查 | `feature.json` 不得指向 `specs/archive/` 路径 |
| 安全例外过期检查 | 扫描 `docs/security/exceptions/` 下所有 `.md`，检查截止日期是否过期 |
| 活跃 Spec 目录完整性 | `specs/` 下每个非 archive 的 `<编号-功能名>/` 目录应至少包含 `spec.md` |
| 治理资产索引 | 汇总所有活跃 Spec、ADR、安全例外数量和状态，输出概览 |

**检查范围（不检查）：**
| 排除项 | 说明 |
|---|---|
| CI 工具链一致性 | 属于 P6 缺口 Spec 范围 |
| 集成测试门禁 | 属于 P6 缺口 Spec 范围 |
| 发布签名硬门禁 | 属于 P6 缺口 Spec 范围 |
| 测试宿主状态快照 | 属于 P6 缺口 Spec 范围 |

---

## 任务分解

### Task 1: 创建 governance 测试固件

**前置条件：** P2A（Spec Kit 模板覆盖）、P2B（项目治理模板）、P3（文档链接验证）已完成。当前工作区无未提交变更。

**文件列表：**
- Create: `scripts/docs/tests/fixtures/governance/valid/spec.md`
- Create: `scripts/docs/tests/fixtures/governance/valid/plan.md`
- Create: `scripts/docs/tests/fixtures/governance/valid/spec-no-plan/spec.md`
- Create: `scripts/docs/tests/fixtures/governance/valid/constitution-valid.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/spec-missing-meta.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/spec-missing-approval.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/spec-with-placeholder.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/plan-no-constitution-check.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/constitution-placeholder.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/constitution-missing-version.md`
- Create: `scripts/docs/tests/fixtures/governance/invalid/constitution-wrong-state.md`
- Create: `scripts/docs/tests/fixtures/governance/mixed/README.md`

**接口：** 不涉及代码逻辑；固件为纯 Markdown 文件，供验证器的 SelfTest 模式消费。

**步骤：**

- [ ] **Step 1: 创建目录结构**

```powershell
$fixtures = 'scripts/docs/tests/fixtures/governance'
New-Item -ItemType Directory -Path "$fixtures/valid/spec-no-plan" -Force | Out-Null
New-Item -ItemType Directory -Path "$fixtures/invalid" -Force | Out-Null
New-Item -ItemType Directory -Path "$fixtures/mixed" -Force | Out-Null
```

- [ ] **Step 2: 创建 valid/spec.md**

创建符合 P2A 覆盖模板规范的活跃 Spec。必须包含：
- 元数据块：文档类型 `Feature Spec`、状态 `Draft`、文档版本 `0.1.0`、创建日期、最后更新、所有者 `xinyan <zuijiuhjd@foxmail.com>`、权威范围
- `## Approval History` 表，包含至少一条 `Pending` 状态记录
- `## 变更历史` 表
- 无 `TODO`、`TBD`、`[NEEDS CLARIFICATION]` 标记

- [ ] **Step 3: 创建 valid/plan.md**

创建符合 P2A 覆盖模板规范的 Plan：
- 元数据块：文档类型 `Plan`、状态 `Planned`、关联 Spec 版本 `0.1.0`
- `## Constitution 合规检查` 表，逐条使用合法结果值
- `## Approval History` 表
- `## 变更历史` 表

- [ ] **Step 4: 创建 valid/spec-no-plan/spec.md**

仅含 `spec.md` 无 `plan.md` 的合法中间态目录，格式同 valid/spec.md。

- [ ] **Step 5: 创建 valid/constitution-valid.md**

模拟 Constitution `1.0.0-rc.N` 格式：
- `## 文档状态` 表，含版本 `1.0.0-rc.4`、状态 `候选版，尚未正式批准`
- `Ratified: Pending` 字段
- `## Approval History` 表
- 无未解决占位符

- [ ] **Step 6: 创建 invalid/ 下 7 个负面固件**

| 固件 | 故意违反的规则 |
|---|---|
| `spec-missing-meta.md` | 缺失 `**文档版本**` 和 `**状态**` 字段 |
| `spec-missing-approval.md` | 包含元数据但无 `## Approval History` 节 |
| `spec-with-placeholder.md` | 正文含 `TODO: 待补充` 和 `[NEEDS CLARIFICATION]` |
| `plan-no-constitution-check.md` | Plan 中无 `## Constitution 合规检查` 表 |
| `constitution-placeholder.md` | 正文含 `TBD` 占位符 |
| `constitution-missing-version.md` | `## 文档状态` 表缺失 `版本` 行 |
| `constitution-wrong-state.md` | `**状态**` 字段值为 `Draft`（不在合法集合内） |

- [ ] **Step 7: 创建 mixed/README.md**

说明 `mixed/` 目录用于 Task 2 实现 `validate-governance.ps1` 时按需创建跨文档一致性测试固件。

**验证命令（Task 1 完成时）：**
```powershell
# 确认目录结构
Get-ChildItem -Recurse scripts/docs/tests/fixtures/governance | Select-Object FullName

# 确认固件文件存在且非空
Get-ChildItem -Recurse scripts/docs/tests/fixtures/governance -File | ForEach-Object {
    $size = (Get-Item $_.FullName).Length
    Write-Host "$($_.FullName): $size bytes"
}
```

---

### Task 2: 实现 validate-specs.ps1、validate-constitution.ps1、validate-governance.ps1 及 governance CI workflow

**前置条件：** Task 1 已完成，所有固件文件存在且内容正确。

**文件列表：**
- Create: `scripts/docs/validate-specs.ps1`
- Create: `scripts/docs/validate-constitution.ps1`
- Create: `scripts/docs/validate-governance.ps1`
- Create: `.github/workflows/governance.yaml`

**接口：** 参见 §接口契约。

#### Task 2.1: 实现 validate-specs.ps1

- [ ] **Step 1: 创建脚本骨架**

```powershell
# scripts/docs/validate-specs.ps1
# XDev active Spec validator.
# Checks spec.md in active spec directories for required metadata,
# Approval History, and placeholder-free content.
#
# Usage:
#   ./scripts/docs/validate-specs.ps1 -SpecDir ./specs/001-my-feature
#   ./scripts/docs/validate-specs.ps1 -SpecDir ./specs -Report ./build/spec-report.json
#   ./scripts/docs/validate-specs.ps1 -SelfTest
#
# Exit codes: 0 = all pass, 1 = violations found, 2 = bad args / error.

[CmdletBinding(DefaultParameterSetName='Normal')]
param(
    [Parameter(ParameterSetName='Normal')]
    [string[]]$SpecDir = @(Join-Path (Get-Location).Path 'specs'),
    [Parameter(ParameterSetName='Normal')]
    [string]$Report = '',
    [Parameter(ParameterSetName='SelfTest', Mandatory=$true)]
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
$script:results = @()
$script:exitCode = 0

# 合法 Spec 状态集合
$script:ValidStates = @('Draft', 'Approved', 'In Progress', 'Verified', 'Accepted', 'Archived')

function Write-Result { ... }
function Get-SpecMetadata { ... }
function Validate-SpecFile { ... }
function Invoke-SelfTest { ... }
```

- [ ] **Step 2: 实现元数据提取函数 `Get-SpecMetadata`**

使用正则从 `spec.md` 内容中提取：
- `文档类型`（`\*\*文档类型\*\*[：:]\s*(.+)`）
- `状态`（`\*\*状态\*\*[：:]\s*(.+)`）
- `文档版本`（`\*\*文档版本\*\*[：:]\s*`?([0-9][^`\s\n]*)`?`）
- `创建日期`（`\*\*创建日期\*\*[：:]\s*(\d{4}-\d{2}-\d{2})`）
- `最后更新`（`\*\*最后更新\*\*[：:]\s*(\d{4}-\d{2}-\d{2})`）
- `所有者`（`\*\*所有者\*\*[：:]\s*(.+)`）
- `权威范围`（`\*\*权威范围\*\*[：:]\s*(.+)`）

参考 `validate-approvals.ps1` 的 `DocVersion` / `DocState` 实现模式，复用其正则设计风格。

- [ ] **Step 3: 实现核心验证函数 `Validate-SpecFile`**

对单个 `spec.md` 执行以下检查（返回 PASS/FAIL + 问题列表）：

1. **元数据存在性**：7 个必需字段全部存在且非空。
2. **状态合法性**：`$ValidStates -contains $state`。
3. **版本格式**：匹配 `^\d+\.\d+\.\d+(-rc\.\d+)?$`。
4. **日期格式**：匹配 `\d{4}-\d{2}-\d{2}`。
5. **Approval History 存在**：文件包含 `## Approval History` 或 `## 批准历史`，且下方包含至少一行 Markdown 表格数据行（`| ... |`）。
6. **无占位符**：正则 `\b(TODO|TBD|\[NEEDS CLARIFICATION\])\b` 不匹配（大小写不敏感）。
7. **Plan 一致性**（可选，同目录存在 `plan.md` 时执行）：Spec 状态不得为 `Draft`。

- [ ] **Step 4: 实现 SelfTest 模式**

```powershell
function Invoke-SelfTest {
    $fixtureRoot = Join-Path $PSScriptRoot 'tests/fixtures/governance'
    
    # Test valid fixtures: all should PASS
    $validSpecs = @(
        (Join-Path $fixtureRoot 'valid/spec.md'),
        (Join-Path $fixtureRoot 'valid/spec-no-plan/spec.md')
    )
    foreach ($s in $validSpecs) {
        $result = Validate-SpecFile $s
        if ($result.Status -ne 'PASS') {
            Write-Host "  [FAIL] valid fixture '$s': expected PASS, got $($result.Status)" -ForegroundColor Red
            exit 1
        }
    }
    Write-Host "  [OK]   valid fixtures: all PASS" -ForegroundColor Green
    
    # Test invalid fixtures: each should FAIL with specific issue
    $invalidTests = @{
        'invalid/spec-missing-meta.md'       = @('missing.*meta', '缺失.*元数据')
        'invalid/spec-missing-approval.md'   = @('missing.*approval', '缺失.*Approval History')
        'invalid/spec-with-placeholder.md'   = @('placeholder', 'TODO', '占位符')
    }
    # ... 逐项检查
}
```

- [ ] **Step 5: 实现主逻辑**

```powershell
if ($SelfTest) { Invoke-SelfTest; exit 0 }

# 收集所有活跃 Spec 目录（排除 archive）
$specDirs = @()
foreach ($d in $SpecDir) {
    $subs = Get-ChildItem -LiteralPath $d -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -notmatch '^archive$' -and $_.Name -match '^\d' }
    $specDirs += $subs.FullName
}

foreach ($dir in $specDirs) {
    $specFile = Join-Path $dir 'spec.md'
    if (Test-Path -LiteralPath $specFile) {
        $result = Validate-SpecFile $specFile
        $script:results += $result
    } else {
        $script:results += [PSCustomObject]@{ SpecDir=$dir; Status='FAIL'; Issues=@('spec.md not found') }
    }
}

# 输出与退出
$failed = @($script:results | Where-Object { $_.Status -eq 'FAIL' })
if ($failed.Count -gt 0) {
    Write-Host "FAILED: $($failed.Count) spec(s) have violations" -ForegroundColor Red
    $script:exitCode = 1
}
if ($Report) { $script:results | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $Report -Encoding utf8 }
exit $script:exitCode
```

**验证命令（Task 2.1 完成时）：**
```powershell
# SelfTest 模式
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-specs.ps1 -SelfTest

# 扫描实际活跃 Spec 目录
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-specs.ps1 -SpecDir specs
```

#### Task 2.2: 实现 validate-constitution.ps1

- [ ] **Step 1: 创建脚本骨架**

```powershell
# scripts/docs/validate-constitution.ps1
# XDev Constitution validator.
# Checks .specify/memory/constitution.md for placeholders,
# version validity, state validity, and approval consistency.
#
# Usage:
#   ./scripts/docs/validate-constitution.ps1 -ConstitutionPath ./.specify/memory/constitution.md
#   ./scripts/docs/validate-constitution.ps1 -SelfTest

[CmdletBinding(DefaultParameterSetName='Normal')]
param(
    [Parameter(ParameterSetName='Normal')]
    [string]$ConstitutionPath = (Join-Path (Get-Location).Path '.specify/memory/constitution.md'),
    [Parameter(ParameterSetName='Normal')]
    [string]$Report = '',
    [Parameter(ParameterSetName='SelfTest', Mandatory=$true)]
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
$script:checks = @()

# 合法状态集合（候选版期间）
$script:ValidConstitutionStates = @('候选版，尚未正式批准', 'Candidate', 'Ratified', 'Superseded')
```

- [ ] **Step 2: 实现核心检查函数**

```powershell
function Invoke-ConstitutionChecks {
    param([string]$Content, [string]$FilePath)
    
    $checks = @()
    
    # Check 1: 版本字段存在且格式合法
    $version = ''
    if ($Content -match '\*\*版本\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') {
        $version = $Matches[1].Trim()
        if ($version -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$') {
            $checks += @{Check='version-format'; Status='FAIL'; Detail="Invalid version: $version"}
        } else {
            $checks += @{Check='version-format'; Status='PASS'; Detail="Version: $version"}
        }
    } else {
        $checks += @{Check='version-format'; Status='FAIL'; Detail='Version field missing'}
    }
    
    # Check 2: 状态字段存在且合法
    # ... 类似逻辑
    
    # Check 3: Ratified: Pending 存在
    if ($Content -match '(?:\*{0,2})Ratified(?:\*{0,2})\s*:\s*Pending') {
        $checks += @{Check='ratified-pending'; Status='PASS'; Detail='Ratified: Pending'}
    } else {
        $checks += @{Check='ratified-pending'; Status='FAIL'; Detail='Ratified: Pending field missing or not Pending'}
    }
    
    # Check 4: 无未解决占位符
    if ($Content -notmatch '\b(TODO|TBD|PLACEHOLDER)\b') {
        $checks += @{Check='no-placeholders'; Status='PASS'}
    } else {
        $checks += @{Check='no-placeholders'; Status='FAIL'; Detail="Placeholder found"}
    }
    
    # Check 5: Approval History 存在
    if ($Content -match '## Approval History') {
        $checks += @{Check='approval-history'; Status='PASS'}
    } else {
        $checks += @{Check='approval-history'; Status='FAIL'; Detail='Approval History section missing'}
    }
    
    # Check 6: 批准者一致性
    # 提取 Approval History 表中批准者列，检查是否均为 xinyan <zuijiuhjd@foxmail.com>
    
    return $checks
}
```

- [ ] **Step 3: 实现 SelfTest 模式**

对 `fixtures/governance/valid/constitution-valid.md` 应全部 PASS；对 `invalid/constitution-*.md` 三个文件应分别 FAIL 对应检查项。

- [ ] **Step 4: 实现主逻辑**

类似 `validate-specs.ps1` 的结构。

**验证命令（Task 2.2 完成时）：**
```powershell
# SelfTest 模式
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-constitution.ps1 -SelfTest

# 检查实际 Constitution
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-constitution.ps1 -ConstitutionPath .specify/memory/constitution.md
```

#### Task 2.3: 实现 validate-governance.ps1

- [ ] **Step 1: 创建脚本骨架**

```powershell
# scripts/docs/validate-governance.ps1
# XDev comprehensive governance validator.
# Orchestrates validate-specs, validate-constitution, validate-approvals,
# and validate-links; adds cross-document consistency checks.
#
# Usage:
#   ./scripts/docs/validate-governance.ps1 -RepoRoot .
#   ./scripts/docs/validate-governance.ps1 -SelfTest
#   ./scripts/docs/validate-governance.ps1 -SkipExternal

[CmdletBinding(DefaultParameterSetName='Normal')]
param(
    [Parameter(ParameterSetName='Normal')]
    [string]$RepoRoot = (Get-Location).Path,
    [Parameter(ParameterSetName='Normal')]
    [string]$Report = '',
    [Parameter(ParameterSetName='Normal')]
    [switch]$SkipExternal,
    [Parameter(ParameterSetName='SelfTest', Mandatory=$true)]
    [switch]$SelfTest
)
```

- [ ] **Step 2: 实现子验证器编排**

```powershell
function Invoke-ChildValidator {
    param([string]$ScriptPath, [hashtable]$Arguments)
    
    $argList = @()
    foreach ($k in $Arguments.Keys) {
        $argList += "-$k"
        $argList += $Arguments[$k]
    }
    $output = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $ScriptPath @argList 2>&1
    return [PSCustomObject]@{
        ExitCode = $LASTEXITCODE
        Output   = $output
    }
}
```

依次调用：
1. `validate-specs.ps1 -SpecDir (Join-Path $RepoRoot 'specs')`
2. `validate-constitution.ps1 -ConstitutionPath (Join-Path $RepoRoot '.specify/memory/constitution.md')`
3. `validate-approvals.ps1 -BaseRef HEAD~1 -ResultFile <temp>`（仅当 `$SkipExternal` 为 false）
4. `validate-links.ps1 -Path (Join-Path $RepoRoot 'docs'),(Join-Path $RepoRoot 'specs')`

- [ ] **Step 3: 实现跨文档一致性检查**

```powershell
function Invoke-CrossDocChecks {
    # 1. Spec → Constitution 版本一致性
    #    扫描活跃 spec.md 中的 "关联 Constitution.*rc\.(\d+)" 引用，
    #    与实际 constitution.md 的版本比较
    
    # 2. Plan → Spec 版本一致性
    #    扫描活跃 plan.md 中的 "关联 Spec.*版本.*`(\S+)`" 引用，
    #    与实际 spec.md 的文档版本比较
    
    # 3. feature.json 不指向 archive
    $featureJson = Join-Path $RepoRoot '.specify/feature.json'
    if (Test-Path $featureJson) {
        $fj = Get-Content $featureJson -Raw | ConvertFrom-Json
        # 检查 feature 路径不包含 'archive'
    }
    
    # 4. 安全例外过期检查
    $exceptionsDir = Join-Path $RepoRoot 'docs/security/exceptions'
    if (Test-Path $exceptionsDir) {
        Get-ChildItem $exceptionsDir -Filter '*.md' -Exclude 'README.md' | ForEach-Object {
            $content = Get-Content $_.FullName -Raw
            # 提取截止日期并与当前日期比较
        }
    }
}
```

- [ ] **Step 4: 实现 SelfTest 模式**

使用 `fixtures/governance/mixed/` 目录创建综合测试场景（跨文档版本不一致、feature.json 指向 archive、过期安全例外等等）。

- [ ] **Step 5: 实现主逻辑**

汇总所有子验证器结果和跨文档检查结果，生成综合报告。退出码为所有子检查的 OR 聚合。

**验证命令（Task 2.3 完成时）：**
```powershell
# SelfTest 模式
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-governance.ps1 -SelfTest

# 综合检查（跳过外部脚本，仅测试内建逻辑）
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-governance.ps1 -RepoRoot . -SkipExternal

# 完整综合检查
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-governance.ps1 -RepoRoot .
```

#### Task 2.4: 创建 .github/workflows/governance.yaml

- [ ] **Step 1: 创建 workflow 文件**

```yaml
name: Governance

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  governance:
    runs-on: windows-latest

    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 2  # 需要 HEAD~1 用于 validate-approvals

      - name: Validate Constitution
        id: constitution
        continue-on-error: true  # 报告模式：不阻塞
        shell: powershell
        run: |
          $result = & .\scripts\docs\validate-constitution.ps1 `
            -ConstitutionPath .\.specify\memory\constitution.md `
            -Report .\build\governance-constitution-report.json
          if ($LASTEXITCODE -ne 0) {
            Write-Host "::warning::Constitution validation found issues"
          }

      - name: Validate Active Specs
        id: specs
        continue-on-error: true  # 报告模式：不阻塞
        shell: powershell
        run: |
          $result = & .\scripts\docs\validate-specs.ps1 `
            -SpecDir .\specs `
            -Report .\build\governance-specs-report.json
          if ($LASTEXITCODE -ne 0) {
            Write-Host "::warning::Spec validation found issues"
          }

      - name: Validate Approvals
        id: approvals
        continue-on-error: true  # 报告模式：不阻塞
        shell: powershell
        run: |
          $result = & .\scripts\docs\validate-approvals.ps1 `
            -BaseRef HEAD~1 `
            -ResultFile .\build\governance-approvals-result.txt
          $status = Get-Content .\build\governance-approvals-result.txt
          if ($status -match 'BLOCK') {
            Write-Host "::warning::Approval gate blocked: $status"
          }

      - name: Validate Documentation Links
        id: links
        continue-on-error: true  # 报告模式：不阻塞
        shell: powershell
        run: |
          $result = & .\scripts\docs\validate-links.ps1 `
            -Path .\docs,.\specs `
            -Report .\build\governance-links-report.json
          if ($LASTEXITCODE -ne 0) {
            Write-Host "::warning::Link validation found broken links"
          }

      - name: Comprehensive Governance Check
        id: governance
        continue-on-error: true  # 报告模式：不阻塞
        shell: powershell
        run: |
          New-Item -ItemType Directory -Path .\build -Force | Out-Null
          $result = & .\scripts\docs\validate-governance.ps1 `
            -RepoRoot . `
            -Report .\build\governance-report.json
          if ($LASTEXITCODE -ne 0) {
            Write-Host "::warning::Governance validation found issues"
          }

      - name: Upload Governance Reports
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: governance-reports
          path: |
            build/governance-*.json
            build/governance-*.txt
          retention-days: 30
```

**关键设计决策：**
- **所有步骤使用 `continue-on-error: true`**，确保单个验证器失败不中断后续步骤也不阻塞 PR/push。这是 "报告模式" 的核心差异——P7 之后将移除 `continue-on-error` 并设置 `fail-fast: true`。
- 使用 `windows-latest` runner（原生 PowerShell 5.1 兼容），不使用 `ubuntu-latest`（PowerShell 7+ 语法差异风险）。
- `fetch-depth: 2` 确保 `validate-approvals.ps1` 可以使用 `HEAD~1` 作为基线。
- `if: always()` 上传报告确保即使前面的步骤失败，报告仍然被归档。
- Workflow 名称 `Governance` 与 `ci.yaml` 的 `CI` 名称明确区分，不存在名称冲突或触发条件重叠。

**验证命令（Task 2.4 完成时）：**
```powershell
# 本地语法检查（GitHub Actions workflow YAML）
# 可手动在 GitHub UI 中检查 workflow 是否能正确解析
# 或使用本地工具：gh workflow view governance.yaml（如可用）
```

---

### Task 3: 验证与修复

**前置条件：** Task 2 全部子任务已完成。

- [ ] **Step 1: 运行所有 SelfTest**

```powershell
# 依次运行三个验证器的 SelfTest
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-specs.ps1 -SelfTest
if ($LASTEXITCODE -ne 0) { throw "validate-specs SelfTest failed" }

powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-constitution.ps1 -SelfTest
if ($LASTEXITCODE -ne 0) { throw "validate-constitution SelfTest failed" }

powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-governance.ps1 -SelfTest
if ($LASTEXITCODE -ne 0) { throw "validate-governance SelfTest failed" }
```

- [ ] **Step 2: 在真实仓库上运行全量验证**

```powershell
# 对实际仓库内容运行验证（报告模式）
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-specs.ps1 -SpecDir specs
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-constitution.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/docs/validate-governance.ps1 -RepoRoot .
```

- [ ] **Step 3: 修复实现缺陷**

根据 SelfTest 和真实仓库运行结果，修复验证器中的正则错误、漏检或误报。每次修复后重新运行 Step 1 和 Step 2。

- [ ] **Step 4: 确认 governance.yaml 语法正确**

手动或通过工具检查 workflow YAML 语法。验证不会与现有 `ci.yaml` 产生触发冲突（governance.yaml 使用独立 job 名称和独立 workflow 名称）。

---

### Task 4: 审查与提交

**前置条件：** Task 3 全部验证通过。所有 SelfTest 和真实仓库运行无意外失败（预期失败项已记录）。

- [ ] **Step 1: Spec 合规审查**

由非实现者 Agent 审查以下内容：
1. 所有新增文件路径在白名单内（见 §文件结构）。
2. 未修改 `.github/workflows/ci.yaml`。
3. 所有 PowerShell 脚本兼容 Windows PowerShell 5.1（无 `??`、`|` pipeline chain assignment、`?:` ternary、`ForEach-Object -Parallel` 等 PS7 语法）。
4. Governance workflow 所有步骤使用 `continue-on-error: true`。
5. 验证器 SelfTest 全部通过。
6. 测试固件目录结构与计划一致。

- [ ] **Step 2: 工程质量审查**

由非实现者 Agent 审查：
1. 代码无硬编码密钥或凭据。
2. 正则表达式覆盖中英文元数据标签。
3. 错误处理和退出码正确。
4. JSON 报告格式有效。
5. 文件编码为 UTF-8 无 BOM。
6. Governance workflow 不产生副作用（只读操作，仅写入 `build/` 临时文件）。

- [ ] **Step 3: 用户批准**

向 `xinyan <zuijiuhjd@foxmail.com>` 提交审查结论和差异包，等待明确批准。

- [ ] **Step 4: 创建提交**

```powershell
git add scripts/docs/validate-specs.ps1
git add scripts/docs/validate-constitution.ps1
git add scripts/docs/validate-governance.ps1
git add scripts/docs/tests/fixtures/governance/
git add .github/workflows/governance.yaml
git commit -m "feat(governance): P5 governance automation scripts and CI workflow

- Add validate-specs.ps1: active spec metadata, approval history, and placeholder check
- Add validate-constitution.ps1: constitution placeholder, version, state, and ratified field check
- Add validate-governance.ps1: comprehensive cross-document consistency validation
- Add test fixtures for all three validators (valid + invalid cases)
- Add .github/workflows/governance.yaml: report-mode CI workflow
- All validators include PowerShell 5.1 compatible SelfTest mode
- Governance CI runs with continue-on-error (report mode, P7 switches to blocking)"
```

---

## 验收标准

| 编号 | 标准 | 验证方式 |
|---|---|---|
| **AC-P5-01** | `validate-specs.ps1 -SelfTest` 全部通过 | 自动化 |
| **AC-P5-02** | `validate-constitution.ps1 -SelfTest` 全部通过 | 自动化 |
| **AC-P5-03** | `validate-governance.ps1 -SelfTest` 全部通过 | 自动化 |
| **AC-P5-04** | 三个验证器对合法固件返回 `0` 退出码 | 自动化 |
| **AC-P5-05** | 三个验证器对非法固件分别检测到对应违规并返回 `1` | 自动化 |
| **AC-P5-06** | `.github/workflows/governance.yaml` 在语法上有效 | 手动/CI |
| **AC-P5-07** | Governance CI 所有步骤使用 `continue-on-error: true` | 代码审查 |
| **AC-P5-08** | Governance CI 不阻塞 PR/push（即使验证器返回非零退出码） | CI 行为 |
| **AC-P5-09** | 未修改 `.github/workflows/ci.yaml` | 差异检查 |
| **AC-P5-10** | 所有 PowerShell 脚本兼容 Windows PowerShell 5.1 | 审查 + 实际运行 |
| **AC-P5-11** | 所有新增文件使用 UTF-8 无 BOM 编码 | 审查 |
| **AC-P5-12** | 测试固件覆盖正面和负面场景，每个验证器至少 3 个 PASS 和 3 个 FAIL 用例 | 审查 |

---

## Governance CI 报告模式与 P7 切换说明

### 当前（P5）行为

`.github/workflows/governance.yaml` 中所有步骤使用 `continue-on-error: true`：

```yaml
- name: Validate Constitution
  continue-on-error: true   # <-- 报告模式关键标记
  run: |
    ...
```

这确保：
- **PR 不会被治理检查失败阻塞**（即使 Constitution 含占位符、Spec 缺 Approval History、链接失效等）。
- **治理检查结果仍然可见**（通过 `::warning::` 注解和上传的 JSON 报告）。
- **团队可以逐步修复治理问题**，而不被自动化硬阻塞。
- **Constitution 尚未正式批准（`Ratified: Pending`）**，因此治理验证在逻辑上不能阻塞。

### P7 切换为阻塞模式

在 P7（Constitution 正式批准）之后，仅需对 `governance.yaml` 做两处变更：

1. **移除所有 `continue-on-error: true`**。
2. **在 job 级别或第一步添加 `fail-fast: true`**（或默认行为即 fail-fast）。

```yaml
# P7 之后的 governance.yaml 关键差异：
jobs:
  governance:
    runs-on: windows-latest
    # 移除 continue-on-error，所有步骤失败即阻塞
    steps:
      - name: Validate Constitution
        # 无 continue-on-error → 失败阻塞 PR
        run: ...
```

**P5 不执行此切换。** P7 实施计划中应明确该切换步骤，并在用户批准 Constitution `1.0.0` 后执行。

---

## 明确不授权范围

本计划不授权执行以下任何事项：

- ❌ 修改 `.github/workflows/ci.yaml`
- ❌ 将 governance CI 从报告模式切换为阻塞模式
- ❌ 将 Constitution 升级为 `1.0.0`（`Ratified: Pending` 保持不变）
- ❌ 实现 P6 已知缺口 Feature Spec
- ❌ 执行 P7 Constitution 正式批准流程
- ❌ 修改 `.specify/templates/` 核心模板
- ❌ 修改现有安全审计、旧计划或 `.trae/` 文件
- ❌ 删除或重命名现有文件（白名单外文件）

---

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Governance Automation Plan | Pending | — | — | `1.0.0-rc.1` | P5 治理自动化实施计划的完整内容，包括三个验证器、测试固件和 governance CI workflow；待用户批准后进入执行 |

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `1.0.0-rc.1` | 2026-07-18 | Initial | 创建 P5 治理自动化实施计划，定义三个验证器、测试固件、governance CI report-mode workflow 的完整实现路径 | — |
