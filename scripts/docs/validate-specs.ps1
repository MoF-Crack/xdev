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
    [string[]]$SpecDir = @((Join-Path (Get-Location).Path 'specs')),
    [Parameter(ParameterSetName='Normal')]
    [string]$Report = '',
    [Parameter(ParameterSetName='SelfTest', Mandatory=$true)]
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
$script:results = @()
$script:exitCode = 0

# ---- helpers ----

function Write-Result {
    param([string]$SpecPath, [string]$Status, [string[]]$Issues)
    $script:results += [PSCustomObject]@{
        Spec   = $SpecPath
        Status = $Status
        Issues = $Issues
    }
    $icon = if ($Status -eq 'PASS') { '[PASS]' } elseif ($Status -eq 'WARN') { '[WARN]' } else { '[FAIL]' }
    Write-Host "  $icon $SpecPath"
    foreach ($i in $Issues) {
        $color = if ($Status -eq 'FAIL') { 'Red' } else { 'Yellow' }
        Write-Host "         $i" -ForegroundColor $color
    }
}

# Extract metadata using regex patterns compatible with PS 5.1
# Uses Unicode escapes for Chinese field labels (same pattern as validate-approvals.ps1)
function Get-SpecMetadata {
    param([string]$Content)

    $meta = @{
        DocType        = ''
        State          = ''
        DocVersion     = ''
        CreatedDate    = ''
        LastUpdated    = ''
        Owner          = ''
        AuthorityScope = ''
    }

    # \u6587\u6863\u7C7B\u578B = DocType / DocType
    if ($Content -match '\*\*(?:\u6587\u6863\u7C7B\u578B|DocType)\*\*[：:]\s*(.+?)(?:\s*\n|$)') {
        $meta.DocType = $Matches[1].Trim()
    }
    # \u72B6\u6001 = State
    if ($Content -match '\*\*(?:\u72B6\u6001|State|Status)\*\*[：:]\s*(.+?)(?:\s*\n|$)') {
        $meta.State = $Matches[1].Trim()
    }
    # \u6587\u6863\u7248\u672C = Version
    if ($Content -match '\*\*(?:\u6587\u6863\u7248\u672C|Version)\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') {
        $meta.DocVersion = $Matches[1].Trim()
    }
    # \u521B\u5EFA\u65E5\u671F = Created Date
    if ($Content -match '\*\*(?:\u521B\u5EFA\u65E5\u671F|Created[ _]Date|Create[ _]Date)\*\*[：:]\s*(\d{4}-\d{2}-\d{2})') {
        $meta.CreatedDate = $Matches[1].Trim()
    }
    # \u6700\u540E\u66F4\u65B0 = Last Updated
    if ($Content -match '\*\*(?:\u6700\u540E\u66F4\u65B0|Last[ _]Updated|Last[ _]Update)\*\*[：:]\s*(\d{4}-\d{2}-\d{2})') {
        $meta.LastUpdated = $Matches[1].Trim()
    }
    # \u6240\u6709\u8005 = Owner
    if ($Content -match '\*\*(?:\u6240\u6709\u8005|Owner|Author)\*\*[：:]\s*(.+?)(?:\s*\n|$)') {
        $meta.Owner = $Matches[1].Trim()
    }
    # \u6743\u5A01\u8303\u56F4 = Authority Scope
    if ($Content -match '\*\*(?:\u6743\u5A01\u8303\u56F4|Authority[ _]Scope|Scope)\*\*[：:]\s*(.+?)(?:\s*\n|$)') {
        $meta.AuthorityScope = $Matches[1].Trim()
    }

    # Also try English-only patterns as fallback
    if (-not $meta.DocType) {
        if ($Content -match '\*\*Document Type\*\*[：:]\s*(.+?)(?:\s*\n|$)') { $meta.DocType = $Matches[1].Trim() }
    }
    if (-not $meta.State) {
        if ($Content -match '\*\*Status\*\*[：:]\s*(.+?)(?:\s*\n|$)') { $meta.State = $Matches[1].Trim() }
    }
    if (-not $meta.DocVersion) {
        if ($Content -match '\*\*(?:Document[ _])?Version\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') { $meta.DocVersion = $Matches[1].Trim() }
    }

    return $meta
}

function Validate-SpecFile {
    param([string]$SpecPath, [string]$SpecDir)

    $issues = @()
    $content = ''
    try {
        $content = Get-Content -LiteralPath $SpecPath -Raw -Encoding utf8
    } catch {
        $content = Get-Content -LiteralPath $SpecPath -Raw
        if (-not $content) { $content = '' }
    }
    if (-not $content) { $content = '' }

    $meta = Get-SpecMetadata -Content $content

    # Check 1: Required metadata existence
    $requiredMeta = @{
        'DocType'        = $meta.DocType
        'State'          = $meta.State
        'DocVersion'     = $meta.DocVersion
        'CreatedDate'    = $meta.CreatedDate
        'LastUpdated'    = $meta.LastUpdated
        'Owner'          = $meta.Owner
        'AuthorityScope' = $meta.AuthorityScope
    }

    $missing = @()
    foreach ($k in $requiredMeta.Keys) {
        if ([string]::IsNullOrWhiteSpace($requiredMeta[$k])) {
            $missing += $k
        }
    }
    if ($missing.Count -gt 0) {
        $issues += "Missing required metadata fields: $($missing -join ', ')"
    }

    # Check 2: State validity
    $validStates = @('Draft', 'Approved', 'In Progress', 'Verified', 'Accepted', 'Archived')
    if ($meta.State -and ($validStates -notcontains $meta.State)) {
        $issues += "Invalid state '$($meta.State)'; must be one of: $($validStates -join ', ')"
    }

    # Check 3: Version format
    if ($meta.DocVersion -and ($meta.DocVersion -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$')) {
        $issues += "Invalid version format '$($meta.DocVersion)'; expected SemVer (X.Y.Z or X.Y.Z-rc.N)"
    }

    # Check 4: Date format
    if ($meta.CreatedDate -and ($meta.CreatedDate -notmatch '^\d{4}-\d{2}-\d{2}$')) {
        $issues += "Invalid created date format '$($meta.CreatedDate)'; expected YYYY-MM-DD"
    }
    if ($meta.LastUpdated -and ($meta.LastUpdated -notmatch '^\d{4}-\d{2}-\d{2}$')) {
        $issues += "Invalid last updated date format '$($meta.LastUpdated)'; expected YYYY-MM-DD"
    }

    # Check 5: Approval History exists and has at least one record
    # Accept Chinese+English mixed headers like \u6279\u51C6\u8BB0\u5F55 (Approval History)
    if ($content -match '## (?:Approval History|\u6279\u51C6\u8BB0\u5F55)') {
        $afterApproval = $content -replace '(?s)^.*?## (?:Approval History|\u6279\u51C6\u8BB0\u5F55)\s*\n', ''
        $rows = 0
        foreach ($line in ($afterApproval -split "`r`n|`n|`r")) {
            if ($line -match '^\| [^-]' -and $line -notmatch '^\|[- ]+\|[- ]+\|') {
                $rows++
            }
            if ($line -match '^## ' -and $rows -gt 0) { break }
        }
        if ($rows -eq 0) {
            $issues += 'Approval History section exists but contains no records'
        }
    } else {
        $issues += 'Missing ## Approval History section'
    }

    # Check 6: No unresolved placeholders (case-insensitive)
    $placeholderPattern = '\b(TODO|TBD|\[NEEDS CLARIFICATION\])\b'
    $placeholderMatches = [regex]::Matches($content, $placeholderPattern, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    if ($placeholderMatches.Count -gt 0) {
        $found = ($placeholderMatches | ForEach-Object { $_.Value }) -join ', '
        $issues += "Unresolved placeholders found: $found"
    }

    # Check 7: Plan consistency check (if plan.md exists in same dir)
    if ($SpecDir) {
        $planPath = Join-Path $SpecDir 'plan.md'
        if (Test-Path -LiteralPath $planPath -PathType Leaf) {
            if ($meta.State -eq 'Draft') {
                $issues += 'Spec state is Draft but plan.md exists; Spec should be at least Approved when Plan is present'
            }
        }
    }

    # Check 8: SDD Loop completeness for Accepted/Verified specs
    # Accepted or Verified specs must have plan.md, tasks.md, and reviews/ directory
    if ($SpecDir -and ($meta.State -eq 'Accepted' -or $meta.State -eq 'Verified')) {
        $planPath = Join-Path $SpecDir 'plan.md'
        $tasksPath = Join-Path $SpecDir 'tasks.md'
        $reviewsPath = Join-Path $SpecDir 'reviews'

        if (-not (Test-Path -LiteralPath $planPath -PathType Leaf)) {
            $issues += "SDD Loop: Spec is $($meta.State) but plan.md is missing"
        }
        if (-not (Test-Path -LiteralPath $tasksPath -PathType Leaf)) {
            $issues += "SDD Loop: Spec is $($meta.State) but tasks.md is missing"
        }
        if (-not (Test-Path -LiteralPath $reviewsPath -PathType Container)) {
            $issues += "SDD Loop: Spec is $($meta.State) but reviews/ directory is missing"
        } else {
            # Check for at least 2 review files (spec review + quality review)
            $reviewFiles = @(Get-ChildItem -LiteralPath $reviewsPath -Filter '*.md' -ErrorAction SilentlyContinue)
            if ($reviewFiles.Count -lt 2) {
                $issues += "SDD Loop: reviews/ has $($reviewFiles.Count) file(s), expected at least 2 (spec review + quality review)"
            }
        }
    }

    $status = if ($issues.Count -eq 0) { 'PASS' } else { 'FAIL' }
    Write-Result -SpecPath $SpecPath -Status $status -Issues $issues

    return @{ Status = $status; Issues = $issues }
}

# ---- SelfTest ----

function Invoke-SelfTest {
    $fixtureRoot = Join-Path $PSScriptRoot 'tests/fixtures/governance'
    if (-not (Test-Path -LiteralPath $fixtureRoot)) {
        Write-Host "  [FAIL] Fixture directory not found: $fixtureRoot" -ForegroundColor Red
        exit 1
    }

    $allPassed = $true

    # Test valid fixtures
    Write-Host '--- Valid Fixtures ---' -ForegroundColor Cyan
    $validTests = @(
        @{ Path = (Join-Path $fixtureRoot 'valid/spec.md'); Dir = (Join-Path $fixtureRoot 'valid'); Name = 'valid/spec.md' },
        @{ Path = (Join-Path $fixtureRoot 'valid/spec-no-plan/spec.md'); Dir = (Join-Path $fixtureRoot 'valid/spec-no-plan'); Name = 'valid/spec-no-plan/spec.md' }
    )
    foreach ($t in $validTests) {
        if (-not (Test-Path -LiteralPath $t.Path)) {
            Write-Host "  [FAIL] $($t.Name): fixture file not found" -ForegroundColor Red
            $allPassed = $false
            continue
        }
        $r = Validate-SpecFile -SpecPath $t.Path -SpecDir $t.Dir
        if ($r.Status -ne 'PASS') {
            Write-Host "  [FAIL] $($t.Name): expected PASS, got FAIL" -ForegroundColor Red
            foreach ($iss in $r.Issues) { Write-Host "         $iss" -ForegroundColor Red }
            $allPassed = $false
        } else {
            Write-Host "  [OK]   $($t.Name): PASS (expected)" -ForegroundColor Green
        }
    }

    # Test invalid fixtures
    Write-Host ''
    Write-Host '--- Invalid Fixtures ---' -ForegroundColor Cyan
    $invalidTests = @(
        @{ Path = (Join-Path $fixtureRoot 'invalid/spec-missing-meta.md'); Name = 'spec-missing-meta.md'; ExpectedIssue = 'metadata' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/spec-missing-approval.md'); Name = 'spec-missing-approval.md'; ExpectedIssue = 'Approval History' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/spec-with-placeholder.md'); Name = 'spec-with-placeholder.md'; ExpectedIssue = 'placeholder' }
    )
    foreach ($t in $invalidTests) {
        if (-not (Test-Path -LiteralPath $t.Path)) {
            Write-Host "  [FAIL] $($t.Name): fixture file not found" -ForegroundColor Red
            $allPassed = $false
            continue
        }
        $r = Validate-SpecFile -SpecPath $t.Path -SpecDir ''
        if ($r.Status -ne 'FAIL') {
            Write-Host "  [FAIL] $($t.Name): expected FAIL, got $($r.Status)" -ForegroundColor Red
            $allPassed = $false
        } else {
            $matchDesc = if ($t.ExpectedIssue) { " (should mention: $($t.ExpectedIssue))" } else { '' }
            Write-Host "  [OK]   $($t.Name): FAIL (expected)$matchDesc" -ForegroundColor Green
        }
    }

    Write-Host ''
    if ($allPassed) {
        Write-Host 'SelfTest passed' -ForegroundColor Green
    } else {
        Write-Host 'SelfTest FAILED' -ForegroundColor Red
        exit 1
    }
}

# === DRIVER ===

if ($SelfTest) {
    Invoke-SelfTest
    exit 0
}

# Collect active spec directories (exclude archive)
$specDirs = @()
foreach ($d in $SpecDir) {
    if (-not (Test-Path -LiteralPath $d)) {
        Write-Host "WARNING: Spec directory not found: $d" -ForegroundColor Yellow
        continue
    }
    # Direct spec directory given (ends with spec.md inside)
    $specFile = Join-Path $d 'spec.md'
    if (Test-Path -LiteralPath $specFile -PathType Leaf) {
        $specDirs += $d
    } else {
        # Look for subdirectories starting with digit (active spec dirs, exclude archive)
        $subDirs = @(Get-ChildItem -LiteralPath $d -Directory -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -match '^\d' -and $_.Name -notmatch '^archive|^000-enhance' })
        foreach ($sub in $subDirs) {
            $specDirs += $sub.FullName
        }
    }
}

if ($specDirs.Count -eq 0) {
    Write-Host 'No active spec directories found.' -ForegroundColor Yellow
    exit 0
}

Write-Host "Validating $($specDirs.Count) active spec directory/directories..." -ForegroundColor Cyan
Write-Host ''

foreach ($dir in $specDirs) {
    $specFile = Join-Path $dir 'spec.md'
    if (Test-Path -LiteralPath $specFile -PathType Leaf) {
        $null = Validate-SpecFile -SpecPath $specFile -SpecDir $dir
    } else {
        Write-Result -SpecPath $dir -Status 'FAIL' -Issues @('spec.md not found in directory')
    }
}

# Summary
$total = $script:results.Count
$failed = @($script:results | Where-Object { $_.Status -eq 'FAIL' }).Count
$passed = @($script:results | Where-Object { $_.Status -eq 'PASS' }).Count
$warn = @($script:results | Where-Object { $_.Status -eq 'WARN' }).Count

Write-Host ''
Write-Host "Spec Validation Summary: $total total, $passed PASS, $failed FAIL, $warn WARN" -ForegroundColor $(if ($failed -gt 0) { 'Red' } else { 'Green' })

# Write JSON report if requested
if ($Report) {
    $reportDir = Split-Path -Parent $Report
    if ($reportDir -and -not (Test-Path -LiteralPath $reportDir)) {
        New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
    }
    $script:results | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $Report -Encoding utf8
    Write-Host "JSON report written to: $Report" -ForegroundColor Cyan
}

if ($failed -gt 0) { exit 1 }
exit 0