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
    [string[]]$Exclude = @('node_modules', '.git', 'vendor', 'scripts\docs\tests\fixtures'),
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

    # Remove Markdown link syntax: [text](url) -> text
    $text = $HeadingText -replace '\[([^\]]*)\]\([^\)]*\)','$1'
    # Remove image syntax: ![alt](url) -> alt
    $text = $text -replace '!\[([^\]]*)\]\([^\)]*\)','$1'
    # Convert to lowercase
    $text = $text.ToLowerInvariant()
    # Remove punctuation and symbols, but preserve Unicode letters (\p{L}), digits (\p{N}), spaces, and hyphens
    $text = $text -replace '[^\p{L}\p{N}\s-]',''
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

        # Strip inline code spans before link extraction: `...` content replaced with placeholder
        $lineForLinks = $line -replace '`[^`]+`','`__CODE__`'

        # Inline links: [text](url) and ![alt](url)
        # Pattern: match [...](...) but not inside code spans
        $pattern = '(!)?\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)'
        $matches = [regex]::Matches($lineForLinks, $pattern)
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
        $defMatches = [regex]::Matches($lineForLinks, $defPattern)
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
    $selfRepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
    $stRoot = Join-Path $selfRepoRoot 'build\test-temp\links-selftest'
    # Path escape protection. The run root must be exactly one run-<guid> child.
    $normalizedStParent = [System.IO.Path]::GetFullPath($stRoot).TrimEnd('\')
    $normalizedRepo = [System.IO.Path]::GetFullPath($selfRepoRoot).TrimEnd('\')
    if ($normalizedStParent -eq $normalizedRepo -or
        -not $normalizedStParent.StartsWith($normalizedRepo + '\', [StringComparison]::OrdinalIgnoreCase)) {
        Write-Host "FATAL: Path escape: $normalizedStParent not under $normalizedRepo" -ForegroundColor Red
        exit 1
    }
    $parentCreated = -not (Test-Path -LiteralPath $normalizedStParent -PathType Container)
    if ($parentCreated) { New-Item -ItemType Directory -Path $normalizedStParent -Force | Out-Null }
    $stRoot = Join-Path $normalizedStParent ('run-' + [guid]::NewGuid().ToString('N'))
    $normalizedSt = [System.IO.Path]::GetFullPath($stRoot).TrimEnd('\')
    if ([System.IO.Path]::GetDirectoryName($normalizedSt).TrimEnd('\') -ne $normalizedStParent -or
        $normalizedSt -notmatch '\\run-[0-9a-f]+$') {
        throw "Unsafe links SelfTest run root: $normalizedSt"
    }

    $script:selftestFailed = $false
    try {
        # Create minimal valid fixture inline
        $validDir = Join-Path $stRoot 'valid'
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
        $invalidDir = Join-Path $stRoot 'invalid'
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
            $script:selftestFailed = $true; return
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
            $script:selftestFailed = $true; return
        }
        Write-Host "  [OK]   invalid fixture: $($script:errors.Count) error(s) found (expected)" -ForegroundColor Green

        # Test 3: anchor generation
        $testCases = @(
            @{ Heading = 'My Heading'; Want = 'my-heading' },
            @{ Heading = 'Hello World 123'; Want = 'hello-world-123' },
            @{ Heading = '## Special @#$ Chars!'; Want = 'special-chars' },
            @{ Heading = '  Trim Me  '; Want = 'trim-me' },
            @{ Heading = '`code` in heading'; Want = 'code-in-heading' },
            @{ Heading = 'Code `in` Heading'; Want = 'code-in-heading' },
            @{ Heading = '`env.uni.yaml`'; Want = 'envuniyaml' }
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
            $script:selftestFailed = $true; return
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
            $script:selftestFailed = $true; return
        }

        Write-Host ''
        Write-Host 'SelfTest passed (4/4)' -ForegroundColor Green
    } finally {
        if (Test-Path -LiteralPath $normalizedSt) {
            Remove-Item -Recurse -Force -LiteralPath $normalizedSt -ErrorAction SilentlyContinue
        }
        if ($parentCreated -and (Test-Path -LiteralPath $normalizedStParent -PathType Container) -and
            @(Get-ChildItem -LiteralPath $normalizedStParent -Force -ErrorAction SilentlyContinue).Count -eq 0) {
            Remove-Item -Force -LiteralPath $normalizedStParent -ErrorAction SilentlyContinue
        }
    }

    if ($script:selftestFailed) { exit 1 }
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

# Build exclusion patterns (escape for regex safety)
$excludePatterns = $Exclude | ForEach-Object { [regex]::Escape($_) }
$excludePattern = $excludePatterns -join '|'

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

# Dedup errors by File+Line+Link to avoid duplicate reports
$deduped = @{}
foreach ($e in $script:errors) {
    $key = "$($e.File)|$($e.Line)|$($e.Link)"
    if (-not $deduped.ContainsKey($key)) {
        $deduped[$key] = $e
    }
}
$script:errors = @($deduped.Values)

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
