# scripts/docs/validate-governance.ps1
# XDev comprehensive governance validator.

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

$ErrorActionPreference = 'Stop'
$script:repoRoot = (Resolve-Path $RepoRoot).Path
$script:reportSections = @{}
$script:hasFailures = $false

function Write-SectionHeader {
    param([string]$Title)
    Write-Host ''
    Write-Host "=== $Title ===" -ForegroundColor Cyan
}

function Invoke-ChildValidator {
    param([string]$ScriptPath, [hashtable]$Arguments, [string]$Label)
    if (-not (Test-Path -LiteralPath $ScriptPath -PathType Leaf)) {
        $msg = "[$Label] Script not found: $ScriptPath"
        Write-Host "  $msg" -ForegroundColor Red
        $script:reportSections[$Label] = @{ Status = 'FAIL'; Output = $msg }
        $script:hasFailures = $true
        return @{ ExitCode = 2; Output = $msg }
    }
    $childArguments = @()
    foreach ($k in $Arguments.Keys) {
        $value = $Arguments[$k]
        if ($value -is [System.Array]) {
            $items = @($value | ForEach-Object { "'$([string]$_ -replace "'", "''")'" }) -join ', '
            $childArguments += "-$k @($items)"
        } else {
            $childArguments += "-$k '$([string]$value -replace "'", "''")'"
        }
    }
    $escapedScriptPath = $ScriptPath -replace "'", "''"
    $childCommand = "& '$escapedScriptPath' $($childArguments -join ' ')"
    Write-Host "  Running: $Label..." -ForegroundColor DarkGray
    $output = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $childCommand 2>&1
    $exitCode = if ($LASTEXITCODE -ne $null) { $LASTEXITCODE } else { 0 }
    $resultOutput = ($output | Out-String).Trim()
    $resultBlocked = $false
    if ($Arguments.ContainsKey('ResultFile')) {
        $resultFile = [string]$Arguments['ResultFile']
        if (-not (Test-Path -LiteralPath $resultFile -PathType Leaf)) {
            $resultBlocked = $true
            $resultOutput = if ($resultOutput) { "$resultOutput`nVALIDATOR_NO_RESULT: $resultFile" } else { "VALIDATOR_NO_RESULT: $resultFile" }
        } else {
            $resultLine = (Get-Content -LiteralPath $resultFile -Raw -Encoding utf8).Trim()
            if ($resultLine -match '^BLOCK\t') {
                $resultBlocked = $true
                $resultOutput = $resultLine
            } elseif ($resultLine -notmatch '^PASS\t') {
                $resultBlocked = $true
                $resultOutput = $resultLine
            }
        }
    }
    $effectiveExitCode = if ($exitCode -ne 0) { $exitCode } elseif ($resultBlocked) { 1 } else { 0 }
    $status = if ($effectiveExitCode -eq 0) { 'PASS' } else { 'FAIL' }
    $script:reportSections[$Label] = @{ Status = $status; ExitCode = $effectiveExitCode; Output = $resultOutput }
    if ($effectiveExitCode -ne 0) {
        $script:hasFailures = $true
        Write-Host "  [$Label] FAIL (exit=$effectiveExitCode)" -ForegroundColor Red
    } else {
        Write-Host "  [$Label] PASS" -ForegroundColor Green
    }
    return @{ ExitCode = $effectiveExitCode; Output = $resultOutput }
}

function Get-VersionFromText {
    param([string]$Text)
    if ($Text -match '[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?') {
        return $Matches[0]
    }
    return ''
}

function Invoke-CrossDocChecks {
    Write-SectionHeader 'Cross-Document Consistency'
    $issues = @()

    $constitutionPath = Join-Path $script:repoRoot '.specify\memory\constitution.md'
    $constitutionRc = ''
    if (Test-Path -LiteralPath $constitutionPath) {
        $cc = ''
        try { $cc = Get-Content -LiteralPath $constitutionPath -Raw -Encoding utf8 } catch { $cc = '' }
        if ($cc -match '\*\*Version\*\*[:\uFF1A]\s*`?([0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+)') {
            $constitutionRc = $Matches[1]
        } elseif ($cc -match 'Version.*?`?([0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+)') {
            $constitutionRc = $Matches[1]
        }
    }

    $specDirs = @(Get-ChildItem -LiteralPath (Join-Path $script:repoRoot 'specs') -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^\d' -and $_.Name -notmatch '^archive' })

    foreach ($dir in $specDirs) {
        $specFile = Join-Path $dir.FullName 'spec.md'
        if (-not (Test-Path -LiteralPath $specFile)) {
            $issues += "Active spec directory missing spec.md: $($dir.FullName)"
            continue
        }
        $sc = ''
        try { $sc = Get-Content -LiteralPath $specFile -Raw -Encoding utf8 } catch { $sc = '' }
        if (-not $sc) { continue }

        $specRefRc = ''
        if ($sc -match 'Constitution.*?`?([0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+)') {
            $specRefRc = $Matches[1]
        }
        if ($specRefRc -and $constitutionRc -and $specRefRc -ne $constitutionRc) {
            $dn = Split-Path -Leaf $dir.FullName
            $issues += "Cross-ref mismatch: spec '$dn' refs Constitution '$specRefRc' but actual is '$constitutionRc'"
        }

        $planFile = Join-Path $dir.FullName 'plan.md'
        if (Test-Path -LiteralPath $planFile) {
            $pc = ''
            try { $pc = Get-Content -LiteralPath $planFile -Raw -Encoding utf8 } catch { $pc = '' }
            if ($pc) {
                $planRefVer = ''
                if ($pc -match 'Spec.*?(?:Version|版本).*?`?([0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?)') {
                    $planRefVer = $Matches[1]
                }
                $specVer = Get-VersionFromText -Text $sc
                if ($planRefVer -and $specVer -and $planRefVer -ne $specVer) {
                    $dn = Split-Path -Leaf $dir.FullName
                    $issues += "Cross-ref mismatch: plan refs spec v'$planRefVer' but spec.md is v'$specVer' in '$dn'"
                }
            }
        }
    }

    $featureJson = Join-Path $script:repoRoot '.specify\feature.json'
    if (Test-Path -LiteralPath $featureJson) {
        try {
            $fj = Get-Content -LiteralPath $featureJson -Raw -Encoding utf8 | ConvertFrom-Json
            foreach ($prop in $fj.PSObject.Properties) {
                if ($prop.Value -is [string] -and $prop.Value -match 'archive') {
                    $issues += ".specify/feature.json: '$($prop.Name)' points to archive: $($prop.Value)"
                }
            }
        } catch {
            $issues += ".specify/feature.json: JSON parse error"
        }
    }

    $excDir = Join-Path $script:repoRoot 'docs\security\exceptions'
    if (Test-Path -LiteralPath $excDir -PathType Container) {
        $excFiles = @(Get-ChildItem -LiteralPath $excDir -Filter '*.md' -File -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -notmatch '^README\.md$' })
        $today = Get-Date
        foreach ($ef in $excFiles) {
            $ec = ''
            try { $ec = Get-Content -LiteralPath $ef.FullName -Raw -Encoding utf8 } catch { $ec = '' }
            if ($ec -match '(?:Deadline|Expiry)[:\uFF1A]\s*(\d{4}-\d{2}-\d{2})') {
                $deadline = [datetime]::ParseExact($Matches[1], 'yyyy-MM-dd', $null)
                if ($deadline -lt $today) {
                    $issues += "Security exception expired: $($ef.Name) (deadline: $($Matches[1]))"
                }
            }
        }
    }

    $status = if ($issues.Count -eq 0) { 'PASS' } else { 'FAIL' }
    $script:reportSections['cross-document'] = @{ Status = $status; Issues = $issues }
    if ($issues.Count -gt 0) {
        $script:hasFailures = $true
        foreach ($i in $issues) { Write-Host "  [FAIL] $i" -ForegroundColor Red }
    } else {
        Write-Host "  [PASS] All cross-doc checks passed" -ForegroundColor Green
    }
}
# ---- SelfTest ----
function Invoke-SelfTest {
    Write-Host 'Governance SelfTest (in-built checks only)' -ForegroundColor Cyan
    $td = Join-Path $env:TEMP ('xdev-gov-st-' + [guid]::NewGuid().ToString().Substring(0,6))
    try {
        New-Item -ItemType Directory -Path $td -Force | Out-Null
        $sd = Join-Path $td '.specify'
        New-Item -ItemType Directory -Path $sd -Force | Out-Null
        '{"current-feature":"specs/archive/2026/000-test/"}' | Set-Content -LiteralPath (Join-Path $sd 'feature.json') -Encoding utf8

        $specsDir = Join-Path $td 'specs'
        $ad = Join-Path $specsDir '001-test'
        $ard = Join-Path $specsDir 'archive\2026\000-test'
        New-Item -ItemType Directory -Path $ad -Force | Out-Null
        New-Item -ItemType Directory -Path $ard -Force | Out-Null

        $specLines = @(
            '# Test Spec',
            '> **DocType**: Feature Spec',
            '> **Status**: Draft',
            '> **Version**: 0.1.0',
            '> **Constitution Ref**: .specify/memory/constitution.md `1.0.0-rc.3`'
        )
        $specLines -join "`r`n" | Set-Content -LiteralPath (Join-Path $ad 'spec.md') -Encoding utf8

        $md = Join-Path $sd 'memory'
        New-Item -ItemType Directory -Path $md -Force | Out-Null
        $constLines = @(
            '# Constitution',
            '| Field | Value |',
            '|---|---|',
            '| **Version** | `1.0.0-rc.4` |',
            '| **Status** | Candidate |',
            '**Ratified**: Pending'
        )
        $constLines -join "`r`n" | Set-Content -LiteralPath (Join-Path $md 'constitution.md') -Encoding utf8

        $old = $script:repoRoot
        $script:repoRoot = $td
        $script:reportSections = @{}
        $script:hasFailures = $false
        Invoke-CrossDocChecks

        $cr = $script:reportSections['cross-document']
        if ($cr.Status -ne 'FAIL') {
            Write-Host "  [FAIL] Expected FAIL, got $($cr.Status)" -ForegroundColor Red
            exit 1
        }
        Write-Host "  [OK]   cross-document: FAIL (expected)" -ForegroundColor Green

        $hasArchive = ($cr.Issues | Where-Object { $_ -match 'feature.json.*archive' }).Count -gt 0
        $hasVer = ($cr.Issues | Where-Object { $_ -match 'Cross-ref mismatch' }).Count -gt 0
        if (-not $hasArchive) { Write-Host "  [FAIL] Missing archive detection" -ForegroundColor Red; exit 1 }
        if (-not $hasVer) { Write-Host "  [FAIL] Missing version mismatch" -ForegroundColor Red; exit 1 }
        Write-Host "  [OK]   Archive detection: OK" -ForegroundColor Green
        Write-Host "  [OK]   Version mismatch:  OK" -ForegroundColor Green
        Write-Host ''
        Write-Host 'SelfTest passed' -ForegroundColor Green
    } finally {
        Remove-Item -Recurse -Force $td -ErrorAction SilentlyContinue
    }
}

# === DRIVER ===
if ($SelfTest) {
    Invoke-SelfTest
    exit 0
}

Write-Host "Governance Validation - Repo: $($script:repoRoot)" -ForegroundColor Cyan
$script:reportSections['repo'] = @{ Root = $script:repoRoot; Timestamp = (Get-Date -Format 'yyyy-MM-ddTHH:mm:ss') }

Write-SectionHeader 'Constitution Validation'
if (-not $SkipExternal) {
    Invoke-ChildValidator -ScriptPath (Join-Path $PSScriptRoot 'validate-constitution.ps1') `
        -Arguments @{ ConstitutionPath = (Join-Path $script:repoRoot '.specify\memory\constitution.md') } `
        -Label 'constitution'
} else { Write-Host "  [SKIP]" -ForegroundColor Yellow }

Write-SectionHeader 'Active Specs Validation'
if (-not $SkipExternal) {
    Invoke-ChildValidator -ScriptPath (Join-Path $PSScriptRoot 'validate-specs.ps1') `
        -Arguments @{ SpecDir = @((Join-Path $script:repoRoot 'specs')) } `
        -Label 'specs'
} else { Write-Host "  [SKIP]" -ForegroundColor Yellow }

Write-SectionHeader 'Approval Gate Validation'
if (-not $SkipExternal) {
    $appScript = Join-Path $PSScriptRoot 'validate-approvals.ps1'
    if (Test-Path -LiteralPath $appScript) {
        $tr = Join-Path $env:TEMP 'gov-approval-result.txt'
        Invoke-ChildValidator -ScriptPath $appScript -Arguments @{ BaseRef = 'HEAD~1'; ResultFile = $tr } -Label 'approvals'
        Remove-Item $tr -ErrorAction SilentlyContinue
    } else {
        Write-Host "  [WARN] validate-approvals.ps1 not found" -ForegroundColor Yellow
        $script:reportSections['approvals'] = @{ Status = 'WARN'; Output = 'Script not found' }
    }
} else { Write-Host "  [SKIP]" -ForegroundColor Yellow }

Write-SectionHeader 'Document Link Validation'
if (-not $SkipExternal) {
    $linkScript = Join-Path $PSScriptRoot 'validate-links.ps1'
    if (Test-Path -LiteralPath $linkScript) {
        Invoke-ChildValidator -ScriptPath $linkScript `
            -Arguments @{ Path = @((Join-Path $script:repoRoot 'docs'), (Join-Path $script:repoRoot 'specs'), $script:repoRoot) } `
            -Label 'links'
    } else {
        Write-Host "  [WARN] validate-links.ps1 not found" -ForegroundColor Yellow
        $script:reportSections['links'] = @{ Status = 'WARN'; Output = 'Script not found' }
    }
} else { Write-Host "  [SKIP]" -ForegroundColor Yellow }

Invoke-CrossDocChecks

Write-Host ''
Write-Host '===================================================' -ForegroundColor Cyan
Write-Host '  Governance Validation Summary' -ForegroundColor Cyan
Write-Host '==================================================='

$sp = 0; $sf = 0; $sw = 0
foreach ($k in ($script:reportSections.Keys | Sort-Object)) {
    if ($k -eq 'repo') { continue }
    $sec = $script:reportSections[$k]
    $s = $sec.Status
    $icon = if ($s -eq 'PASS') { '[PASS]' } elseif ($s -eq 'WARN') { '[WARN]' } else { '[FAIL]' }
    $color = if ($s -eq 'FAIL') { 'Red' } elseif ($s -eq 'WARN') { 'Yellow' } else { 'Green' }
    Write-Host "  $icon $k" -ForegroundColor $color
    if ($s -eq 'PASS') { $sp++ } elseif ($s -eq 'WARN') { $sw++ } else { $sf++ }
}
Write-Host ''
Write-Host "Total sections: $($sp+$sf+$sw) | PASS: $sp | FAIL: $sf | WARN: $sw" `
    -ForegroundColor $(if ($sf -gt 0) { 'Red' } else { 'Green' })

if ($Report) {
    $rd = Split-Path -Parent $Report
    if ($rd -and -not (Test-Path -LiteralPath $rd)) { New-Item -ItemType Directory -Path $rd -Force | Out-Null }
    $jr = [PSCustomObject]@{ repo = $script:reportSections['repo']; sections = [PSCustomObject]@{} }
    foreach ($k in ($script:reportSections.Keys | Sort-Object)) {
        if ($k -ne 'repo') {
            $so = [PSCustomObject]$script:reportSections[$k]
            $jr.sections | Add-Member -MemberType NoteProperty -Name $k -Value $so
        }
    }
    $jr | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $Report -Encoding utf8
    Write-Host "JSON report: $Report" -ForegroundColor Cyan
}

if ($sf -gt 0) { exit 1 }
exit 0
