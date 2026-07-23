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

# Helper: parse Approval History section and check for a Ratified status row
function Test-ApprovalHistoryHasRatifiedRow {
    param([string]$Content)
    # Split on ## Approval History markers; check each section after a heading
    $parts = $Content -split '## Approval History'
    if ($parts.Count -lt 2) { return $false }
    for ($i = 1; $i -lt $parts.Count; $i++) {
        $section = $parts[$i]
        # Trim to next ## heading (or end)
        if ($section -match '\n## ') {
            $section = $section.Substring(0, $section.IndexOf("`n## "))
        }
        $lines = $section -split "\r?\n"
        $headerFound = $false
        $statusColIdx = -1
        foreach ($line in $lines) {
            if ($line -match '^## ') { break }
            # Detect header row (contains Status/状态 before separator)
            if (-not $headerFound -and $line -match '^\|[^|]*\|[^|]*\|') {
                $cols = $line -split '\|'
                for ($j = 0; $j -lt $cols.Count; $j++) {
                    if ($cols[$j] -match '(状态|Status)') {
                        $statusColIdx = $j
                        $headerFound = $true
                        break
                    }
                }
                continue
            }
            # Skip separator row (|---|---|)
            if ($line -match '^\|[- ]+\|[- ]+\|') { continue }
            # Data row
            if ($headerFound -and $line -match '^\|') {
                $cols = $line -split '\|'
                if ($statusColIdx -ge 0 -and $statusColIdx -lt $cols.Count) {
                    $clean = ($cols[$statusColIdx].Trim()) -replace '[`*]',''
                    if ($clean -eq 'Ratified') { return $true }
                }
            }
        }
    }
    return $false
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

    # State classification (used by Check 3 and Check 8)
    $isRatifiedState = ($cleanState -match '^Ratified$') -or ($cleanState -match '正式批准')
    $isCandidateState = ($cleanState -match '^Candidate$') -or ($cleanState -match '候选版|候选|尚未正式批准')
    $isSupersededState = ($cleanState -match '^Superseded$')

    # Check 3: Ratified field — state-aware validation
    # Extract Ratified field value (supports **Ratified**: <value>, table row, and Chinese labels)
    $ratifiedValue = ''
    # Match bold label: **Ratified**: <value> — stop at newline or pipe
    if ($Content -match '(?:\*\*)?(?:Ratified|\u6B63\u5F0F\u6279\u51C6\u65E5\u671F)(?:\*\*)?\s*[：:]\s*([^|\n]+)') {
        $ratifiedValue = $Matches[1].Trim()
    }
    # Also match table row: | Ratified Date | <value> |
    if (-not $ratifiedValue -and $Content -match '\|\s*(?:Ratified Date|Ratified|正式批准日期)\s*\|\s*([^|\n]+)\s*\|') {
        $ratifiedValue = $Matches[1].Trim()
    }
    # Strip backticks/asterisks for clean comparison
    $cleanRatified = $ratifiedValue -replace '[`*]',''
    $isDate = $cleanRatified -match '^\d{4}-\d{2}-\d{2}$'
    $isPending = $cleanRatified -eq 'Pending'
    $isMissing = ($cleanRatified -eq '')

    # State-aware rules
    $ratifiedPass = $false
    $ratifiedDetail = ''

    if ($isCandidateState) {
        if ($isMissing) {
            # Candidate state with missing Ratified field
            $ratifiedPass = $false
            $ratifiedDetail = "Candidate state requires Ratified field (set to Pending or planned date); field is missing"
        } elseif ($isPending) {
            $ratifiedPass = $true
            $ratifiedDetail = "Ratified field: Pending"
        } else {
            # Candidate with non-Pending value (date or arbitrary string) — strict: only Pending allowed
            $ratifiedPass = $false
            $ratifiedDetail = "Candidate state requires Ratified field to be 'Pending'; found '$cleanRatified'"
        }
    } elseif ($isRatifiedState) {
        if ($isPending) {
            $ratifiedPass = $false
            $ratifiedDetail = "Ratified state must have a valid YYYY-MM-DD date; found 'Pending'"
        } elseif ($isMissing) {
            $ratifiedPass = $false
            $ratifiedDetail = "Ratified state requires a valid YYYY-MM-DD date; field is missing"
        } elseif ($isDate) {
            # Ratified state with valid date — must also have at least one Ratified row in Approval History
            $hasRatifiedRow = Test-ApprovalHistoryHasRatifiedRow -Content $Content
            if ($hasRatifiedRow) {
                $ratifiedPass = $true
                $ratifiedDetail = "Ratified date: $cleanRatified"
            } else {
                $ratifiedPass = $false
                $ratifiedDetail = "Ratified state requires at least one Ratified status row in Approval History; none found"
            }
        } else {
            $ratifiedPass = $false
            $ratifiedDetail = "Ratified state requires a valid YYYY-MM-DD date; found '$cleanRatified'"
        }
    } elseif ($isSupersededState) {
        # Superseded: Ratified field must be a valid YYYY-MM-DD date (the original ratification date)
        if ($isDate) {
            $ratifiedPass = $true
            $ratifiedDetail = "Ratified date: $cleanRatified (superseded)"
        } else {
            $ratifiedPass = $false
            $ratifiedDetail = "Superseded state requires Ratified field to be YYYY-MM-DD date; found '$cleanRatified'"
        }
    } else {
        # Unrecognized/invalid state — always fail-closed
        $ratifiedPass = $false
        $ratifiedDetail = "Unrecognized or invalid state '$cleanState'; ratified-pending validation cannot pass"
    }

    if ($ratifiedPass) {
        $checks += @{Check='ratified-pending'; Status='PASS'; Detail=$ratifiedDetail}
    } else {
        $checks += @{Check='ratified-pending'; Status='FAIL'; Detail=$ratifiedDetail}
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
                    # Strip backticks and asterisks for clean comparison
                    $cleanApprover = $approver -replace '[`*]',''
                    # Skip empty, dash-only, em-dash, and horizontal bar markers
                    if ($cleanApprover -and $cleanApprover -ne '-' -and $cleanApprover -ne '' -and
                        $cleanApprover -ne $([char]0x2014) -and $cleanApprover -ne $([char]0x2015) -and
                        $cleanApprover -notmatch '^[-—–]+$' -and
                        $cleanApprover -ne 'xinyan <zuijiuhjd@foxmail.com>') {
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

    # Check 8: Superseded successor validation (only when state is Superseded)
    if ($isSupersededState) {
        $supersededBy = ''
        # Try table row: | Superseded By | <version> |
        if ($Content -match '\|\s*Superseded By\s*\|\s*([^|\n]+)\s*\|') {
            $supersededBy = $Matches[1].Trim()
        }
        # Try footer: **Superseded By**: <version>
        if (-not $supersededBy -and $Content -match '\*\*Superseded By\*\*\s*[：:]\s*([^|\n]+)') {
            $supersededBy = $Matches[1].Trim()
        }
        $cleanSupersededBy = $supersededBy -replace '[`*]',''

        if (-not $cleanSupersededBy) {
            $checks += @{Check='superseded-successor'; Status='FAIL'; Detail='Superseded state requires Superseded By field with valid successor SemVer'}
        } elseif ($cleanSupersededBy -notmatch '^\d+\.\d+\.\d+$') {
            $checks += @{Check='superseded-successor'; Status='FAIL'; Detail="Invalid successor version: '$cleanSupersededBy' (expected X.Y.Z SemVer)"}
        } elseif ($cleanSupersededBy -eq $version) {
            $checks += @{Check='superseded-successor'; Status='FAIL'; Detail="Superseded successor version '$cleanSupersededBy' must differ from current version '$version'"}
        } else {
            $checks += @{Check='superseded-successor'; Status='PASS'; Detail="Superseded by: $cleanSupersededBy"}
        }
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
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-wrong-state.md'); Name = 'constitution-wrong-state.md'; ExpectedCheck = @('state-validity', 'ratified-pending') },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-illegal-approver.md'); Name = 'constitution-illegal-approver.md'; ExpectedCheck = 'approver-consistency' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-ratified-pending.md'); Name = 'constitution-ratified-pending.md'; ExpectedCheck = 'ratified-pending' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-ratified-no-history.md'); Name = 'constitution-ratified-no-history.md'; ExpectedCheck = 'ratified-pending' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-candidate-missing-ratified.md'); Name = 'constitution-candidate-missing-ratified.md'; ExpectedCheck = 'ratified-pending' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-candidate-with-date.md'); Name = 'constitution-candidate-with-date.md'; ExpectedCheck = 'ratified-pending' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-superseded-bad-ratified-value.md'); Name = 'constitution-superseded-bad-ratified-value.md'; ExpectedCheck = 'ratified-pending' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-superseded-no-successor.md'); Name = 'constitution-superseded-no-successor.md'; ExpectedCheck = 'superseded-successor' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-superseded-bad-semver.md'); Name = 'constitution-superseded-bad-semver.md'; ExpectedCheck = 'superseded-successor' },
        @{ Path = (Join-Path $fixtureRoot 'invalid/constitution-superseded-same-version.md'); Name = 'constitution-superseded-same-version.md'; ExpectedCheck = 'superseded-successor' }
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

        $expectedChecks = if ($t.ExpectedCheck -is [array]) { $t.ExpectedCheck } else { @($t.ExpectedCheck) }
        foreach ($expectedCheck in $expectedChecks) {
            $targetResult = @($results | Where-Object { $_.Check -eq $expectedCheck })[0]

            if ($targetResult -and $targetResult.Status -eq 'FAIL') {
                Write-Host "  [OK]   $($t.Name): $expectedCheck = FAIL (expected)" -ForegroundColor Green
            } elseif ($targetResult -and $targetResult.Status -ne 'FAIL') {
                Write-Host "  [FAIL] $($t.Name): $expectedCheck expected FAIL, got $($targetResult.Status)" -ForegroundColor Red
                $allPassed = $false
            } else {
                Write-Host "  [FAIL] $($t.Name): $expectedCheck check not found in results" -ForegroundColor Red
                $allPassed = $false
            }
        }
    }

    Write-Host ''
    Write-Host '--- Ratified Fixture ---' -ForegroundColor Cyan
    $ratifiedPath = Join-Path $fixtureRoot 'valid/constitution-ratified.md'
    if (Test-Path -LiteralPath $ratifiedPath) {
        $ratifiedContent = Get-Content -LiteralPath $ratifiedPath -Raw -Encoding utf8
        if (-not $ratifiedContent) { $ratifiedContent = '' }
        $ratifiedResults = Invoke-ConstitutionChecks -Content $ratifiedContent -FilePath $ratifiedPath
        $ratifiedFailures = @($ratifiedResults | Where-Object { $_.Status -eq 'FAIL' })
        if ($ratifiedFailures.Count -gt 0) {
            Write-Host "  [FAIL] valid/constitution-ratified.md: expected all PASS, got $($ratifiedFailures.Count) FAIL(s)" -ForegroundColor Red
            foreach ($f in $ratifiedFailures) { Write-Host "         $($f.Check): $($f.Detail)" -ForegroundColor Red }
            $allPassed = $false
        } else {
            Write-Host "  [OK]   valid/constitution-ratified.md: all $($ratifiedResults.Count) checks PASS" -ForegroundColor Green
        }
    } else {
        Write-Host "  [FAIL] valid/constitution-ratified.md: file not found" -ForegroundColor Red
        $allPassed = $false
    }

    Write-Host ''
    Write-Host '--- Candidate Pending Fixture ---' -ForegroundColor Cyan
    $candPendingPath = Join-Path $fixtureRoot 'valid/constitution-candidate-pending.md'
    if (Test-Path -LiteralPath $candPendingPath) {
        $cpContent = Get-Content -LiteralPath $candPendingPath -Raw -Encoding utf8
        if (-not $cpContent) { $cpContent = '' }
        $cpResults = Invoke-ConstitutionChecks -Content $cpContent -FilePath $candPendingPath
        $cpFailures = @($cpResults | Where-Object { $_.Status -eq 'FAIL' })
        if ($cpFailures.Count -gt 0) {
            Write-Host "  [FAIL] valid/constitution-candidate-pending.md: expected all PASS, got $($cpFailures.Count) FAIL(s)" -ForegroundColor Red
            foreach ($f in $cpFailures) { Write-Host "         $($f.Check): $($f.Detail)" -ForegroundColor Red }
            $allPassed = $false
        } else {
            Write-Host "  [OK]   valid/constitution-candidate-pending.md: all $($cpResults.Count) checks PASS" -ForegroundColor Green
        }
    } else {
        Write-Host "  [FAIL] valid/constitution-candidate-pending.md: file not found" -ForegroundColor Red
        $allPassed = $false
    }

    Write-Host ''
    Write-Host '--- Superseded Fixtures ---' -ForegroundColor Cyan
    $supOkPath = Join-Path $fixtureRoot 'valid/constitution-superseded-ok.md'
    if (Test-Path -LiteralPath $supOkPath) {
        $supOkContent = Get-Content -LiteralPath $supOkPath -Raw -Encoding utf8
        if (-not $supOkContent) { $supOkContent = '' }
        $supOkResults = Invoke-ConstitutionChecks -Content $supOkContent -FilePath $supOkPath

        $supOkFail = $false

        # MUST assert ALL results have no FAIL — any single FAIL means SelfTest fails
        $supOkFailures = @($supOkResults | Where-Object { $_.Status -eq 'FAIL' })
        if ($supOkFailures.Count -gt 0) {
            Write-Host "  [FAIL] valid/constitution-superseded-ok.md: expected all PASS, got $($supOkFailures.Count) FAIL(s)" -ForegroundColor Red
            foreach ($f in $supOkFailures) { Write-Host "         $($f.Check): $($f.Detail)" -ForegroundColor Red }
            $supOkFail = $true
        }

        # Additionally verify superseded-successor and ratified-pending specifically
        $supCheck = @($supOkResults | Where-Object { $_.Check -eq 'superseded-successor' })[0]
        $ratCheck = @($supOkResults | Where-Object { $_.Check -eq 'ratified-pending' })[0]

        if (-not $supCheck -or $supCheck.Status -ne 'PASS') {
            Write-Host "  [FAIL] valid/constitution-superseded-ok.md: superseded-successor expected PASS, got $($supCheck.Status)" -ForegroundColor Red
            if ($supCheck) { Write-Host "         $($supCheck.Check): $($supCheck.Detail)" -ForegroundColor Red }
            $supOkFail = $true
        }
        if (-not $ratCheck -or $ratCheck.Status -ne 'PASS') {
            Write-Host "  [FAIL] valid/constitution-superseded-ok.md: ratified-pending expected PASS, got $($ratCheck.Status)" -ForegroundColor Red
            if ($ratCheck) { Write-Host "         $($ratCheck.Check): $($ratCheck.Detail)" -ForegroundColor Red }
            $supOkFail = $true
        }
        if (-not $supOkFail) {
            Write-Host "  [OK]   valid/constitution-superseded-ok.md: all $($supOkResults.Count) checks PASS (superseded-successor PASS, ratified-pending PASS)" -ForegroundColor Green
        } else {
            $allPassed = $false
        }
    } else {
        Write-Host "  [FAIL] valid/constitution-superseded-ok.md: file not found" -ForegroundColor Red
        $allPassed = $false
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
