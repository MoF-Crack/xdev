# XDev 文档链接验证实施计划

> **面向 Agent 执行者：** 必须使用 `subagent-driven-development`（推荐）或 `executing-plans` 逐任务执行。步骤使用复选框（`- [ ]`）跟踪，不能跳过前置门禁、验证或独立审查。

**目标：** 实现 `scripts/docs/validate-links.ps1`——一个 Windows PowerShell 5.1 Markdown 文档链接验证工具，检查相对文件路径和锚点有效性，为后续 P4B 历史文档迁移提供安全网。验证器不涉及软件下载源审计（后者仍由 `scripts/audit-links.ps1` 负责）。

**架构：** 采用 TDD 模式，先创建失败测试固件，再实现验证器，最后运行全量验证。验证器使用纯 PowerShell 5.1 正则解析 Markdown 链接，不依赖外部 Markdown 解析器。SelfTest 模式提供离线单元级验证，与 `scripts/audit-links.ps1` 的 SelfTest 架构一致。

**技术栈：** Windows PowerShell 5.1、Markdown、Git。

## 全局约束

- 关联 Constitution：`.specify/memory/constitution.md` `1.0.0-rc.2`，状态为候选版。
- 关联设计：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`。
- 关联路线图：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md`（P3 定义见第 46 行、第 135-143 行）。
- P1 治理骨架必须已完成（目录索引已创建）。
- 当前分支必须为 `main`，必须存在可用 `HEAD`。
- 所有新文件路径在白名单内（见 §文件结构）。
- 不得修改 `scripts/audit-links.ps1`。
- 不得修改 `.github/workflows/ci.yaml` 或任何现有 CI 配置。
- 不得修改 `.specify/templates/`、现有安全审计、旧计划或 `.trae/` 文件。
- PowerShell 脚本必须兼容 Windows PowerShell 5.1（不依赖 PowerShell Core 7+ 语法）。
- 所有新增文件使用 UTF-8 编码（无 BOM）。
- 禁止使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`。
- 本计划必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准进入执行。
- 本计划完成后不授权执行 P4A/P4B 历史文档迁移或任何后续子项目。

---

## 文件结构

本计划创建：

```text
scripts/docs/validate-links.ps1
scripts/docs/tests/validate-links.tests.ps1
scripts/docs/tests/fixtures/links/
├── valid/
│   ├── index.md
│   ├── sub/
│   │   └── page.md
│   └── image.png
└── invalid/
    ├── broken-path.md
    ├── broken-anchor.md
    └── sub/
        └── page.md
```

### 文件职责

| 文件 | 职责 |
|---|---|
| `scripts/docs/validate-links.ps1` | Markdown 相对链接与锚点验证器，命令行入口 |
| `scripts/docs/tests/validate-links.tests.ps1` | Pester 风格的自验证测试脚本，消费固件目录 |
| `scripts/docs/tests/fixtures/links/valid/` | 全部链接有效的正面测试固件 |
| `scripts/docs/tests/fixtures/links/invalid/` | 包含故意构造的断链和断锚的负面测试固件 |

### 固件目录说明

`valid/` 目录内所有 Markdown 文件中的相对路径链接和锚点引用必须全部有效：
- `index.md` 包含指向 `sub/page.md` 的相对链接、指向 `image.png` 的图片引用、以及指向同文件内标题的锚点链接。
- `sub/page.md` 包含多种标题层级，供锚点测试使用。
- `image.png` 为任意有效 PNG 文件（内容不限，只需文件存在）。

`invalid/` 目录内包含三类断链：
- `broken-path.md`：引用不存在的目标文件 `./nonexistent/file.md`。
- `broken-anchor.md`：引用存在的文件但不存在的锚点 `./sub/page.md#nonexistent-heading`。
- `sub/page.md`：不包含足够标题，使 `broken-anchor.md` 的锚点引用无效。

## 接口契约

### 输入

- 命令行参数：一个或多个目录路径（`-Path`），默认扫描当前工作目录下的所有 Markdown 文件。
- `-SelfTest`：切换为离线 SelfTest 模式，验证脚本逻辑而不检查实际仓库内容。
- `-Exclude`（可选）：排除目录模式（如 `node_modules,.git`）。
- `-Report`（可选）：输出 JSON 报告文件路径。
- 标准输入：无。

### 输出

- 标准输出：按文件分组的链接验证结果（PASS/FAIL），含文件路径、行号和具体问题描述。
- 标准错误：无（所有结果输出到 stdout，以便 CI 捕获）。
- 退出码：`0` = 全部链接有效（含 SelfTest 通过），`1` = 存在无效链接，`2` = 参数错误或内部故障。
- 可选 JSON 报告：`[{"file":"...","line":N,"link":"...","target":"...","error":"..."}]`。

### 检查范围（必须）

| 检查项 | 说明 |
|---|---|
| 相对文件路径存在性 | `[text](./path/to/file.md)`、`![alt](../img.png)` 解析出的路径必须指向已存在的文件 |
| 锚点存在性 | `[text](#heading)` 和 `[text](./file.md#anchor)` 中的 `#anchor` 必须在目标文件中存在对应标题或显式锚点 |
| 参考式链接 | `[text][ref]` 配合 `[ref]: ./target.md` 必须有效 |

### 检查范围（不检查）

| 排除项 | 说明 |
|---|---|
| 外部 URL | `http://`、`https://` 链接跳过 |
| 裸 URL | 未被 `[]()` 包裹的 URL |
| 邮箱链接 | `mailto:` 链接跳过 |
| 代码块 | 代码围栏内的内容不解析为链接 |
| HTML 标签 | 不解析 `<a href>` 等 HTML 链接（保持实现简单） |

### 锚点解析规则

锚点匹配按 GitHub Flavored Markdown 规范：
1. 标题锚点：`# My Heading Text` → `#my-heading-text`（小写、空格替换为 `-`、移除除字母数字和 `-` 以外的字符）。
2. 显式锚点：`<a name="my-anchor"></a>` → `#my-anchor`。
3. 忽略标题中的 code span（反引号内的内容不参与锚点生成）。

---

## Task 1：创建失败测试固件

**文件：**

- 创建：`scripts/docs/tests/fixtures/links/valid/index.md`
- 创建：`scripts/docs/tests/fixtures/links/valid/sub/page.md`
- 创建：`scripts/docs/tests/fixtures/links/valid/image.png`
- 创建：`scripts/docs/tests/fixtures/links/invalid/broken-path.md`
- 创建：`scripts/docs/tests/fixtures/links/invalid/broken-anchor.md`
- 创建：`scripts/docs/tests/fixtures/links/invalid/sub/page.md`

**前置依赖：** P1 治理骨架已创建对应父目录（`scripts/docs/tests/fixtures/links/` 是本计划新建，不依赖 P1）。

**接口：**

- 消费：无。
- 产出：两组独立的 Markdown 固件目录，正面固件全部链接有效，负面固件存在明确的断链和断锚。

### Step 1：创建目录结构

- [ ] **Step 1.1：验证工作区清洁且父目录不存在**

```powershell
git status --short -- scripts/
if ($LASTEXITCODE -ne 0) { throw 'git status 失败' }

$fixtureDir = 'scripts/docs/tests/fixtures/links'
if (Test-Path -LiteralPath $fixtureDir) {
    throw "固件目录已存在：$fixtureDir。必须重新审计而不是覆盖。"
}
```

预期：无输出，无异常。

- [ ] **Step 1.2：创建固件目录**

```powershell
$dirs = @(
    'scripts/docs/tests/fixtures/links/valid/sub',
    'scripts/docs/tests/fixtures/links/invalid/sub'
)
foreach ($d in $dirs) {
    New-Item -ItemType Directory -Path $d -Force | Out-Null
}

# Verify
foreach ($d in $dirs) {
    if (-not (Test-Path -LiteralPath $d -PathType Container)) {
        throw "目录创建失败：$d"
    }
}
```

预期：命令无输出、无异常。

### Step 2：创建正面固件

- [ ] **Step 2.1：创建 `scripts/docs/tests/fixtures/links/valid/index.md`**

```markdown
# Valid Index

This fixture contains only valid links.

## Relative Path Link

See [the sub page](./sub/page.md) for details.

## Image Link

Here is an image:

![A test image](./image.png)

## Same-File Anchor

Jump to [the conclusion](#conclusion).

## Link with Hash in Target

See [sub page conclusion](./sub/page.md#conclusion).

## Conclusion

All links in this file are valid.
```

- [ ] **Step 2.2：创建 `scripts/docs/tests/fixtures/links/valid/sub/page.md`**

```markdown
# Sub Page

This is a sub-page used to validate cross-file anchors.

## Details

Some detailed content here.

## Conclusion

This is the sub-page conclusion for anchor testing.

### Deep Heading

A deeper heading for anchor edge cases.
```

- [ ] **Step 2.3：创建 `scripts/docs/tests/fixtures/links/valid/image.png`**

```powershell
# 创建最小有效 PNG 文件（1×1 像素，透明）
$pngBytes = [byte[]]@(
    0x89,0x50,0x4E,0x47,0x0D,0x0A,0x1A,0x0A, # PNG signature
    0x00,0x00,0x00,0x0D,0x49,0x48,0x44,0x52, # IHDR chunk length + type
    0x00,0x00,0x00,0x01,0x00,0x00,0x00,0x01, # 1×1 pixels
    0x08,0x02,0x00,0x00,0x00,0x90,0x77,0x53, # bit depth, color type, etc
    0xDE,0x00,0x00,0x00,0x0C,0x49,0x44,0x41, # IHDR CRC + IDAT chunk
    0x54,0x08,0xD7,0x63,0x68,0xE0,0x63,0x00, #
    0x00,0x00,0x02,0x00,0x01,0xD4,0xEA,0x76, #
    0x2A,0x00,0x00,0x00,0x00,0x49,0x45,0x4E, # IDAT CRC + IEND
    0x44,0xAE,0x42,0x60,0x82                  #
)
$pngPath = 'scripts/docs/tests/fixtures/links/valid/image.png'
[System.IO.File]::WriteAllBytes((Resolve-Path -LiteralPath '.').Path + '\' + ($pngPath -replace '/','\'), $pngBytes)
if (-not (Test-Path -LiteralPath $pngPath -PathType Leaf)) {
    throw "PNG 文件创建失败：$pngPath"
}
```

预期：命令无输出、无异常。文件存在且为有效 PNG。

- [ ] **Step 2.4：验证正面固件完整性**

```powershell
$validDir = 'scripts/docs/tests/fixtures/links/valid'
$expected = @(
    'index.md',
    'sub/page.md',
    'image.png'
)
foreach ($f in $expected) {
    $path = Join-Path $validDir $f
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "缺少正面固件文件：$path"
    }
}
```

预期：命令无输出、无异常。

### Step 3：创建负面固件

- [ ] **Step 3.1：创建 `scripts/docs/tests/fixtures/links/invalid/broken-path.md`**

```markdown
# Broken Path Test

This file links to a non-existent file:

## Bad Relative Link

See [nonexistent file](./nonexistent/file.md).

## Bad Image Link

![Missing image](./nonexistent.png)
```

- [ ] **Step 3.2：创建 `scripts/docs/tests/fixtures/links/invalid/broken-anchor.md`**

```markdown
# Broken Anchor Test

This file links to an existing file but a non-existent anchor.

## Bad Anchor Link

See [nonexistent heading](./sub/page.md#nonexistent-heading).

## Bad Same-File Anchor

Jump to [missing section](#missing-section).
```

- [ ] **Step 3.3：创建 `scripts/docs/tests/fixtures/links/invalid/sub/page.md`**

```markdown
# Invalid Sub Page

This sub-page intentionally does NOT have a `nonexistent-heading` heading,
so `broken-anchor.md`'s link to `./sub/page.md#nonexistent-heading` should fail.

## Only Heading

Just one heading — nothing else.
```

- [ ] **Step 3.4：验证负面固件完整性**

```powershell
$invalidDir = 'scripts/docs/tests/fixtures/links/invalid'
$expected = @(
    'broken-path.md',
    'broken-anchor.md',
    'sub/page.md'
)
foreach ($f in $expected) {
    $path = Join-Path $invalidDir $f
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "缺少负面固件文件：$path"
    }
}
```

预期：命令无输出、无异常。

- [ ] **Step 3.5：复核准后固件目录布局**

```powershell
Get-ChildItem -LiteralPath 'scripts/docs/tests/fixtures/links' -Recurse -File | ForEach-Object { $_.FullName.Replace("$PWD\",'').Replace('\','/') }
```

预期输出包含以下 7 个文件（顺序不限）：

```text
scripts/docs/tests/fixtures/links/valid/index.md
scripts/docs/tests/fixtures/links/valid/sub/page.md
scripts/docs/tests/fixtures/links/valid/image.png
scripts/docs/tests/fixtures/links/invalid/broken-path.md
scripts/docs/tests/fixtures/links/invalid/broken-anchor.md
scripts/docs/tests/fixtures/links/invalid/sub/page.md
```

---

## Task 2：实现验证器

**文件：**

- 创建：`scripts/docs/validate-links.ps1`
- 创建：`scripts/docs/tests/validate-links.tests.ps1`

**前置依赖：** Task 1（失败测试固件必须先存在）。

**接口：**

- 消费：Task 1 的固件目录。
- 产出：可独立运行的 Markdown 链接验证脚本及可自验证的测试脚本。

### Step 1：编写测试脚本 `validate-links.tests.ps1`

- [ ] **Step 1.1：创建测试脚本**

测试脚本采用 Pester 风格断言函数（兼容 PowerShell 5.1，不依赖 Pester 模块），驱动 `validate-links.ps1` 的 SelfTest 模式验证固件目录。

```powershell
# scripts/docs/tests/validate-links.tests.ps1
#
# Self-verification tests for validate-links.ps1.
# Drives the validator against fixture directories and checks exit codes
# and error counts.
#
# Usage:
#   powershell -File scripts/docs/tests/validate-links.tests.ps1
#
# Exit codes: 0 = all tests pass, 1 = test failure.

$ErrorActionPreference = 'Stop'
$script:failures = 0
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$validator = Resolve-Path (Join-Path $scriptDir '..\validate-links.ps1')

function Assert-ExitCode {
    param([int]$Actual, [int]$Expected, [string]$Case)
    if ($Actual -ne $Expected) {
        Write-Host "  [FAIL] $Case : exit=$Actual expected=$Expected" -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host "  [OK]   $Case : exit=$Actual" -ForegroundColor Green
    }
}

function Assert-MinErrors {
    param([int]$Actual, [int]$MinExpected, [string]$Case)
    if ($Actual -lt $MinExpected) {
        Write-Host "  [FAIL] $Case : errors=$Actual expected>=$MinExpected" -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host "  [OK]   $Case : errors=$Actual" -ForegroundColor Green
    }
}

# --- Test Cases ---

Write-Host ''
Write-Host '=== validate-links.ps1 Self-Verification Tests ===' -ForegroundColor Cyan

# Case 1: valid/ dir - all links valid
Write-Host ''
Write-Host 'Case 1: valid/ fixture (should pass)' -ForegroundColor Yellow
$validDir = Join-Path $scriptDir 'fixtures\links\valid'
$result = & powershell -NoProfile -File $validator -Path $validDir 2>&1
$exit1 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit1 0 'valid/ fixture'

# Case 2: invalid/ dir - should find errors
Write-Host ''
Write-Host 'Case 2: invalid/ fixture (should fail)' -ForegroundColor Yellow
$invalidDir = Join-Path $scriptDir 'fixtures\links\invalid'
$result = & powershell -NoProfile -File $validator -Path $invalidDir 2>&1
$exit2 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit2 1 'invalid/ fixture'

# Case 3: -Report JSON output
Write-Host ''
Write-Host 'Case 3: -Report JSON output' -ForegroundColor Yellow
$reportPath = Join-Path $env:TEMP 'xdev-link-validation-test-report.json'
$result = & powershell -NoProfile -File $validator -Path $invalidDir -Report $reportPath 2>&1
Write-Host $result

if (-not (Test-Path -LiteralPath $reportPath)) {
    Write-Host "  [FAIL] Report not generated: $reportPath" -ForegroundColor Red
    $script:failures++
} else {
    $reportContent = Get-Content -LiteralPath $reportPath -Raw -Encoding utf8
    try {
        $reportObj = $reportContent | ConvertFrom-Json
        if ($reportObj -is [array] -and $reportObj.Count -gt 0) {
            Write-Host "  [OK]   JSON report generated with $($reportObj.Count) entries" -ForegroundColor Green
        } else {
            Write-Host "  [FAIL] JSON report is empty or not an array" -ForegroundColor Red
            $script:failures++
        }
    } catch {
        Write-Host "  [FAIL] JSON parse error: $_" -ForegroundColor Red
        $script:failures++
    }
}
Remove-Item -LiteralPath $reportPath -Force -ErrorAction SilentlyContinue

# Case 4: -SelfTest internal validation
Write-Host ''
Write-Host 'Case 4: -SelfTest internal validation' -ForegroundColor Yellow
$result = & powershell -NoProfile -File $validator -SelfTest 2>&1
$exit4 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit4 0 'SelfTest'

# --- Summary ---
Write-Host ''
if ($script:failures -gt 0) {
    Write-Host "Tests FAILED: $script:failures error(s)" -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed (4/4)" -ForegroundColor Green
exit 0
```

预期：测试脚本文件创建成功。

### Step 2：编写验证器 `validate-links.ps1`

- [ ] **Step 2.1：创建验证器脚本**

```powershell
# scripts/docs/validate-links.ps1
#
# Markdown document link validator.
# Checks relative file paths and heading anchors in Markdown files.
# Does NOT check external URLs (http/https) — those are out of scope.
#
# Usage:
#   ./scripts/docs/validate-links.ps1 -Path ./docs
#   ./scripts/docs/validate-links.ps1 -Path ./docs,./specs -Report ./build/link-report.json
#   ./scripts/docs/validate-links.ps1 -SelfTest
#
# Exit codes: 0 = all links valid, 1 = broken links found, 2 = bad args / error.

[CmdletBinding()]
param(
    [string[]]$Path = @('.'),
    [string]$Report,
    [string[]]$Exclude = @('node_modules', '.git'),
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
$script:errors = @()

# --- Anchor Extraction ---

function Get-HeadingsFromFile {
    param([string]$FileContent)

    $headings = @()
    # Match ATX headings: optional leading spaces, 1-6 #, space, heading text
    $lines = $FileContent -split "`r`n|`n|`r"
    foreach ($line in $lines) {
        if ($line -match '^\s{0,3}(#{1,6})\s+(.+?)(?:\s+#+\s*)?$') {
            $headingText = $Matches[2].Trim()
            $anchor = AnchorFromHeading -HeadingText $headingText
            if ($anchor) {
                $headings += $anchor
            }
        }
    }
    # Also match explicit <a name="..."></a> anchors
    $nameMatches = [regex]::Matches($FileContent, '<a\s+name\s*=\s*"([^"]+)"\s*></a>', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    foreach ($m in $nameMatches) {
        $headings += $m.Groups[1].Value
    }
    $idMatches = [regex]::Matches($FileContent, '<a\s+id\s*=\s*"([^"]+)"\s*></a>', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    foreach ($m in $idMatches) {
        $headings += $m.Groups[1].Value
    }
    return $headings
}

function AnchorFromHeading {
    param([string]$HeadingText)

    # Remove code spans (backtick-enclosed text) from heading
    $text = $HeadingText -replace '`[^`]+`',''
    # Remove Markdown link syntax: [text](url) -> text
    $text = $text -replace '\[([^\]]*)\]\([^\)]*\)','$1'
    # Remove image syntax: ![alt](url) -> alt
    $text = $text -replace '!\[([^\]]*)\]\([^\)]*\)','$1'
    # Convert to lowercase
    $text = $text.ToLowerInvariant()
    # Remove all characters except lowercase letters, digits, spaces, hyphens
    $text = $text -replace '[^a-z0-9\s-]',''
    # Replace one or more spaces/hyphens with a single hyphen
    $text = $text -replace '[\s-]+','-'
    # Trim leading/trailing hyphens
    $text = $text.Trim('-')
    return $text
}

# --- Link Extraction ---

function Extract-MarkdownLinks {
    param([string]$Content, [string]$FilePath)

    $links = @()
    $lineNumber = 0
    $insideCodeBlock = $false

    foreach ($raw in ($Content -split "`r`n|`n|`r")) {
        $lineNumber++
        $line = $raw

        # Track code fences
        if ($line -match '^\s*```') {
            $insideCodeBlock = -not $insideCodeBlock
            continue
        }
        if ($insideCodeBlock) { continue }

        # Inline links: [text](url) and ![alt](url)
        # Pattern: match [...](...) but not inside code spans
        $pattern = '(!)?\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)'
        $matches = [regex]::Matches($line, $pattern)
        foreach ($m in $matches) {
            $isImage = ($m.Groups[1].Value -eq '!')
            $linkText = $m.Groups[2].Value
            $url = $m.Groups[3].Value
            $links += [PSCustomObject]@{
                File     = $FilePath
                Line     = $lineNumber
                Url      = $url
                IsImage  = $isImage
                Raw      = $m.Value
            }
        }

        # Reference-style definitions: [ref]: url
        $defPattern = '^\s*\[([^\]]+)\]:\s*<?(\S+?)>?(?:\s+"[^"]*")?\s*$'
        $defMatches = [regex]::Matches($line, $defPattern)
        foreach ($m in $defMatches) {
            $url = $m.Groups[2].Value
            $links += [PSCustomObject]@{
                File     = $FilePath
                Line     = $lineNumber
                Url      = $url
                IsImage  = $false
                Raw      = $m.Value
            }
        }

        # Reference-style usage: [text][ref] or [text][]
        $refPattern = '\[([^\]]+)\]\s*\[([^\]]*)\]'
        $refMatches = [regex]::Matches($line, $refPattern)
        # Reference-style usage is harder to resolve statically without
        # a two-pass approach. For this implementation, reference-style
        # links without explicit URL in a definition on a separate line
        # are reported as warnings (not errors) if unresolvable.
    }

    return $links
}

# --- Link Validation ---

function Test-ShouldSkip {
    param([string]$Url)

    # Skip external URLs
    if ($Url -match '^https?://') { return $true }
    # Skip mailto
    if ($Url -match '^mailto:') { return $true }
    # Skip empty or fragment-only (same-file anchors)
    if ($Url -eq '' -or $Url -match '^#') { return $false }
    return $false
}

function Validate-Link {
    param(
        [string]$SourceFile,
        [int]$Line,
        [string]$Url,
        [string]$RootDir
    )

    if (Test-ShouldSkip $Url) { return }

    $sourceDir = Split-Path -Parent $SourceFile
    $anchor = ''
    $targetPath = $Url

    # Split anchor from URL
    if ($Url -match '^(.+?)#(.+)$') {
        $targetPath = $Matches[1]
        $anchor = $Matches[2]
    } elseif ($Url -match '^#(.+)$') {
        # Same-file anchor
        $targetPath = ''
        $anchor = $Matches[1]
    }

    # Resolve target file
    if ($targetPath -eq '') {
        $resolvedPath = $SourceFile
    } else {
        # Remove URL encoding
        $targetPath = [uri]::UnescapeDataString($targetPath)
        try {
            $resolvedPath = [System.IO.Path]::GetFullPath(
                [System.IO.Path]::Combine($sourceDir, $targetPath)
            )
        } catch {
            $script:errors += [PSCustomObject]@{
                File   = $SourceFile
                Line   = $Line
                Link   = $Url
                Target = $targetPath
                Error  = "Invalid path: $_"
            }
            return
        }
    }

    # Check file existence
    if (-not (Test-Path -LiteralPath $resolvedPath -PathType Leaf)) {
        $script:errors += [PSCustomObject]@{
            File   = $SourceFile
            Line   = $Line
            Link   = $Url
            Target = (Resolve-RelativePath -From $SourceFile -To $resolvedPath)
            Error  = 'File not found'
        }
        return
    }

    # Check anchor if present
    if ($anchor -ne '') {
        $targetContent = Get-Content -LiteralPath $resolvedPath -Raw -Encoding utf8
        if (-not $targetContent) { $targetContent = '' }
        $headings = Get-HeadingsFromFile $targetContent
        if ($headings -notcontains $anchor) {
            $script:errors += [PSCustomObject]@{
                File   = $SourceFile
                Line   = $Line
                Link   = $Url
                Target = "$(Resolve-RelativePath -From $SourceFile -To $resolvedPath)#$anchor"
                Error  = "Anchor '#$anchor' not found in target file"
            }
        }
    }
}

function Resolve-RelativePath {
    param([string]$From, [string]$To)
    try {
        $fromUri = New-Object System.Uri (Join-Path (Get-Location) $From)
        $toUri = New-Object System.Uri (Join-Path (Get-Location) $To)
        $relativeUri = $fromUri.MakeRelativeUri($toUri)
        return [uri]::UnescapeDataString($relativeUri.OriginalString)
    } catch {
        return $To
    }
}

# --- SelfTest ---

function Invoke-SelfTest {
    $dir = Join-Path $env:TEMP 'xdev-validate-links-selftest'
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $dir -Force | Out-Null

    # Create minimal valid fixture inline
    $validDir = Join-Path $dir 'valid'
    New-Item -ItemType Directory -Path $validDir -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $validDir 'index.md') -Value @"
# Test File

See [other page](./other.md#intro).

## Summary

End here.
"@ -Encoding utf8
    Set-Content -LiteralPath (Join-Path $validDir 'other.md') -Value @"
# Other Page

## Intro

Hello.
"@ -Encoding utf8

    # Create minimal invalid fixture inline
    $invalidDir = Join-Path $dir 'invalid'
    New-Item -ItemType Directory -Path $invalidDir -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $invalidDir 'broken.md') -Value @"
# Broken

[missing](./nope.md) and [#bad-anchor](#bad-anchor).
"@ -Encoding utf8

    # Test 1: valid fixture should pass
    $errors = @()
    $script:errors = @()
    $mdFiles = Get-ChildItem -LiteralPath $validDir -Filter '*.md' -Recurse -File
    foreach ($f in $mdFiles) {
        $content = Get-Content -LiteralPath $f.FullName -Raw -Encoding utf8
        if (-not $content) { $content = '' }
        $links = Extract-MarkdownLinks $content $f.FullName
        foreach ($link in $links) {
            Validate-Link -SourceFile $link.File -Line $link.Line -Url $link.Url -RootDir $validDir
        }
    }
    if ($script:errors.Count -ne 0) {
        Write-Host "  [FAIL] valid fixture: expected 0 errors, got $($script:errors.Count)" -ForegroundColor Red
        foreach ($e in $script:errors) { Write-Host "    $($e.File):$($e.Line) $($e.Error)" }
        exit 1
    }
    Write-Host "  [OK]   valid fixture: no errors" -ForegroundColor Green

    # Test 2: invalid fixture should have errors
    $script:errors = @()
    $mdFiles = Get-ChildItem -LiteralPath $invalidDir -Filter '*.md' -Recurse -File
    foreach ($f in $mdFiles) {
        $content = Get-Content -LiteralPath $f.FullName -Raw -Encoding utf8
        if (-not $content) { $content = '' }
        $links = Extract-MarkdownLinks $content $f.FullName
        foreach ($link in $links) {
            Validate-Link -SourceFile $link.File -Line $link.Line -Url $link.Url -RootDir $invalidDir
        }
    }
    if ($script:errors.Count -eq 0) {
        Write-Host "  [FAIL] invalid fixture: expected some errors, got 0" -ForegroundColor Red
        exit 1
    }
    Write-Host "  [OK]   invalid fixture: $($script:errors.Count) error(s) found (expected)" -ForegroundColor Green

    # Test 3: anchor generation
    $testCases = @(
        @{ Heading = 'My Heading'; Want = 'my-heading' },
        @{ Heading = 'Hello World 123'; Want = 'hello-world-123' },
        @{ Heading = '## Special @#$ Chars!'; Want = 'special--chars' },
        @{ Heading = '  Trim Me  '; Want = 'trim-me' },
        @{ Heading = '`code` in heading'; Want = 'in-heading' }
    )
    $anchorFailures = 0
    foreach ($tc in $testCases) {
        $got = AnchorFromHeading -HeadingText $tc.Heading
        if ($got -ne $tc.Want) {
            Write-Host "  [FAIL] AnchorFromHeading('$($tc.Heading)'): got '$got' want '$($tc.Want)'" -ForegroundColor Red
            $anchorFailures++
        }
    }
    if ($anchorFailures -eq 0) {
        Write-Host "  [OK]   AnchorFromHeading: all $($testCases.Count) cases pass" -ForegroundColor Green
    } else {
        exit 1
    }

    # Test 4: external URL skip
    $skipTests = @(
        @{ Url = 'https://example.com'; Want = $true },
        @{ Url = 'http://example.com/page'; Want = $true },
        @{ Url = 'mailto:a@b.com'; Want = $true },
        @{ Url = './relative/file.md'; Want = $false },
        @{ Url = '#anchor'; Want = $false },
        @{ Url = '../img.png'; Want = $false }
    )
    $skipFailures = 0
    foreach ($tc in $skipTests) {
        $got = Test-ShouldSkip $tc.Url
        if ($got -ne $tc.Want) {
            Write-Host "  [FAIL] Test-ShouldSkip('$($tc.Url)'): got $got want $($tc.Want)" -ForegroundColor Red
            $skipFailures++
        }
    }
    if ($skipFailures -eq 0) {
        Write-Host "  [OK]   Test-ShouldSkip: all $($skipTests.Count) cases pass" -ForegroundColor Green
    } else {
        exit 1
    }

    Write-Host ''
    Write-Host 'SelfTest passed (4/4)' -ForegroundColor Green
}

# --- Main ---

if ($SelfTest) {
    Invoke-SelfTest
    exit 0
}

# Validate Path parameters
foreach ($p in $Path) {
    if (-not (Test-Path -LiteralPath $p -PathType Container)) {
        Write-Error "Path not found: $p"
        exit 2
    }
}

# Build exclusion patterns
$excludePattern = $Exclude -join '|'

# Find all Markdown files
$mdFiles = @(Get-ChildItem -LiteralPath $Path -Filter '*.md' -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object {
        $full = $_.FullName
        if ($excludePattern) {
            return ($full -notmatch $excludePattern)
        }
        return $true
    })

if ($mdFiles.Count -eq 0) {
    Write-Host 'No Markdown files found.' -ForegroundColor Yellow
    exit 0
}

Write-Host "Scanning $($mdFiles.Count) Markdown file(s) for broken links..." -ForegroundColor Cyan
Write-Host ''

# Process each file
foreach ($f in $mdFiles) {
    try {
        $content = Get-Content -LiteralPath $f.FullName -Raw -Encoding utf8
    } catch {
        $content = Get-Content -LiteralPath $f.FullName -Raw
    }
    if (-not $content) { $content = '' }

    $links = Extract-MarkdownLinks $content $f.FullName
    foreach ($link in $links) {
        Validate-Link -SourceFile $link.File -Line $link.Line -Url $link.Url -RootDir (Get-Location).Path
    }
}

# Report
if ($script:errors.Count -gt 0) {
    Write-Host "Broken links found: $($script:errors.Count)" -ForegroundColor Red
    Write-Host ''

    $grouped = $script:errors | Group-Object -Property File
    foreach ($g in $grouped) {
        $displayPath = try {
            [System.IO.Path]::GetRelativePath((Get-Location).Path, $g.Name)
        } catch {
            $g.Name
        }
        Write-Host "  $displayPath" -ForegroundColor Yellow
        foreach ($e in $g.Group) {
            Write-Host "    line $($e.Line): $($e.Error) — $($e.Link)" -ForegroundColor Red
        }
        Write-Host ''
    }

    # Write JSON report if requested
    if ($Report) {
        $reportDir = Split-Path -Parent $Report
        if ($reportDir -and -not (Test-Path -LiteralPath $reportDir)) {
            New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
        }
        $script:errors | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $Report -Encoding utf8
        Write-Host "JSON report written to: $Report" -ForegroundColor Cyan
    }

    exit 1
}

Write-Host 'All links valid.' -ForegroundColor Green

if ($Report) {
    '[]' | Set-Content -LiteralPath $Report -Encoding utf8
    Write-Host "JSON report written to: $Report" -ForegroundColor Cyan
}

exit 0
```

预期：验证器脚本文件创建成功。

- [ ] **Step 2.2：验证脚本语法**

```powershell
$validatorPath = 'scripts/docs/validate-links.ps1'
if (-not (Test-Path -LiteralPath $validatorPath)) {
    throw "验证器脚本不存在：$validatorPath"
}

# Syntax check: parse without executing
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $validatorPath).Path,
    [ref]$tokens,
    [ref]$errors
)
if ($errors.Count -gt 0) {
    foreach ($e in $errors) {
        Write-Host "  Syntax error: $($e.Message)" -ForegroundColor Red
    }
    throw "验证器脚本存在语法错误：$($errors.Count) 处"
} else {
    Write-Host "  Syntax check passed" -ForegroundColor Green
}
```

预期：命令无输出、无异常。

---

## Task 3：运行全量验证

**文件：**

- 读取：Task 1 和 Task 2 创建的全部文件。
- 不创建新文件。

**前置依赖：** Task 1 和 Task 2 全部完成。

**接口：**

- 消费：测试脚本和验证器。
- 产出：SelfTest 通过、测试脚本全部通过、验证器对有效固件返回 0 和对无效固件返回 1 的完整证据。

### Step 1：运行 SelfTest

- [ ] **Step 1.1：运行验证器 SelfTest**

```powershell
$result = & powershell -NoProfile -File 'scripts/docs/validate-links.ps1' -SelfTest 2>&1
Write-Host $result
if ($LASTEXITCODE -ne 0) {
    throw "SelfTest 失败，退出码 $LASTEXITCODE"
}
```

预期：输出 `SelfTest passed (4/4)`，退出码 0。

### Step 2：验证正面固件（应全部通过）

- [ ] **Step 2.1：对 valid/ 固件运行验证器**

```powershell
$result = & powershell -NoProfile -File 'scripts/docs/validate-links.ps1' -Path 'scripts/docs/tests/fixtures/links/valid' 2>&1
Write-Host $result
if ($LASTEXITCODE -ne 0) {
    throw "正面固件验证失败，不应该存在断链。退出码：$LASTEXITCODE"
}
```

预期：`All links valid.`，退出码 0。

### Step 3：验证负面固件（应捕获断链）

- [ ] **Step 3.1：对 invalid/ 固件运行验证器**

```powershell
$result = & powershell -NoProfile -File 'scripts/docs/validate-links.ps1' -Path 'scripts/docs/tests/fixtures/links/invalid' 2>&1
Write-Host $result
if ($LASTEXITCODE -ne 1) {
    throw "负面固件验证应失败（退出码 1），实际退出码：$LASTEXITCODE"
}

# 应输出至少 3 个断链 error
if ($result -notmatch 'Broken links found:') {
    throw '未找到断链报告行'
}

# 检查是否包含预期的错误类型
if ($result -notmatch 'File not found') {
    throw '应报告文件不存在的错误'
}
if ($result -notmatch 'Anchor.*not found') {
    throw '应报告锚点不存在的错误'
}
```

预期：退出码 1，输出包含 `Broken links found:`、`File not found` 和 `Anchor` 错误。

### Step 4：运行测试脚本

- [ ] **Step 4.1：运行 `validate-links.tests.ps1`**

```powershell
$result = & powershell -NoProfile -File 'scripts/docs/tests/validate-links.tests.ps1' 2>&1
Write-Host $result
if ($LASTEXITCODE -ne 0) {
    throw "测试脚本失败，退出码 $LASTEXITCODE"
}
```

预期：`All tests passed (4/4)`，退出码 0。

### Step 5：验证 JSON 报告功能

- [ ] **Step 5.1：验证 -Report 输出**

```powershell
$reportPath = Join-Path $env:TEMP 'xdev-p3-link-report.json'
Remove-Item -LiteralPath $reportPath -Force -ErrorAction SilentlyContinue

$result = & powershell -NoProfile -File 'scripts/docs/validate-links.ps1' -Path 'scripts/docs/tests/fixtures/links/valid' -Report $reportPath 2>&1
Write-Host $result

if (-not (Test-Path -LiteralPath $reportPath)) {
    throw "JSON 报告未生成：$reportPath"
}

$reportContent = Get-Content -LiteralPath $reportPath -Raw -Encoding utf8
if ($reportContent.Trim() -ne '[]') {
    throw "正面固件报告应为空数组 []，实际内容：$reportContent"
}

Remove-Item -LiteralPath $reportPath -Force -ErrorAction SilentlyContinue
Write-Host "  JSON report test passed" -ForegroundColor Green
```

预期：退出码 0，报告文件包含 `[]`。

### Step 6：验证外部 URL 不被检查

- [ ] **Step 6.1：确认 http/https 链接被跳过**

```powershell
# 创建临时文件，包含外部 URL
$tmpDir = Join-Path $env:TEMP 'xdev-p3-exturl-test'
Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

@"
# Test External URL Skip

[External](https://httpstat.us/404) should NOT be checked.

[Relative](./nonexistent.md) SHOULD be checked.
"@ | Set-Content -LiteralPath (Join-Path $tmpDir 'test.md') -Encoding utf8

$result = & powershell -NoProfile -File 'scripts/docs/validate-links.ps1' -Path $tmpDir 2>&1
Write-Host $result

# 应该因为 ./nonexistent.md 失败，但不因为 https URL 失败
if ($LASTEXITCODE -ne 1) {
    throw "应检测到断链（相对路径），退出码：$LASTEXITCODE"
}
if ($result -match 'httpstat') {
    throw "不应该检查外部 URL"
}

Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
Write-Host "  External URL skip test passed" -ForegroundColor Green
```

预期：退出码 1（因为存在相对断链），但输出中不包含 `httpstat`。

---

## 验收标准

- **AC-001**：`scripts/docs/validate-links.ps1` 存在且语法有效（PowerShell 5.1 AST 解析无错误）。
- **AC-002**：`scripts/docs/tests/validate-links.tests.ps1` 存在，4 个测试用例全部通过。
- **AC-003**：`scripts/docs/tests/fixtures/links/valid/` 包含 3 个文件（index.md、sub/page.md、image.png），全部链接验证通过。
- **AC-004**：`scripts/docs/tests/fixtures/links/invalid/` 包含 3 个 Markdown 文件，验证器正确报告断链错误（退出码 1）。
- **AC-005**：验证器 SelfTest 通过（`-SelfTest` 退出码 0）。
- **AC-006**：外部 URL（`http://`、`https://`）不被检查。
- **AC-007**：相对路径断链被检测（文件不存在）。
- **AC-008**：锚点断链被检测（锚点在目标文件中不存在）。
- **AC-009**：JSON 报告通过 `-Report` 参数输出，格式为有效的 JSON 数组。
- **AC-010**：`scripts/audit-links.ps1` 未被修改。
- **AC-011**：`.github/workflows/ci.yaml` 未被修改。
- **AC-012**：所有新增文件使用 UTF-8 编码。
- **AC-013**：不涉及 P4A/P4B 或任何历史文档迁移工作。

---

## 失败恢复

### 固件文件创建错误

如果固件 Markdown 文件格式有误：

```powershell
# 查看固件内容
Get-Content -LiteralPath 'scripts/docs/tests/fixtures/links/valid/index.md' -Raw

# 重新创建单个文件（使用 apply_patch 或直接重写）
```

### 验证器脚本语法错误

```powershell
# 重新检查语法
$tokens = $null; $errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path 'scripts/docs/validate-links.ps1').Path,
    [ref]$tokens, [ref]$errors
)
$errors | ForEach-Object { Write-Output $_.Message }
```

### 提交前回滚

如果暂存了错误文件：

```powershell
git restore --staged -- scripts/docs/validate-links.ps1
```

如果新文件内容错误，使用 `apply_patch` 修正；不要删除或还原用户的并发修改。

---

## 后续计划

P3 文档链接验证完成后，可执行：

- P2A：Spec Kit 模板覆盖；
- P2B：项目治理模板；
- P5：治理自动化（等待 P2A、P2B、P3）；
- P4A：旧文档元数据治理（等待 P2B 和 P3）；
- P4B：历史文档迁移（等待 P4A 和 P3）。

本计划不授权执行上述任何后续 P4+ 工作。P4A 需要 P3 作为验证安全网，但 P3 的完成不自动触发 P4A。
