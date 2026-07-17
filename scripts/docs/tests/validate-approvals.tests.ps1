# scripts/docs/tests/validate-approvals.tests.ps1
# XDev approval gate test suite. Creates isolated temporary Git repositories
# and validates every protected-asset scenario from the approved design.
#
# Usage: .\scripts\docs\tests\validate-approvals.tests.ps1
# Exit:   0 if all tests pass, 1 if any fail.

[CmdletBinding()]
param(
    [string]$Validator = (Join-Path $PSScriptRoot '..\validate-approvals.ps1'),
    [string]$GitExe = (Get-Command git -ErrorAction Stop).Source
)

$ErrorActionPreference = 'Stop'
$script:Passed = 0
$script:Failed = 0
$script:Repos = @()

function Cleanup-Repos {
    foreach ($r in $script:Repos) {
        if (Test-Path -LiteralPath $r) {
            Remove-Item -Recurse -Force -LiteralPath $r -ErrorAction SilentlyContinue
        }
    }
}
trap { Cleanup-Repos; break }

function Invoke-Git {
    param([string]$Repo, [string[]]$Arguments)
    $out = @(& $script:GitExe -C $Repo @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "git $($Arguments -join ' ') failed (exit=$LASTEXITCODE)" }
    return $out
}

function New-TestRepo {
    $suffix = [guid]::NewGuid().ToString('N').Substring(0, 8)
    $repo = Join-Path $env:TEMP "xdev-approval-gate-tests-$suffix"
    New-Item -ItemType Directory -Path $repo -Force | Out-Null
    $script:Repos += $repo
    $null = Invoke-Git $repo @('init', '--initial-branch=main')
    $null = Invoke-Git $repo @('config', 'user.name', 'Approval Gate Test')
    $null = Invoke-Git $repo @('config', 'user.email', 'approval-gate@example.invalid')
    return $repo
}

function Write-Utf8NoBom {
    param([string]$Path, [string]$Content)
    $parent = Split-Path -Parent $Path
    if ($parent) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
    [System.IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding($false)))
}

function Invoke-Validator {
    param([string]$Repo)
    if (-not (Test-Path -LiteralPath $Validator -PathType Leaf)) {
        return [PSCustomObject]@{ ExitCode = 2; Output = "VALIDATOR_MISSING: $Validator" }
    }
    $rf = Join-Path $env:TEMP "xdev-val-result-$([guid]::NewGuid()).txt"
    try {
        $null = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Validator -Repo $Repo -Staged -ResultFile $rf 2>&1
        if (Test-Path $rf) {
            $line = Get-Content $rf -Raw
            if ($line -match 'BLOCK\t(.+)$') {
                return [PSCustomObject]@{ ExitCode = 1; Output = $Matches[1] }
            } elseif ($line -match 'PASS\t(.+)$') {
                return [PSCustomObject]@{ ExitCode = 0; Output = $Matches[1] }
            }
        }
        return [PSCustomObject]@{ ExitCode = 1; Output = "VALIDATOR_NO_RESULT" }
    } finally {
        Remove-Item $rf -ErrorAction SilentlyContinue
    }
}

function Assert-ExitCode {
    param([string]$Case, $Result, [int]$Expected, [string]$TextMustContain = '')
    if ($Result.ExitCode -ne $Expected) {
        Write-Host "  FAIL [$Case]: expected exit=$Expected, got exit=$($Result.ExitCode)"
        $script:Failed++
        return $false
    }
    if ($TextMustContain -and $Result.Output -notmatch [regex]::Escape($TextMustContain)) {
        Write-Host "  FAIL [$Case]: output missing '$TextMustContain'"
        $script:Failed++
        return $false
    }
    Write-Host "  PASS [$Case]"
    $script:Passed++
    return $true
}

# -------------------------------------------------------------------
Write-Host "=== Approval Gate Test Suite ==="

# --- CASE: No protected changes ---
Write-Host "`n--- No-change baseline ---"
$repo = New-TestRepo
$baseline = "# Dummy file`nThis is not a governance asset.`nVersion: 1.0`n"
Write-Utf8NoBom "$repo/dummy.md" $baseline
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
Write-Utf8NoBom "$repo/dummy.md" ($baseline.Replace('1.0', '1.1'))
Invoke-Git $repo @('add', '-u')
$r = Invoke-Validator $repo
Assert-ExitCode 'no-protected-changes' $r 0 ''

# --- CASE: Constitution version change, no approval ---
Write-Host "`n--- Constitution version blocked (no approval) ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" ($con.Replace('1.0.0-rc.2', '1.0.0-rc.3'))
Invoke-Git $repo @('add', '-u')
$r = Invoke-Validator $repo
Assert-ExitCode 'constitution-version-blocked' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: Approval JSON exists, but doc has no matching ID ---
Write-Host "`n--- JSON without doc Approval History match ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" ($con.Replace('1.0.0-rc.2', '1.0.0-rc.3'))
Invoke-Git $repo @('add', '-u')
$aj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.3","target_state":"Pending","scope":"Approve rc.3","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve rc.3","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $aj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'json-no-doc-match' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: Mismatched approval ID ---
Write-Host "`n--- Mismatched approval ID ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
$con2 = "# XDev Constitution`n**Version**: ``1.0.0-rc.3```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.3 approved | Approved Candidate | ``2026-07-16-constitution-rc3-999`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con2
$aj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.3","target_state":"Pending","scope":"Match test","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve rc.3","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $aj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'mismatched-id' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: target_path mismatch ---
Write-Host "`n--- target_path mismatch ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
$con2 = "# XDev Constitution`n**Version**: ``1.0.0-rc.3```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.3 approved | Approved Candidate | ``2026-07-16-constitution-rc3-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con2
$aj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/wrong-path.md","decision":"approve-candidate","target_version":"1.0.0-rc.3","target_state":"Pending","scope":"Mismatch","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve rc.3","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $aj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'target-path-mismatch' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: version mismatch ---
Write-Host "`n--- target_version mismatch ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
$con2 = "# XDev Constitution`n**Version**: ``1.0.0-rc.3```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.3 approved | Approved Candidate | ``2026-07-16-constitution-rc3-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con2
$aj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.4","target_state":"Pending","scope":"Mismatch","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve rc.3","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $aj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'target-version-mismatch' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: empty authorization_text ---
Write-Host "`n--- Empty authorization_text ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
$con2 = "# XDev Constitution`n**Version**: ``1.0.0-rc.3```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.3 approved | Approved Candidate | ``2026-07-16-constitution-rc3-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con2
$aj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.3","target_state":"Pending","scope":"Empty auth","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $aj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'empty-authorization' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: Baseline approval reuse ---
Write-Host "`n--- Baseline approval reuse ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.2 approved | Approved Candidate | ``2026-07-15-rc2-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
$oldAj = '{"schema_version":1,"approval_id":"2026-07-15-rc2-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.2","target_state":"Pending","scope":"Original","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-15","authorization_text":"Approve rc.2","recorded_at":"2026-07-15","recorded_by":"Human","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-15-rc2-001.json" $oldAj
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" ($con.Replace('1.0.0-rc.2', '1.0.0-rc.3'))
Invoke-Git $repo @('add', '-u')
$r = Invoke-Validator $repo
Assert-ExitCode 'baseline-reuse-blocked' $r 1 'APPROVAL_GATE_BLOCKED' ''

# --- CASE: Matching Constitution approval (GREEN) ---
Write-Host "`n--- Matching Constitution approval (GREEN) ---"
$repo = New-TestRepo
New-Item -ItemType Directory -Path "$repo/.specify/memory" -Force | Out-Null
New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
$con = "# XDev Constitution`n**Version**: ``1.0.0-rc.2```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.2 approved | Approved Candidate | ``2026-07-15-rc2-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con
$oldAj = '{"schema_version":1,"approval_id":"2026-07-15-rc2-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.2","target_state":"Pending","scope":"Original","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-15","authorization_text":"Approve rc.2","recorded_at":"2026-07-15","recorded_by":"Human","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-15-rc2-001.json" $oldAj
Invoke-Git $repo @('add', '.')
Invoke-Git $repo @('commit', '-m', 'baseline')
$con2 = "# XDev Constitution`n**Version**: ``1.0.0-rc.3```n**Status**: Pending`n**Ratified**: Pending`n## Approval History`n| rc.2 approved | Approved Candidate | ``2026-07-15-rc2-001`` |`n| rc.3 approved | Approved Candidate | ``2026-07-16-constitution-rc3-001`` |`n"
Write-Utf8NoBom "$repo/.specify/memory/constitution.md" $con2
$newAj = '{"schema_version":1,"approval_id":"2026-07-16-constitution-rc3-001","asset_type":"constitution","target_path":".specify/memory/constitution.md","decision":"approve-candidate","target_version":"1.0.0-rc.3","target_state":"Pending","scope":"Approve rc.3 candidate","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve rc.3","recorded_at":"2026-07-16","recorded_by":"Orchestrator","supersedes":[]}'
Write-Utf8NoBom "$repo/.specify/approvals/2026-07-16-constitution-rc3-001.json" $newAj
Invoke-Git $repo @('add', '.')
$r = Invoke-Validator $repo
Assert-ExitCode 'constitution-green' $r 0 'APPROVAL_GATE_PASSED' ''

# --- Per-asset RED/GREEN tests ---
$assetCases = @(
    @{ Name='adr';        Path='docs/adr/0001-test.md';            ToState='Accepted';  AssetType='adr' },
    @{ Name='exception'; Path='docs/security/exceptions/test.md';  ToState='Active';     AssetType='security-exception' },
    @{ Name='spec';      Path='specs/001-test/spec.md';             ToState='Approved';   AssetType='spec' },
    @{ Name='plan';      Path='specs/001-test/plan.md';             ToState='Ready';      AssetType='plan' },
    @{ Name='acceptance';Path='specs/001-test/acceptance.md';       ToState='Accepted';   AssetType='acceptance' },
    @{ Name='archive';   Path='specs/archive/2026/001-test/archive-metadata.md'; ToState='Archived'; AssetType='archive' }
)

$stateTransitions = @{
    'adr'        = @{ From='Proposed'; To='Accepted' }
    'exception'  = @{ From='Draft';    To='Active' }
    'spec'       = @{ From='Draft';    To='Approved' }
    'plan'       = @{ From='Planned';  To='Ready' }
    'acceptance' = @{ From='Verified'; To='Accepted' }
    'archive'    = @{ From='Accepted'; To='Archived' }
}

foreach ($case in $assetCases) {
    $st = $stateTransitions[$case.Name]
    $fromLine = "**Status**: $($st.From)"
    $toLine = "**Status**: $($st.To)"
    $appBase = "# $($case.Name) Asset`n**Version**: ``1.0.0```n$fromLine`n## Approval History`n"
    $appChanged = "# $($case.Name) Asset`n**Version**: ``1.0.0```n$toLine`n## Approval History`n"

    Write-Host "`n--- $($case.Name): RED (no approval) ---"
    $repo = New-TestRepo
    Write-Utf8NoBom "$repo/$($case.Path)" $appBase
    Invoke-Git $repo @('add', '.')
    Invoke-Git $repo @('commit', '-m', "$($case.Name) baseline")
    Write-Utf8NoBom "$repo/$($case.Path)" $appChanged
    Invoke-Git $repo @('add', '-u')
    $r = Invoke-Validator $repo
    Assert-ExitCode "$($case.Name)-red" $r 1 'APPROVAL_GATE_BLOCKED' ''

    Write-Host "`n--- $($case.Name): GREEN (matching approval) ---"
    $repo = New-TestRepo
    Write-Utf8NoBom "$repo/$($case.Path)" $appBase
    Invoke-Git $repo @('add', '.')
    Invoke-Git $repo @('commit', '-m', "$($case.Name) baseline")
    $approvalId = "2026-07-16-$($case.Name)-state-001"
    $appChangedWithApproval = "$appChanged| $($case.Name) transition | Approved | ``xinyan <zuijiuhjd@foxmail.com>`` | 2026-07-16 | ``1.0.0`` | ``$approvalId`` | Approve transition to $($st.To) |" + "`n"
    Write-Utf8NoBom "$repo/$($case.Path)" $appChangedWithApproval
    Invoke-Git $repo @('add', '-u')
    New-Item -ItemType Directory -Path "$repo/.specify/approvals" -Force | Out-Null
    $aj = '{"schema_version":1,"approval_id":"' + $approvalId + '","asset_type":"' + $case.AssetType + '","target_path":"' + $case.Path + '","decision":"approve-state-transition","target_version":"1.0.0","target_state":"' + $st.To + '","scope":"Approve ' + $case.Name + ' state","approved_by":"xinyan <zuijiuhjd@foxmail.com>","approved_at":"2026-07-16","authorization_text":"Approve ' + $case.Name + '","recorded_at":"2026-07-16","recorded_by":"Test","supersedes":[]}'
    Write-Utf8NoBom "$repo/.specify/approvals/$approvalId.json" $aj
    Invoke-Git $repo @('add', '.')
    $r = Invoke-Validator $repo
    Assert-ExitCode "$($case.Name)-green" $r 0 'APPROVAL_GATE_PASSED' ''
}

# --- Summary ---
Write-Host "`n=== Test Suite Summary ==="
Write-Host "Passed: $script:Passed"
Write-Host "Failed: $script:Failed"
if ($script:Failed -gt 0) {
    Write-Host "`nRED: $script:Failed test(s) failed."
    Cleanup-Repos
    exit 1
} else {
    Write-Host "`nGREEN: All $script:Passed tests pass."
    Cleanup-Repos
    exit 0
}