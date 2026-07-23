# scripts/docs/tests/validate-governance.tests.ps1
# XDev governance cross-document ratification chain test suite.
# Creates isolated temporary repos and validates cross-document
# consistency including final-SemVer ratification chain detection.
#
# Usage: .\scripts\docs\tests\validate-governance.tests.ps1
# Exit:   0 if all tests pass, 1 if any fail.

[CmdletBinding()]
param(
    [string]$Validator = '',
    [string]$TestRoot = '',
    [switch]$SkipSafetyProbes
)

$ErrorActionPreference = 'Stop'
$script:TestScriptPath = [System.IO.Path]::GetFullPath($MyInvocation.MyCommand.Path)
$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $Validator) { $Validator = Join-Path $scriptDirectory '..\validate-governance.ps1' }

# Resolve repo root from script path (3 levels up: tests/ -> docs/ -> scripts/ -> repo)
# No cwd dependency: $scriptDirectory is derived from $MyInvocation.MyCommand.Path
$script:RepoRoot = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $scriptDirectory))
# TestRoot must be a new, dedicated child directly under the governance temp parent.
$resolvedRepo = [System.IO.Path]::GetFullPath((Resolve-Path $script:RepoRoot).Path).TrimEnd('\')
$script:TestParent = [System.IO.Path]::GetFullPath((Join-Path $resolvedRepo 'build\test-temp\governance')).TrimEnd('\')
$script:ParentExisted = Test-Path -LiteralPath $script:TestParent -PathType Container
if (-not $TestRoot) { $TestRoot = Join-Path $script:TestParent ('run-' + [guid]::NewGuid().ToString('N')) }
$resolvedTest = [System.IO.Path]::GetFullPath($TestRoot).TrimEnd('\')
$parentOfTest = [System.IO.Path]::GetDirectoryName($resolvedTest).TrimEnd('\')
if ($parentOfTest -ne $script:TestParent -or $resolvedTest -notmatch '\\run-[0-9a-f]+$' -or
    $resolvedTest -eq $script:TestParent -or $resolvedTest -eq $resolvedRepo -or
    (Test-Path -LiteralPath $resolvedTest)) {
    throw "TestRoot rejected: must be a new dedicated child directly under '$script:TestParent': '$resolvedTest'"
}
$script:TestRoot = $resolvedTest

# Fresh safety probes stop after the real TestRoot admission check. This switch
# must not be able to bypass that check above.
if ($SkipSafetyProbes) { exit 0 }

function Assert-UnsafeTestRootRejected {
    param([string]$Candidate)
    $candidateFull = [System.IO.Path]::GetFullPath($Candidate).TrimEnd('\')
    $candidateParent = [System.IO.Path]::GetDirectoryName($candidateFull).TrimEnd('\')
    $rejected = ($candidateParent -ne $script:TestParent -or $candidateFull -notmatch '\\run-[0-9a-f]+$' -or
        $candidateFull -eq $script:TestParent -or $candidateFull -eq $resolvedRepo -or
        (Test-Path -LiteralPath $candidateFull))
    if (-not $rejected) { throw "TestRoot safety assertion failed to reject '$candidateFull'" }
}
function Invoke-BoundedProcess {
    param([string]$FileName, [string]$Arguments, [int]$TimeoutMs = 60000)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $FileName
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.Arguments = $Arguments
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $psi
    try {
        try { if (-not $process.Start()) { throw 'process start returned false' } } catch { throw "process start failed: $_" }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $process.WaitForExit([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds))) {
            try { $process.Kill() } catch { }
            $killWait = [Math]::Min(1000, [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds))
            if (-not $process.WaitForExit($killWait)) { throw 'process did not exit after timeout termination' }
            throw "process timed out after $TimeoutMs ms"
        }
        $probeExit = $process.ExitCode
        if (-not $stdoutTask.Wait([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)) -or $stdoutTask.Status -ne 'RanToCompletion') { throw 'stdout read failed or did not complete' }
        if (-not $stderrTask.Wait([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)) -or $stderrTask.Status -ne 'RanToCompletion') { throw 'stderr read failed or did not complete' }
        try { $stdout = $stdoutTask.Result; $stderr = $stderrTask.Result } catch { throw "process output read faulted: $_" }
    } finally {
        $process.Dispose()
        $psi = $null
    }
    return [PSCustomObject]@{ ExitCode = $probeExit; StdOut = $stdout; StdErr = $stderr }
}
function Invoke-FreshRootProbe {
    param([string]$Candidate)
    $quote = { param([string]$Value) '"' + $Value.Replace('"', '\"') + '"' }
    $args = '-NoProfile -ExecutionPolicy Bypass -File ' + (& $quote $script:TestScriptPath) +
        ' -Validator ' + (& $quote $Validator) + ' -TestRoot ' + (& $quote $Candidate) + ' -SkipSafetyProbes'
    Invoke-BoundedProcess -FileName ((Get-Command powershell.exe -ErrorAction Stop).Source) -Arguments $args -TimeoutMs 60000
}
function Assert-RootRejectedByFreshProcess {
    param([string]$Candidate)
    $probe = Invoke-FreshRootProbe $Candidate
    if ($probe.ExitCode -eq 0) { throw "Fresh process accepted unsafe TestRoot '$Candidate'" }
}
function Assert-RootAcceptedByFreshProcess {
    param([string]$Candidate)
    $probe = Invoke-FreshRootProbe $Candidate
    if ($probe.ExitCode -ne 0) { throw "Fresh process rejected valid TestRoot '$Candidate'" }
}
$unsafeRoots = @(
    $script:TestParent,
    (Join-Path $script:TestParent 'not-run'),
    (Join-Path $script:TestParent 'run-zzzz'),
    (Join-Path $resolvedRepo 'build\test-temp\governance-sibling'),
    (Join-Path $resolvedRepo 'scripts'),
    (Join-Path $script:TestParent 'run-existing')
)
foreach ($unsafeRoot in $unsafeRoots) {
    Assert-UnsafeTestRootRejected $unsafeRoot
    Assert-RootRejectedByFreshProcess $unsafeRoot
}

$freshValidProbeRoot = Join-Path $script:TestParent ('run-' + [guid]::NewGuid().ToString('N'))
if (-not (Test-Path -LiteralPath $script:TestParent -PathType Container)) {
    New-Item -ItemType Directory -Path $script:TestParent -Force | Out-Null
}
try {
    Assert-RootAcceptedByFreshProcess $freshValidProbeRoot
} finally {
    if (Test-Path -LiteralPath $freshValidProbeRoot) {
        Remove-Item -Recurse -Force -LiteralPath $freshValidProbeRoot -ErrorAction SilentlyContinue
    }
}

$validProbeRoot = Join-Path $script:TestParent ('run-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $validProbeRoot -Force | Out-Null
try {
    $existingProbe = Invoke-FreshRootProbe $validProbeRoot
    if ($existingProbe.ExitCode -eq 0) { throw "Fresh process accepted existing valid TestRoot '$validProbeRoot'" }
} finally {
    if (Test-Path -LiteralPath $validProbeRoot) {
        Remove-Item -Recurse -Force -LiteralPath $validProbeRoot -ErrorAction SilentlyContinue
    }
}

$script:Passed = 0
$script:Failed = 0
$script:Repos = @()

function Cleanup-Repos {
    foreach ($r in $script:Repos) {
        if (Test-Path -LiteralPath $r) {
            Remove-Item -Recurse -Force -LiteralPath $r -ErrorAction SilentlyContinue
        }
    }
    # Only the exact dedicated run root may be removed.
    $cleanTest = [System.IO.Path]::GetFullPath($script:TestRoot).TrimEnd('\')
    $cleanParent = [System.IO.Path]::GetDirectoryName($cleanTest).TrimEnd('\')
    if ($cleanParent -eq $script:TestParent -and $cleanTest -ne $script:TestParent -and
        $cleanTest -ne $resolvedRepo -and (Test-Path -LiteralPath $script:TestRoot)) {
        Remove-Item -Recurse -Force -LiteralPath $script:TestRoot -ErrorAction SilentlyContinue
    }
    if (-not $script:ParentExisted -and (Test-Path -LiteralPath $script:TestParent -PathType Container)) {
        $children = @(Get-ChildItem -LiteralPath $script:TestParent -Force -ErrorAction SilentlyContinue)
        if ($children.Count -eq 0) { Remove-Item -Force -LiteralPath $script:TestParent -ErrorAction SilentlyContinue }
    }
}

function New-TestRepo {
    $suffix = [guid]::NewGuid().ToString('N').Substring(0, 8)
    $repo = Join-Path $script:TestRoot "xdev-gov-cd-test-$suffix"
    New-Item -ItemType Directory -Path $repo -Force | Out-Null
    $script:Repos += $repo
    return $repo
}

function Write-Utf8NoBom {
    param([string]$Path, [string]$Content)
    $parent = Split-Path -Parent $Path
    if ($parent) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
    [System.IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding($false)))
}

function New-ConstitutionTable {
    param([string]$Version, [string]$State = 'Ratified', [string]$RatifiedDate = '2026-07-16')
    $lines = @(
        '# XDev Constitution',
        '',
        '## Document Status',
        '',
        '| Field | Value |',
        '|---|---|',
        '| Project | XDev |',
        "| Version | ``$Version`` |",
        "| State | $State |",
        '| Candidate Date | 2026-07-15 |',
        "| Ratified Date | $RatifiedDate |",
        '| Approver | ``xinyan <zuijiuhjd@foxmail.com>`` |',
        '',
        '## Non-Negotiable Principles',
        '',
        '### I. Security and Trust Boundary',
        '',
        'Test principle content.',
        '',
        '## Approval History',
        '',
        '| Stage | Status | Approver | Date | DocVersion | ApprovalScope |',
        '|---|---|---|---|---|---|',
        "| Ratification | Ratified | ``xinyan <zuijiuhjd@foxmail.com>`` | 2026-07-16 | ``$Version`` | ``test-rat-id`` | Test ratification |",
        '',
        "**Version**: ``$Version`` | **Candidate Date**: 2026-07-15 | **Ratified**: $RatifiedDate | **Last Amended**: 2026-07-16"
    )
    return ($lines -join "`r`n")
}

function New-ConstitutionBold {
    param([string]$Version, [string]$State = 'Ratified', [string]$RatifiedDate = '2026-07-16')
    $lines = @(
        '# XDev Constitution',
        '',
        "**Version**: ``$Version``",
        "**Status**: $State",
        "**Ratified**: $RatifiedDate",
        '',
        '## Non-Negotiable Principles',
        '',
        '### I. Security and Trust Boundary',
        '',
        'Test principle content.',
        '',
        '## Approval History',
        '',
        '| Stage | Status | Approver | Date | DocVersion | ApprovalScope |',
        '|---|---|---|---|---|---|',
        "| Ratification | Ratified | ``xinyan <zuijiuhjd@foxmail.com>`` | 2026-07-16 | ``$Version`` | ``test-rat-id`` | Test ratification |"
    )
    return ($lines -join "`r`n")
}

function New-SpecFile {
    param([string]$ConstitutionRefVersion, [string]$SpecVersion = '0.1.0')
    $lines = @(
        '# Test Spec',
        "> **DocType**: Feature Spec",
        "> **Status**: Draft",
        "> **Version**: $SpecVersion",
        "> **Constitution Ref**: .specify/memory/constitution.md ``$ConstitutionRefVersion``",
        '',
        '## Objective',
        '',
        'Test spec content.',
        '',
        '## Approval History',
        '',
        '| Change | Status | Approver | Date | Version | ApprovalID | Scope |',
        '|---|---|---|---|---|---|---|',
        '| Initial draft | Draft | --- | 2026-07-18 | 0.1.0 | --- | Initial creation |'
    )
    return ($lines -join "`r`n")
}

function New-ApprovalJson {
    param(
        [string]$ApprovalId,
        [string]$Decision,
        [string]$TargetVersion,
        [string]$TargetState,
        [string]$TargetPath = '.specify/memory/constitution.md',
        [string]$AssetType = 'constitution',
        [string]$ApprovedBy = 'xinyan <zuijiuhjd@foxmail.com>',
        [string[]]$Supersedes = @()
    )
    $sup = if ($Supersedes.Count -gt 0) {
        '["' + ($Supersedes -join '","') + '"]'
    } else {
        '[]'
    }
    $json = @"
{
  "schema_version": 1,
  "approval_id": "$ApprovalId",
  "asset_type": "$AssetType",
  "target_path": "$TargetPath",
  "decision": "$Decision",
  "target_version": "$TargetVersion",
  "target_state": "$TargetState",
  "scope": "Test approval for governance validation",
  "approved_by": "$ApprovedBy",
  "approved_at": "2026-07-16",
  "authorization_text": "Test approval authorization",
  "recorded_at": "2026-07-16",
  "recorded_by": "Test Suite",
  "supersedes": $sup
}
"@
    return $json
}

function Invoke-GovernanceValidator {
    param([string]$Repo)
    if (-not (Test-Path -LiteralPath $Validator -PathType Leaf)) {
        return [PSCustomObject]@{ ExitCode = 2; Output = "VALIDATOR_MISSING: $Validator" }
    }
    $quote = { param([string]$Value) '"' + $Value.Replace('"', '\"') + '"' }
    $args = '-NoProfile -ExecutionPolicy Bypass -File ' + (& $quote $Validator) +
        ' -RepoRoot ' + (& $quote $Repo) + ' -SkipExternal'
    $r = Invoke-BoundedProcess -FileName ((Get-Command powershell.exe -ErrorAction Stop).Source) -Arguments $args
    return [PSCustomObject]@{ ExitCode = $r.ExitCode; Output = (($r.StdOut + "`n" + $r.StdErr).Trim()) }
}

function Assert-ExitCode {
    param([string]$Case, $Result, [int]$Expected, [string]$TextMustContain = '', [string]$TextMustNotContain = '')
    $ok = $true
    $failure = $false
    if ($Result.ExitCode -ne $Expected) {
        Write-Host "  FAIL [$Case]: expected exit=$Expected, got exit=$($Result.ExitCode)" -ForegroundColor Red
        $failure = $true
        $ok = $false
    }
    if (-not $failure -and $TextMustContain -and $Result.Output -notmatch [regex]::Escape($TextMustContain)) {
        Write-Host "  FAIL [$Case]: output missing '$TextMustContain'" -ForegroundColor Red
        Write-Host "       Output was: $($Result.Output)" -ForegroundColor DarkGray
        $failure = $true
        $ok = $false
    }
    if (-not $failure -and $TextMustNotContain -and $Result.Output -match [regex]::Escape($TextMustNotContain)) {
        Write-Host "  FAIL [$Case]: output must NOT contain '$TextMustNotContain'" -ForegroundColor Red
        $failure = $true
        $ok = $false
    }
    if ($failure) { $script:Failed++ }
    if ($ok) {
        $label = if ($Expected -eq 0) { 'PASS' } else { 'FAIL' }
        Write-Host "  $label [$Case]" -ForegroundColor $(if ($Expected -eq 0) { 'Green' } else { 'Yellow' })
        $script:Passed++
    }
    return $ok
}

# ===================================================================
# Suite start: safely clean and create the dedicated test root
# ===================================================================
New-Item -ItemType Directory -Path $script:TestRoot -Force | Out-Null

# ===================================================================
$script:FinalExitCode = 0
try {
Write-Host "=== XDev Governance Cross-Document Test Suite ===" -ForegroundColor Cyan
Write-Host "Validator: $Validator"
Write-Host ''

# -------------------------------------------------------------------
# TEST 1: final==final PASS (table format)
# Constitution: 1.0.0 (table), Spec refs: 1.0.0, No approvals
# Expected: PASS (exact match, no chain needed)
# -------------------------------------------------------------------
Write-Host "--- TEST 1: final==final PASS (table format) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'final-eq-final-table' $r 0 'All cross-doc checks passed' ''

# -------------------------------------------------------------------
# TEST 2: final==final PASS (bold format)
# Constitution: 1.0.0 (bold), Spec refs: 1.0.0, No approvals
# Expected: PASS (exact match)
# -------------------------------------------------------------------
Write-Host "`n--- TEST 2: final==final PASS (bold format) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionBold -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'final-eq-final-bold' $r 0 'All cross-doc checks passed' ''

# -------------------------------------------------------------------
# TEST 3: final vs rc WITHOUT ratification record FAIL
# Constitution: 1.0.0 (table), Spec refs: 1.0.0-rc.3, No approvals
# Expected: FAIL (rc ref without ratification chain)
# -------------------------------------------------------------------
Write-Host "`n--- TEST 3: final vs rc, no ratification FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'final-vs-rc-no-rat' $r 1 'Ratification chain invalid' ''

# -------------------------------------------------------------------
# TEST 4: Full explicit chain PASS (valid ratification)
# Constitution: 1.0.0 (table), Spec refs: 1.0.0-rc.3
# Approvals: rc3 record + ratification with proper supersedes
# Expected: PASS (valid ratification chain)
# -------------------------------------------------------------------
Write-Host "`n--- TEST 4: Full explicit ratification chain PASS ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'full-chain-pass' $r 0 'All cross-doc checks passed' ''

# -------------------------------------------------------------------
# TEST 5: Ratification supersedes wrong ID FAIL
# rc3 correct, ratification supersedes non-existent ID
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 5: Ratification supersedes wrong ID FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-WRONG-ID-999')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rat-supersedes-wrong' $r 1 'not found' ''

# -------------------------------------------------------------------
# TEST 6: Ratification wrong target_version FAIL
# rc3 correct, ratification target_version 2.0.0
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 6: Ratification wrong target_version FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '2.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rat-wrong-target-version' $r 1 'no ratification record found for constitution' ''

# -------------------------------------------------------------------
# TEST 7: Ratification wrong target_path FAIL
# rc3 correct, ratification target_path mismatch
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 7: Ratification wrong target_path FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -TargetPath '.specify/memory/wrong-constitution.md' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rat-wrong-target-path' $r 1 'no ratification record found for constitution' ''

# -------------------------------------------------------------------
# TEST 8: Ratification illegal approved_by FAIL
# rc3 correct, ratification illegal approver
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 8: Ratification illegal approved_by FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -ApprovedBy 'evil <evil@hacker.com>' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rat-illegal-approver' $r 1 'approved_by must be' ''

# -------------------------------------------------------------------
# TEST 9: RC record missing FAIL
# Only ratification, no rc3 record (supersedes references non-existent)
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 9: RC record missing FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rc-record-missing' $r 1 'not found' ''

# -------------------------------------------------------------------
# TEST 10: RC record wrong target_version FAIL
# rc3 record target_version 1.0.0-rc.4, ratification correct
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 10: RC record wrong target_version FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.4' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rc-wrong-target-version' $r 1 'target_version' ''

# -------------------------------------------------------------------
# TEST 11: RC record wrong target_path FAIL
# rc3 record wrong target_path, ratification correct
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 11: RC record wrong target_path FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending' `
    -TargetPath '.specify/memory/wrong-constitution.md'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rc-wrong-target-path' $r 1 'target_path' ''

# -------------------------------------------------------------------
# TEST 12: RC record illegal approved_by FAIL
# rc3 record illegal approver, ratification correct
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 12: RC record illegal approved_by FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending' `
    -ApprovedBy 'evil <evil@hacker.com>'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rc-illegal-approver' $r 1 'approved_by must be' ''

# -------------------------------------------------------------------
# TEST 13: Version base different FAIL
# Constitution: 1.0.0, Spec refs: 2.0.0-rc.1 (MAJOR mismatch)
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 13: Version base different FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '2.0.0-rc.1'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'version-base-different' $r 1 'different base version' ''

# -------------------------------------------------------------------
# TEST 14: Spec constitution ref malformed version (not semver) FAIL
# Constitution: 1.0.0, Spec refs: "not-a-version" (unparseable as semver)
# Expected: FAIL with 'malformed'
# -------------------------------------------------------------------
Write-Host "`n--- TEST 14: Spec ref malformed FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$specMalformed = New-SpecFile -ConstitutionRefVersion 'not-a-version'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $specMalformed
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'spec-ref-malformed' $r 1 'malformed' ''

# -------------------------------------------------------------------
# TEST 15: Constitution version unresolvable FAIL
# Constitution: missing version, Spec refs: 1.0.0-rc.3
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 15: Constitution version unresolvable FAIL ---" -ForegroundColor Cyan
$repo = New-TestRepo
$badConst = "# XDev Constitution`n`n## Document Status`n`nNo version field here.`n`n## Principles`n`nTest principle.`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $badConst
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'const-version-unresolvable' $r 1 'unparseable' ''

# -------------------------------------------------------------------
# TEST 17: Constitution body text version without metadata -> FAIL (R2)
# Constitution has version in body text but no table/bold metadata header
# Expected: FAIL (unparseable, no metadata format detected)
# -------------------------------------------------------------------
Write-Host "`n--- TEST 17: Constitution body-only version FAIL (R2) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$bodyOnlyConst = @"
# XDev Constitution

## Document Status

This document is at version 1.0.0 and does not use metadata format.

## Principles

Test principle.
"@
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $bodyOnlyConst
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'const-body-only-fallback' $r 1 'unparseable' ''

# -------------------------------------------------------------------
# TEST 18: Spec constitution ref line MISSING -> FAIL (R1)
# Spec has no Constitution ref line at all
# Expected: FAIL with 'missing'
# -------------------------------------------------------------------
Write-Host "`n--- TEST 18: Spec ref missing FAIL (R1) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
# Spec without any Constitution reference
$specNoRef = @"
# Test Spec Without Ref
> **DocType**: Feature Spec
> **Status**: Draft
> **Version**: 0.1.0

## Objective
Test spec with no Constitution reference line.
"@
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $specNoRef
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'spec-ref-missing' $r 1 'missing' ''

# -------------------------------------------------------------------
# TEST 19: Spec constitution ref EMPTY version -> FAIL (R1)
# Spec has Constitution ref but version is empty
# Expected: FAIL with 'missing' or 'malformed'
# -------------------------------------------------------------------
Write-Host "`n--- TEST 19: Spec ref empty version FAIL (R1) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$specEmptyRef = @"
# Test Spec Empty Ref
> **DocType**: Feature Spec
> **Status**: Draft
> **Version**: 0.1.0
> **Constitution Ref**: .specify/memory/constitution.md

## Objective
Test spec with empty Constitution ref version.
"@
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $specEmptyRef
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'spec-ref-empty' $r 1 'missing' ''

# -------------------------------------------------------------------
# TEST 20: Spec constitution ref TBD -> FAIL (R1)
# Spec has Constitution ref version = TBD
# Expected: FAIL with 'malformed' or 'missing'
# -------------------------------------------------------------------
Write-Host "`n--- TEST 20: Spec ref TBD FAIL (R1) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$specTbdRef = @"
# Test Spec TBD Ref
> **DocType**: Feature Spec
> **Status**: Draft
> **Version**: 0.1.0
> **Constitution Ref**: .specify/memory/constitution.md TBD

## Objective
Test spec with TBD Constitution ref version.
"@
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $specTbdRef
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'spec-ref-tbd' $r 1 'malformed' ''

# -------------------------------------------------------------------
# TEST 21: Spec constitution ref v1.0 prefix malformed -> FAIL (R1)
# Spec has v1.0 (not valid semver, starts with 'v')
# Expected: FAIL with 'malformed'
# -------------------------------------------------------------------
Write-Host "`n--- TEST 21: Spec ref v-prefix malformed FAIL (R1) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$specVPrefixRef = @"
# Test Spec v-prefix Ref
> **DocType**: Feature Spec
> **Status**: Draft
> **Version**: 0.1.0
> **Constitution Ref**: .specify/memory/constitution.md v1.0

## Objective
Test spec with v-prefix (malformed semver).
"@
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $specVPrefixRef
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'spec-ref-v-prefix' $r 1 'malformed' ''

# -------------------------------------------------------------------
# TEST 22: Multi-predecessor chain PASS (R3)
# Two rc records superseded, one matches SpecRefVer
# Expected: PASS (at least one supersede matches)
# -------------------------------------------------------------------
Write-Host "`n--- TEST 22: Multi-predecessor chain PASS (R3) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
# First rc record (rc.2) - different version, won't match SpecRefVer
$rc2Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc2-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.2' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc2-001.json" $rc2Json
# Second rc record (rc.3) - matches SpecRefVer
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
# Ratification supersedes both
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc2-001','2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'multi-predecessor-pass' $r 0 'All cross-doc checks passed' ''

# -------------------------------------------------------------------
# TEST 23: No matching SpecRefVer in supersedes FAIL (R3)
# Two rc records, neither matches SpecRefVer 1.0.0-rc.3
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 23: No matching SpecRefVer in supersedes FAIL (R3) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc1Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc1-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.1' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc1-001.json" $rc1Json
$rc2Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc2-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.2' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc2-001.json" $rc2Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc1-001','2026-07-16-constitution-rc2-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'no-matching-supersede' $r 1 'Ratification chain invalid' ''

# -------------------------------------------------------------------
# TEST 24: RC record wrong decision FAIL (R4)
# RC record has decision=approve-ratification instead of approve-candidate
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 24: RC record wrong decision FAIL (R4) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
# RC record with wrong decision type
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'rc-wrong-decision' $r 1 'decision' ''

# -------------------------------------------------------------------
# TEST 25: Malformed JSON approvals -> FAIL with filename (R5)
# Creates broken JSON in approvals dir
# Expected: FAIL with filename
# -------------------------------------------------------------------
Write-Host "`n--- TEST 25: Malformed JSON FAIL with filename (R5) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
# Valid rc record
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
# Malformed JSON - truncated/missing braces
$badJson = '{ "approval_id": "2026-07-16-broken-001", "asset_type": "constitution", "target_path":'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-broken-001.json" $badJson
# Valid ratification record that supersedes
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'malformed-json' $r 1 'broken' ''

# -------------------------------------------------------------------
# TEST 26: Duplicate approval_id FAIL (R6)
# Two JSON files with same approval_id
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 26: Duplicate approval_id FAIL (R6) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
# First rc record
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
# Duplicate ID in another file
$dupJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001-copy.json" $dupJson
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'duplicate-approval-id' $r 1 'duplicate' ''

# -------------------------------------------------------------------
# TEST 27: Trailing whitespace in fields -> PASS (R7)
# Fields have trailing spaces, should trim and accept
# Expected: PASS
# -------------------------------------------------------------------
Write-Host "`n--- TEST 27: Trailing whitespace trim acceptance (R7) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
# RC record with trailing spaces in fields
$rc3JsonTrailing = @"
{
  "schema_version": 1,
  "approval_id": "2026-07-16-constitution-rc3-001",
  "asset_type": "constitution  ",
  "target_path": ".specify/memory/constitution.md  ",
  "decision": "approve-candidate  ",
  "target_version": "1.0.0-rc.3  ",
  "target_state": "Pending  ",
  "scope": "Test  ",
  "approved_by": "xinyan <zuijiuhjd@foxmail.com>  ",
  "approved_at": "2026-07-16  ",
  "authorization_text": "Test  ",
  "recorded_at": "2026-07-16  ",
  "recorded_by": "Test  ",
  "supersedes": []
}
"@
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3JsonTrailing
$ratJson = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'trailing-whitespace-pass' $r 0 'All cross-doc checks passed' ''

# -------------------------------------------------------------------
# TEST 28: Multiple ratification records FAIL (R9)
# Two ratification records for same constitution version
# Expected: FAIL
# -------------------------------------------------------------------
Write-Host "`n--- TEST 28: Multiple ratification records FAIL (R9) ---" -ForegroundColor Cyan
$repo = New-TestRepo
$const = New-ConstitutionTable -Version '1.0.0' -State 'Ratified'
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $const
New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
$spec = New-SpecFile -ConstitutionRefVersion '1.0.0-rc.3'
Write-Utf8NoBom "$repo/specs/001-test/spec.md" $spec
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$rc3Json = New-ApprovalJson -ApprovalId '2026-07-16-constitution-rc3-001' `
    -Decision 'approve-candidate' -TargetVersion '1.0.0-rc.3' -TargetState 'Pending'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $rc3Json
# First ratification
$ratJson1 = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-001' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-001.json" $ratJson1
# Second ratification for same version
$ratJson2 = New-ApprovalJson -ApprovalId '2026-07-16-constitution-ratification-002' `
    -Decision 'approve-ratification' -TargetVersion '1.0.0' -TargetState 'Ratified' `
    -Supersedes @('2026-07-16-constitution-rc3-001')
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-ratification-002.json" $ratJson2
$r = Invoke-GovernanceValidator $repo
Assert-ExitCode 'multiple-ratification-records' $r 1 'multiple ratification records' ''

# -------------------------------------------------------------------
# TEST 16: SelfTest compatibility
# -------------------------------------------------------------------
Write-Host "`n--- TEST 16: SelfTest compatibility ---" -ForegroundColor Cyan
$selfArgs = '-NoProfile -ExecutionPolicy Bypass -File "' + $Validator.Replace('"', '\"') + '" -SelfTest'
$selfResult = Invoke-BoundedProcess -FileName ((Get-Command powershell.exe -ErrorAction Stop).Source) -Arguments $selfArgs
$exitCode = $selfResult.ExitCode
$outputText = (($selfResult.StdOut + "`n" + $selfResult.StdErr).Trim())
if ($exitCode -ne 0) {
    Write-Host "  FAIL [self-test-compat]: SelfTest exit=$exitCode" -ForegroundColor Red
    Write-Host "       Output: $outputText" -ForegroundColor Red
    $script:Failed++
} else {
    Write-Host "  PASS [self-test-compat]" -ForegroundColor Green
    $script:Passed++
}

# ===================================================================
# Summary
# ===================================================================
Write-Host "`n=== Governance Test Suite Summary ===" -ForegroundColor Cyan
Write-Host "Passed: $script:Passed" -ForegroundColor Green
Write-Host "Failed: $script:Failed" -ForegroundColor $(if ($script:Failed -gt 0) { 'Red' } else { 'Green' })
Write-Host ''
if ($script:Failed -gt 0) {
    Write-Host "TESTS FAILED: $($script:Failed) assertion(s) failed." -ForegroundColor Red
    $script:FinalExitCode = 1
} else {
    Write-Host "All $script:Passed assertions PASS (GREEN)." -ForegroundColor Green
    $script:FinalExitCode = 0
}
} catch {
    Write-Host "FATAL EXCEPTION: $_" -ForegroundColor Red
    if ($script:FinalExitCode -eq 0) { $script:FinalExitCode = 2 }
} finally {
    Cleanup-Repos
}

# ===================================================================
# Cleanup assertions (after finally, before exit)
# ===================================================================
# Assert all repo children are deleted
$leakedRepos = @($script:Repos | Where-Object { Test-Path -LiteralPath $_ })
if ($leakedRepos.Count -gt 0) {
    Write-Host "  [CLEANUP-ASSERT FAIL] $($leakedRepos.Count) repo(s) not cleaned:" -ForegroundColor Red
    foreach ($lr in $leakedRepos) { Write-Host "    LEAKED: $lr" -ForegroundColor Red }
    if ($script:FinalExitCode -eq 0) { $script:FinalExitCode = 3 }
}
# Assert TestRoot was removed
if (Test-Path -LiteralPath $script:TestRoot) {
    Write-Host "  [CLEANUP-ASSERT FAIL] TestRoot still exists: $($script:TestRoot)" -ForegroundColor Red
    if ($script:FinalExitCode -eq 0) { $script:FinalExitCode = 3 }
} else {
    Write-Host "  [CLEANUP-OK] TestRoot cleanly removed: $($script:TestRoot)" -ForegroundColor DarkGray
}
if (-not $script:ParentExisted -and (Test-Path -LiteralPath $script:TestParent)) {
    Write-Host "  [CLEANUP-ASSERT FAIL] TestParent still exists: $($script:TestParent)" -ForegroundColor Red
    if ($script:FinalExitCode -eq 0) { $script:FinalExitCode = 3 }
}

if (($script:Passed + $script:Failed) -ne 28) {
    Write-Host "  [COUNT-ASSERT FAIL] expected 28 cases, got $($script:Passed + $script:Failed)" -ForegroundColor Red
    if ($script:FinalExitCode -eq 0) { $script:FinalExitCode = 4 }
}

# Exit with the saved final exit code (handles both success and exception paths)
exit $script:FinalExitCode
