# scripts/docs/tests/validate-approvals.tests.ps1
# XDev approval gate test suite. Creates isolated temporary Git repositories
# and validates every protected-asset scenario from the approved design.
#
# Usage: .\scripts\docs\tests\validate-approvals.tests.ps1
# Exit:   0 if all tests pass, 1 if any fail.

[CmdletBinding()]
param(
    [string]$Validator = '',
    [string]$GitExe = (Get-Command git -ErrorAction Stop).Source
)

$ErrorActionPreference = 'Stop'
$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $Validator) { $Validator = Join-Path $scriptDirectory '..\validate-approvals.ps1' }

# ---- Test root: repo-relative, never C: ----
$script:RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDirectory '..\..\..'))
$script:ApprovalsParent = Join-Path $script:RepoRoot 'build\test-temp\approvals'
$script:ApprovalsParentExisted = Test-Path -LiteralPath $script:ApprovalsParent -PathType Container
$script:TestRoot = Join-Path $script:ApprovalsParent ('run-' + [guid]::NewGuid().ToString('N'))

# Path escape protection: resolved test root must be a new direct run child.
$normalizedTestRoot = [System.IO.Path]::GetFullPath($script:TestRoot).TrimEnd('\')
$normalizedApprovalsParent = [System.IO.Path]::GetFullPath($script:ApprovalsParent).TrimEnd('\')
$normalizedRepoRoot = [System.IO.Path]::GetFullPath($script:RepoRoot).TrimEnd('\')
if ($normalizedApprovalsParent -eq $normalizedRepoRoot -or
    -not $normalizedApprovalsParent.StartsWith($normalizedRepoRoot + '\', [System.StringComparison]::OrdinalIgnoreCase) -or
    [System.IO.Path]::GetDirectoryName($normalizedTestRoot).TrimEnd('\') -ne $normalizedApprovalsParent -or
    $normalizedTestRoot -notmatch '\\run-[0-9a-f]+$' -or
    (Test-Path -LiteralPath $normalizedTestRoot)) {
    throw "SECURITY: Test root '$normalizedTestRoot' escapes repo root '$normalizedRepoRoot' — ABORT"
}
if ($normalizedTestRoot -match '^[A-Z]:\\Windows\\') {
    throw "SECURITY: Test root '$normalizedTestRoot' is under system directory — ABORT"
}

$script:Passed = 0
$script:Failed = 0

# ---- Suite bootstrap: clean/create test root ----
$script:ApprovalsParentCreated = -not (Test-Path -LiteralPath $normalizedApprovalsParent -PathType Container)
if ($script:ApprovalsParentCreated) { New-Item -ItemType Directory -Path $normalizedApprovalsParent -Force | Out-Null }
New-Item -ItemType Directory -Path $normalizedTestRoot -Force | Out-Null

# ---- Safe cleanup (runs on exit/exception) ----
function Cleanup-TestRoot {
    $cleanRoot = [System.IO.Path]::GetFullPath($script:TestRoot).TrimEnd('\')
    if ([System.IO.Path]::GetDirectoryName($cleanRoot).TrimEnd('\') -eq $normalizedApprovalsParent -and
        $cleanRoot -match '\\run-[0-9a-f]+$' -and $cleanRoot -ne $normalizedApprovalsParent -and
        (Test-Path -LiteralPath $cleanRoot)) {
        Remove-Item -Recurse -Force -LiteralPath $cleanRoot -ErrorAction Stop
    }
    if ($script:ApprovalsParentCreated -and (Test-Path -LiteralPath $normalizedApprovalsParent -PathType Container)) {
        $children = @(Get-ChildItem -LiteralPath $normalizedApprovalsParent -Force -ErrorAction SilentlyContinue)
        if ($children.Count -eq 0) { Remove-Item -Force -LiteralPath $normalizedApprovalsParent -ErrorAction SilentlyContinue }
    }
}

# ---- Invoke-Git: safe native capture (same pattern as validator Git helper) ----
function Invoke-Git {
    param([string]$Repo, [string[]]$Arguments, [int]$TimeoutMs = 60000)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $GitExe
    $argList = @('-C', $Repo) + $Arguments
    $sb = New-Object System.Text.StringBuilder
    foreach ($arg in $argList) {
        if ($sb.Length -gt 0) { [void]$sb.Append(' ') }
        if ($arg -match '[\s"]') {
            [void]$sb.Append('"').Append($arg.Replace('"', '\"')).Append('"')
        } else {
            [void]$sb.Append($arg)
        }
    }
    $psi.Arguments = $sb.ToString()
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    $psi.StandardOutputEncoding = [System.Text.UTF8Encoding]::new($false)
    $psi.StandardErrorEncoding = [System.Text.UTF8Encoding]::new($false)

    $proc = $null
    try {
        $proc = $null
        $proc = [System.Diagnostics.Process]::Start($psi)
        $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
        $stderrTask = $proc.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $proc.WaitForExit($TimeoutMs)) {
            try { $proc.Kill() } catch { }
            $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
            if (-not $proc.WaitForExit($remaining)) { throw "git $($Arguments -join ' ') did not exit after timeout termination" }
            throw "git $($Arguments -join ' ') timed out after $TimeoutMs ms"
        }
        $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stdoutTask.Wait($remaining)) { throw "git stdout read did not complete within timeout budget" }
        $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stderrTask.Wait($remaining)) { throw "git stderr read did not complete within timeout budget" }
        if ($stdoutTask.Status -ne 'RanToCompletion') { throw 'git stdout read failed' }
        if ($stderrTask.Status -ne 'RanToCompletion') { throw 'git stderr read failed' }
        $stdout = $stdoutTask.Result
        $stderr = $stderrTask.Result
        $exitCode = $proc.ExitCode
    } finally {
        if ($proc) { $proc.Dispose() }
    }

    if ($exitCode -ne 0) {
        $errMsg = if ($stderr.Trim()) { $stderr.Trim() } else { '(no stderr)' }
        throw "git $($Arguments -join ' ') failed (exit=$exitCode, stderr: $errMsg)"
    }
    if ($stderr.Trim()) {
        Write-Warning "Invoke-Git: git $($Arguments -join ' ') stderr (exit 0): $($stderr.Trim())"
    }
    if ([string]::IsNullOrEmpty($stdout)) { return @() }
    return @($stdout -split "`r?`n")
}

function New-TestRepo {
    $suffix = [guid]::NewGuid().ToString('N').Substring(0, 8)
    $repo = Join-Path $script:TestRoot "repo-$suffix"
    New-Item -ItemType Directory -Path $repo -Force | Out-Null
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
    param([string]$Repo, [string]$GitWrapperPath = '', [string[]]$GitPrefixArgs = @(), [int]$GitTimeoutMs = 60000)
    if (-not (Test-Path -LiteralPath $Validator -PathType Leaf)) {
        return [PSCustomObject]@{ ExitCode = 2; Output = 'VALIDATOR_MISSING: ' + $Validator }
    }
    $rf = Join-Path $script:TestRoot ('val-result-' + [guid]::NewGuid().ToString() + '.txt')
    try {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = 'powershell.exe'
        $argStr = "-NoProfile -ExecutionPolicy Bypass -File `"$Validator`" -Repo `"$Repo`" -Staged -ResultFile `"$rf`""
        if ($GitWrapperPath) {
            $argStr += " -GitExe `"$GitWrapperPath`""
        }
        $argStr += " -GitTimeoutMs $GitTimeoutMs"
        if ($GitPrefixArgs.Count -gt 0) {
            $argText = $GitPrefixArgs -join '|'
            $env:XDEV_APPROVAL_GIT_PREFIX_ARGS = $argText
        }
        $psi.Arguments = $argStr
        $psi.UseShellExecute = $false
        if ($GitPrefixArgs.Count -gt 0) {
            $psi.EnvironmentVariables['XDEV_APPROVAL_GIT_PREFIX_ARGS'] = ($GitPrefixArgs -join '|')
        }
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.CreateNoWindow = $true
        $psi.StandardOutputEncoding = [System.Text.UTF8Encoding]::new($false)
        $psi.StandardErrorEncoding = [System.Text.UTF8Encoding]::new($false)

        $proc = [System.Diagnostics.Process]::Start($psi)
        $vstdoutTask = $proc.StandardOutput.ReadToEndAsync()
        $vstderrTask = $proc.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $proc.WaitForExit(60000)) {
            try { $proc.Kill() } catch { }
            $remaining = [Math]::Max(1, 60000 - [int]$clock.ElapsedMilliseconds)
            if (-not $proc.WaitForExit($remaining)) { throw 'validator did not exit after timeout termination' }
            throw "validator timeout after 60 seconds"
        }
        $remaining = [Math]::Max(1, 60000 - [int]$clock.ElapsedMilliseconds)
        if (-not $vstdoutTask.Wait($remaining)) { throw 'validator stdout read did not complete within timeout budget' }
        $remaining = [Math]::Max(1, 60000 - [int]$clock.ElapsedMilliseconds)
        if (-not $vstderrTask.Wait($remaining)) { throw 'validator stderr read did not complete within timeout budget' }
        if ($vstdoutTask.Status -ne 'RanToCompletion') { throw 'validator stdout read failed' }
        if ($vstderrTask.Status -ne 'RanToCompletion') { throw 'validator stderr read failed' }
        $vstdout = $vstdoutTask.Result
        $vstderr = $vstderrTask.Result
        $validatorExitCode = $proc.ExitCode
        if ($validatorExitCode -ne 0) {
            throw "validator exited unexpectedly with code $validatorExitCode"
        }

        if ($vstderr.Trim()) {
            Write-Warning "Validator stderr: $($vstderr.Trim())"
        }

        if (Test-Path $rf) {
            $line = Get-Content $rf -Raw
            if ($line -match 'BLOCK\t(.+)$') {
                return [PSCustomObject]@{ ExitCode = 1; Output = $Matches[1] }
            } elseif ($line -match 'PASS\t(.+)$') {
                return [PSCustomObject]@{ ExitCode = 0; Output = $Matches[1] }
            }
        }
        return [PSCustomObject]@{ ExitCode = 1; Output = 'VALIDATOR_NO_RESULT ' + $vstderr.Trim() }
    } finally {
        if ($proc) { $proc.Dispose() }
        Remove-Item Env:XDEV_APPROVAL_GIT_PREFIX_ARGS -ErrorAction SilentlyContinue
        Remove-Item $rf -ErrorAction SilentlyContinue
    }
}

function Assert-ExitCode {
    param([string]$Case, $Result, [int]$Expected, [string]$TextMustContain = '')
    if ($Result.ExitCode -ne $Expected) {
        Write-Host "  FAIL [$Case]: expected exit=$Expected, got exit=$($Result.ExitCode)"
        Write-Host "         Output: $($Result.Output)"
        $script:Failed++
        return $false
    }
    if ($TextMustContain -and $Result.Output -notmatch [regex]::Escape($TextMustContain)) {
        Write-Host "  FAIL [$Case]: output missing '$TextMustContain'"
        Write-Host "         Output: $($Result.Output)"
        $script:Failed++
        return $false
    }
    Write-Host "  PASS [$Case]"
    $script:Passed++
    return $true
}

# ===================================================================
# MAIN TEST EXECUTION (wrapped for guaranteed cleanup)
# ===================================================================
try {
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
    Assert-ExitCode 'constitution-version-blocked' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'json-no-doc-match' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'mismatched-id' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'target-path-mismatch' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'target-version-mismatch' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'empty-authorization' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'baseline-reuse-blocked' $r 1 'APPROVAL_GATE_BLOCKED'

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
    Assert-ExitCode 'constitution-green' $r 0 'APPROVAL_GATE_PASSED'

    # --- CASE: English table Version change, no approval ---
    Write-Host "`n--- English table Version blocked (no approval) ---"
    $repo = New-TestRepo
    New-Item -ItemType Directory -Path "$repo/specs/001-test" -Force | Out-Null
    $tableSpec = "# Table Spec`n| Field | Value |`n|---|---|`n| Version | ``1.0.0`` |`n| Status | Draft |`n## Approval History`n"
    Write-Utf8NoBom "$repo/specs/001-test/spec.md" $tableSpec
    Invoke-Git $repo @('add', '.')
    Invoke-Git $repo @('commit', '-m', 'baseline')
    Write-Utf8NoBom "$repo/specs/001-test/spec.md" ($tableSpec.Replace('1.0.0', '1.0.1'))
    Invoke-Git $repo @('add', '-u')
    $r = Invoke-Validator $repo
    Assert-ExitCode 'english-table-version-blocked' $r 1 'APPROVAL_GATE_BLOCKED'

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
        Assert-ExitCode "$($case.Name)-red" $r 1 'APPROVAL_GATE_BLOCKED'

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
        Assert-ExitCode "$($case.Name)-green" $r 0 'APPROVAL_GATE_PASSED'
    }

    # ===================================================================
    # NEW REGRESSION TESTS: Git stderr warning handling (Task6 Lane A)
    # ===================================================================

    # Use the installed PowerShell executable as a disposable runner. Prefix
    # arguments are passed as one delimiter-encoded value, avoiding cmd parsing.
    $gitWrapperExe = (Get-Command powershell.exe -ErrorAction Stop).Source
    $gitWrapperWarning = Join-Path $script:TestRoot 'git-warning.ps1'
    $gitWrapperStress = Join-Path $script:TestRoot 'git-stress.ps1'
    $gitWrapperFail = Join-Path $script:TestRoot 'git-fail.ps1'
    $gitWrapperSleep = Join-Path $script:TestRoot 'git-sleep.ps1'

    # Warning wrapper: writes to stderr, delegates to real git, forwards exit code
    @"
param()
Write-Error 'warning: fake test warning for xdev-approval-gate'
& '$GitExe' @args
exit `$LASTEXITCODE
"@ | Set-Content -LiteralPath $gitWrapperWarning -Encoding UTF8
    @"
param()
1..1000 | ForEach-Object { [Console]::Out.WriteLine('stress-stdout-' + ('x' * 100)); [Console]::Error.WriteLine('stress-stderr-' + ('x' * 100)) }
& '$GitExe' @args
exit `$LASTEXITCODE
"@ | Set-Content -LiteralPath $gitWrapperStress -Encoding UTF8

    # Non-zero wrapper: writes to stderr, exits 128 immediately (no real git)
    @"
param()
[Console]::Error.WriteLine('fatal: fake test failure for xdev-approval-gate')
exit 128
"@ | Set-Content -LiteralPath $gitWrapperFail -Encoding UTF8
    @"
param()
Start-Sleep -Seconds 30
"@ | Set-Content -LiteralPath $gitWrapperSleep -Encoding UTF8

    # --- NEW CASE: Git stderr warning on success does NOT block validation ---
    Write-Host "`n=== NEW: Git success with stderr warning ==="
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
    # Use warning wrapper: should PASS despite stderr warning
    $r = Invoke-Validator $repo -GitWrapperPath $gitWrapperExe -GitPrefixArgs @('-File',$gitWrapperWarning)
    Assert-ExitCode 'git-warning-success' $r 0 'APPROVAL_GATE_PASSED'

    # Both redirected streams exceed the Windows pipe buffer. Completion within
    # the helper timeout proves the validator drains stdout and stderr concurrently.
    Write-Host "`n=== NEW: Git large dual-stream output completes ==="
    $stressRepo = New-TestRepo
    Write-Utf8NoBom "$stressRepo/dummy.md" "# Stress`n"
    Invoke-Git $stressRepo @('add', '.')
    Invoke-Git $stressRepo @('commit', '-m', 'stress baseline')
    $largeResult = Invoke-Validator $stressRepo -GitWrapperPath $gitWrapperExe -GitPrefixArgs @('-File',$gitWrapperStress)
    $clock = [System.Diagnostics.Stopwatch]::StartNew()
    $timeoutResult = Invoke-Validator $stressRepo -GitWrapperPath $gitWrapperExe -GitPrefixArgs @('-File',$gitWrapperSleep) -GitTimeoutMs 250
    $clock.Stop()
    $timeoutOk = $timeoutResult.ExitCode -eq 1 -and $timeoutResult.Output -match 'timed out after 250 ms' -and $clock.ElapsedMilliseconds -lt 10000
    $combined = [PSCustomObject]@{
        ExitCode = if ($largeResult.ExitCode -eq 0 -and $timeoutOk) { 0 } else { 1 }
        Output = "large=$($largeResult.Output); timeout=$($timeoutResult.Output); elapsed_ms=$($clock.ElapsedMilliseconds)"
    }
    Assert-ExitCode 'git-large-dual-stream-and-timeout' $combined 0 'APPROVAL_GATE_PASSED'

    # --- NEW CASE: Git non-zero exit still fails validation ---
    Write-Host "`n=== NEW: Git non-zero exit failure ==="
    $repo = New-TestRepo
    Write-Utf8NoBom "$repo/dummy.md" "# Dummy`n"
    Invoke-Git $repo @('add', '.')
    Invoke-Git $repo @('commit', '-m', 'baseline')
    Write-Utf8NoBom "$repo/dummy.md" "# Dummy modified`n"
    Invoke-Git $repo @('add', '-u')
    # Use fail wrapper: should BLOCK because git exits 128
    $r = Invoke-Validator $repo -GitWrapperPath $gitWrapperExe -GitPrefixArgs @('-File',$gitWrapperFail)
    Assert-ExitCode 'git-nonzero-fail' $r 1 'APPROVAL_GATE_BLOCKED'

    # --- Summary ---
    Write-Host "`n=== Test Suite Summary ==="
    $total = $script:Passed + $script:Failed
    Write-Host "Passed: $script:Passed"
    Write-Host "Failed: $script:Failed"
    Write-Host "Total:  $total"
    if ($total -ne 25) {
        Write-Host "RED: fixed regression count expected 25, got $total."
        exit 1
    }
    if ($script:Failed -gt 0) {
        Write-Host "`nRED: $script:Failed test(s) failed (of $total total)."
        exit 1
    } else {
        Write-Host "`nGREEN: All $total tests pass."
        exit 0
    }
} finally {
    Cleanup-TestRoot
}
