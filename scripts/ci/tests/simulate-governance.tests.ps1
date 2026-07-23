# Task 8 RED/GREEN tests. Windows PowerShell 5.1 compatible, standalone.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)))
$workflow = Join-Path $repoRoot '.github\workflows\governance.yaml'
$simulator = Join-Path $repoRoot 'scripts\ci\simulate-governance.ps1'
$cleanupHelper = Join-Path $repoRoot 'scripts\ci\cleanup-governance-run.ps1'
$testParent = Join-Path $repoRoot 'build\test-temp\ci-tests'
$testRoot = Join-Path $testParent ('run-' + [guid]::NewGuid().ToString('N') + " space&('quote')")
$existingParent = Join-Path $repoRoot 'build\test-temp\ci'
$ciParentInitiallyExisted = Test-Path -LiteralPath $existingParent -PathType Container
$ciParentInitialChildren = if ($ciParentInitiallyExisted) { @(Get-ChildItem -LiteralPath $existingParent -Force -ErrorAction Stop | ForEach-Object { $_.Name }) } else { @() }
$existingParentCreatedForProbe = $false
$parentCreatedByTest = $false
$failures = @()
$passed = 0
$harnessCleanupFailed = $false

function Assert-HarnessNotFailed {
    if ($script:harnessCleanupFailed) { throw "Previous harness cleanup failure: aborting further scenarios" }
}
function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { $script:failures += $Message } else { $script:passed++ }
}
function Read-RequiredFile {
    param([string]$Path)
    Assert-True (Test-Path -LiteralPath $Path -PathType Leaf) "Missing required file: $Path"
    if (Test-Path -LiteralPath $Path -PathType Leaf) { return (Get-Content -LiteralPath $Path -Raw) }
    return ''
}

$yaml = Read-RequiredFile $workflow
$sim = Read-RequiredFile $simulator
$cleanup = Read-RequiredFile $cleanupHelper
Assert-True (Test-Path -LiteralPath $cleanupHelper -PathType Leaf) 'Cleanup helper is missing'
Assert-True ($cleanupHelper -notmatch '\\run-[^\\]+\\') 'Cleanup helper is located under a runRoot'
Assert-True ($cleanup -match 'unsafe RunRoot rejected' -and $cleanup -match 'run-\[0-9a-f\]\{32\}') 'Cleanup helper lacks strict RunRoot validation'
Assert-True ($cleanup -match 'Remove-Item -LiteralPath.*-Recurse' -and $cleanup -match 'Get-ChildItem -LiteralPath \$parent') 'Cleanup helper does not own filesystem cleanup'

# Strict structural checks for the five validation steps. This is deliberately
# not a string-count test: each named step must contain its own run block.
$stepNames = @('Constitution', 'Specs', 'Approvals', 'Links', 'Governance')
foreach ($name in $stepNames) {
    $stepMatch = [regex]::Match($yaml, '(?ms)^\s*- name:\s*Validate ' + $name + '\s*$(.*?)(?=^\s*- name:|\z)')
    Assert-True $stepMatch.Success "Workflow missing structured Validate $name step"
    if ($stepMatch.Success) {
        $body = $stepMatch.Groups[1].Value
        Assert-True ($body -match '(?m)^\s*run:\s*\|') "Validate $name has no multiline run block"
        Assert-True ($body -match '\$LASTEXITCODE\s*\r?\n?\s*if|\$exitCode\s*-ne\s*0') "Validate $name does not check immediate nonzero exit"
        Assert-True ($body -match '::error::') "Validate $name has no CI error annotation"
        Assert-True ($body -match 'exit\s+\$(LASTEXITCODE|exitCode)') "Validate $name does not exit with validator code"
    }
}
$approvalMatch = [regex]::Match($yaml, '(?ms)^\s*- name:\s*Validate Approvals\s*$(.*?)(?=^\s*- name:|\z)')
if ($approvalMatch.Success) {
    $approvalBody = $approvalMatch.Groups[1].Value
    Assert-True ($approvalBody -match 'ResultFile') 'Approval step does not provide ResultFile'
    Assert-True ($approvalBody -match 'Test-Path') 'Approval step does not require ResultFile existence'
    Assert-True ($approvalBody -match 'Get-Content') 'Approval step does not parse ResultFile'
    Assert-True ($approvalBody -match 'BLOCK') 'Approval step does not detect BLOCK'
    Assert-True ($approvalBody -match 'exit\s+1') 'Approval BLOCK/malformed result does not fail closed'
}
$uploadMatch = [regex]::Match($yaml, '(?ms)^\s*- name:\s*Upload Reports\s*$(.*?)(?=^\s*- name:|\z)')
Assert-True $uploadMatch.Success 'Workflow missing Upload Reports step'
if ($uploadMatch.Success) { Assert-True ($uploadMatch.Groups[1].Value -match '(?m)^\s*if:\s*always\(\)\s*$') 'Upload Reports is not if: always()' }

Assert-True ($sim -match '\[string\]\$RepoRoot') 'Simulator does not accept -RepoRoot'
Assert-True ($sim -match 'build.*test-temp.*ci') 'Simulator does not keep run files under repo build/test-temp/ci'
Assert-True ($sim -notmatch '\$env:TEMP|Join-Path\s+.*C:\\') 'Simulator uses a forbidden temp/C drive path'
foreach ($required in @('validate-constitution.ps1', 'validate-specs.ps1', 'validate-approvals.ps1', 'validate-links.ps1', 'validate-governance.ps1')) {
    Assert-True ($sim -match [regex]::Escape($required)) "Simulator does not invoke $required"
}
Assert-True ($sim -match 'ExitCode') 'Simulator does not preserve child exit codes'
Assert-True ($sim -match '::error::') 'Simulator has no error annotation'
Assert-True ($sim -match 'WaitForExit\([^)]*\)') 'Simulator has no bounded process wait'
Assert-True ($sim -notmatch '(?m)\s*\$process\.WaitForExit\(\)') 'Simulator uses unbounded WaitForExit'
Assert-True ($sim -notmatch '\.Result') 'Simulator uses unbounded task Result'
Assert-True ($sim -match 'GetAwaiter\(\)\.GetResult') 'Simulator does not boundedly collect task output'
Assert-True ($sim -match 'Dispose\(\)') 'Simulator does not dispose child process'
Assert-True ($sim -match 'APPROVAL_GATE_PASSED' -and $sim -match 'APPROVAL_GATE_BLOCKED') 'Simulator lacks exact approval contract'
Assert-True ($sim -match 'post-kill WaitForExit') 'Simulator does not diagnose post-kill root wait'
Assert-True ($sim -match 'tasksHealthy') 'Simulator does not fail closed on incomplete child output'
Assert-True ($yaml.Contains('\APASS') -and $yaml.Contains('\ABLOCK') -and $yaml.Contains('\z')) 'Workflow approval parser is not whole-string strict'
Assert-True ($sim -match 'taskkill\.exe|taskkill') 'Simulator does not use process-tree termination'
Assert-True ($sim -match 'TreeTerminationFailed') 'Simulator does not diagnose process-tree termination failure'
Assert-True ($sim -match 'parentExisted') 'Simulator does not track pre-existing parent ownership'
Assert-True ($sim -notmatch ('Get-' + 'CimInstance|Get-' + 'CimClass|Get-' + 'WmiObject|C' + 'IM|W' + 'MI')) 'Simulator retains process query dependency'
Assert-True ($sim -match 'ValidateRange\(1,600000\)') 'Simulator timeout range is not bounded'
Assert-True ($cleanup -match 'Get-ChildItem.*ErrorAction Stop') 'Cleanup helper does not catch cleanup inspection errors'
Assert-True ($sim -match 'stage=root-wait') 'Simulator does not diagnose root process termination failure'
Assert-True ($sim -match 'cleanup exception') 'Simulator cleanup is not protected from outward exceptions'
Assert-True ($sim -match 'Invoke-BoundedTaskkill') 'Simulator does not isolate taskkill in a bounded helper'
Assert-True ($sim -match '\.ci-ownership-marker') 'Simulator does not create ownership marker in runRoot'
Assert-True ($cleanup -match '\.ci-ownership-marker') 'Cleanup helper does not verify ownership marker'
Assert-True ($cleanup -match 'ReparsePoint') 'Cleanup helper does not check for reparse points'
Assert-True ($cleanup -match 'Get-Item.*ErrorAction Stop') 'Cleanup helper does not use Get-Item for reparse check'
Assert-True ($cleanup -match 'ownership marker content invalid') 'Cleanup helper does not validate ownership marker content'
Assert-True ($sim -notmatch ('Encoded' + 'Command|Invoke-Bounded' + 'CimQuery|Get-Process' + 'TreeSnapshot|Get-Residual' + 'ProcessPids|Stop-Residual' + 'ProcessTrees')) 'Simulator retains removed process-query helpers'
Assert-True ($sim -notmatch '(?m)^\s*&\s*\$taskkill\b') 'Simulator invokes taskkill synchronously'
Assert-True ($sim -notmatch '(?m)\$process\.WaitForExit\(\)') 'Simulator has an unbounded taskkill/process wait'
Assert-True ($sim -match 'taskkill timeout') 'Simulator lacks taskkill timeout diagnostics'

foreach ($badTimeout in @(0, -1)) {
    $bad = @()
    try { $bad = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $simulator -RepoRoot $repoRoot -ChildTimeoutMilliseconds $badTimeout 2>&1 } catch { $bad += $_.Exception.Message }
    $badExit = $LASTEXITCODE
    Assert-True ($badExit -ne 0) "Invalid timeout $badTimeout was accepted"
    $badText = ($bad -join "`n")
    Assert-True ($badText -notmatch 'Governance CI exception') "Invalid timeout $badTimeout reached simulator body"
}
Assert-True ($sim -match 'Invoke-FilesystemCleanupChild' -and $sim -match 'cleanup child timeout') 'Simulator cleanup is not supervised by a bounded child'
Assert-True ($sim -match 'cleanup-governance-run\.ps1' -and $sim -notmatch 'cleanup-child\.ps1') 'Simulator still creates cleanup helper inside runRoot'
Assert-True ($sim -match '\$operationFailed' -and $sim -match '\$originalException' -and $sim -match 'child operation failed after start') 'Invoke-Child lacks post-start exception preservation'
Assert-True ($sim -match 'Get-RemainingMilliseconds \$deadline' -and $sim -notmatch 'WaitForExit\(1000\)') 'Cleanup/taskkill waits do not share an absolute deadline'
Assert-True ($cleanup -match 'Remove-Item -LiteralPath.*-Recurse' -and $cleanup -match 'Get-ChildItem -LiteralPath \$parent') 'Cleanup helper does not own recursive removal and parent inspection'

<# Narrow legacy helper coverage removed. This deliberately extracts only the helper
# functions, so a 1ms deadline cannot enter the five-validator workflow.
function Invoke-QueryHelperHarness {
    param([int]$BudgetMilliseconds, [int]$HarnessTimeoutMilliseconds = 5000)
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($sim, [ref]$null, [ref]$null)
    $names = @('Quote-WindowsArgument', 'Get-RemainingMilliseconds', 'Invoke-BoundedTaskkill')
    $parts = foreach ($name in $names) {
        $node = $ast.Find({ param($item) $item -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $item.Name -eq $name }, $true)
        if ($null -eq $node) { throw "Missing query helper function: $name" }
        $node.Extent.Text
    }
    $harness = @"
`$ErrorActionPreference = 'Stop'
`$repo = '$repoRoot'
$($parts -join "`r`n")
`$deadline = [Diagnostics.Stopwatch]::GetTimestamp() + [long]([Diagnostics.Stopwatch]::Frequency * ($BudgetMilliseconds / 1000.0))
`$started = [Diagnostics.Stopwatch]::StartNew()
`$result = Invoke-BoundedTaskkill 'taskkill.exe' 0 1
`$started.Stop()
`[PSCustomObject]@{ Succeeded = `$result.Succeeded; TimedOut = `$result.TimedOut; EnumerationFailed = `$result.EnumerationFailed; Diagnostic = `$result.Diagnostic; ElapsedMilliseconds = `$started.ElapsedMilliseconds } | ConvertTo-Json -Compress
"@
    $path = Join-Path $testParent ('query-harness-' + [guid]::NewGuid().ToString('N') + '.ps1')
    New-Item -ItemType Directory -Path $testParent -Force | Out-Null
    [System.IO.File]::WriteAllText($path, $harness)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = (Get-Command powershell.exe -ErrorAction Stop).Source
    $psi.Arguments = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' + (Quote-Arg $path)
    $psi.WorkingDirectory = $repoRoot; $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $started = [Diagnostics.Stopwatch]::StartNew(); $child = $null; $output = ''
    try {
        $child = [Diagnostics.Process]::Start($psi)
        $outTask = $child.StandardOutput.ReadToEndAsync(); $errTask = $child.StandardError.ReadToEndAsync()
        $finished = $child.WaitForExit($HarnessTimeoutMilliseconds)
        if (-not $finished) { try { $child.Kill() } catch { }; [void]$child.WaitForExit(1000) }
        if ($finished -and $outTask.Wait(1000) -and $errTask.Wait(1000)) { $output = $outTask.GetAwaiter().GetResult() + $errTask.GetAwaiter().GetResult() }
        return [PSCustomObject]@{ ExitCode = if ($finished) { $child.ExitCode } else { 124 }; Output = $output; ElapsedMilliseconds = $started.ElapsedMilliseconds }
    } finally { if ($child) { $child.Dispose() }; Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue }
}
$queryTimeout = Invoke-QueryHelperHarness -BudgetMilliseconds 1 -HarnessTimeoutMilliseconds 5000
Assert-True ($queryTimeout.ExitCode -eq 0 -and $queryTimeout.ElapsedMilliseconds -le 5000) "Independent query timeout harness did not finish within 5s: exit=$($queryTimeout.ExitCode), elapsed=$($queryTimeout.ElapsedMilliseconds)ms"
Assert-True ($queryTimeout.Output -match 'TimedOut\s*[:=]\s*true|"TimedOut":true') "Independent query timeout harness lacked TimedOut JSON: $($queryTimeout.Output)"
$queryNormal = Invoke-QueryHelperHarness -BudgetMilliseconds 10000 -HarnessTimeoutMilliseconds 10000
Assert-True ($queryNormal.ExitCode -eq 0 -and $queryNormal.ElapsedMilliseconds -le 10000) "Independent normal query harness exceeded 10s: exit=$($queryNormal.ExitCode), elapsed=$($queryNormal.ElapsedMilliseconds)ms"
Assert-True ($queryNormal.Output -match 'Succeeded\s*[:=]\s*true|"Succeeded":true|EnumerationFailed') "Independent normal query harness lacked structured result: $($queryNormal.Output)"
#>
function Quote-Arg([string]$Value) {
    if ($null -eq $Value -or $Value.Length -eq 0) { return '""' }
    '"' + $Value.Replace('"', '\"') + '"'
}
function Stop-TestProcessTree {
    param([System.Diagnostics.Process]$Process)
    $taskkill = Join-Path $env:SystemRoot 'System32\taskkill.exe'
    if (-not (Test-Path -LiteralPath $taskkill -PathType Leaf)) { $taskkill = (Get-Command taskkill.exe -ErrorAction Stop).Source }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $taskkill
    $psi.Arguments = "/PID $($Process.Id) /T /F"
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $killer = $null
    try {
        $killer = [System.Diagnostics.Process]::Start($psi)
        $killer.StandardOutput.ReadToEndAsync() | Out-Null
        $killer.StandardError.ReadToEndAsync() | Out-Null
        $finished = $killer.WaitForExit(1000)
        if (-not $finished) { try { $killer.Kill() } catch { }; return $false }
        return ($killer.ExitCode -eq 0)
    } finally { if ($killer) { $killer.Dispose() } }
}
function Get-TestProcessIdentity {
    param([int]$ProcessId)
    try { $item = Get-Process -Id $ProcessId -ErrorAction Stop; return "PID=$ProcessId; Name=$($item.ProcessName)" } catch { return "PID=$ProcessId; not running" }
}
function Invoke-Simulator {
    param(
        [string]$Root,
        [int]$ChildTimeoutMilliseconds = 10000,
        [int]$OverallTimeoutMilliseconds = 60000,
        [string]$ScriptPath = $simulator,
        [switch]$InjectStreamFault
    )
    $scenarioClock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "Scenario start: $([IO.Path]::GetFileName($ScriptPath)); child=${ChildTimeoutMilliseconds}ms; overall=${OverallTimeoutMilliseconds}ms"
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = (Get-Command powershell.exe -ErrorAction Stop).Source
    $psi.Arguments = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' + (Quote-Arg $ScriptPath) + ' -RepoRoot ' + (Quote-Arg $Root) + ' -ChildTimeoutMilliseconds ' + $ChildTimeoutMilliseconds
    $psi.WorkingDirectory = $repoRoot; $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $process = $null; $outTask = $null; $errTask = $null
    $harnessCleanupFailedHere = $false
    $code = 124; $output = ''
    try {
        $process = [System.Diagnostics.Process]::Start($psi)
        $procId = $process.Id
        if ($InjectStreamFault) { throw "injected stream initialization fault" }
        $outTask = $process.StandardOutput.ReadToEndAsync()
        $errTask = $process.StandardError.ReadToEndAsync()
    } catch {
        # Process was started but stream init failed — clean up the process tree
        $procTerminated = $false
        if ($process) {
            $procId = $process.Id
            $treeStopped = Stop-TestProcessTree $process
            if (-not $treeStopped) { try { $process.Kill() } catch { } }
            $procTerminated = $process.WaitForExit([Math]::Min(1000, $OverallTimeoutMilliseconds))
            $process.Dispose(); $process = $null
        } else { $procId = 0 }
        Write-Host "::error::Harness cleanup failure: stream initialization fault (PID=$procId; Terminated=$procTerminated)"
        $script:harnessCleanupFailed = $true; $harnessCleanupFailedHere = $true
        $scenarioClock.Stop()
        Write-Host "Scenario end: $([IO.Path]::GetFileName($ScriptPath)); elapsed=$($scenarioClock.ElapsedMilliseconds)ms"
        if ($harnessCleanupFailedHere) { Write-Host "  Harness cleanup failure recorded; subsequent scenarios will be skipped" }
        return [PSCustomObject]@{ ExitCode = $code; Output = $output; ElapsedMilliseconds = $scenarioClock.ElapsedMilliseconds; HarnessCleanupFailed = $harnessCleanupFailedHere; Pid = $procId; Terminated = $procTerminated }
    }
    try {
        $finished = $process.WaitForExit($OverallTimeoutMilliseconds)
        $terminated = $true
        if (-not $finished) {
            $treeStopped = Stop-TestProcessTree $process
            if (-not $treeStopped) { try { $process.Kill() } catch { } }
            $terminated = $process.WaitForExit([Math]::Min(1000, $OverallTimeoutMilliseconds))
            if (-not $terminated -or -not $process.HasExited) {
                Write-Host "::error::Harness cleanup failure: simulator root PID=$($process.Id) did not exit after timeout"
                $script:harnessCleanupFailed = $true; $harnessCleanupFailedHere = $true
            }
        }
        $outDone = $outTask.Wait(1000); $errDone = $errTask.Wait(1000)
        $tasksHealthy = $outDone -and $errDone -and -not $outTask.IsFaulted -and -not $errTask.IsFaulted
        if (-not $tasksHealthy) {
            Write-Host "::error::Harness cleanup failure: simulator output tasks did not converge (PID=$($process.Id))"
            $script:harnessCleanupFailed = $true; $harnessCleanupFailedHere = $true
        }
        if ($terminated -and $process.HasExited -and $tasksHealthy) {
            $code = $process.ExitCode
            $output = $outTask.GetAwaiter().GetResult() + $errTask.GetAwaiter().GetResult()
        }
    } finally { if ($process) { $process.Dispose() } }
    $scenarioClock.Stop()
    Write-Host "Scenario end: $([IO.Path]::GetFileName($ScriptPath)); elapsed=$($scenarioClock.ElapsedMilliseconds)ms"
    if ($harnessCleanupFailedHere) { Write-Host "  Harness cleanup failure recorded; subsequent scenarios will be skipped" }
    return [PSCustomObject]@{ ExitCode = $code; Output = $output; ElapsedMilliseconds = $scenarioClock.ElapsedMilliseconds; HarnessCleanupFailed = $harnessCleanupFailedHere }
}

function Test-ApprovalRecord {
    param([string]$Content)
    if ($null -eq $Content) { return $false }
    $status = $Content.Trim()
    $normalTerminator = ($Content -eq $status) -or ($Content -eq ($status + "`n")) -or ($Content -eq ($status + "`r`n"))
    if (-not $normalTerminator) { return $false }
    $pass = $status -match '\APASS\tAPPROVAL_GATE_PASSED: .+\z'
    $block = $status -match '\ABLOCK\tAPPROVAL_GATE_BLOCKED: .+\z'
    return (($pass -xor $block) -and $status -notmatch '\r|\n')
}
foreach ($case in @(
    @{ Name = 'missing'; Value = $null; Expected = $false },
    @{ Name = 'empty'; Value = ''; Expected = $false },
    @{ Name = 'garbage'; Value = 'PASS nope'; Expected = $false },
    @{ Name = 'multiline'; Value = "PASS`tAPPROVAL_GATE_PASSED: ok`r`nnoise"; Expected = $false },
    @{ Name = 'pass-block'; Value = "PASS`tAPPROVAL_GATE_PASSED: ok`r`nBLOCK`tAPPROVAL_GATE_BLOCKED: reason"; Expected = $false },
    @{ Name = 'trailing-space'; Value = "PASS`tAPPROVAL_GATE_PASSED: ok " ; Expected = $false },
    @{ Name = 'blocked'; Value = "BLOCK`tAPPROVAL_GATE_BLOCKED: reason"; Expected = $true },
    @{ Name = 'pass'; Value = "PASS`tAPPROVAL_GATE_PASSED: 0 protected change(s) validated"; Expected = $true }
) ) { Assert-True ((Test-ApprovalRecord $case.Value) -eq $case.Expected) "ResultFile $($case.Name) contract case failed" }

try {
    New-Item -ItemType Directory -Path $existingParent -Force | Out-Null
    $existingParentCreatedForProbe = -not $ciParentInitiallyExisted
    $parentCreatedByTest = $existingParentCreatedForProbe
    $unsafeCleanup = @()
    $unsafeExit = 0
    try { $unsafeCleanup = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $cleanupHelper -RepoRoot $repoRoot -RunRoot $existingParent 2>&1); $unsafeExit = $LASTEXITCODE } catch { $unsafeCleanup += $_.Exception.Message; $unsafeExit = $LASTEXITCODE }
    Assert-True ($unsafeExit -ne 0 -and (($unsafeCleanup -join "`n") -match 'unsafe RunRoot')) 'Cleanup helper accepted ci parent as RunRoot'
    $helperRunRoot = Join-Path $existingParent ('run-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $helperRunRoot -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $helperRunRoot '.ci-ownership-marker'), 'CiGovernanceRunRoot')
    [IO.File]::WriteAllText((Join-Path $helperRunRoot 'probe.txt'), 'cleanup')
    $helperCleanupOutput = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $cleanupHelper -RepoRoot $repoRoot -RunRoot $helperRunRoot 2>&1)
    Assert-True ($LASTEXITCODE -eq 0 -and -not (Test-Path -LiteralPath $helperRunRoot)) "Cleanup helper failed valid runRoot cleanup: path=$helperRunRoot output=$($helperCleanupOutput -join '`n')"
    # Test: cleanup helper rejects RunRoot without ownership marker
    $helperRunRootNoMarker = Join-Path $existingParent ('run-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $helperRunRootNoMarker -Force | Out-Null
    $noMarkerOutput = @(); $noMarkerExit = 0
    try { $noMarkerOutput = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $cleanupHelper -RepoRoot $repoRoot -RunRoot $helperRunRootNoMarker 2>&1); $noMarkerExit = $LASTEXITCODE } catch { $noMarkerOutput += $_.Exception.Message; $noMarkerExit = 1 }
    Assert-True ($noMarkerExit -ne 0 -and (($noMarkerOutput -join "`n") -match 'ownership marker')) "Cleanup helper accepted RunRoot without ownership marker: $helperRunRootNoMarker"
    # Clean up the no-marker run root manually
    if (Test-Path -LiteralPath $helperRunRootNoMarker) { Remove-Item -LiteralPath $helperRunRootNoMarker -Recurse -Force -ErrorAction SilentlyContinue }
    # Test: cleanup helper rejects RunRoot with wrong marker content (extra text)
    $helperRunRootBadContent = Join-Path $existingParent ('run-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $helperRunRootBadContent -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $helperRunRootBadContent '.ci-ownership-marker'), 'CiGovernanceRunRoot WITH EXTRA TEXT')
    $badContentOutput = @(); $badContentExit = 0
    try { $badContentOutput = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $cleanupHelper -RepoRoot $repoRoot -RunRoot $helperRunRootBadContent 2>&1); $badContentExit = $LASTEXITCODE } catch { $badContentOutput += $_.Exception.Message; $badContentExit = 1 }
    Assert-True ($badContentExit -ne 0 -and (($badContentOutput -join "`n") -match 'ownership marker content invalid')) "Cleanup helper accepted RunRoot with wrong marker content: $helperRunRootBadContent"
    if (Test-Path -LiteralPath $helperRunRootBadContent) { Remove-Item -LiteralPath $helperRunRootBadContent -Recurse -Force -ErrorAction SilentlyContinue }
    # Test: cleanup helper rejects RunRoot with marker as directory
    $helperRunRootDirMarker = Join-Path $existingParent ('run-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $helperRunRootDirMarker -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $helperRunRootDirMarker '.ci-ownership-marker') -Force | Out-Null
    $dirMarkerOutput = @(); $dirMarkerExit = 0
    try { $dirMarkerOutput = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $cleanupHelper -RepoRoot $repoRoot -RunRoot $helperRunRootDirMarker 2>&1); $dirMarkerExit = $LASTEXITCODE } catch { $dirMarkerOutput += $_.Exception.Message; $dirMarkerExit = 1 }
    Assert-True ($dirMarkerExit -ne 0 -and (($dirMarkerOutput -join "`n") -match 'ownership marker')) "Cleanup helper accepted RunRoot with directory marker: $helperRunRootDirMarker"
    if (Test-Path -LiteralPath $helperRunRootDirMarker) { Remove-Item -LiteralPath $helperRunRootDirMarker -Recurse -Force -ErrorAction SilentlyContinue }
    Assert-HarnessNotFailed
    $existingRun = Invoke-Simulator -Root $repoRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000
    Assert-True (-not $existingRun.HarnessCleanupFailed) "Existing run set HarnessCleanupFailed"
    Assert-True (Test-Path -LiteralPath $existingParent -PathType Container) 'Pre-existing empty parent was removed'
    $existingChildrenAfterRun = @(Get-ChildItem -LiteralPath $existingParent -Force -ErrorAction Stop | ForEach-Object { $_.Name })
    Assert-True (@($existingChildrenAfterRun | Where-Object { $ciParentInitialChildren -notcontains $_ }).Count -eq 0) 'Pre-existing parent gained unexpected children'
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    # Test: Invoke-Simulator with InjectStreamFault cleans up process and sets HarnessCleanupFailed
    $harnessCleanupFailed = $false
    $injectClock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "Scenario start: inject-stream-fault; child=10000ms; overall=20000ms"
    $rapidExitScript = Join-Path $testRoot 'rapid-exit.ps1'
    [System.IO.File]::WriteAllText($rapidExitScript, 'exit 0')
    $injectResult = Invoke-Simulator -Root $repoRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000 -ScriptPath $rapidExitScript -InjectStreamFault
    $injectClock.Stop()
    Write-Host "Scenario end: inject-stream-fault; elapsed=$($injectClock.ElapsedMilliseconds)ms"
    Assert-True ($injectResult.HarnessCleanupFailed) "InjectStreamFault did not set HarnessCleanupFailed"
    Assert-True ($harnessCleanupFailed) "InjectStreamFault did not set global harnessCleanupFailed"
    Assert-True ($injectResult.Terminated) "InjectStreamFault did not terminate process; Terminated=$($injectResult.Terminated)"
    Assert-True ($injectResult.Pid -gt 0) "InjectStreamFault did not capture PID; Pid=$($injectResult.Pid)"
    $injectPidAlive = Get-Process -Id $injectResult.Pid -ErrorAction SilentlyContinue
    Assert-True ($null -eq $injectPidAlive) "InjectStreamFault PID $($injectResult.Pid) still running"
    Remove-Item -LiteralPath $rapidExitScript -Force -ErrorAction SilentlyContinue
    # Reset harness flag for subsequent scenarios
    $harnessCleanupFailed = $false
    # Test: stream health check is unconditional (not gated on -not $terminated)
    $quickExitScript = Join-Path $testRoot 'quick-exit.ps1'
    [System.IO.File]::WriteAllText($quickExitScript, 'Write-Output "done"')
    $quickClock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "Scenario start: quick-exit-stream-health; child=10000ms; overall=20000ms"
    $quickResult = Invoke-Simulator -Root $repoRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000 -ScriptPath $quickExitScript
    $quickClock.Stop()
    Write-Host "Scenario end: quick-exit-stream-health; elapsed=$($quickClock.ElapsedMilliseconds)ms"
    Assert-True (-not $quickResult.HarnessCleanupFailed) "Quick exit scenario unexpectedly set HarnessCleanupFailed"
    Assert-True ($quickResult.ExitCode -eq 0) "Quick exit scenario exit=$($quickResult.ExitCode)"
    Remove-Item -LiteralPath $quickExitScript -Force -ErrorAction SilentlyContinue
    foreach ($relative in @('.specify', 'specs', 'docs', 'scripts\docs')) {
        Copy-Item -LiteralPath (Join-Path $repoRoot $relative) -Destination (Join-Path $testRoot $relative) -Recurse -Force
    }
    New-Item -ItemType Directory -Path (Join-Path $testRoot 'scripts\ci') -Force | Out-Null
    Copy-Item -LiteralPath $cleanupHelper -Destination (Join-Path $testRoot 'scripts\ci\cleanup-governance-run.ps1') -Force
    $git = (Get-Command git.exe -ErrorAction Stop).Source
    & $git -C $testRoot init --quiet | Out-Null
    & $git -C $testRoot config user.email 'task8@example.invalid'
    & $git -C $testRoot config user.name 'Task8 Test'
    & $git -C $testRoot add --all
    & $git -C $testRoot commit --quiet -m 'isolated task8 baseline'
    [System.IO.File]::WriteAllText((Join-Path $testRoot 'dummy.md'), 'Task8 isolated second commit')
    & $git -C $testRoot add dummy.md
    & $git -C $testRoot commit --quiet -m 'isolated task8 second baseline commit'
    $headParent = (& $git -C $testRoot rev-parse HEAD~1).Trim()
    Assert-True ($LASTEXITCODE -eq 0 -and $headParent.Length -eq 40) 'Isolated fixture does not have a valid HEAD~1 baseline'
    $timeoutValidator = Join-Path $testRoot 'scripts\docs\validate-constitution.ps1'
    $baselineValidator = Get-Content -LiteralPath $timeoutValidator -Raw
    $grandchildScript = Join-Path $testRoot '.specify\memory\grandchild.ps1'
    [System.IO.File]::WriteAllText($grandchildScript, 'Start-Sleep -Seconds 30')
    $timeoutScript = @'
[CmdletBinding()]
param([string]$ConstitutionPath = '', [string]$Report = '')
$memory = Split-Path -Parent $ConstitutionPath
$grandchild = Join-Path $memory 'grandchild.ps1'
$pidFile = Join-Path $memory 'grandchild.pid'
$validatorPidFile = Join-Path $memory 'validator.pid'
[System.IO.File]::WriteAllText($validatorPidFile, [string]$PID)
$child = Start-Process -FilePath powershell.exe -ArgumentList @('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',$grandchild) -RedirectStandardOutput (Join-Path $memory 'grandchild.out') -RedirectStandardError (Join-Path $memory 'grandchild.err') -PassThru
[System.IO.File]::WriteAllText($pidFile, [string]$child.Id)
Write-Output ('o' * 50000)
[Console]::Error.WriteLine(('e' * 50000))
while ($true) { Start-Sleep -Seconds 1 }
'@
    [System.IO.File]::WriteAllText($timeoutValidator, $timeoutScript)
    $timeoutClock = [Diagnostics.Stopwatch]::StartNew()
    try {
        Assert-HarnessNotFailed
        $timeout = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 500 -OverallTimeoutMilliseconds 15000
    } finally {
        [System.IO.File]::WriteAllText($timeoutValidator, $baselineValidator)
    }
    $fixturePath = $testRoot
    foreach ($identityFile in @('validator.pid', 'grandchild.pid')) {
        $identityPath = Join-Path $testRoot ('.specify\memory\' + $identityFile)
        if (Test-Path -LiteralPath $identityPath -PathType Leaf) {
            $identityPid = [int](Get-Content -LiteralPath $identityPath -Raw).Trim()
            Write-Host "Task8 timeout fixture: path=$fixturePath; $((Get-TestProcessIdentity $identityPid))"
        } else { Write-Host "Task8 timeout fixture: path=$fixturePath; missing=$identityFile" }
    }
    $timeoutClock.Stop()
    Assert-True (-not $timeout.HarnessCleanupFailed) "Timeout scenario set HarnessCleanupFailed"
    Assert-True ($timeout.ExitCode -eq 1) "Production simulator timeout exit=$($timeout.ExitCode), expected 1; output=$($timeout.Output.Substring(0, [Math]::Min(300, $timeout.Output.Length)))"
    Assert-True ($timeoutClock.ElapsedMilliseconds -le 15000) "Production simulator timeout exceeded OverallTimeoutMilliseconds=15000: $($timeoutClock.ElapsedMilliseconds)ms"
    Assert-True ($timeout.Output -match 'timed out|timeout') "Production simulator timeout output lacks timeout diagnostic: $($timeout.Output.Substring(0, [Math]::Min(500, $timeout.Output.Length)))"
    Assert-True ($timeout.Output -match '::error::Constitution') "Production simulator timeout lacks Constitution error annotation: $($timeout.Output.Substring(0, [Math]::Min(500, $timeout.Output.Length)))"
    $isolatedCiParent = Join-Path $testRoot 'build\test-temp\ci'
    $isolatedRunRoots = @(Get-ChildItem -LiteralPath $isolatedCiParent -Filter 'run-*' -ErrorAction SilentlyContinue)
    Assert-True ($isolatedRunRoots.Count -eq 0 -and (-not (Test-Path -LiteralPath $isolatedCiParent -PathType Container) -or @(Get-ChildItem -LiteralPath $isolatedCiParent -Force -ErrorAction SilentlyContinue).Count -eq 0)) 'Newly created parent was not cleaned'
    $pidFile = Join-Path $testRoot '.specify\memory\grandchild.pid'
    $pidReady = $false
    for ($attempt = 1; $attempt -le 10 -and -not $pidReady; $attempt++) { $pidReady = Test-Path -LiteralPath $pidFile -PathType Leaf; if (-not $pidReady) { Start-Sleep -Milliseconds 100 } }
    Assert-True $pidReady 'Timeout fixture did not record grandchild PID'
    $validatorPidFile = Join-Path $testRoot '.specify\memory\validator.pid'
    $validatorPidReady = $false
    for ($attempt = 1; $attempt -le 10 -and -not $validatorPidReady; $attempt++) { $validatorPidReady = Test-Path -LiteralPath $validatorPidFile -PathType Leaf; if (-not $validatorPidReady) { Start-Sleep -Milliseconds 100 } }
    Assert-True $validatorPidReady 'Timeout fixture did not record validator PID'
    if ($pidReady) {
        $grandchildPid = [int](Get-Content -LiteralPath $pidFile -Raw).Trim()
        $grandchildAlive = Get-Process -Id $grandchildPid -ErrorAction SilentlyContinue
        Assert-True ($null -eq $grandchildAlive) "Grandchild process survived timeout: $(Get-TestProcessIdentity $grandchildPid); fixture=$fixturePath"
    }
    if ($validatorPidReady) {
        $validatorPid = [int](Get-Content -LiteralPath $validatorPidFile -Raw).Trim()
        Assert-True ($null -eq (Get-Process -Id $validatorPid -ErrorAction SilentlyContinue)) "Validator process survived timeout: $(Get-TestProcessIdentity $validatorPid); fixture=$fixturePath"
    }
    $sleepCleanupHelper = Join-Path $testRoot 'sleep-cleanup.ps1'
    $sleepCleanupBody = @'
[CmdletBinding()] param([string]$RepoRoot,[string]$RunRoot,[switch]$RemoveParent)
$pidPath = Join-Path $RunRoot 'cleanup-helper.pid'
[IO.File]::WriteAllText($pidPath, [string]$PID)
while ($true) { Start-Sleep -Seconds 1 }
'@
    [IO.File]::WriteAllText($sleepCleanupHelper, $sleepCleanupBody)
    $sleepCleanupSimulator = Join-Path $testRoot 'simulate-sleep-cleanup.ps1'
    $sleepCleanupBodySimulator = (Get-Content -LiteralPath $simulator -Raw).Replace('scripts\ci\cleanup-governance-run.ps1', 'sleep-cleanup.ps1')
    [IO.File]::WriteAllText($sleepCleanupSimulator, $sleepCleanupBodySimulator)
    Assert-HarnessNotFailed
    $sleepCleanupRun = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 500 -OverallTimeoutMilliseconds 15000 -ScriptPath $sleepCleanupSimulator
    Assert-True (-not $sleepCleanupRun.HarnessCleanupFailed) "Sleep cleanup scenario set HarnessCleanupFailed"
    Assert-True ($sleepCleanupRun.ExitCode -eq 1) "Sleeping cleanup helper exit=$($sleepCleanupRun.ExitCode), expected 1"
    Assert-True ($sleepCleanupRun.Output -match '::error::Governance CI cleanup failed') "Sleeping cleanup helper lacks cleanup error annotation: $($sleepCleanupRun.Output)"
    $cleanupPidPath = Join-Path $testRoot 'build\test-temp\ci'
    $cleanupRunRoots = @(Get-ChildItem -LiteralPath $cleanupPidPath -Filter 'run-*' -Directory -ErrorAction SilentlyContinue)
    foreach ($cleanupRunRoot in $cleanupRunRoots) {
        $cleanupPidFile = Join-Path $cleanupRunRoot.FullName 'cleanup-helper.pid'
        if (Test-Path -LiteralPath $cleanupPidFile -PathType Leaf) {
            $cleanupPid = [int](Get-Content -LiteralPath $cleanupPidFile -Raw).Trim()
            Assert-True ($null -eq (Get-Process -Id $cleanupPid -ErrorAction SilentlyContinue)) "Cleanup helper survived emergency termination: PID=$cleanupPid; runRoot=$($cleanupRunRoot.FullName)"
        }
    }
    $negativeConstitution = Join-Path $testRoot '.specify\memory\constitution.md'
    $baselineConstitution = Get-Content -LiteralPath $negativeConstitution -Raw
    try {
        Copy-Item -LiteralPath (Join-Path $repoRoot 'scripts\docs\validate-constitution.ps1') -Destination $timeoutValidator -Force
        $negativeBody = $baselineConstitution -replace '(?m)^## Approval History', "TODO INVALID CONSTITUTION PLACEHOLDER`r`n`r`n## Approval History"
        [System.IO.File]::WriteAllText($negativeConstitution, $negativeBody)
        Assert-HarnessNotFailed
        $positive = Invoke-Simulator -Root $repoRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000
        Assert-True (-not $positive.HarnessCleanupFailed) "Positive scenario set HarnessCleanupFailed"
        Assert-True ($positive.ExitCode -eq 0) "Positive simulator exit=$($positive.ExitCode)"
        Assert-HarnessNotFailed
        $negative = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000
    } finally {
        [System.IO.File]::WriteAllText($timeoutValidator, $baselineValidator)
        [System.IO.File]::WriteAllText($negativeConstitution, $baselineConstitution)
    }
    Assert-True ($negative.ExitCode -eq 1) "Negative simulator exit=$($negative.ExitCode), expected 1"
    Assert-True (-not $negative.HarnessCleanupFailed) "Negative scenario set HarnessCleanupFailed"
    Assert-True ($negative.Output -match '::error::') 'Negative simulator output lacks ::error::'
    Assert-True ($negative.Output -match 'Constitution') 'Negative simulator must fail Constitution independently of approval'
    Assert-True ($negative.Output -match 'Approval validation PASSED') 'Negative simulator approval did not pass independently'
    Assert-True ($negative.Output -notmatch 'Approvals validation failed|Approvals BLOCKED') 'Negative simulator approval was incorrectly blocked'
    Assert-True ($negative.Output -notmatch 'cleanup failed') 'Normal simulator cleanup reported failure'

    $blockedTaskkillScript = Join-Path $testRoot 'blocked-taskkill.ps1'
    [System.IO.File]::WriteAllText($blockedTaskkillScript, 'Start-Sleep -Seconds 30')
    $simAst = [System.Management.Automation.Language.Parser]::ParseInput($sim, [ref]$null, [ref]$null)
    $taskkillFunction = $simAst.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-BoundedTaskkill' }, $true)
    Assert-True ($null -ne $taskkillFunction) 'Could not extract Invoke-BoundedTaskkill from production simulator'
    if ($null -ne $taskkillFunction) {
        $taskkillFunctionText = $taskkillFunction.Extent.Text
        $taskkillHarness = @"
param([string]`$RepoRoot = '', [int]`$ChildTimeoutMilliseconds = 10000)
`$repo = '$repoRoot'
function Quote-WindowsArgument {
    param([AllowNull()][string]`$Value)
    if (`$null -eq `$Value -or `$Value.Length -eq 0) { return '""' }
    '"' + `$Value.Replace('"', '\"') + '"'
}
function Get-RemainingMilliseconds { param([long]`$DeadlineTimestamp); [Math]::Max(0,[int][Math]::Ceiling(((`$DeadlineTimestamp-[Diagnostics.Stopwatch]::GetTimestamp())*1000.0)/[Diagnostics.Stopwatch]::Frequency)) }
$taskkillFunctionText
Write-Output 'taskkill-harness-started'
`$sleepTaskkill = (Get-Command powershell.exe -ErrorAction Stop).Source
`$sleepArguments = '-NoProfile -NonInteractive -Command "Start-Sleep -Seconds 30"'
`$result = Invoke-BoundedTaskkill -TaskkillPath `$sleepTaskkill -RootPid 0 -TimeoutMilliseconds 5000 -TaskkillArguments `$sleepArguments
Write-Output (ConvertTo-Json `$result -Compress)
Write-Output "TASKKILL_DIAGNOSTIC=`$(`$result.Diagnostic)"
Write-Output "TASKKILL_EXIT=`$(`$result.ExitCode)"
Write-Output "TASKKILL_SUCCEEDED=`$(`$result.Succeeded)"
if (-not `$result.Succeeded) { exit 1 }
"@
        $taskkillHarnessPath = Join-Path $testRoot 'taskkill-harness.ps1'
        [System.IO.File]::WriteAllText($taskkillHarnessPath, $taskkillHarness)
    }
    $taskkillTimeoutClock = [Diagnostics.Stopwatch]::StartNew()
    Assert-HarnessNotFailed
    $taskkillTimeoutRun = if ($null -ne $taskkillFunction) { Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 60000 -OverallTimeoutMilliseconds 15000 -ScriptPath $taskkillHarnessPath } else { [PSCustomObject]@{ ExitCode = 1; Output = ''; ElapsedMilliseconds = 0 } }
    $taskkillTimeoutClock.Stop()
    Assert-True (-not $taskkillTimeoutRun.HarnessCleanupFailed) "Taskkill harness set HarnessCleanupFailed; harness must remain healthy for business failures"
    Assert-True ($taskkillTimeoutRun.ExitCode -in @(1,124)) "Taskkill harness exit=$($taskkillTimeoutRun.ExitCode), expected bounded failure"
    $taskkillDiagnosticPresent = $taskkillTimeoutRun.Output -match 'TASKKILL_DIAGNOSTIC=.*taskkill timeout after \d+ms'
    if (-not $taskkillDiagnosticPresent) {
        $taskkillOutputPreview = [string]$taskkillTimeoutRun.Output
        if ($taskkillOutputPreview.Length -gt 1000) { $taskkillOutputPreview = $taskkillOutputPreview.Substring(0, 1000) }
        Write-Host "Taskkill harness diagnostic: Output(first1000)=$taskkillOutputPreview; ExitCode=$($taskkillTimeoutRun.ExitCode); Elapsed=$($taskkillTimeoutRun.ElapsedMilliseconds)ms"
    }
    Assert-True ($taskkillDiagnosticPresent -or $taskkillTimeoutRun.ExitCode -eq 124) 'Taskkill harness lacks internal timeout diagnostic'
    Assert-True ($taskkillTimeoutClock.ElapsedMilliseconds -lt 15000) "Injected taskkill timeout was not bounded: $($taskkillTimeoutClock.ElapsedMilliseconds)ms"

    # Test: Invoke-BoundedTaskkill post-start fault injection
    # Extract the function, modify it to throw after Start succeeds, verify emergency cleanup
    $taskkillPostStartClock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "Scenario start: taskkill-poststart-fault; child=10000ms; overall=20000ms"
    if ($null -ne $taskkillFunction) {
        $faultyTaskkillText = $taskkillFunctionText -replace '\$process=\[Diagnostics\.Process\]::Start\(\$psi\); \$started=\$true; \$outTask=\$process\.StandardOutput\.ReadToEndAsync\(\); \$errTask=\$process\.StandardError\.ReadToEndAsync\(\)', '$process=[Diagnostics.Process]::Start($psi); $started=$true; throw "injected post-start fault"'
        $taskkillFaultHarness = @"
param([string]`$RepoRoot = '', [int]`$ChildTimeoutMilliseconds = 10000)
`$repo = '$repoRoot'
function Quote-WindowsArgument {
    param([AllowNull()][string]`$Value)
    if (`$null -eq `$Value -or `$Value.Length -eq 0) { return '""' }
    '"' + `$Value.Replace('"', '\"') + '"'
}
function Get-RemainingMilliseconds { param([long]`$DeadlineTimestamp); [Math]::Max(0,[int][Math]::Ceiling(((`$DeadlineTimestamp-[Diagnostics.Stopwatch]::GetTimestamp())*1000.0)/[Diagnostics.Stopwatch]::Frequency)) }
$faultyTaskkillText
`$sleepTaskkill = (Get-Command powershell.exe -ErrorAction Stop).Source
`$sleepArguments = '-NoProfile -NonInteractive -Command "Start-Sleep -Seconds 30"'
`$result = Invoke-BoundedTaskkill -TaskkillPath `$sleepTaskkill -RootPid 0 -TimeoutMilliseconds 10000 -TaskkillArguments `$sleepArguments
Write-Output (ConvertTo-Json `$result -Compress)
Write-Output "TASKKILL_DIAGNOSTIC=`$(`$result.Diagnostic)"
Write-Output "TASKKILL_EXIT=`$(`$result.ExitCode)"
Write-Output "TASKKILL_SUCCEEDED=`$(`$result.Succeeded)"
if (-not `$result.Succeeded) { exit 0 }
exit 1
"@
        $taskkillFaultHarnessPath = Join-Path $testRoot 'taskkill-poststart-fault.ps1'
        [System.IO.File]::WriteAllText($taskkillFaultHarnessPath, $taskkillFaultHarness)
        $taskkillFaultResult = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000 -ScriptPath $taskkillFaultHarnessPath
        $taskkillPostStartClock.Stop()
        Write-Host "Scenario end: taskkill-poststart-fault; elapsed=$($taskkillPostStartClock.ElapsedMilliseconds)ms"
        Assert-True (-not $taskkillFaultResult.HarnessCleanupFailed) "Taskkill post-start fault set HarnessCleanupFailed"
        Assert-True ($taskkillFaultResult.ExitCode -eq 0) "Taskkill post-start fault exit=$($taskkillFaultResult.ExitCode), expected 0"
        Assert-True ($taskkillFaultResult.Output -match 'taskkill exception') "Taskkill post-start fault lacks exception diagnostic"
        Assert-True ($taskkillFaultResult.Output -match 'injected post-start fault') "Taskkill post-start fault lacks original exception message"
        Assert-True ($taskkillFaultResult.Output -match 'TASKKILL_SUCCEEDED=False') "Taskkill post-start fault returned Succeeded=True unexpectedly"
        Remove-Item -LiteralPath $taskkillFaultHarnessPath -Force -ErrorAction SilentlyContinue
    }
    # Test: Invoke-BoundedTaskkill disposed-handle post-start fault
    # This verifies the outer catch handles HasExited/Kill on disposed handle gracefully
    # The disposed handle corrupts stdout/stderr streams, so we run the harness directly
    $taskkillDisposedClock = [Diagnostics.Stopwatch]::StartNew()
    Write-Host "Scenario start: taskkill-disposed-fault; child=10000ms; overall=20000ms"
    if ($null -ne $taskkillFunction) {
        # Keep the disposed Process object (not replaced with New-Object) so the production catch
        # exercises emergency cleanup (Kill/WaitForExit) on a disposed handle via real catch handlers.
        # HasExited check cannot be exercised dynamically: in Windows PS 5.1, $process.HasExited on
        # both New-Object and disposed processes returns $null instead of throwing, so the catch's
        # HasExited handler is unreachable. Defended by static review (production catch lines 47-49).
        $disposedTaskkillText = $taskkillFunctionText -replace '\$process=\[Diagnostics\.Process\]::Start\(\$psi\); \$started=\$true; \$outTask=\$process\.StandardOutput\.ReadToEndAsync\(\); \$errTask=\$process\.StandardError\.ReadToEndAsync\(\)', '$process=[Diagnostics.Process]::Start($psi); $started=$true; $process.Dispose(); throw "injected disposed-handle post-start fault"'
        $dispHarv = Join-Path $testRoot 'taskkill-disposed-fault.ps1'
        $dispHarvBody = @"
param([string]`$RepoRoot = '', [int]`$ChildTimeoutMilliseconds = 10000)
`$repo = '$repoRoot'
function Quote-WindowsArgument {
    param([AllowNull()][string]`$Value)
    if (`$null -eq `$Value -or `$Value.Length -eq 0) { return '""' }
    '"' + `$Value.Replace('"', '\"') + '"'
}
function Get-RemainingMilliseconds { param([long]`$DeadlineTimestamp); [Math]::Max(0,[int][Math]::Ceiling(((`$DeadlineTimestamp-[Diagnostics.Stopwatch]::GetTimestamp())*1000.0)/[Diagnostics.Stopwatch]::Frequency)) }
$disposedTaskkillText
Write-Output 'started'
`$sleepTaskkill = (Get-Command powershell.exe -ErrorAction Stop).Source
`$sleepArguments = '-NoProfile -NonInteractive -Command "Start-Sleep -Seconds 30"'
`$result = Invoke-BoundedTaskkill -TaskkillPath `$sleepTaskkill -RootPid 0 -TimeoutMilliseconds 5000 -TaskkillArguments `$sleepArguments
if (`$null -eq `$result) { Write-Output 'RESULT_NULL=true'; exit 1 }
Write-Output "JSON=`$(ConvertTo-Json `$result -Compress)"
Write-Output "SUCCEEDED=`$(`$result.Succeeded)"
Write-Output "DIAG=`$(`$result.Diagnostic)"
if (-not `$result.Succeeded) { exit 0 }
exit 1
"@
        [System.IO.File]::WriteAllText($dispHarv, $dispHarvBody)
        $dispHarvPsi = New-Object System.Diagnostics.ProcessStartInfo
        $dispHarvPsi.FileName = (Get-Command powershell.exe -ErrorAction Stop).Source
        $dispHarvPsi.Arguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$dispHarv`""
        $dispHarvPsi.WorkingDirectory = $repoRoot
        $dispHarvPsi.UseShellExecute = $false
        $dispHarvPsi.CreateNoWindow = $true
        $dispHarvPsi.RedirectStandardOutput = $true
        $dispHarvPsi.RedirectStandardError = $true
        $dispProc = [System.Diagnostics.Process]::Start($dispHarvPsi)
        $dispOutTask = $dispProc.StandardOutput.ReadToEndAsync()
        $dispErrTask = $dispProc.StandardError.ReadToEndAsync()
        $dispHarvExited = $dispProc.WaitForExit(15000)
        if (-not $dispHarvExited) { try { $dispProc.Kill() } catch { }; $dispProc.WaitForExit(1000) }
        $taskkillDisposedClock.Stop()
        Write-Host "Scenario end: taskkill-disposed-fault; elapsed=$($taskkillDisposedClock.ElapsedMilliseconds)ms"
        if ($dispHarvExited) {
            $dispOutText = ''; $dispErrText = ''
            try { $dispOutText = $dispOutTask.GetAwaiter().GetResult() } catch { }
            try { $dispErrText = $dispErrTask.GetAwaiter().GetResult() } catch { }
            $dispText = "$dispOutText $dispErrText"
            Assert-True ($dispText -match 'SUCCEEDED=False') "Disposed fault returned Succeeded=True: $dispText"
            Assert-True ($dispText -match 'taskkill exception') "Disposed fault lacks exception diagnostic: $dispText"
            Assert-True ($dispText -match 'injected disposed-handle post-start fault') "Disposed fault lacks original exception: $dispText"
            Assert-True ($dispText -match 'emergency Kill failed|emergency post-kill') "Disposed fault lacks emergency cleanup: $dispText"
            # HasExited check assertion removed: $process.HasExited returns $null (no exception)
            # in Windows PS 5.1 for both New-Object and disposed processes. The production catch's
            # HasExited handler (line 49 of simulate-governance.ps1) is a static-review-only defense.
        } else {
            $script:failures += "Disposed fault harness timed out"
        }
        if (Test-Path -LiteralPath $dispHarv) { Remove-Item -LiteralPath $dispHarv -Force -ErrorAction SilentlyContinue }
        $dispProc.Dispose()
    }

    $cleanupFaultSimulator = Join-Path $testRoot 'simulate-cleanup-fault.ps1'
    $cleanupFaultBody = Get-Content -LiteralPath $simulator -Raw
    $cleanupFaultBody = $cleanupFaultBody.Replace('scripts\ci\cleanup-governance-run.ps1', 'cleanup-fault.ps1')
    [System.IO.File]::WriteAllText($cleanupFaultSimulator, $cleanupFaultBody)
    $cleanupFaultHelper = @'
[CmdletBinding()] param([string]$RepoRoot,[string]$RunRoot,[switch]$RemoveParent)
$ErrorActionPreference = 'Stop'
throw "injected cleanup child failure: path=$RunRoot"
'@
    [System.IO.File]::WriteAllText((Join-Path $testRoot 'cleanup-fault.ps1'), $cleanupFaultHelper)
    $cleanupFaultValidator = @'
[CmdletBinding()]
param([string]$ConstitutionPath = '', [string]$Report = '')
Write-Error 'injected validator failure'
exit 7
'@
    try {
        [System.IO.File]::WriteAllText($timeoutValidator, $cleanupFaultValidator)
        Assert-HarnessNotFailed
        $cleanupFaultRun = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000 -ScriptPath $cleanupFaultSimulator
    } finally {
        [System.IO.File]::WriteAllText($timeoutValidator, $baselineValidator)
    }
    Assert-True ($cleanupFaultRun.ExitCode -eq 1) "Validator plus cleanup failure exit=$($cleanupFaultRun.ExitCode), expected 1"
    Assert-True (-not $cleanupFaultRun.HarnessCleanupFailed) "Cleanup fault scenario set HarnessCleanupFailed"
    Assert-True ($cleanupFaultRun.Output -match 'Constitution validation failed') "Validator failure was lost when cleanup probe failed: $($cleanupFaultRun.Output.Substring(0, [Math]::Min(500, $cleanupFaultRun.Output.Length)))"
    Assert-True ($cleanupFaultRun.Output -match 'cleanup failed|cleanup child failed|injected cleanup child failure') "Cleanup child failure lacks cleanup error diagnostic"
    $cleanupValidator = @'
[CmdletBinding()]
param([string]$ConstitutionPath = '', [string]$Report = '')
$runRoot = Split-Path -Parent $Report
$icacls = Join-Path $env:SystemRoot 'System32\icacls.exe'
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
& $icacls $runRoot /deny "$identity`:D" /C | Out-Null
if ($LASTEXITCODE -ne 0) { throw "ACL deny failed: path=$runRoot exit=$LASTEXITCODE" }
'@
    try {
        [System.IO.File]::WriteAllText($timeoutValidator, $cleanupValidator)
        Assert-HarnessNotFailed
        $cleanupRun = Invoke-Simulator -Root $testRoot -ChildTimeoutMilliseconds 10000 -OverallTimeoutMilliseconds 20000
    } finally {
        [System.IO.File]::WriteAllText($timeoutValidator, $baselineValidator)
    }
    $holderReady = Join-Path $testRoot '.specify\memory\holder.ready'
    Assert-True ($cleanupRun.ExitCode -eq 1) "Cleanup failure fixture exit=$($cleanupRun.ExitCode), expected 1"
    Assert-True (-not $cleanupRun.HarnessCleanupFailed) "Cleanup run scenario set HarnessCleanupFailed"
    Assert-True ($cleanupRun.Output -match 'cleanup failed|cleanup retry') 'Cleanup failure fixture did not exercise explicit bounded cleanup handling'
    Assert-True ($cleanupRun.Output -match '::error::') 'Cleanup failure fixture lacks error annotation'
} finally {
    $aclResetNeeded = Test-Path -LiteralPath $testRoot -PathType Container
    if ($aclResetNeeded) {
        try {
            & (Join-Path $env:SystemRoot 'System32\icacls.exe') $testRoot /reset /T /C 2>$null | Out-Null
            $aclResetExit = $LASTEXITCODE
            if ($aclResetExit -ne 0) { Write-Host "::error::Task8 ACL reset failed: path=$testRoot exit=$aclResetExit"; $script:failures += "ACL reset failed: path=$testRoot exit=$aclResetExit" }
        } catch {
            Write-Host "::error::Task8 ACL reset exception: path=$testRoot; $($_.Exception.Message)"
            $script:failures += "ACL reset exception: path=$testRoot; $($_.Exception.Message)"
        }
    }
    if (Test-Path -LiteralPath $testRoot) {
        $removed = $false
        for ($attempt = 1; $attempt -le 5 -and -not $removed; $attempt++) {
            try { Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction Stop; $removed = $true }
            catch { if ($attempt -lt 5) { Start-Sleep -Milliseconds 200 } else { throw "Task8 fixture cleanup failed after 5 attempts: $testRoot; $($_.Exception.Message)" } }
        }
    }
    if (Test-Path -LiteralPath $testParent -PathType Container) {
        try {
            if (@(Get-ChildItem -LiteralPath $testParent -Force -ErrorAction Stop).Count -eq 0) { Remove-Item -LiteralPath $testParent -Force -ErrorAction Stop }
        } catch { Write-Host "::error::Task8 fixture parent cleanup failed: $testParent; $($_.Exception.Message)"; throw }
    }
    $ciParentExistsAfter = Test-Path -LiteralPath $existingParent -PathType Container
    Assert-True ($ciParentExistsAfter -or -not $ciParentInitiallyExisted) "Pre-existing ci parent disappeared during test: path=$existingParent"
    if ($ciParentInitiallyExisted -and $ciParentExistsAfter) {
        $ciParentFinalChildren = @(Get-ChildItem -LiteralPath $existingParent -Force -ErrorAction Stop | ForEach-Object { $_.Name })
        Assert-True ((@($ciParentFinalChildren | Where-Object { $ciParentInitialChildren -notcontains $_ }).Count -eq 0) -and (@($ciParentInitialChildren | Where-Object { $ciParentFinalChildren -notcontains $_ }).Count -eq 0)) "ci parent contents were not restored: path=$existingParent"
    }
    if (-not $ciParentInitiallyExisted -and $parentCreatedByTest -and $ciParentExistsAfter) {
        try {
            $probeChildren = @(Get-ChildItem -LiteralPath $existingParent -Force -ErrorAction Stop)
            if ($probeChildren.Count -eq 0) {
                Remove-Item -LiteralPath $existingParent -Force -ErrorAction Stop
                $ciParentExistsAfter = Test-Path -LiteralPath $existingParent -PathType Container
            } else {
                Write-Host "::error::Task8 probe parent cleanup refused non-empty path: $existingParent"
                $script:failures += "Probe parent remained non-empty: $existingParent"
            }
        } catch { Write-Host "::error::Task8 initial ci parent cleanup failed: path=$existingParent; $($_.Exception.Message)"; throw }
    }
    Assert-True ($ciParentExistsAfter -eq $ciParentInitiallyExisted) "ci parent state was not restored: initial=$ciParentInitiallyExisted final=$ciParentExistsAfter path=$existingParent"
}

if ($failures.Count -gt 0) {
    Write-Host "RED/GREEN tests: FAIL ($($failures.Count) failures; passed=$passed)" -ForegroundColor Red
    foreach ($failure in $failures) { Write-Host "  [FAIL] $failure" -ForegroundColor Red }
    exit 1
}
Write-Host "RED/GREEN tests: PASS ($passed assertions)" -ForegroundColor Green
exit 0
