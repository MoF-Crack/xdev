# scripts/docs/validate-constitution.ps1
# XDev Constitution validator.
# Checks .specify/memory/constitution.md for placeholders,
# version validity, state validity, and approval consistency.
#
# Usage:
#   ./scripts/docs/validate-constitution.ps1 -ConstitutionPath ./.specify/memory/constitution.md
#   ./scripts/docs/validate-constitution.ps1 -SelfTest
#
# Exit codes: 0 = all pass, 1 = violations found, 2 = bad args / error.

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
$script:exitCode = 0

# ---- helpers ----

function Write-Check {
    param([string]$Check, [string]$Status, [string]$Detail)
    $script:checks += [PSCustomObject]@{
        Check  = $Check
        Status = $Status
        Detail = $Detail
    }
    $icon = if ($Status -eq 'PASS') { '[PASS]' } elseif ($Status -eq 'WARN') { '[WARN]' } else { '[FAIL]' }
    Write-Host "  $icon $Check"
    if ($Detail) {
        $color = if ($Status -eq 'FAIL') { 'Red' } elseif ($Status -eq 'WARN') { 'Yellow' } else { 'Green' }
        Write-Host "       $Detail" -ForegroundColor $color
    }
}

function Invoke-ConstitutionChecks {
    param([string]$Content, [string]$FilePath)

    $checks = @()

    # Check 1: Version field exists and format is valid
    # Try Chinese label first (\u7248\u672C = version), then English
    $version = ''
    if ($Content -match '\*\*(?:\u7248\u672C|Version)\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') {
        $version = $Matches[1].Trim()
        if ($version -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$') {
            $checks += @{Check='version-format'; Status='FAIL'; Detail="Invalid version format: '$version'"}
        } else {
            $checks += @{Check='version-format'; Status='PASS'; Detail="Version: $version"}
        }
    } else {
        # Also try table-style version field
        if ($Content -match '\|\s*\*{0,2}(?:\u7248\u672C|Version)\*{0,2}\s*\|\s*`?([0-9][^`\|\s\n]*)`?') {
            $version = $Matches[1].Trim()
            if ($version -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$') {
                $checks += @{Check='version-format'; Status='FAIL'; Detail="Invalid version format: '$version'"}
            } else {
                $checks += @{Check='version-format'; Status='PASS'; Detail="Version: $version"}
            }
        } elseif ($Content -match '\*\*(?:Version|version)\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') {
            $version = $Matches[1].Trim()
            if ($version -notmatch '^\d+\.\d+\.\d+(-rc\.\d+)?$') {
                $checks += @{Check='version-format'; Status='FAIL'; Detail="Invalid version format: '$version'"}
            } else {
                $checks += @{Check='version-format'; Status='PASS'; Detail="Version: $version"}
            }
        } else {
            $checks += @{Check='version-format'; Status='FAIL'; Detail='Version field missing or unparseable'}
        }
    }

    # Check 2: State field exists and is valid
    # \u72B6\u6001 = state, look for Chinese or English labels
    $state = ''
    if ($Content -match '\*\*(?:\u72B6\u6001|State|Status)\*\*[：:]\s*(.+?)(?:\s*\n|$)') {
        $state = $Matches[1].Trim()
    } elseif ($Content -match '\|\s*\*{0,2}(?:\u72B6\u6001|State|Status)\*{0,2}\s*\|\s*([^|\n]+)\s*\|') {
        $state = $Matches[1].Trim()
    }

    # Clean state value (strip backticks, asterisks)
    $cleanState = if ($state) { $state -replace '[`*]','' } else { '' }

    # Valid constitution states
    $validStates = @('Candidate', 'Ratified', 'Superseded')

    # Also accept Chinese equivalents via pattern matching
    $isCandidateCN = $cleanState -match '\u5019\u9009\u7248'  # contains candidate-chinese prefix
    $isRatifiedCN = $cleanState -match '\u6B63\u5F0F\u6279\u51C6'  # formal-approval
    $isPendingCN  = $cleanState -match '\u5C1A\u672A\u6B63\u5F0F\u6279\u51C6'  # not-yet-formally-approved

    if ($cleanState) {
        if (($validStates -contains $cleanState) -or $isCandidateCN -or $isRatifiedCN -or $isPendingCN) {
            $checks += @{Check='state-validity'; Status='PASS'; Detail="State: $cleanState"}
        } else {
            $checks += @{Check='state-validity'; Status='FAIL'; Detail="Invalid constitution state: '$cleanState' (expected: Candidate, Ratified, Superseded, or Chinese equivalent)"}
        }
    } else {
        $checks += @{Check='state-validity'; Status='FAIL'; Detail='State field missing'}
    }

    # Check 3: Ratified: Pending field
    if ($Content -match '(?:\*{0,2})Ratified(?:\*{0,2})\s*:\s*Pending') {
        $checks += @{Check='ratified-pending'; Status='PASS'; Detail='Ratified: Pending'}
    } else {
        # Also check for Chinese equivalent
        if ($Content -match '\u6B63\u5F0F\u6279\u51C6.*?Pending' -or $Content -match 'Ratified.*?\u5F85\u5B9A') {
            $checks += @{Check='ratified-pending'; Status='PASS'; Detail='Ratified field exists and indicates Pending'}
        } else {
            $checks += @{Check='ratified-pending'; Status='FAIL'; Detail='Ratified: Pending field missing or not set to Pending'}
        }
    }

    # Check 4: No unresolved placeholders in body (exclude Approval History section)
    $bodyForPlaceholder = $Content
    if ($bodyForPlaceholder -match '(?s)^(.*?)## Approval History') {
        $bodyForPlaceholder = $Matches[1]
    } elseif ($bodyForPlaceholder -match '(?s)^(.*?)#+\s*Approval History') {
        $bodyForPlaceholder = $Matches[1]
    }
    $placeholderPattern = '\b(TODO|TBD|PLACEHOLDER)\b'
    $placeholderMatches = [regex]::Matches($bodyForPlaceholder, $placeholderPattern, [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    if ($placeholderMatches.Count -gt 0) {
        $found = ($placeholderMatches | ForEach-Object { $_.Value }) -join ', '
        $checks += @{Check='no-placeholders'; Status='FAIL'; Detail="Placeholders found: $found"}
    } else {
        $checks += @{Check='no-placeholders'; Status='PASS'}
    }

    # Check 5: Approval History section exists
    if ($Content -match '## Approval History') {
        $afterApproval = $Content -replace '^[\s\S]*?## Approval History\s*\n', ''
        $rows = 0
        foreach ($line in ($afterApproval -split "`r`n|`n|`r")) {
            if ($line -match '^\| [^-]' -and $line -notmatch '^\|[- ]+\|[- ]+\|') {
                $rows++
            }
            if ($line -match '^## ' -and $rows -gt 0) { break }
        }
        if ($rows -gt 0) {
            $checks += @{Check='approval-history'; Status='PASS'; Detail="$rows record(s)"}
        } else {
            $checks += @{Check='approval-history'; Status='FAIL'; Detail='Approval History section exists but no records found'}
        }
    } else {
        $checks += @{Check='approval-history'; Status='FAIL'; Detail='## Approval History section missing'}
    }

    # Check 6: Approver consistency in Approval History
    if ($Content -match '## Approval History') {
        $afterApproval = $Content -replace '(?s)^.*?## Approval History\s*\n', ''
        $badApprovers = @()
        $tableSkipped = 0
        foreach ($line in ($afterApproval -split "`r`n|`n|`r")) {
            # When a new Approval History section starts, reset the table skip counter
            if ($line -match '^## Approval History') { $tableSkipped = 0; continue }
            # Detect separator row (|---|---|)
            if ($line -match '^\|[- ]+\|[- ]+\|') { $tableSkipped++; continue }
            # Skip the header row (first row before separator in each table)
            if ($tableSkipped -eq 0 -and $line -match '^\| [^-]') {
                $tableSkipped++
                continue
            }
            if ($line -match '^## ' -and $badApprovers.Count -gt 0) { break }
            if ($line -match '^\| [^-]') {
                $cols = $line -split '\|'
                if ($cols.Count -ge 5) {
                    $approver = $cols[3].Trim()
                    if ($approver -and $approver -ne '-' -and $approver -ne '' -and
                        $approver -ne 'xinyan <zuijiuhjd@foxmail.com>') {
                        $badApprovers += $approver
                    }
                }
            }
            if ($line -match '^## ' -and $badApprovers.Count -gt 0) { break }
        }
        if ($badApprovers.Count -gt 0) {
            $checks += @{Check='approver-consistency'; Status='FAIL'; Detail="Non-standard approver(s): $($badApprovers -join ', ')"}
        } else {
            $checks += @{Check='approver-consistency'; Status='PASS'}
        }
    } else {
        $checks += @{Check='approver-consistency'; Status='FAIL'; Detail='Cannot check: Approval History missing'}
    }

    # Check 7: Document status table exists
    $hasStatusTable = ($Content -match '## \u6587\u6863\u72B6\u6001') -or ($Content -match 'Document Status') -or ($Content -match '\|\s*(?:\u9879\u76EE|Project)\s*\|')
    if ($hasStatusTable) {
        $checks += @{Check='status-table'; Status='PASS'; Detail='Document status metadata present'}
    } else {
        $checks += @{Check='status-table'; Status='WARN'; Detail='Document status section not found'}
    }

    return $checks
}

# ---- SelfTest ----

function Invoke-SelfTest {
    $fixtureRoot = Join-Path $PSScriptRoot 'tests/fixtures/governance'
    if (-not (Test-Path -LiteralPath $fixtureRoot)) {
        Write-Host "  [FAIL] Fixture directory not found: $fixtureRoot" -ForegroundColor Red
        exit 1
    }

    $allPassed = $true

    # Test valid fixture
    Write-Host '--- Valid Fixture ---' -ForegroundColor Cyan
    $validPath = Join-Path $fixtureRoot 'valid/constitution-valid.md'
    if (Test-Path -LiteralPath $validPath) {
        $content = Get-Content -LiteralPath $validPath -Raw -Encoding utf8
        if (-not $content) { $content = '' }
        $results = Invoke-ConstitutionChecks -Content $content -FilePath $validPath
        $failures = @($results | Where-Object { $_.Status -eq 'FAIL' })
        if ($failures.Count -gt 0) {
            Write-Host "  [FAIL] valid/constitution-valid.md: expected all PASS, got $($failures.Count) FAIL(s)" -ForegroundColor Red
            foreach ($f in $failures) { Write-Host "         $($f.Check): $($f.Detail)" -ForegroundColor Red }
            $allPassed = $false
        } else {
            Write-Host "  [OK]   valid/constitution-valid.md: all $($results.Count) checks PASS" -ForegroundColor Green
        }
    } else {
        Write-Host "  [FAIL] valid/constitution-valid.md: file not found" -ForegroundColor Red
        $allPassed = $false
    }

    # Test invalid fixtures
    Write-Host ''
    Write-Host '--- Invalid Fixtures ---' -ForegroundColor Cyan

    $invalidTests = @(
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-placeholder.md'); Name = 'constitution-placeholder.md'; ExpectedCheck = 'no-placeholders' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-missing-version.md'); Name = 'constitution-missing-version.md'; ExpectedCheck = 'version-format' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-wrong-state.md'); Name = 'constitution-wrong-state.md'; ExpectedCheck = 'state-validity' }
    )

    foreach ($t in $invalidTests) {
        if (-not (Test-Path -LiteralPath $t.Path)) {
            Write-Host "  [FAIL] $($t.Name): fixture file not found" -ForegroundColor Red
            $allPassed = $false
            continue
        }
        $content = Get-Content -LiteralPath $t.Path -Raw -Encoding utf8
        if (-not $content) { $content = '' }
        $results = Invoke-ConstitutionChecks -Content $content -FilePath $t.Path
        $targetResult = @($results | Where-Object { $_.Check -eq $t.ExpectedCheck })[0]

        if ($targetResult -and $targetResult.Status -eq 'FAIL') {
            Write-Host "  [OK]   $($t.Name): $($t.ExpectedCheck) = FAIL (expected)" -ForegroundColor Green
        } elseif ($targetResult -and $targetResult.Status -ne 'FAIL') {
            Write-Host "  [FAIL] $($t.Name): $($t.ExpectedCheck) expected FAIL, got $($targetResult.Status)" -ForegroundColor Red
            $allPassed = $false
        } else {
            Write-Host "  [FAIL] $($t.Name): $($t.ExpectedCheck) check not found in results" -ForegroundColor Red
            $allPassed = $false
        }
    }

    Write-Host ''
    Write-Host '--- Approval History Placeholder Exclusion ---' -ForegroundColor Cyan
    $historyOnlyPlaceholder = @'
# XDev Constitution
**Version**: `1.0.0-rc.3`
**Status**: Candidate
**Ratified**: Pending
## Approval History
| Change | Note |
|---|---|
| Example | TODO retained as historical text |
'@
    $historyResults = Invoke-ConstitutionChecks -Content $historyOnlyPlaceholder -FilePath 'selftest-history-placeholder.md'
    $placeholderCheck = @($historyResults | Where-Object { $_.Check -eq 'no-placeholders' })[0]
    if ($placeholderCheck.Status -ne 'PASS') {
        Write-Host "  [FAIL] Approval History TODO must be excluded, got $($placeholderCheck.Status)" -ForegroundColor Red
        $allPassed = $false
    } else {
        Write-Host '  [OK]   Approval History placeholder exclusion: PASS' -ForegroundColor Green
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

# Main validation
if (-not (Test-Path -LiteralPath $ConstitutionPath -PathType Leaf)) {
    Write-Host "ERROR: Constitution file not found: $ConstitutionPath" -ForegroundColor Red
    exit 2
}

Write-Host "Validating Constitution: $ConstitutionPath" -ForegroundColor Cyan
Write-Host ''

$content = ''
try {
    $content = Get-Content -LiteralPath $ConstitutionPath -Raw -Encoding utf8
} catch {
    $content = Get-Content -LiteralPath $ConstitutionPath -Raw
}
if (-not $content) { $content = '' }

$results = Invoke-ConstitutionChecks -Content $content -FilePath $ConstitutionPath

# Display results
foreach ($r in $results) {
    $statusDisplay = if ($r.Status -eq 'PASS') { '  [PASS]' } elseif ($r.Status -eq 'WARN') { '  [WARN]' } else { '  [FAIL]' }
    $color = if ($r.Status -eq 'FAIL') { 'Red' } elseif ($r.Status -eq 'WARN') { 'Yellow' } else { 'Green' }
    Write-Host "$statusDisplay $($r.Check)" -ForegroundColor $color
    if ($r.Detail) {
        Write-Host "         $($r.Detail)" -ForegroundColor $color
    }
}

# Summary
$failed = @($results | Where-Object { $_.Status -eq 'FAIL' }).Count
$passed = @($results | Where-Object { $_.Status -eq 'PASS' }).Count
$warn = @($results | Where-Object { $_.Status -eq 'WARN' }).Count

Write-Host ''
Write-Host "Constitution Validation Summary: $($results.Count) total, $passed PASS, $failed FAIL, $warn WARN" -ForegroundColor $(if ($failed -gt 0) { 'Red' } else { 'Green' })

# Write JSON report
if ($Report) {
    $reportDir = Split-Path -Parent $Report
    if ($reportDir -and -not (Test-Path -LiteralPath $reportDir)) {
        New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
    }
    $results | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $Report -Encoding utf8
    Write-Host "JSON report written to: $Report" -ForegroundColor Cyan
}

if ($failed -gt 0) { exit 1 }
exit 0
