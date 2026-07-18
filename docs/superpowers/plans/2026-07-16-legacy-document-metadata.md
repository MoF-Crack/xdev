# XDev 旧文档元数据治理实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

**目标：** 对现有旧文档在原位置添加状态、来源和替代关系元数据头，为 P4B 历史文档迁移提供准确的文档分类和目标路径信息，不移动任何文件。

**架构：** 本阶段不改写文档内容语义，只在每个目标文件顶部插入符合设计规格的元数据头（blockquote 格式）。元数据声明文档类型、当前治理状态、来源路径、P4B 迁移目标和替代关系。P4A 是 P4B 的前置条件——P4A 定义"文档是什么、应该去哪"，P4B 执行实际移动。

**技术栈：** Markdown、Git、Windows PowerShell 5.1、`scripts/docs/validate-links.ps1`。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.3`，状态为候选版，尚未正式批准。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`。
- 关联路线图：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`。
- 本计划依赖 P1（治理骨架）、P2B（治理模板）和 P3（文档链接验证）的交付物。这三个子项目的交付物均已就位：
  - P1：治理目录与索引已创建（`docs/README.md`、`docs/adr/`、`docs/security/`、`docs/audits/`、`docs/history/`、`specs/` 等）；
  - P2B：治理模板已创建（`.specify/templates/overrides/` 下的 `adr-template.md`、`security-exception-template.md`、`archive-metadata-template.md`、`review-record-template.md`）；
  - P3：文档链接验证脚本 `scripts/docs/validate-links.ps1` 已就位。
- 当前分支必须为 `main`，必须存在可用 `HEAD`，且不得配置远程仓库或执行推送。
- 所有元数据字段名和说明使用简体中文；路径、状态名、命令和 Git 提交信息保留英文。
- **不移动任何文件。** 文件移动由 P4B 负责。
- **不修改文档内容语义。** 只添加元数据头，不改变正文、代码块、表格、列表或引用。
- 元数据头以 blockquote 格式（`>` 行）插入到文件顶部，位于任何已有 YAML front matter 之后、正文标题之前。
- 文件已有元数据头时（如 `docs/superpowers/specs/` 和 `docs/superpowers/plans/` 下的文件），不重复添加。
- Constitution 候选期限制：P4A 添加的元数据不能把候选 Constitution 描述为已生效的最高权威；元数据中的状态必须体现"迁移尚未执行、P4B 待完成"。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 本计划必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准进入执行。

---

## 文件结构

本计划在以下文件的**原位置**添加元数据头：

### 批次 1：原位保留的长期参考文档（4 个文件）

这些文档位于 `docs/`，文档类型为长期参考文档，P4B 不移动，原位保留并维护。

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `docs/architecture.md` | 长期参考文档 | `Living` | 原位保留 |
| `docs/technical-reference.md` | 长期参考文档 | `Living` | 原位保留 |
| `docs/operations.md` | 长期参考文档 | `Living` | 原位保留 |
| `docs/test-plan.md` | 长期参考文档 | `Living` | 原位保留 |

### 批次 2：P4B 将迁移的安全文档（3 个文件）

这些文档当前位于 `docs/`，将由 P4B 迁移到 `docs/security/` 子目录。

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `docs/download-source-policy.md` | 安全政策 | `Deprecated`（待迁移） | `docs/security/policy/download-source-policy.md` |
| `docs/signing-guide.md` | 安全操作流程 | `Deprecated`（待迁移） | `docs/security/procedures/signing-guide.md` |
| `docs/download-cache-recovery.md` | 安全操作流程 | `Deprecated`（待迁移） | `docs/security/procedures/download-cache-recovery.md` |

### 批次 3：P4B 将迁移的审计文档（2 个文件）

这些文档当前位于 `docs/`，将由 P4B 迁移到 `docs/audits/`。

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `docs/security-audit.md` | 时间点审计 | `Snapshot`（待迁移） | `docs/audits/2026-07-04-security-audit.md` |
| `docs/security-audit-verification.md` | 时间点审计 | `Snapshot`（待迁移） | `docs/audits/2026-07-08-security-audit-verification.md` |

### 批次 4：P4B 将处理的分析与规划文档（4 个文件）

这些文档将由 P4B 拆分、转换或归档。

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `docs/pending-work-plan.md` | 工作计划 | `Frozen`（待拆分） | 独立事项转为 Feature Spec 后归档至 `docs/history/implementation-plans/` |
| `docs/progress-and-defects-analysis.md` | 分析报告 | `Snapshot` | 原位保留或由用户决定目标 |
| `docs/competitive-analysis.md` | 分析报告 | `Snapshot` | 原位保留或由用户决定目标 |
| `docs/git-initial-import-plan.md` | 实施计划 | `Superseded`（待迁移） | `docs/history/implementation-plans/git-initial-import-plan.md` |

### 批次 5：根目录与 `.trae` 旧文档（3 组文件）

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `IMPROVEMENT-PLAN.md` | Backlog | `Frozen`（待转换） | `docs/backlog.md` |
| `.trae/documents/xdev-improvement-roadmap.md` | Roadmap | `Frozen`（待转换） | `docs/roadmap.md` |
| `.trae/specs/enhance-gui-ux-and-fixes/spec.md` | Feature Spec（旧） | `Archived`（待迁移） | `specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md` |
| `.trae/specs/enhance-gui-ux-and-fixes/tasks.md` | 任务（旧） | `Archived`（待迁移） | `specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md` |
| `.trae/specs/enhance-gui-ux-and-fixes/checklist.md` | Checklist（旧） | `Archived`（待迁移） | `specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md` |

### 批次 6：Superpowers 计划文档（1 个文件）

| 文件路径 | 文档类型 | 当前状态 | P4B 迁移目标 |
|---|---|---|---|
| `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` | 实施计划 | `Superseded`（待迁移） | `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` |

> **注意：** `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` 已有完整元数据头，`docs/superpowers/plans/` 下已有元数据的文件（如 P1 计划）不重复添加。`docs/README.md`、`docs/history/README.md` 等 P1 已创建的索引文件已有元数据，不纳入本计划。

### 不纳入本计划的文件

- `docs/superpowers/specs/` 下的设计文档（已有元数据）；
- `docs/superpowers/plans/` 下已有元数据头的计划文档；
- `docs/adr/`、`docs/security/`、`docs/audits/`、`docs/history/`、`specs/` 下的索引文件（P1 已创建，已有元数据）；
- `docs/security/exceptions/manifest-signature-verification.md`（P1 已创建，已有元数据）；
- `.specify/` 和 `.github/` 下的文件。

## 接口契约

### 输入

- `.specify/memory/constitution.md` `1.0.0-rc.3`：治理原则和候选期限制。
- `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`：文档分类、状态模型、元数据格式和迁移矩阵（第 14 节）。
- `docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`：子项目边界、文件所有权和 P4A/P4B 串行约束。
- `scripts/docs/validate-links.ps1`：链接验证脚本（P3 交付物）。
- `.specify/templates/overrides/archive-metadata-template.md`：归档元数据模板（P2B 交付物，作为参考）。
- 本计划文件清单中列出的全部目标文件原文。

### 输出

- 每个目标文件顶部新增符合治理设计规范的元数据头。
- 文件内容语义、正文结构和引用关系不变。
- P4B 执行时可通过元数据头直接获取迁移目标路径。

---

## 元数据格式

### 通用元数据头模板

```markdown
> **文档类型**：<类型>
> **状态**：<状态>
> **来源**：<原始路径>
> **P4B 迁移目标**：<目标路径或"原位保留">
> **替代文档**：<替代文档路径或"无">
> **治理记录**：P4A 元数据治理，<YYYY-MM-DD>
```

### 字段说明

| 字段 | 说明 | 示例 |
|---|---|---|
| `文档类型` | 取自设计规格第 5 节的合法文档类型 | `长期参考文档`、`安全政策`、`时间点审计`、`Feature Spec（旧）`、`实施计划`、`Backlog`、`Roadmap`、`分析报告`、`工作计划`、`安全操作流程` |
| `状态` | 取自设计规格第 5 节的合法状态，附加 `（待迁移）` 或 `（待拆分）` 后缀表示 P4B 未完成 | `Living`、`Deprecated（待迁移）`、`Snapshot（待迁移）`、`Frozen（待拆分）`、`Frozen（待转换）`、`Superseded（待迁移）`、`Archived（待迁移）` |
| `来源` | 文件在治理体系前的原始路径，即当前路径 | `docs/architecture.md` |
| `P4B 迁移目标` | P4B 执行时的目标路径；原位保留时写 `原位保留`；需用户决定时写 `待用户决定` | `docs/security/policy/download-source-policy.md` |
| `替代文档` | 已有替代文档的路径；无替代时写 `无` | `docs/backlog.md` |
| `治理记录` | 固定格式，日期为 P4A 执行日期 | `P4A 元数据治理，2026-07-16` |

### 原位保留文档专用元数据

```markdown
> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/<文件名>.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

### P4B 待迁移文档专用元数据

```markdown
> **文档类型**：<类型>
> **状态**：<状态>（待迁移）
> **来源**：<当前路径>
> **P4B 迁移目标**：<目标路径>
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

### 插入位置规则

1. 如果文件以 YAML front matter（`---` 开始和结束）开头，元数据头插入在 front matter 之后、正文标题（`#`）之前。
2. 如果文件以 `#` 标题开头，元数据头插入在第一个 `#` 标题之前。
3. 如果文件已有 blockquote 元数据头（以 `> **文档类型**` 开头），跳过该文件，不重复添加。
4. 元数据头与前后内容之间各空一行。

---

## Task 1：执行前门禁和基线记录

**文件：**

- 读取：`.specify/memory/constitution.md`
- 读取：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`
- 读取：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`
- 读取：`docs/superpowers/plans/2026-07-15-governance-scaffolding.md`
- 不修改文件

**接口：**

- 消费：P1、P2B、P3 交付物和当前仓库状态。
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
    throw '仓库尚无 HEAD。请先执行 docs/git-initial-import-plan.md。'
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

- [ ] **Step 5：验证上游依赖 P1、P2B、P3 已就位**

```powershell
$dependencies = @(
    'docs/README.md',
    'docs/adr/README.md',
    'docs/security/README.md',
    'docs/security/exceptions/README.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md',
    '.specify/templates/overrides/adr-template.md',
    '.specify/templates/overrides/security-exception-template.md',
    '.specify/templates/overrides/archive-metadata-template.md',
    '.specify/templates/overrides/review-record-template.md',
    'scripts/docs/validate-links.ps1'
)

$missing = @($dependencies | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    $missing
    throw '上游依赖交付物缺失，P4A 不能执行'
}
```

预期：无缺失依赖。

- [ ] **Step 6：验证目标文件全部存在**

```powershell
$targets = @(
    'docs/architecture.md',
    'docs/technical-reference.md',
    'docs/operations.md',
    'docs/test-plan.md',
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/pending-work-plan.md',
    'docs/progress-and-defects-analysis.md',
    'docs/competitive-analysis.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md',
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md',
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

$missing = @($targets | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    $missing
    throw '目标文件缺失，请核实文件清单'
}

Write-Output "目标文件总数：$($targets.Count)"
```

预期：19 个目标文件全部存在。

- [ ] **Step 7：扫描目标文件是否已有元数据头**

```powershell
$alreadyTagged = @()
foreach ($file in $targets) {
    $firstLines = Get-Content -LiteralPath $file -First 10 -ErrorAction SilentlyContinue
    if ($firstLines -join "`n" -match '> \*\*文档类型\*\*') {
        $alreadyTagged += $file
    }
}
if ($alreadyTagged.Count -gt 0) {
    Write-Output "以下文件已有元数据头，将跳过："
    $alreadyTagged
} else {
    Write-Output '无已有元数据头的文件。'
}
```

预期：输出已有元数据头的文件清单（预期为空或仅已治理文件）。

- [ ] **Step 8：运行 P3 链接验证脚本建立基线**

```powershell
$result = & 'scripts/docs/validate-links.ps1' 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Output '警告：链接验证脚本返回非零退出码。'
    Write-Output $result
    Write-Output '请审查链接问题：如果问题是预先存在的（非 P4A 引入），可以继续。'
    Write-Output 'P4A 完成后将重新运行，确保没有新增链接问题。'
}
```

预期：记录当前链接校验状态作为基线。预先存在的链接问题不阻塞 P4A，但必须在 Task 10 验证无新增问题。

- [ ] **Step 9：记录实施基线**

```powershell
$baseCommit = git rev-parse HEAD
$baseStatus = git status --short

Write-Output "BASE_COMMIT=$baseCommit"
Write-Output 'BASE_STATUS:'
$baseStatus
```

预期：记录完整 commit ID。若工作区存在与本计划目标路径重叠的修改，停止执行。

- [ ] **Step 10：确认用户已批准执行本计划**

检查当前对话或版本化批准记录中是否存在 `xinyan <zuijiuhjd@foxmail.com>` 对"执行 `2026-07-16-legacy-document-metadata.md`"的明确批准。没有明确批准时必须停止，不能把设计确认或计划编写授权解释为实施授权。

---

## Task 2：批次 1 — 原位保留长期参考文档元数据

**文件：**

- 修改：`docs/architecture.md`
- 修改：`docs/technical-reference.md`
- 修改：`docs/operations.md`
- 修改：`docs/test-plan.md`

**接口：**

- 消费：四层文档架构中"长期参考文档"的分类和 `Living` 状态定义。
- 产出：4 个文件的顶部增加原位保留元数据头，正文内容不变。

- [ ] **Step 1：为 `docs/architecture.md` 添加元数据头**

读取文件前 20 行，确认没有 YAML front matter 和已有元数据头。在第一个 `#` 标题之前插入：

```markdown
> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/architecture.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

元数据头与标题之间保留一个空行。

- [ ] **Step 2：为 `docs/technical-reference.md` 添加元数据头**

同上，插入：

```markdown
> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/technical-reference.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 3：为 `docs/operations.md` 添加元数据头**

同上，插入：

```markdown
> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/operations.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 4：为 `docs/test-plan.md` 添加元数据头**

同上，插入：

```markdown
> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/test-plan.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 5：验证批次 1 修改**

```powershell
$batch1 = @(
    'docs/architecture.md',
    'docs/technical-reference.md',
    'docs/operations.md',
    'docs/test-plan.md'
)

foreach ($file in $batch1) {
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -notmatch '> \*\*文档类型\*\*：长期参考文档') {
        throw "缺失元数据头：$file"
    }
    if ($content -notmatch '> \*\*P4B 迁移目标\*\*：原位保留') {
        throw "缺失原位保留声明：$file"
    }
    if ($content -notmatch '> \*\*治理记录\*\*：P4A 元数据治理') {
        throw "缺失治理记录：$file"
    }
}

Write-Output '批次 1 验证通过：4 个文件全部具有原位保留元数据。'
```

预期：命令无输出、无异常。

---

## Task 3：批次 2 — P4B 待迁移安全文档元数据

**文件：**

- 修改：`docs/download-source-policy.md`
- 修改：`docs/signing-guide.md`
- 修改：`docs/download-cache-recovery.md`

**接口：**

- 消费：设计规格第 14.2 节的安全文档迁移目标路径。
- 产出：3 个文件顶部增加待迁移元数据头，正文内容不变。

- [ ] **Step 1：为 `docs/download-source-policy.md` 添加元数据头**

```markdown
> **文档类型**：安全政策
> **状态**：Deprecated（待迁移）
> **来源**：docs/download-source-policy.md
> **P4B 迁移目标**：docs/security/policy/download-source-policy.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 2：为 `docs/signing-guide.md` 添加元数据头**

```markdown
> **文档类型**：安全操作流程
> **状态**：Deprecated（待迁移）
> **来源**：docs/signing-guide.md
> **P4B 迁移目标**：docs/security/procedures/signing-guide.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 3：为 `docs/download-cache-recovery.md` 添加元数据头**

```markdown
> **文档类型**：安全操作流程
> **状态**：Deprecated（待迁移）
> **来源**：docs/download-cache-recovery.md
> **P4B 迁移目标**：docs/security/procedures/download-cache-recovery.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 4：验证批次 2 修改**

```powershell
$batch2 = @{
    'docs/download-source-policy.md' = 'docs/security/policy/download-source-policy.md'
    'docs/signing-guide.md' = 'docs/security/procedures/signing-guide.md'
    'docs/download-cache-recovery.md' = 'docs/security/procedures/download-cache-recovery.md'
}

foreach ($entry in $batch2.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    if ($content -notmatch '> \*\*文档类型\*\*：') {
        throw "缺失文档类型：$($entry.Key)"
    }
    if ($content -notmatch '（待迁移）') {
        throw "缺失待迁移状态：$($entry.Key)"
    }
    if ($content -notmatch [regex]::Escape($entry.Value)) {
        throw "P4B 迁移目标不匹配：$($entry.Key)，期望：$($entry.Value)"
    }
}

Write-Output '批次 2 验证通过：3 个文件全部具有正确的迁移目标。'
```

预期：命令无输出、无异常。

---

## Task 4：批次 3 — P4B 待迁移审计文档元数据

**文件：**

- 修改：`docs/security-audit.md`
- 修改：`docs/security-audit-verification.md`

**接口：**

- 消费：设计规格第 14.2 节的审计文档迁移目标路径和审计日期。
- 产出：2 个文件顶部增加待迁移元数据头，正文内容不变。

- [ ] **Step 1：为 `docs/security-audit.md` 添加元数据头**

```markdown
> **文档类型**：时间点审计
> **状态**：Snapshot（待迁移）
> **来源**：docs/security-audit.md
> **审计日期**：2026-07-04
> **P4B 迁移目标**：docs/audits/2026-07-04-security-audit.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 2：为 `docs/security-audit-verification.md` 添加元数据头**

```markdown
> **文档类型**：时间点审计
> **状态**：Snapshot（待迁移）
> **来源**：docs/security-audit-verification.md
> **审计日期**：2026-07-08
> **P4B 迁移目标**：docs/audits/2026-07-08-security-audit-verification.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 3：验证批次 3 修改**

```powershell
$batch3 = @{
    'docs/security-audit.md' = @{
        Type = '时间点审计'
        Target = 'docs/audits/2026-07-04-security-audit.md'
        Date = '2026-07-04'
    }
    'docs/security-audit-verification.md' = @{
        Type = '时间点审计'
        Target = 'docs/audits/2026-07-08-security-audit-verification.md'
        Date = '2026-07-08'
    }
}

foreach ($entry in $batch3.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    if ($content -notmatch '> \*\*文档类型\*\*：时间点审计') {
        throw "缺失文档类型：$($entry.Key)"
    }
    if ($content -notmatch [regex]::Escape($entry.Value.Date)) {
        throw "缺失审计日期：$($entry.Key)"
    }
    if ($content -notmatch [regex]::Escape($entry.Value.Target)) {
        throw "P4B 迁移目标不匹配：$($entry.Key)"
    }
}

Write-Output '批次 3 验证通过：2 个文件全部具有正确的审计日期和迁移目标。'
```

预期：命令无输出、无异常。

---

## Task 5：批次 4 — P4B 待处理的分析与规划文档元数据

**文件：**

- 修改：`docs/pending-work-plan.md`
- 修改：`docs/progress-and-defects-analysis.md`
- 修改：`docs/competitive-analysis.md`
- 修改：`docs/git-initial-import-plan.md`

**接口：**

- 消费：设计规格第 14.1 节的计划与 backlog 文档处理方式。
- 产出：4 个文件顶部增加元数据头，正文内容不变。

- [ ] **Step 1：为 `docs/pending-work-plan.md` 添加元数据头**

```markdown
> **文档类型**：工作计划
> **状态**：Frozen（待拆分）
> **来源**：docs/pending-work-plan.md
> **P4B 迁移目标**：独立事项转为 Feature Spec 后归档至 docs/history/implementation-plans/
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 2：为 `docs/progress-and-defects-analysis.md` 添加元数据头**

```markdown
> **文档类型**：分析报告
> **状态**：Snapshot
> **来源**：docs/progress-and-defects-analysis.md
> **P4B 迁移目标**：待用户决定
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 3：为 `docs/competitive-analysis.md` 添加元数据头**

```markdown
> **文档类型**：分析报告
> **状态**：Snapshot
> **来源**：docs/competitive-analysis.md
> **P4B 迁移目标**：待用户决定
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 4：为 `docs/git-initial-import-plan.md` 添加元数据头**

```markdown
> **文档类型**：实施计划
> **状态**：Superseded（待迁移）
> **来源**：docs/git-initial-import-plan.md
> **P4B 迁移目标**：docs/history/implementation-plans/git-initial-import-plan.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 5：验证批次 4 修改**

```powershell
$batch4 = @{
    'docs/pending-work-plan.md' = 'Frozen（待拆分）'
    'docs/progress-and-defects-analysis.md' = 'Snapshot'
    'docs/competitive-analysis.md' = 'Snapshot'
    'docs/git-initial-import-plan.md' = 'Superseded（待迁移）'
}

foreach ($entry in $batch4.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    if ($content -notmatch '> \*\*文档类型\*\*：') {
        throw "缺失文档类型：$($entry.Key)"
    }
    if ($content -notmatch [regex]::Escape($entry.Value)) {
        throw "状态不匹配：$($entry.Key)，期望：$($entry.Value)"
    }
}

Write-Output '批次 4 验证通过：4 个文件全部具有正确的文档类型和状态。'
```

预期：命令无输出、无异常。

---

## Task 6：批次 5 — 根目录与 `.trae` 旧文档元数据

**文件：**

- 修改：`IMPROVEMENT-PLAN.md`
- 修改：`.trae/documents/xdev-improvement-roadmap.md`
- 修改：`.trae/specs/enhance-gui-ux-and-fixes/spec.md`
- 修改：`.trae/specs/enhance-gui-ux-and-fixes/tasks.md`
- 修改：`.trae/specs/enhance-gui-ux-and-fixes/checklist.md`

**接口：**

- 消费：设计规格第 14.1 节的 `.trae` 和根目录文档迁移目标。
- 产出：5 个文件顶部增加元数据头，正文内容不变。

- [ ] **Step 1：为 `IMPROVEMENT-PLAN.md` 添加元数据头**

```markdown
> **文档类型**：Backlog
> **状态**：Frozen（待转换）
> **来源**：IMPROVEMENT-PLAN.md
> **P4B 迁移目标**：docs/backlog.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 2：为 `.trae/documents/xdev-improvement-roadmap.md` 添加元数据头**

```markdown
> **文档类型**：Roadmap
> **状态**：Frozen（待转换）
> **来源**：.trae/documents/xdev-improvement-roadmap.md
> **P4B 迁移目标**：docs/roadmap.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 3：为 `.trae/specs/enhance-gui-ux-and-fixes/spec.md` 添加元数据头**

```markdown
> **文档类型**：Feature Spec（旧）
> **状态**：Archived（待迁移）
> **来源**：.trae/specs/enhance-gui-ux-and-fixes/spec.md
> **P4B 迁移目标**：specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 4：为 `.trae/specs/enhance-gui-ux-and-fixes/tasks.md` 添加元数据头**

```markdown
> **文档类型**：任务（旧）
> **状态**：Archived（待迁移）
> **来源**：.trae/specs/enhance-gui-ux-and-fixes/tasks.md
> **P4B 迁移目标**：specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 5：为 `.trae/specs/enhance-gui-ux-and-fixes/checklist.md` 添加元数据头**

```markdown
> **文档类型**：Checklist（旧）
> **状态**：Archived（待迁移）
> **来源**：.trae/specs/enhance-gui-ux-and-fixes/checklist.md
> **P4B 迁移目标**：specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 6：验证批次 5 修改**

```powershell
$batch5 = @{
    'IMPROVEMENT-PLAN.md' = 'docs/backlog.md'
    '.trae/documents/xdev-improvement-roadmap.md' = 'docs/roadmap.md'
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md' = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md'
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md' = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md'
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md' = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md'
}

foreach ($entry in $batch5.GetEnumerator()) {
    $content = Get-Content -LiteralPath $entry.Key -Raw
    if ($content -notmatch '> \*\*文档类型\*\*：') {
        throw "缺失文档类型：$($entry.Key)"
    }
    if ($content -notmatch [regex]::Escape($entry.Value)) {
        throw "P4B 迁移目标不匹配：$($entry.Key)，期望：$($entry.Value)"
    }
}

Write-Output '批次 5 验证通过：5 个文件全部具有正确的迁移目标。'
```

预期：命令无输出、无异常。

---

## Task 7：批次 6 — Superpowers 计划文档元数据

**文件：**

- 修改：`docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md`

**接口：**

- 消费：设计规格第 14.1 节的旧实施计划迁移目标。
- 产出：1 个文件顶部增加元数据头，正文内容不变。

- [ ] **Step 1：为 `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` 添加元数据头**

```markdown
> **文档类型**：实施计划
> **状态**：Superseded（待迁移）
> **来源**：docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md
> **P4B 迁移目标**：docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
```

- [ ] **Step 2：验证批次 6 修改**

```powershell
$content = Get-Content -LiteralPath 'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md' -Raw
if ($content -notmatch '> \*\*文档类型\*\*：实施计划') {
    throw '缺失文档类型'
}
if ($content -notmatch '> \*\*P4B 迁移目标\*\*：docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md') {
    throw 'P4B 迁移目标不匹配'
}

Write-Output '批次 6 验证通过。'
```

预期：命令无输出、无异常。

---

## Task 8：全量元数据完整性验证

**文件：**

- 验证：本计划修改的全部 19 个目标文件
- 不创建新文件

**接口：**

- 消费：Tasks 2-7 的全部交付物。
- 产出：元数据完整性、字段一致性、内容不变性验证证据。

- [ ] **Step 1：验证全部 19 个文件已添加元数据头**

```powershell
$allTargets = @(
    'docs/architecture.md',
    'docs/technical-reference.md',
    'docs/operations.md',
    'docs/test-plan.md',
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/pending-work-plan.md',
    'docs/progress-and-defects-analysis.md',
    'docs/competitive-analysis.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md',
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md',
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

$untagged = @($allTargets | Where-Object {
    $content = Get-Content -LiteralPath $_ -Raw
    $content -notmatch '> \*\*文档类型\*\*：'
})

if ($untagged.Count -gt 0) {
    $untagged
    throw '以下文件缺失元数据头'
}

Write-Output "全部 $($allTargets.Count) 个文件已具有元数据头。"
```

预期：无缺失文件。

- [ ] **Step 2：验证元数据字段完整性**

```powershell
$requiredFields = @('文档类型', '状态', '来源', 'P4B 迁移目标', '替代文档', '治理记录')
$fieldProblems = @()

foreach ($file in $allTargets) {
    $content = Get-Content -LiteralPath $file -Raw
    foreach ($field in $requiredFields) {
        if ($content -notmatch [regex]::Escape("> **${field}**：")) {
            $fieldProblems += "${file}：缺少字段 `${field}`"
        }
    }
}

if ($fieldProblems.Count -gt 0) {
    $fieldProblems
    throw '元数据字段不完整'
}

Write-Output '全部文件元数据字段完整。'
```

预期：无字段缺失。

- [ ] **Step 3：验证没有元数据误写为已生效状态**

```powershell
$invalidClaims = @()

foreach ($file in $allTargets) {
    $content = Get-Content -LiteralPath $file -Raw
    # 检查没有错误声明 Constitution 已生效
    if ($content -match 'Constitution.*已生效|Constitution.*正式批准.*已') {
        $invalidClaims += "${file}：错误声明 Constitution 已生效"
    }
    # 检查没有声明 P4B 迁移已完成
    if ($content -match 'P4B.*已完成|迁移已执行|已迁移') {
        $invalidClaims += "${file}：错误声明迁移已完成"
    }
}

if ($invalidClaims.Count -gt 0) {
    $invalidClaims
    throw '元数据包含不实声明'
}

Write-Output '无错误生效或迁移完成声明。'
```

预期：无输出、无异常。

- [ ] **Step 4：验证文档正文内容未被修改**

通过与基线 commit 的 diff 检查，确认每个修改文件的变更**仅限于**新增的元数据头行，正文未发生任何语义变化。

```powershell
$baseCommit = '<Task 1 Step 9 记录的 BASE_COMMIT>'

foreach ($file in $allTargets) {
    $diff = git diff $baseCommit -- $file
    if (-not $diff) {
        throw "${file}：没有变更，元数据头未添加？"
    }
    # 粗略检查：所有新增行应以 '>' 开头（元数据块）或为空行
    $addedLines = ($diff | Select-String -Pattern '^\+[^+]' | ForEach-Object { $_.Line })
    foreach ($line in $addedLines) {
        $trimmed = $line -replace '^\+', ''
        if ($trimmed -ne '' -and $trimmed -notmatch '^>') {
            throw "${file}：非元数据行被修改：$trimmed"
        }
    }
}

Write-Output '全部文件仅新增元数据头，正文未修改。'
```

预期：命令无输出、无异常。

---

## Task 9：运行链接验证（P3 交付物）

**文件：**

- 执行：`scripts/docs/validate-links.ps1`
- 不修改文件

**接口：**

- 消费：P3 交付的链接验证脚本。
- 产出：链接验证结果，与 Task 1 基线对比确认无新增问题。

- [ ] **Step 1：重新运行链接验证脚本**

```powershell
$result = & 'scripts/docs/validate-links.ps1' 2>&1
$exitCode = $LASTEXITCODE

Write-Output $result
Write-Output "退出码：$exitCode"
```

- [ ] **Step 2：对比 Task 1 基线，确认无新增链接问题**

对比 Task 1 Step 8 的基线输出与本次输出：

- 如果两次退出码相同且错误数量相同，确认无新增问题。
- 如果新增链接错误，审查是否为元数据头中的路径引入。元数据头中的 `来源` 和 `P4B 迁移目标` 使用纯文本路径（非 Markdown 链接），不应产生链接问题。
- 如果新增链接问题与元数据头无关，记录但不阻塞 P4A 验收（这些是预先存在或 P4A 范围外的问题）。

---

## Task 10：双重独立审查与提交

**文件：**

- 审查：本计划修改的全部 19 个目标文件
- 提交：仅本计划修改的文件白名单

**接口：**

- 消费：完整元数据差异和验证证据。
- 产出：两个独立审查结论、用户验收和一个本地 Git 提交。

- [ ] **Step 1：重新声明白名单并生成可审查的暂存差异**

```powershell
$expected = @(
    'docs/architecture.md',
    'docs/technical-reference.md',
    'docs/operations.md',
    'docs/test-plan.md',
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/pending-work-plan.md',
    'docs/progress-and-defects-analysis.md',
    'docs/competitive-analysis.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md',
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md',
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

if ($expected.Count -ne 19) {
    throw "白名单文件数量错误：$($expected.Count)"
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
docs/superpowers/plans/2026-07-16-legacy-document-metadata.md
本计划产生的差异
```

必须逐项判定：

- 19 个文件是否全部在 P4A 文件所有权范围内（路线图第 145-147 行）；
- 元数据字段是否与设计规格第 5-6 节一致；
- 是否错误声明 Constitution 已生效或迁移已完成；
- 文档类型和状态是否与迁移矩阵（设计规格第 14 节）一致；
- P4B 迁移目标路径是否符合四层文档架构（设计规格第 4 节）；
- 是否移动了任何文件（P4A 绝不允许移动）；
- 是否修改了文档正文语义。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 3：执行工程质量审查**

使用另一个全新的独立 Agent 会话执行审查，不复用实现者或 Spec 合规审查者会话。第二审查者必须不同于实现者和 Spec 合规审查者，检查：

- 元数据头格式是否统一、插入位置是否一致；
- 所有路径（来源、P4B 迁移目标、替代文档）是否使用正斜杠；
- 状态名是否与设计规格第 5 节合法状态一致；
- `（待迁移）`、`（待拆分）`、`（待转换）` 后缀是否准确匹配设计规格第 14 节的处理方式；
- 元数据头中的日期是否一致（全部为 P4A 执行日期）；
- 是否存在敏感数据、不可验证声明或过度设计；
- 所有 diff 变更是否仅限元数据头（无正文修改）。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 4：请求用户验收**

向用户提供：

- 修改文件清单（19 个文件）；
- 聚焦验证结果（Task 8 全量完整性验证）；
- 链接验证对比结果（Task 9）；
- Spec 合规审查结论；
- 工程质量审查结论；
- 明确说明：元数据已添加，文件未移动，P4B 尚未执行。

只有用户明确回复批准 P4A 验收或含义同等明确的指令后才能继续提交。泛化的"继续"不得解释为验收批准。

- [ ] **Step 5：按白名单暂存**

```powershell
git add -- $expected

if ($expected.Count -ne 19) {
    throw "白名单文件数量错误：$($expected.Count)"
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

预期：暂存区只包含 19 个目标文件。

- [ ] **Step 6：创建提交**

```powershell
git commit -m "docs: add governance metadata to legacy documents (P4A)"
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

- 提交包含且仅包含 19 个目标文件；
- `keys/priv.key` 查询无输出；
- 不存在本计划产生的未提交文件。

---

## 验收标准

- **AC-001**：19 个旧文档全部在原位置添加元数据头，无文件被移动。
- **AC-002**：每个元数据头包含 6 个必需字段：`文档类型`、`状态`、`来源`、`P4B 迁移目标`、`替代文档`、`治理记录`。
- **AC-003**：原位保留的长期参考文档（4 个）状态为 `Living`，P4B 迁移目标为 `原位保留`。
- **AC-004**：P4B 待迁移文档（15 个）状态正确标记 `（待迁移）`、`（待拆分）` 或 `（待转换）`，P4B 迁移目标与设计规格迁移矩阵一致。
- **AC-005**：审计文档记录审计日期字段。
- **AC-006**：文档正文内容语义未被修改，diff 仅含新增元数据头行。
- **AC-007**：链接验证与 P4A 基线对比无新增问题。
- **AC-008**：元数据不含错误声明（Constitution 已生效、迁移已完成等）。
- **AC-009**：`keys/priv.key` 未被跟踪或暂存。
- **AC-010**：两个独立审查门禁和用户验收均通过后才创建提交。
- **AC-011**：`.trae/specs/enhance-gui-ux-and-fixes/` 下的 3 个文件均添加元数据，P4B 迁移目标指向 `specs/archive/2026/000-enhance-gui-ux-and-fixes/`。

---

## 失败恢复

### 提交前

如果暂存了错误文件：

```powershell
git restore --staged -- <path>
```

如果某个文件的元数据头格式需要修正，直接编辑该文件，不还原用户的其他并发修改。

### 提交后

如果提交后发现非敏感遗漏（如元数据字段笔误），创建后续修复提交，不自动 amend。

如果私钥或其他凭据进入提交，立即停止，不配置 remote、不推送，并启动独立密钥事件响应方案。

---

## 不授权事项

P4A 明确不授权以下工作：

- **P4B 历史文档迁移：** 文件移动、转换、归档和引用更新由 P4B 负责。P4A 只提供元数据，不执行任何文件移动。
- **P5 治理自动化：** 治理脚本、CI 报告模式等由 P5 负责。
- **文档内容改写：** 不修正旧文档中的错误信息、过时结论或不完整描述。纠偏通过 `errata.md` 或替代关系在 P4B 及后续阶段完成。
- **新建 Feature Spec：** `docs/pending-work-plan.md` 的拆分需要在 P4B 或独立计划中创建正式 Feature Spec，P4A 只标记"待拆分"状态。
- **冻结或删除旧位置：** P4B 移动文件后，旧位置的冻结和清理由 P4B 负责。
- **用户批准之外的权限：** 不代填批准记录、不自行将任何文档标记为已生效、不声称 P4A 完成了 P4B 的工作。

## 后续计划

P4A 完成并经用户验收后，可进入 P4B：

- `docs/superpowers/plans/2026-07-15-document-migration.md`（历史文档迁移）

P4B 在执行时将读取 P4A 添加的 `P4B 迁移目标` 字段执行实际移动。P4A 与 P4B 必须串行执行，不能并行。
