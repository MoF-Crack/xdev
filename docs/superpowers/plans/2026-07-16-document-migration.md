# XDev 历史文档迁移实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

**目标：** 读取 P4A 在旧文档顶部添加的元数据头中的 `P4B 迁移目标` 字段，使用 `git mv` 将文档实际移动到目标位置，保留 Git 历史，更新仓库内交叉引用，为归档规格创建归档元数据文件。

**架构：** P4B 是 P4A 的直接下游子项目，二者串行执行。P4A 已在每个文档的元数据头中声明了迁移动机、目标路径和当前状态；P4B 读取这些声明，逐批次执行实际文件移动操作。迁移完成后更新元数据状态（去掉 `（待迁移）` 后缀），运行链接验证确认无新增断链，创建归档元数据记录。

**技术栈：** Markdown、Git（`git mv`）、Windows PowerShell 5.1、`scripts/docs/validate-links.ps1`。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.3`，状态为候选版，尚未正式批准。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`。
- 关联路线图：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`。
- 关联上游计划：`docs/superpowers/plans/2026-07-16-legacy-document-metadata.md`（P4A）。P4A 必须已完成且经用户验收；P4B 不可在 P4A 完成前执行。
- 本计划依赖 P1（治理骨架）、P3（文档链接验证）和 P4A（旧文档元数据治理）的交付物：
  - P1：目标目录已创建（`docs/security/policy/`、`docs/security/procedures/`、`docs/audits/`、`docs/history/implementation-plans/`、`specs/archive/` 等）；
  - P3：文档链接验证脚本 `scripts/docs/validate-links.ps1` 已就位；
  - P4A：10 个待迁移文件的元数据头已添加，`P4B 迁移目标` 字段包含准确的目标路径。
- **所有文件移动必须使用 `git mv`**，不得使用操作系统复制+删除或 PowerShell `Move-Item`。`git mv` 保留文件的完整 Git 历史（blame、log、diff 可追溯）。
- **不修改文档正文内容。** 只允许以下两类内容变更：
  1. 元数据头中的状态后缀更新（将 `（待迁移）` / `（待拆分）` / `（待转换）` 更新为已完成状态）；
  2. 其他 Markdown 文件中指向旧路径的链接更新。
- 不得重写、纠偏或改写任何文档的正文语义。正文仅通过 `git mv` 移动位置。
- `git mv` 的目标目录必须已存在（P1 交付物已创建）。如果目标目录下缺少 `README.md` 索引，应在迁移前确认其存在。
- 迁移后立即运行 `scripts/docs/validate-links.ps1`，确认无新增断链。
- 当前分支必须为 `main`，必须存在可用 `HEAD`，且不得配置远程仓库或执行推送。
- `keys/priv.key` 永远不得暂存或提交。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- Constitution 候选期限制：P4B 的迁移操作不能声称 Constitution 已生效或治理体系已完备。元数据状态更新只能反映迁移完成，不能将文档状态声明为治理批准状态。
- 本计划必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准进入执行。

---

## 文件结构

### 迁移矩阵（P4B 执行移动的 10 组文件）

以下为 P4A 元数据头中 `P4B 迁移目标` 不为"原位保留"或"待用户决定"的全部文档。P4B 将这些文件从源路径移动到目标路径：

| 序号 | 源路径 | 目标路径 | P4A 标记状态 | 迁移后状态 | 迁移方式 |
|------|--------|----------|-------------|-----------|---------|
| 1 | `docs/download-source-policy.md` | `docs/security/policy/download-source-policy.md` | `Deprecated（待迁移）` | `Deprecated` | `git mv` 单文件 |
| 2 | `docs/signing-guide.md` | `docs/security/procedures/signing-guide.md` | `Deprecated（待迁移）` | `Deprecated` | `git mv` 单文件 |
| 3 | `docs/download-cache-recovery.md` | `docs/security/procedures/download-cache-recovery.md` | `Deprecated（待迁移）` | `Deprecated` | `git mv` 单文件 |
| 4 | `docs/security-audit.md` | `docs/audits/2026-07-04-security-audit.md` | `Snapshot（待迁移）` | `Snapshot` | `git mv` 单文件 |
| 5 | `docs/security-audit-verification.md` | `docs/audits/2026-07-08-security-audit-verification.md` | `Snapshot（待迁移）` | `Snapshot` | `git mv` 单文件 |
| 6 | `docs/git-initial-import-plan.md` | `docs/history/implementation-plans/git-initial-import-plan.md` | `Superseded（待迁移）` | `Superseded` | `git mv` 单文件 |
| 7 | `IMPROVEMENT-PLAN.md` | `docs/backlog.md` | `Frozen（待转换）` | `Frozen` | `git mv` 单文件 |
| 8 | `.trae/documents/xdev-improvement-roadmap.md` | `docs/roadmap.md` | `Frozen（待转换）` | `Frozen` | `git mv` 单文件 |
| 9 | `.trae/specs/enhance-gui-ux-and-fixes/spec.md` | `specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md` | `Archived（待迁移）` | `Archived` | `git mv` 目录下 3 个文件 |
| — | `.trae/specs/enhance-gui-ux-and-fixes/tasks.md` | `specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md` | `Archived（待迁移）` | `Archived` | 同上批次 |
| — | `.trae/specs/enhance-gui-ux-and-fixes/checklist.md` | `specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md` | `Archived（待迁移）` | `Archived` | 同上批次 |
| 10 | `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` | `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` | `Superseded（待迁移）` | `Superseded` | `git mv` 单文件 |

> **注意：** 序号 9 包含 3 个文件，同属于 `.trae/specs/enhance-gui-ux-and-fixes/` 目录。迁移时需先创建目标目录 `specs/archive/2026/000-enhance-gui-ux-and-fixes/`，然后逐文件执行 `git mv`。

### 不迁移的文件（P4A 标记为"原位保留"）

以下文件已在 P4A 中标记为原位保留，P4B 不移动：

| 文件路径 | P4A 状态 | P4B 处理 |
|----------|---------|---------|
| `docs/architecture.md` | `Living` | 不移动 |
| `docs/technical-reference.md` | `Living` | 不移动 |
| `docs/operations.md` | `Living` | 不移动 |
| `docs/test-plan.md` | `Living` | 不移动 |

### 不迁移的文件（P4A 标记为"待用户决定"或条件未满足）

以下文件在 P4A 中有元数据头，但其迁移目标为"待用户决定"或需要前置条件（如拆分为 Feature Spec），P4B 不移动：

| 文件路径 | P4A 状态 | 原因 |
|----------|---------|------|
| `docs/pending-work-plan.md` | `Frozen（待拆分）` | 需先拆分为独立 Feature Spec 后才能归档；拆分工作不在 P4B 范围内 |
| `docs/progress-and-defects-analysis.md` | `Snapshot` | P4B 迁移目标为"待用户决定" |
| `docs/competitive-analysis.md` | `Snapshot` | P4B 迁移目标为"待用户决定" |

### 目标目录索引文件

P4B 迁移后，以下目标目录索引文件可能需要更新以反映新增内容（如 `docs/history/implementation-plans/README.md`、`docs/security/README.md` 等）。P1 创建的索引文件内容由 P1 维护；如果迁移后的文件列表与索引文件中的描述不一致，P4B 应更新索引文件中相关段落，但不重建索引文件。

---

## 接口契约

### 输入

- `.specify/memory/constitution.md` `1.0.0-rc.3`：治理原则和候选期限制。
- `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`：文档分类、状态模型和迁移矩阵（第 14 节）。
- `docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`：子项目边界和 P4A/P4B 串行约束。
- `docs/superpowers/plans/2026-07-16-legacy-document-metadata.md`：P4A 计划与元数据格式定义。
- `scripts/docs/validate-links.ps1`：链接验证脚本（P3 交付物）。
- `.specify/templates/overrides/archive-metadata-template.md`：归档元数据模板（P2B 交付物）。
- 迁移矩阵中列出的 10 组（共 12 个）源文件。
- 仓库内所有可能包含指向旧路径链接的 Markdown 文件。

### 输出

- 10 组文件使用 `git mv` 从源路径移动到目标路径。
- 每个已迁移文件的元数据头状态后缀更新（去掉 `（待迁移）` 等后缀，并追加迁移记录行）。
- 仓库内其他 Markdown 文件中指向旧路径的链接更新为新路径。
- `specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md` 归档元数据文件。
- 链接验证结果报告（与 P4A 基线对比，确认无新增断链）。

---

## 元数据状态更新规则

P4A 在迁移前文档顶部添加了元数据头。P4B 迁移后必须更新元数据状态以反映迁移完成：

### 状态更新映射

| P4A 状态 | 迁移后状态 | 说明 |
|----------|-----------|------|
| `Deprecated（待迁移）` | `Deprecated` | 安全文档迁移完成，历史政策/流程 |
| `Snapshot（待迁移）` | `Snapshot` | 审计文档迁移完成，时间点记录 |
| `Superseded（待迁移）` | `Superseded` | 旧计划迁移完成，已被替代 |
| `Frozen（待转换）` | `Frozen` | Backlog/Roadmap 转换完成 |
| `Archived（待迁移）` | `Archived` | 旧规格归档完成 |

### 追加迁移记录行

每个迁移后的文件在元数据头末尾追加一行：

```markdown
> **迁移记录**：P4B 迁移完成，2026-07-16，源路径：<原始路径>
```

示例（安全政策文档迁移后完整元数据头）：

```markdown
> **文档类型**：安全政策
> **状态**：Deprecated
> **来源**：docs/download-source-policy.md
> **P4B 迁移目标**：docs/security/policy/download-source-policy.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
> **迁移记录**：P4B 迁移完成，2026-07-16，源路径：docs/download-source-policy.md
```

### 不修改元数据头的文件

以下原位保留文件不修改元数据头：

- `docs/architecture.md`
- `docs/technical-reference.md`
- `docs/operations.md`
- `docs/test-plan.md`
- `docs/pending-work-plan.md`
- `docs/progress-and-defects-analysis.md`
- `docs/competitive-analysis.md`

---

## Task 1：执行前门禁和基线记录

**文件：**

- 读取：`.specify/memory/constitution.md`
- 读取：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`
- 读取：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`
- 读取：`docs/superpowers/plans/2026-07-16-legacy-document-metadata.md`
- 不修改文件

**接口：**

- 消费：P1、P3 和 P4A 交付物、当前仓库状态和 P4A 元数据。
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
if ($commitCount -lt 5) {
    throw "初始导入提交不足 5 个（P4A 已提交至少 1 次）：$commitCount"
}
```

预期：`$commitCount` 至少为 5（Git 初始导入 ≥4 次 + P4A 提交 ≥1 次）。

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

- [ ] **Step 5：验证上游依赖 P1、P3、P4A 已就位**

```powershell
$dependencies = @(
    # P1 交付物
    'docs/README.md',
    'docs/security/README.md',
    'docs/security/policy/README.md',
    'docs/security/procedures/README.md',
    'docs/audits/README.md',
    'docs/history/README.md',
    'docs/history/implementation-plans/README.md',
    'specs/README.md',
    'specs/archive/README.md',
    # P3 交付物
    'scripts/docs/validate-links.ps1',
    # P2B 交付物（归档元数据模板）
    '.specify/templates/overrides/archive-metadata-template.md'
)

$missing = @($dependencies | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    $missing
    throw '上游依赖交付物缺失，P4B 不能执行'
}
```

预期：无缺失依赖。

- [ ] **Step 6：验证 P4A 元数据已就位——读取迁移目标**

```powershell
$migrationSources = @(
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md',
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md',
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

$missing = @($migrationSources | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    $missing
    throw '源文件缺失，P4B 不能执行'
}

Write-Output "源文件总数：$($migrationSources.Count)"
```

预期：12 个源文件全部存在。

- [ ] **Step 7：提取并验证每个文件的 P4A 元数据中的迁移目标**

```powershell
$metadataProblems = @()

foreach ($file in $migrationSources) {
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -notmatch '> \*\*P4B 迁移目标\*\*：(.+)') {
        $metadataProblems += "${file}：缺失 P4B 迁移目标字段"
        continue
    }
    $target = $Matches[1].Trim()
    if ($target -match '原位保留|待用户决定') {
        $metadataProblems += "${file}：P4B 迁移目标为 `${target}`，不在本计划迁移范围内"
        continue
    }
    Write-Output "$file → $target"
}

if ($metadataProblems.Count -gt 0) {
    $metadataProblems
    throw '元数据问题：部分文件的迁移目标不在 P4B 范围内'
}
```

预期：12 个源文件均具有明确的迁移目标路径。

- [ ] **Step 8：验证目标目录全部存在**

```powershell
$targetDirs = @(
    'docs/security/policy',
    'docs/security/procedures',
    'docs/audits',
    'docs/history/implementation-plans',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes'
)

# 需要创建的目标目录（如果尚不存在）
$dirsToCreate = @($targetDirs | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Container) })
if ($dirsToCreate.Count -gt 0) {
    Write-Output '以下目标目录尚不存在，将在迁移前创建：'
    $dirsToCreate
    foreach ($dir in $dirsToCreate) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        Write-Output "已创建目录：$dir"
    }
} else {
    Write-Output '全部目标目录已存在。'
}
```

预期：`specs/archive/2026/000-enhance-gui-ux-and-fixes/` 可能需要创建；其他目标目录应由 P1 创建。

- [ ] **Step 9：运行 P3 链接验证脚本建立 P4B 前基线**

```powershell
$result = & 'scripts/docs/validate-links.ps1' 2>&1
$baselineExitCode = $LASTEXITCODE

Write-Output "P4B 前链接验证退出码：$baselineExitCode"
Write-Output $result

# 保存基线输出到临时文件供后续对比
$result | Out-File -LiteralPath "$env:TEMP\p4b-link-baseline.txt" -Encoding utf8
$baselineExitCode | Out-File -LiteralPath "$env:TEMP\p4b-link-baseline-exitcode.txt" -Encoding utf8

Write-Output "基线已保存至 `$env:TEMP\p4b-link-baseline.txt"
```

预期：记录当前链接校验状态作为 P4B 前基线。预先存在的链接问题不阻塞 P4B，但必须在 Task 10 验证无新增问题。

- [ ] **Step 10：记录实施基线**

```powershell
$baseCommit = git rev-parse HEAD
$baseStatus = git status --short

Write-Output "BASE_COMMIT=$baseCommit"
Write-Output 'BASE_STATUS:'
$baseStatus
```

预期：记录完整 commit ID。若工作区存在与迁移目标路径重叠的修改，停止执行。

- [ ] **Step 11：确认用户已批准执行本计划**

检查当前对话或版本化批准记录中是否存在 `xinyan <zuijiuhjd@foxmail.com>` 对"执行 `2026-07-16-document-migration.md`"的明确批准。没有明确批准时必须停止。

---

## Task 2：批次 1 — 安全文档迁移（3 个文件）

**文件：**

- 移动：`docs/download-source-policy.md` → `docs/security/policy/download-source-policy.md`
- 移动：`docs/signing-guide.md` → `docs/security/procedures/signing-guide.md`
- 移动：`docs/download-cache-recovery.md` → `docs/security/procedures/download-cache-recovery.md`

**接口：**

- 消费：P4A 元数据头中的迁移目标路径。
- 产出：3 个安全文档使用 `git mv` 移动到 `docs/security/` 子目录，元数据状态更新为已完成。

- [ ] **Step 1：使用 `git mv` 迁移 `docs/download-source-policy.md`**

```powershell
git mv docs/download-source-policy.md docs/security/policy/download-source-policy.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/download-source-policy.md'
}
Write-Output '已迁移：docs/download-source-policy.md → docs/security/policy/download-source-policy.md'
```

- [ ] **Step 2：更新 `docs/security/policy/download-source-policy.md` 元数据状态**

读取迁移后文件，将元数据头中的 `Deprecated（待迁移）` 更新为 `Deprecated`，并在 `治理记录` 行后追加 `迁移记录` 行：

```markdown
> **迁移记录**：P4B 迁移完成，2026-07-16，源路径：docs/download-source-policy.md
```

- [ ] **Step 3：使用 `git mv` 迁移 `docs/signing-guide.md`**

```powershell
git mv docs/signing-guide.md docs/security/procedures/signing-guide.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/signing-guide.md'
}
Write-Output '已迁移：docs/signing-guide.md → docs/security/procedures/signing-guide.md'
```

- [ ] **Step 4：更新 `docs/security/procedures/signing-guide.md` 元数据状态**

将 `Deprecated（待迁移）` 更新为 `Deprecated`，追加迁移记录行。

- [ ] **Step 5：使用 `git mv` 迁移 `docs/download-cache-recovery.md`**

```powershell
git mv docs/download-cache-recovery.md docs/security/procedures/download-cache-recovery.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/download-cache-recovery.md'
}
Write-Output '已迁移：docs/download-cache-recovery.md → docs/security/procedures/download-cache-recovery.md'
```

- [ ] **Step 6：更新 `docs/security/procedures/download-cache-recovery.md` 元数据状态**

将 `Deprecated（待迁移）` 更新为 `Deprecated`，追加迁移记录行。

- [ ] **Step 7：验证批次 1 迁移完整性**

```powershell
$batch1 = @(
    @{ Source = 'docs/download-source-policy.md'; Target = 'docs/security/policy/download-source-policy.md' },
    @{ Source = 'docs/signing-guide.md'; Target = 'docs/security/procedures/signing-guide.md' },
    @{ Source = 'docs/download-cache-recovery.md'; Target = 'docs/security/procedures/download-cache-recovery.md' }
)

foreach ($entry in $batch1) {
    if (Test-Path -LiteralPath $entry.Source -PathType Leaf) {
        throw "源文件仍存在：$($entry.Source)"
    }
    if (-not (Test-Path -LiteralPath $entry.Target -PathType Leaf)) {
        throw "目标文件不存在：$($entry.Target)"
    }
    $content = Get-Content -LiteralPath $entry.Target -Raw
    if ($content -notmatch '> \*\*迁移记录\*\*：P4B 迁移完成') {
        throw "缺失迁移记录：$($entry.Target)"
    }
    if ($content -match '（待迁移）') {
        throw "仍含有待迁移状态：$($entry.Target)"
    }
    # 验证 git 历史保留
    $log = git log --oneline --follow -- $entry.Target
    if (-not $log) {
        throw "git log --follow 无输出：$($entry.Target)"
    }
}

Write-Output '批次 1 验证通过：3 个安全文档已迁移，Git 历史保留，元数据状态已更新。'
```

预期：命令无输出、无异常。

---

## Task 3：批次 2 — 审计文档迁移（2 个文件）

**文件：**

- 移动：`docs/security-audit.md` → `docs/audits/2026-07-04-security-audit.md`
- 移动：`docs/security-audit-verification.md` → `docs/audits/2026-07-08-security-audit-verification.md`

**接口：**

- 消费：P4A 元数据头中的审计日期和迁移目标路径。
- 产出：2 个审计文档使用 `git mv` 移动到 `docs/audits/`，元数据状态更新为已完成。

- [ ] **Step 1：使用 `git mv` 迁移 `docs/security-audit.md`**

```powershell
git mv docs/security-audit.md docs/audits/2026-07-04-security-audit.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/security-audit.md'
}
Write-Output '已迁移：docs/security-audit.md → docs/audits/2026-07-04-security-audit.md'
```

- [ ] **Step 2：更新 `docs/audits/2026-07-04-security-audit.md` 元数据状态**

将 `Snapshot（待迁移）` 更新为 `Snapshot`，追加迁移记录行。

- [ ] **Step 3：使用 `git mv` 迁移 `docs/security-audit-verification.md`**

```powershell
git mv docs/security-audit-verification.md docs/audits/2026-07-08-security-audit-verification.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/security-audit-verification.md'
}
Write-Output '已迁移：docs/security-audit-verification.md → docs/audits/2026-07-08-security-audit-verification.md'
```

- [ ] **Step 4：更新 `docs/audits/2026-07-08-security-audit-verification.md` 元数据状态**

将 `Snapshot（待迁移）` 更新为 `Snapshot`，追加迁移记录行。

- [ ] **Step 5：验证批次 2 迁移完整性**

```powershell
$batch2 = @(
    @{ Source = 'docs/security-audit.md'; Target = 'docs/audits/2026-07-04-security-audit.md' },
    @{ Source = 'docs/security-audit-verification.md'; Target = 'docs/audits/2026-07-08-security-audit-verification.md' }
)

foreach ($entry in $batch2) {
    if (Test-Path -LiteralPath $entry.Source -PathType Leaf) {
        throw "源文件仍存在：$($entry.Source)"
    }
    if (-not (Test-Path -LiteralPath $entry.Target -PathType Leaf)) {
        throw "目标文件不存在：$($entry.Target)"
    }
    $content = Get-Content -LiteralPath $entry.Target -Raw
    if ($content -notmatch '> \*\*迁移记录\*\*：P4B 迁移完成') {
        throw "缺失迁移记录：$($entry.Target)"
    }
    if ($content -match '（待迁移）') {
        throw "仍含有待迁移状态：$($entry.Target)"
    }
    $log = git log --oneline --follow -- $entry.Target
    if (-not $log) {
        throw "git log --follow 无输出：$($entry.Target)"
    }
}

Write-Output '批次 2 验证通过：2 个审计文档已迁移，Git 历史保留，元数据状态已更新。'
```

预期：命令无输出、无异常。

---

## Task 4：批次 3 — 历史计划文档迁移（2 组，共 4 个文件）

**文件：**

- 移动：`docs/git-initial-import-plan.md` → `docs/history/implementation-plans/git-initial-import-plan.md`
- 移动：`docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` → `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md`
- 移动：`IMPROVEMENT-PLAN.md` → `docs/backlog.md`
- 移动：`.trae/documents/xdev-improvement-roadmap.md` → `docs/roadmap.md`

**接口：**

- 消费：P4A 元数据头中的迁移目标路径。
- 产出：4 个分析与规划文档使用 `git mv` 移动到目标位置，元数据状态更新为已完成。

- [ ] **Step 1：使用 `git mv` 迁移 `docs/git-initial-import-plan.md`**

```powershell
git mv docs/git-initial-import-plan.md docs/history/implementation-plans/git-initial-import-plan.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/git-initial-import-plan.md'
}
Write-Output '已迁移：docs/git-initial-import-plan.md → docs/history/implementation-plans/git-initial-import-plan.md'
```

- [ ] **Step 2：更新目标文件元数据状态**

将 `Superseded（待迁移）` 更新为 `Superseded`，追加迁移记录行。

- [ ] **Step 3：使用 `git mv` 迁移 `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md`**

```powershell
git mv docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
}
Write-Output '已迁移：docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md → docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md'
```

- [ ] **Step 4：更新目标文件元数据状态**

将 `Superseded（待迁移）` 更新为 `Superseded`，追加迁移记录行。

- [ ] **Step 5：使用 `git mv` 迁移 `IMPROVEMENT-PLAN.md`**

```powershell
git mv IMPROVEMENT-PLAN.md docs/backlog.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：IMPROVEMENT-PLAN.md'
}
Write-Output '已迁移：IMPROVEMENT-PLAN.md → docs/backlog.md'
```

- [ ] **Step 6：更新 `docs/backlog.md` 元数据状态**

将 `Frozen（待转换）` 更新为 `Frozen`，追加迁移记录行。

- [ ] **Step 7：使用 `git mv` 迁移 `.trae/documents/xdev-improvement-roadmap.md`**

```powershell
git mv .trae/documents/xdev-improvement-roadmap.md docs/roadmap.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：.trae/documents/xdev-improvement-roadmap.md'
}
Write-Output '已迁移：.trae/documents/xdev-improvement-roadmap.md → docs/roadmap.md'
```

- [ ] **Step 8：更新 `docs/roadmap.md` 元数据状态**

将 `Frozen（待转换）` 更新为 `Frozen`，追加迁移记录行。

- [ ] **Step 9：验证批次 3 迁移完整性**

```powershell
$batch3 = @(
    @{ Source = 'docs/git-initial-import-plan.md'; Target = 'docs/history/implementation-plans/git-initial-import-plan.md' },
    @{ Source = 'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'; Target = 'docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md' },
    @{ Source = 'IMPROVEMENT-PLAN.md'; Target = 'docs/backlog.md' },
    @{ Source = '.trae/documents/xdev-improvement-roadmap.md'; Target = 'docs/roadmap.md' }
)

foreach ($entry in $batch3) {
    if (Test-Path -LiteralPath $entry.Source -PathType Leaf) {
        throw "源文件仍存在：$($entry.Source)"
    }
    if (-not (Test-Path -LiteralPath $entry.Target -PathType Leaf)) {
        throw "目标文件不存在：$($entry.Target)"
    }
    $content = Get-Content -LiteralPath $entry.Target -Raw
    if ($content -notmatch '> \*\*迁移记录\*\*：P4B 迁移完成') {
        throw "缺失迁移记录：$($entry.Target)"
    }
    $log = git log --oneline --follow -- $entry.Target
    if (-not $log) {
        throw "git log --follow 无输出：$($entry.Target)"
    }
}

Write-Output '批次 3 验证通过：4 个文件已迁移，Git 历史保留，元数据状态已更新。'
```

预期：命令无输出、无异常。

---

## Task 5：批次 4 — `.trae` 规格归档迁移（3 个文件）

**文件：**

- 创建目标目录：`specs/archive/2026/000-enhance-gui-ux-and-fixes/`（如果 Task 1 未创建）
- 移动：`.trae/specs/enhance-gui-ux-and-fixes/spec.md` → `specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md`
- 移动：`.trae/specs/enhance-gui-ux-and-fixes/tasks.md` → `specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md`
- 移动：`.trae/specs/enhance-gui-ux-and-fixes/checklist.md` → `specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md`

**接口：**

- 消费：P4A 元数据头中的迁移目标路径和设计规格第 15 节的归档规则。
- 产出：3 个旧规格文件使用 `git mv` 迁移到 `specs/archive/2026/000-enhance-gui-ux-and-fixes/`，元数据状态更新。

- [ ] **Step 1：确保目标目录存在**

```powershell
$archiveDir = 'specs/archive/2026/000-enhance-gui-ux-and-fixes'
if (-not (Test-Path -LiteralPath $archiveDir -PathType Container)) {
    New-Item -ItemType Directory -Path $archiveDir -Force | Out-Null
    Write-Output "已创建归档目录：$archiveDir"
} else {
    Write-Output "归档目录已存在：$archiveDir"
}
```

- [ ] **Step 2：使用 `git mv` 迁移 `spec.md`**

```powershell
git mv .trae/specs/enhance-gui-ux-and-fixes/spec.md specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：spec.md'
}
Write-Output '已迁移：.trae/specs/enhance-gui-ux-and-fixes/spec.md → specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md'
```

- [ ] **Step 3：更新目标 `spec.md` 元数据状态**

将 `Archived（待迁移）` 更新为 `Archived`，追加迁移记录行。

- [ ] **Step 4：使用 `git mv` 迁移 `tasks.md`**

```powershell
git mv .trae/specs/enhance-gui-ux-and-fixes/tasks.md specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：tasks.md'
}
Write-Output '已迁移：.trae/specs/enhance-gui-ux-and-fixes/tasks.md → specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md'
```

- [ ] **Step 5：更新目标 `tasks.md` 元数据状态**

将 `Archived（待迁移）` 更新为 `Archived`，追加迁移记录行。

- [ ] **Step 6：使用 `git mv` 迁移 `checklist.md`**

```powershell
git mv .trae/specs/enhance-gui-ux-and-fixes/checklist.md specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md
if ($LASTEXITCODE -ne 0) {
    throw 'git mv 失败：checklist.md'
}
Write-Output '已迁移：.trae/specs/enhance-gui-ux-and-fixes/checklist.md → specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md'
```

- [ ] **Step 7：更新目标 `checklist.md` 元数据状态**

将 `Archived（待迁移）` 更新为 `Archived`，追加迁移记录行。

- [ ] **Step 8：验证批次 4 迁移完整性**

```powershell
$batch4 = @(
    @{ Source = '.trae/specs/enhance-gui-ux-and-fixes/spec.md'; Target = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md' },
    @{ Source = '.trae/specs/enhance-gui-ux-and-fixes/tasks.md'; Target = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md' },
    @{ Source = '.trae/specs/enhance-gui-ux-and-fixes/checklist.md'; Target = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md' }
)

foreach ($entry in $batch4) {
    if (Test-Path -LiteralPath $entry.Source -PathType Leaf) {
        throw "源文件仍存在：$($entry.Source)"
    }
    if (-not (Test-Path -LiteralPath $entry.Target -PathType Leaf)) {
        throw "目标文件不存在：$($entry.Target)"
    }
    $content = Get-Content -LiteralPath $entry.Target -Raw
    if ($content -notmatch '> \*\*迁移记录\*\*：P4B 迁移完成') {
        throw "缺失迁移记录：$($entry.Target)"
    }
    $log = git log --oneline --follow -- $entry.Target
    if (-not $log) {
        throw "git log --follow 无输出：$($entry.Target)"
    }
}

Write-Output '批次 4 验证通过：3 个旧规格文件已归档，Git 历史保留，元数据状态已更新。'
```

预期：命令无输出、无异常。

---

## Task 6：创建归档元数据文件

**文件：**

- 创建：`specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md`
- 不修改其他文件

**接口：**

- 消费：`.specify/templates/overrides/archive-metadata-template.md`（P2B 交付物）、设计规格第 15 节的归档元数据字段要求。
- 产出：符合模板格式的归档元数据文件，记录来源、完成状态、迁移日期和已知遗留问题。

- [ ] **Step 1：读取归档元数据模板**

```powershell
$template = Get-Content -LiteralPath '.specify/templates/overrides/archive-metadata-template.md' -Raw
Write-Output "模板行数：$(($template -split '\n').Count)"
```

- [ ] **Step 2：创建 `archive-metadata.md`**

根据模板格式和设计规格第 15 节要求的字段，创建包含以下内容的归档元数据文件：

```markdown
# 归档元数据：enhance-gui-ux-and-fixes

> **文档类型**：归档元数据
> **状态**：Archived
> **创建日期**：2026-07-16
> **所属归档**：specs/archive/2026/000-enhance-gui-ux-and-fixes/

## 原始信息

| 字段 | 值 |
|------|-----|
| **原始路径** | `.trae/specs/enhance-gui-ux-and-fixes/` |
| **原始工具** | Trae Spec System |
| **创建时间** | 未知（迁移自 .trae） |
| **完成时间** | 未知（迁移自 .trae） |
| **迁移日期** | 2026-07-16 |
| **迁移执行** | P4B 历史文档迁移 |

## 完成状态

| 字段 | 值 |
|------|-----|
| **实现状态** | 已实现（部分功能集成至当前 XDev 代码库） |
| **验证状态** | 原始验证记录缺失（.trae 系统未生成持久化审查记录） |
| **最终验收** | 未记录（迁移自旧工具链） |

## 归档文件清单

| 文件 | 说明 |
|------|------|
| `spec.md` | 原始 Feature Spec |
| `tasks.md` | 原始任务分解 |
| `checklist.md` | 原始检查清单 |

## 已知遗留问题

1. **原始审查证据缺失**：`.trae` 工具链未生成独立审查记录，本归档无 Spec 合规审查和工程质量审查证据。
2. **创建和完成日期未知**：`.trae` 系统未记录精确的创建和完成时间戳。
3. **与当前功能的关系未验证**：该 Spec 中描述的功能是否已全部集成到当前 XDev 代码库未经交叉验证。
4. **链接引用可能失效**：原 `.trae` 路径下的交叉引用在迁移后可能失效，详见 errata.md（如有）。

## 迁移后约束

- 本归档中的 `spec.md`、`tasks.md` 和 `checklist.md` 不可改写。
- 发现错误时只能在 `errata.md` 中追加勘误。
- 后续相关工作必须创建新的活跃 Feature Spec（`specs/<编号-功能名>/spec.md`）。

## 替代关系

- 无明确替代文档。相关功能需求应以新的 Feature Spec 重新定义。
```

- [ ] **Step 3：验证 `archive-metadata.md` 完整性**

```powershell
$metaPath = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md'
if (-not (Test-Path -LiteralPath $metaPath -PathType Leaf)) {
    throw 'archive-metadata.md 未创建'
}

$content = Get-Content -LiteralPath $metaPath -Raw

$requiredSections = @(
    '原始信息',
    '完成状态',
    '归档文件清单',
    '已知遗留问题',
    '迁移后约束'
)

foreach ($section in $requiredSections) {
    if ($content -notmatch $section) {
        throw "缺失章节：$section"
    }
}

Write-Output 'archive-metadata.md 验证通过：全部必需章节已包含。'
```

预期：命令无输出、无异常。

---

## Task 7：更新仓库内交叉引用

**文件：**

- 搜索并更新：仓库内所有 Markdown 文件中指向旧路径的链接。
- 特别关注以下可能包含旧路径引用的文件：
  - `docs/README.md`（可能与 `docs/security-audit.md` 等旧路径有链接）
  - `docs/security/README.md`（第 25 行提到待迁移的安全文档旧路径）
  - `docs/superpowers/plans/` 下的多个计划文件（可能引用 `docs/git-initial-import-plan.md` 或 `docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md`）
  - `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md`（第 454-459 行的迁移矩阵可能包含旧路径引用）
  - 其他 Markdown 文件中的交叉引用

**接口：**

- 消费：迁移矩阵中的旧路径→新路径映射。
- 产出：仓库内所有旧路径引用更新为新路径，无残留的无效链接。

- [ ] **Step 1：搜索仓库内所有指向旧路径的 Markdown 引用**

对每个迁移的旧路径，搜索仓库内的引用：

```powershell
$oldPaths = @(
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

foreach ($oldPath in $oldPaths) {
    $escaped = [regex]::Escape($oldPath)
    $matches = git grep -n -- $escaped -- '*.md'
    if ($matches) {
        Write-Output "=== 引用 $oldPath ==="
        Write-Output $matches
        Write-Output ''
    } else {
        Write-Output "无引用：$oldPath"
    }
}
```

- [ ] **Step 2：编译旧路径→新路径映射表**

```powershell
$pathMappings = @{
    'docs/download-source-policy.md' = 'docs/security/policy/download-source-policy.md'
    'docs/signing-guide.md' = 'docs/security/procedures/signing-guide.md'
    'docs/download-cache-recovery.md' = 'docs/security/procedures/download-cache-recovery.md'
    'docs/security-audit.md' = 'docs/audits/2026-07-04-security-audit.md'
    'docs/security-audit-verification.md' = 'docs/audits/2026-07-08-security-audit-verification.md'
    'docs/git-initial-import-plan.md' = 'docs/history/implementation-plans/git-initial-import-plan.md'
    'IMPROVEMENT-PLAN.md' = 'docs/backlog.md'
    '.trae/documents/xdev-improvement-roadmap.md' = 'docs/roadmap.md'
    '.trae/specs/enhance-gui-ux-and-fixes/' = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/'
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md' = 'docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md'
}

$pathMappings.GetEnumerator() | ForEach-Object {
    Write-Output "$($_.Key) → $($_.Value)"
}
```

- [ ] **Step 3：逐文件更新引用**

对 Step 1 发现的每个存在旧路径引用的文件，读取其内容，替换所有旧路径为新路径，然后写回。**注意：只替换 Markdown 链接和纯文本路径引用，不修改代码块内的示例路径或元数据头中的 `来源` 字段。**

```powershell
foreach ($entry in $pathMappings.GetEnumerator()) {
    $oldPath = $entry.Key
    $newPath = $entry.Value
    $escaped = [regex]::Escape($oldPath)
    
    # 找到引用该旧路径的 Markdown 文件（排除已迁移的文件本身）
    $refFiles = git grep -l -- $escaped -- '*.md' | Where-Object { $_ -ne $newPath }
    
    foreach ($refFile in $refFiles) {
        $content = Get-Content -LiteralPath $refFile -Raw
        $newContent = $content -replace $escaped, $newPath
        if ($newContent -ne $content) {
            Set-Content -LiteralPath $refFile -Value $newContent -NoNewline
            Write-Output "已更新引用：$refFile（${oldPath} → ${newPath}）"
        }
    }
}
```

- [ ] **Step 4：特别更新 `docs/security/README.md` 的迁移状态段落**

`docs/security/README.md` 第 23-25 行包含当前迁移状态的说明。迁移完成后，更新该段落以反映文档已就位：

原文第 23-25 行：
```markdown
## 当前迁移状态

现有 `docs/download-source-policy.md`、`docs/signing-guide.md`、`docs/download-cache-recovery.md` 和安全审计将在后续迁移计划中处理。本阶段不移动这些文件。
```

更新为：
```markdown
## 当前迁移状态

安全政策、操作流程和审计文档已完成 P4B 迁移：
- `policy/download-source-policy.md`（已自 `docs/` 迁入）
- `procedures/signing-guide.md`（已自 `docs/` 迁入）
- `procedures/download-cache-recovery.md`（已自 `docs/` 迁入）
- 安全审计文档已迁入 `docs/audits/`。
```

- [ ] **Step 5：重新运行 Step 1 确认无残留旧路径引用**

```powershell
foreach ($oldPath in $oldPaths) {
    $escaped = [regex]::Escape($oldPath)
    $matches = git grep -n -- $escaped -- '*.md'
    if ($matches) {
        Write-Output "残留引用 $oldPath ："
        Write-Output $matches
    }
}

Write-Output '交叉引用更新完成：无残留旧路径引用（除已迁移文件自身的元数据头中的来源字段）。'
```

预期：残留引用仅限于已迁移文件自身的元数据头中 `来源` 字段值（该字段有意保留原始路径），不应有其他残留。

---

## Task 8：清理空目录

**文件：**

- 检查并清理 `.trae/specs/enhance-gui-ux-and-fixes/`（3 个文件迁移后可能为空目录）
- 检查并清理 `.trae/documents/`（`xdev-improvement-roadmap.md` 迁移后可能为空目录）
- 不删除仍包含文件的目录

**接口：**

- 消费：迁移后 Git 工作区状态。
- 产出：空的旧目录被移除。

- [ ] **Step 1：检查并清理 `.trae/specs/enhance-gui-ux-and-fixes/`**

```powershell
$traeSpecDir = '.trae/specs/enhance-gui-ux-and-fixes'

if (Test-Path -LiteralPath $traeSpecDir -PathType Container) {
    $remaining = Get-ChildItem -LiteralPath $traeSpecDir -Force
    if ($remaining.Count -eq 0) {
        Remove-Item -LiteralPath $traeSpecDir -Force
        Write-Output "已删除空目录：$traeSpecDir"
    } else {
        $remainingNames = $remaining.Name -join ', '
        Write-Output "目录未删除，仍有文件：$remainingNames"
    }
} else {
    Write-Output "目录已不存在：$traeSpecDir"
}
```

- [ ] **Step 2：检查并清理 `.trae/documents/`**

```powershell
$traeDocsDir = '.trae/documents'

if (Test-Path -LiteralPath $traeDocsDir -PathType Container) {
    $remaining = Get-ChildItem -LiteralPath $traeDocsDir -Force
    if ($remaining.Count -eq 0) {
        Remove-Item -LiteralPath $traeDocsDir -Force
        Write-Output "已删除空目录：$traeDocsDir"
    } else {
        $remainingNames = $remaining.Name -join ', '
        Write-Output "目录未删除，仍有文件：$remainingNames"
    }
} else {
    Write-Output "目录已不存在：$traeDocsDir"
}
```

- [ ] **Step 3：检查 `.trae/specs/` 和 `.trae/` 是否为空**

如果 `.trae/specs/` 和 `.trae/` 在子目录清理后变为空目录，一并删除。

```powershell
$traeSpecsDir = '.trae/specs'
if ((Test-Path -LiteralPath $traeSpecsDir -PathType Container) -and ((Get-ChildItem -LiteralPath $traeSpecsDir -Force).Count -eq 0)) {
    Remove-Item -LiteralPath $traeSpecsDir -Force
    Write-Output "已删除空目录：$traeSpecsDir"
}

$traeDir = '.trae'
if ((Test-Path -LiteralPath $traeDir -PathType Container) -and ((Get-ChildItem -LiteralPath $traeDir -Force).Count -eq 0)) {
    Remove-Item -LiteralPath $traeDir -Force
    Write-Output "已删除空目录：$traeDir"
}
```

---

## Task 9：全量迁移完整性验证

**文件：**

- 验证：迁移矩阵中全部 12 个源文件和目标文件
- 不创建新文件

**接口：**

- 消费：Tasks 2-8 的全部交付物。
- 产出：迁移完整性、Git 历史保留、元数据状态更新、交叉引用更新的验证证据。

- [ ] **Step 1：验证全部 12 个源文件已不再存在于原位置**

```powershell
$allSources = @(
    'docs/download-source-policy.md',
    'docs/signing-guide.md',
    'docs/download-cache-recovery.md',
    'docs/security-audit.md',
    'docs/security-audit-verification.md',
    'docs/git-initial-import-plan.md',
    'IMPROVEMENT-PLAN.md',
    '.trae/documents/xdev-improvement-roadmap.md',
    '.trae/specs/enhance-gui-ux-and-fixes/spec.md',
    '.trae/specs/enhance-gui-ux-and-fixes/tasks.md',
    '.trae/specs/enhance-gui-ux-and-fixes/checklist.md',
    'docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md'
)

$stillPresent = @($allSources | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
if ($stillPresent.Count -gt 0) {
    $stillPresent
    throw '以下源文件未迁移：'
}

Write-Output "全部 $($allSources.Count) 个源文件已从原位置移除。"
```

预期：无输出、无异常。

- [ ] **Step 2：验证全部目标文件存在且 Git 历史可追溯**

```powershell
$allTargets = @(
    'docs/security/policy/download-source-policy.md',
    'docs/security/procedures/signing-guide.md',
    'docs/security/procedures/download-cache-recovery.md',
    'docs/audits/2026-07-04-security-audit.md',
    'docs/audits/2026-07-08-security-audit-verification.md',
    'docs/history/implementation-plans/git-initial-import-plan.md',
    'docs/backlog.md',
    'docs/roadmap.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md',
    'docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md'
)

foreach ($target in $allTargets) {
    if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
        throw "目标文件不存在：$target"
    }
    # 验证 git log --follow 可追溯完整历史
    $log = git log --oneline --follow -- $target
    if (-not $log) {
        throw "git log --follow 无输出：$target"
    }
    Write-Output "OK: $target（Git 历史 $(($log -split '\n').Count) 条提交）"
}

Write-Output "全部 $($allTargets.Count) 个目标文件已就位且 Git 历史可追溯。"
```

预期：12 个目标文件全部存在，各有 Git 历史。

- [ ] **Step 3：验证元数据状态更新正确**

```powershell
$stateProblems = @()

foreach ($target in $allTargets) {
    $content = Get-Content -LiteralPath $target -Raw
    
    # 不应再有（待迁移）状态
    if ($content -match '（待迁移）|（待转换）|（待拆分）') {
        $stateProblems += "${target}：仍含有待迁移状态后缀"
    }
    
    # 必须有迁移记录
    if ($content -notmatch '> \*\*迁移记录\*\*：P4B 迁移完成') {
        $stateProblems += "${target}：缺失迁移记录"
    }
}

if ($stateProblems.Count -gt 0) {
    $stateProblems
    throw '元数据状态问题'
}

Write-Output '全部 12 个文件元数据状态已更新，迁移记录已追加。'
```

预期：无输出、无异常。

- [ ] **Step 4：验证原位保留文件未被修改**

```powershell
$inPlaceFiles = @(
    'docs/architecture.md',
    'docs/technical-reference.md',
    'docs/operations.md',
    'docs/test-plan.md'
)

foreach ($file in $inPlaceFiles) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "原位保留文件丢失：$file"
    }
    $content = Get-Content -LiteralPath $file -Raw
    if ($content -match '迁移记录') {
        throw "${file}：原位保留文件不应存在迁移记录"
    }
}

Write-Output '全部 4 个原位保留文件未被修改。'
```

预期：无输出、无异常。

- [ ] **Step 5：验证归档元数据文件已创建**

```powershell
$archiveMeta = 'specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md'
if (-not (Test-Path -LiteralPath $archiveMeta -PathType Leaf)) {
    throw '归档元数据文件不存在'
}

$content = Get-Content -LiteralPath $archiveMeta -Raw
$requiredTerms = @('原始路径', '迁移日期', '已知遗留问题', '迁移后约束')
foreach ($term in $requiredTerms) {
    if ($content -notmatch $term) {
        throw "归档元数据缺失必要信息：$term"
    }
}

Write-Output '归档元数据文件已创建并验证。'
```

预期：无输出、无异常。

---

## Task 10：运行链接验证（P3 交付物）并与基线对比

**文件：**

- 执行：`scripts/docs/validate-links.ps1`
- 不修改文件

**接口：**

- 消费：P3 交付的链接验证脚本和 Task 1 记录的 P4B 前基线。
- 产出：链接验证结果，与 Task 1 基线对比确认无新增断链。

- [ ] **Step 1：重新运行链接验证脚本**

```powershell
$result = & 'scripts/docs/validate-links.ps1' 2>&1
$exitCode = $LASTEXITCODE

Write-Output $result
Write-Output "P4B 后链接验证退出码：$exitCode"

$result | Out-File -LiteralPath "$env:TEMP\p4b-link-after.txt" -Encoding utf8
```

- [ ] **Step 2：对比 Task 1 基线，确认无新增链接问题**

```powershell
$baselineLines = Get-Content -LiteralPath "$env:TEMP\p4b-link-baseline.txt" -ErrorAction SilentlyContinue
$afterLines = Get-Content -LiteralPath "$env:TEMP\p4b-link-after.txt" -ErrorAction SilentlyContinue
$baselineExitCode = [int](Get-Content -LiteralPath "$env:TEMP\p4b-link-baseline-exitcode.txt" -ErrorAction SilentlyContinue)

if ($baselineLines -and $afterLines) {
    $baselineErrCount = ($baselineLines | Select-String -Pattern 'error|错误|broken|断链' -CaseSensitive:$false).Count
    $afterErrCount = ($afterLines | Select-String -Pattern 'error|错误|broken|断链' -CaseSensitive:$false).Count
    
    Write-Output "基线链接问题数：$baselineErrCount"
    Write-Output "迁移后链接问题数：$afterErrCount"
    
    if ($afterErrCount -gt $baselineErrCount) {
        Write-Output '警告：链接问题数量增加，请审查新增的链接错误。'
        Write-Output '新增错误：'
        # 找出新增的错误行
        $baselineErrSet = @{}
        $baselineLines | Select-String -Pattern 'error|错误|broken|断链' -CaseSensitive:$false | ForEach-Object { $baselineErrSet[$_.Line] = $true }
        $afterLines | Select-String -Pattern 'error|错误|broken|断链' -CaseSensitive:$false | ForEach-Object {
            if (-not $baselineErrSet.ContainsKey($_.Line)) {
                Write-Output "  NEW: $($_.Line)"
            }
        }
    } elseif ($afterErrCount -eq $baselineErrCount) {
        Write-Output '链接问题数量未增加。'
    } else {
        Write-Output "链接问题数量减少：$($baselineErrCount - $afterErrCount) 个问题已修复。"
    }
} else {
    Write-Output '无法读取基线文件，仅输出本次结果。'
}
```

预期：新增链接问题数量为 0。如果有新增问题，审查是否为交叉引用更新遗漏；修复后重新运行本步骤。

---

## Task 11：双重独立审查与提交

**文件：**

- 审查：迁移矩阵中的全部 12 个目标文件、`archive-metadata.md`、更新了引用的其他文件
- 提交：仅本计划修改和移动的文件白名单

**接口：**

- 消费：完整迁移差异和验证证据。
- 产出：两个独立审查结论、用户验收和一个本地 Git 提交。

- [ ] **Step 1：生成可审查的差异**

```powershell
$baseCommit = '<Task 1 Step 10 记录的 BASE_COMMIT>'

Write-Output "基线：$baseCommit"
Write-Output "当前 HEAD：$(git rev-parse HEAD)"
Write-Output ''

Write-Output '=== 工作区状态 ==='
git status --short

Write-Output ''

Write-Output '=== 变更统计 ==='
git diff --stat $baseCommit

Write-Output ''

Write-Output '=== git mv 重命名检测 ==='
git diff --find-renames --name-status $baseCommit
```

暂存所有变更（仅限白名单）：

```powershell
# 先检查是否有非预期的修改文件
$allChanged = @(git diff --name-only $baseCommit)
$allNew = @(git ls-files --others --exclude-standard)

Write-Output "变更文件总数：$($allChanged.Count)"
Write-Output "新增未跟踪文件：$($allNew.Count)"

# 暂存所有变更
git add -- $allChanged $allNew
git status --short
git diff --cached --stat
```

- [ ] **Step 2：执行 Spec 合规审查**

使用全新的独立 Agent 会话执行审查，不复用实现者会话。审查者必须独立于实现者，读取：

```text
.specify/memory/constitution.md
docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md
docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md
docs/superpowers/plans/2026-07-16-legacy-document-metadata.md
docs/superpowers/plans/2026-07-16-document-migration.md（本计划）
本计划产生的差异
```

必须逐项判定：

- 12 个文件是否全部使用 `git mv` 移动（Git 历史保留）；
- 迁移目标路径是否与 P4A 元数据中 `P4B 迁移目标` 字段一致；
- 迁移目标路径是否符合设计规格第 4 节四层文档架构；
- 元数据状态更新是否正确（去掉 `（待迁移）` 等后缀，追加迁移记录）；
- 原位保留的 4 个文件是否未被修改；
- 是否错误声明 Constitution 已生效或迁移已完成（迁移记录只声明迁移操作完成，不声明治理状态）；
- 文档正文内容是否未被修改（diff 仅限于元数据头更新和交叉引用更新）；
- `archive-metadata.md` 是否包含设计规格第 15 节要求的全部字段；
- `.trae/specs/enhance-gui-ux-and-fixes/` 下的 3 个文件是否全部归档。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 3：执行工程质量审查**

使用另一个全新的独立 Agent 会话执行审查，不复用实现者或 Spec 合规审查者会话。第二审查者必须不同于实现者和 Spec 合规审查者，检查：

- 所有 `git mv` 操作是否正确执行（无操作系统级复制+删除）；
- Git 历史是否通过 `--follow` 可追溯；
- 元数据头中的迁移记录格式是否统一；
- 状态更新映射是否正确（Deprecated/Deprecated → Deprecated 等）；
- 交叉引用更新是否完整（无残留旧路径链接）；
- 归档元数据文件格式是否正确、信息是否完整；
- 是否存在敏感数据、不可验证声明或过度设计；
- `keys/priv.key` 是否未被跟踪或暂存；
- 是否存在 P4B 范围外文件的意外修改。

有阻塞问题时返回实现 LOOP；修复后重新执行本门禁。

- [ ] **Step 4：请求用户验收**

向用户提供：

- 迁移文件清单（12 个文件，按类别分组）；
- 聚焦验证结果（Task 9 全量迁移完整性验证）；
- 交叉引用更新清单；
- 链接验证对比结果（Task 10）；
- Spec 合规审查结论；
- 工程质量审查结论；
- 明确说明：文件已通过 `git mv` 移动、Git 历史保留、正文未修改、P4B 迁移操作已完成。

只有用户明确回复批准 P4B 验收或含义同等明确的指令后才能继续提交。泛化的"继续"不得解释为验收批准。

- [ ] **Step 5：按白名单暂存**

```powershell
$expectedTargets = @(
    'docs/security/policy/download-source-policy.md',
    'docs/security/procedures/signing-guide.md',
    'docs/security/procedures/download-cache-recovery.md',
    'docs/audits/2026-07-04-security-audit.md',
    'docs/audits/2026-07-08-security-audit-verification.md',
    'docs/history/implementation-plans/git-initial-import-plan.md',
    'docs/backlog.md',
    'docs/roadmap.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/tasks.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/checklist.md',
    'docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md',
    'specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md',
    'docs/security/README.md'
)

# 暂存目标文件
git add -- $expectedTargets

# 暂存其他可能的交叉引用更新文件
$otherChanged = @(git diff --name-only HEAD)
foreach ($file in $otherChanged) {
    if ($file -notin $expectedTargets) {
        Write-Output "额外变更文件：$file"
        git add -- $file
    }
}

$staged = @(git diff --cached --name-only)

Write-Output "暂存文件清单："
$staged

git diff --cached --check
git diff --cached --stat
```

- [ ] **Step 6：创建提交**

```powershell
git commit -m "docs: migrate legacy documents to governance directories (P4B)

- git mv security docs to docs/security/policy/ and procedures/
- git mv audit docs to docs/audits/ with dated filenames
- git mv old implementation plans to docs/history/implementation-plans/
- git mv IMPROVEMENT-PLAN.md to docs/backlog.md
- git mv .trae roadmap to docs/roadmap.md
- git mv .trae spec archive to specs/archive/2026/000-enhance-gui-ux-and-fixes/
- Create archive-metadata.md for migrated spec
- Update cross-references and docs/security/README.md"

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

- 提交包含迁移目标文件、归档元数据和引用更新文件；
- `keys/priv.key` 查询无输出；
- 不存在本计划产生的未提交文件。

- [ ] **Step 8：验证提交后 Git 历史可追溯**

随机抽查 3 个迁移文件，确认其完整历史可通过 `--follow` 追溯：

```powershell
$sampleTargets = @(
    'docs/security/policy/download-source-policy.md',
    'docs/audits/2026-07-04-security-audit.md',
    'docs/backlog.md'
)

foreach ($target in $sampleTargets) {
    Write-Output "=== Git log --follow: $target ==="
    git log --oneline --follow -- $target
    Write-Output ''
}
```

预期：每个文件至少有 2 次提交（原始导入 + P4A 元数据添加 + P4B 迁移）。

---

## 验收标准

- **AC-001**：10 组（共 12 个）文件全部使用 `git mv` 从源路径移动到目标路径，源路径不再存在文件。
- **AC-002**：每个迁移文件的 Git 历史通过 `git log --follow` 可完整追溯到初始导入。
- **AC-003**：每个迁移文件的元数据状态后缀已更新（去掉 `（待迁移）` / `（待拆分）` / `（待转换）`），并追加 `迁移记录` 行。
- **AC-004**：文档正文内容未发生任何语义变化；diff 仅限于元数据头更新、交叉引用更新和索引文件说明更新。
- **AC-005**：原位保留的 4 个文件（`docs/architecture.md`、`docs/technical-reference.md`、`docs/operations.md`、`docs/test-plan.md`）未被修改。
- **AC-006**：仓库内所有 Markdown 文件中的旧路径引用已更新为新路径，无残留的无效链接（已迁移文件自身元数据头中的 `来源` 字段除外）。
- **AC-007**：`specs/archive/2026/000-enhance-gui-ux-and-fixes/archive-metadata.md` 已创建，包含原始路径、迁移日期、完成状态、归档文件清单、已知遗留问题和迁移后约束。
- **AC-008**：`docs/security/README.md` 的迁移状态段落已更新为反映迁移完成。
- **AC-009**：链接验证脚本运行结果与 P4B 前基线对比，无新增断链。
- **AC-010**：`keys/priv.key` 未被跟踪或暂存。
- **AC-011**：两个独立审查门禁和用户验收均通过后才创建提交。
- **AC-012**：`.trae/specs/enhance-gui-ux-and-fixes/` 目录下的 3 个文件全部迁移，源目录可安全清理（如为空目录则删除）。

---

## 失败恢复

### 提交前

如果某个 `git mv` 操作失败或暂存了错误文件：

```powershell
# 撤销 git mv（文件尚未提交）
git mv -- <new-path> <old-path>

# 或恢复暂存区
git restore --staged -- <path>
```

如果元数据状态更新错误，直接编辑目标文件中的元数据头，不还原用户的其他并发修改。

如果交叉引用更新遗漏了某个文件，运行 Task 7 Step 1 重新扫描并补更新。

### 提交后

如果提交后发现非敏感遗漏（如某处交叉引用未更新），创建后续修复提交，不自动 amend。

如果私钥或其他凭据进入提交，立即停止，不配置 remote、不推送，并启动独立密钥事件响应方案。

如果 `git mv` 被错误执行为复制+删除（丢失了历史），在提交前还原：从备份中恢复源文件，重新使用 `git mv`。

---

## 不授权事项

P4B 明确不授权以下工作：

- **P5 治理自动化：** 治理脚本、CI 报告模式等由 P5 负责。P4B 不创建、修改或配置任何自动化脚本或 CI workflow。
- **P6 已知缺口 Spec 化：** 七个 Feature Spec 的编写由 P6 负责。P4B 不创建新的活跃 Feature Spec。
- **P7 Constitution 正式批准：** Constitution `1.0.0` 批准由 P7 负责。P4B 不声称 Constitution 已生效。
- **文档内容改写：** 不修正旧文档中的错误信息、过时结论或不完整描述。纠偏通过 `errata.md` 或替代关系在后续阶段完成。
- **`docs/pending-work-plan.md` 拆分：** 该文件的 Feature Spec 拆分工作不在 P4B 范围内。P4B 不移动该文件。
- **`docs/progress-and-defects-analysis.md` 和 `docs/competitive-analysis.md` 处理：** 这两个文件的迁移目标为"待用户决定"，P4B 不移动或修改。
- **新建 Feature Spec：** 不将 Backlog 或 Roadmap 中的条目转为活跃 Feature Spec。
- **冻结旧位置：** P4B 迁移完成后源目录被清空（`.trae/specs/enhance-gui-ux-and-fixes/`），但如果 `.trae/` 目录下仍有其他文件，P4B 不删除 `.trae/` 根目录。
- **用户批准之外的权限：** 不代填批准记录、不自行将任何文档标记为已生效、不声称治理体系已完备。

## 后续计划

P4B 完成并经用户验收后，路线图上 P4A/P4B 的旧文档治理与迁移工作即告完毕。后续可进入：

- P5：治理自动化（`docs/superpowers/plans/2026-07-15-governance-automation.md`）
- P6：已知缺口 Spec 化（`docs/superpowers/plans/2026-07-15-known-gap-spec-program.md`）
- P7：Constitution 正式批准（`docs/superpowers/plans/2026-07-15-constitution-ratification.md`）

P5 依赖 P2A、P2B 和 P3，且 P4B 已完成旧文档路径固化，治理脚本可在稳定的文档结构上运行。
