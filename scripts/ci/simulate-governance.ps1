# Local simulation of the governance workflow. Windows PowerShell 5.1 compatible.
[CmdletBinding()]
param(
    [string]$RepoRoot = (Get-Location).Path,
    [ValidateRange(1,600000)] [int]$ChildTimeoutMilliseconds = 60000
)
$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Resolve-Path -LiteralPath $RepoRoot).Path)
$root = [IO.Path]::GetPathRoot($repo)
if ($repo.Length -gt $root.Length) { $repo = $repo.TrimEnd('\') }
$runParent = Join-Path $repo 'build\test-temp\ci'
$runRoot = Join-Path $runParent ('run-' + [guid]::NewGuid().ToString('N'))
$cleanupScriptPath = Join-Path $repo 'scripts\ci\cleanup-governance-run.ps1'
$parentExisted = Test-Path -LiteralPath $runParent -PathType Container
$parentCreated = $false; $scriptExitCode = 1
$script:ChildTimeoutMilliseconds = $ChildTimeoutMilliseconds
function Quote-WindowsArgument { param([AllowNull()][string]$Value); if ($null -eq $Value -or !$Value.Length) { return '""' }; '"' + $Value.Replace('"','\"') + '"' }
function Get-RemainingMilliseconds { param([long]$DeadlineTimestamp); [Math]::Max(0,[int][Math]::Ceiling((($DeadlineTimestamp-[Diagnostics.Stopwatch]::GetTimestamp())*1000.0)/[Diagnostics.Stopwatch]::Frequency)) }
function Invoke-BoundedTaskkill {
    param([string]$TaskkillPath,[int]$RootPid,[int]$TimeoutMilliseconds=5000,[string]$TaskkillArguments="/PID $RootPid /T /F",[long]$DeadlineTimestamp=0)
    $psi = New-Object Diagnostics.ProcessStartInfo; $psi.FileName=$TaskkillPath; $psi.Arguments=$TaskkillArguments; $psi.WorkingDirectory=$repo
    $psi.UseShellExecute=$false; $psi.CreateNoWindow=$true; $psi.RedirectStandardOutput=$true; $psi.RedirectStandardError=$true
    $process=$null; $outTask=$null; $errTask=$null; $diagnostic=''; $exitCode=1; $finished=$false; $terminated=$false; $started=$false
    try {
        try { $process=[Diagnostics.Process]::Start($psi); $started=$true; $outTask=$process.StandardOutput.ReadToEndAsync(); $errTask=$process.StandardError.ReadToEndAsync() }
        catch { if($started){throw}else{return [PSCustomObject]@{Succeeded=$false;ExitCode=1;Diagnostic="taskkill start failed: $($_.Exception.Message)"}} }
        if (!$DeadlineTimestamp) { $DeadlineTimestamp=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*($TimeoutMilliseconds/1000.0)) }
        $wait=[Math]::Max(1,[Math]::Min($TimeoutMilliseconds,(Get-RemainingMilliseconds $DeadlineTimestamp)))
        $finished=$process.WaitForExit($wait)
        $terminated=$true
        if(!$finished){
            $actualTimeout=[Math]::Min($TimeoutMilliseconds,$wait)
            $diagnostic="taskkill timeout after ${actualTimeout}ms (PID=$RootPid; remaining=$wait)"
            try{$process.Kill()}catch{$diagnostic += "; taskkill Kill failed: $($_.Exception.Message)"}
            $remaining=Get-RemainingMilliseconds $DeadlineTimestamp
            try { $terminated = $process.WaitForExit([Math]::Max(0,$remaining)) } catch { $terminated = $false; $diagnostic += "; post-kill WaitForExit failed: $($_.Exception.Message)" }
            if(!$terminated){$diagnostic += "; post-kill WaitForExit timed out (PID=$RootPid)"}
        }
        $remaining=Get-RemainingMilliseconds $DeadlineTimestamp
        $outDone=$outTask.Wait([Math]::Max(0,$remaining))
        $remaining=Get-RemainingMilliseconds $DeadlineTimestamp
        $errDone=$errTask.Wait([Math]::Max(0,$remaining))
        if(!$outDone -or !$errDone -or $outTask.IsFaulted -or $errTask.IsFaulted){$diagnostic += '; taskkill output tasks did not converge'}
        if($process.HasExited){$exitCode=$process.ExitCode};if($exitCode -ne 0 -and !$diagnostic){$diagnostic="taskkill failed with exit=$exitCode (PID=$RootPid)"}
        [PSCustomObject]@{Succeeded=($finished -and $terminated -and $process.HasExited -and $outDone -and $errDone -and !$outTask.IsFaulted -and !$errTask.IsFaulted -and $exitCode -eq 0);ExitCode=$exitCode;Diagnostic=$diagnostic}
    } catch {
        $diag=$diagnostic
        $emergencyDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*2)
        $hasExited=$false;try{$hasExited=$process.HasExited}catch{$diag += "; HasExited check failed: $($_.Exception.Message)"}
        if($process -and !$hasExited){
            try{$process.Kill()}catch{$diag += "; emergency Kill failed: $($_.Exception.Message)"}
            $rem=Get-RemainingMilliseconds $emergencyDeadline
            try{$terminated=$process.WaitForExit([Math]::Max(0,$rem))}catch{$terminated=$false}
            if(!$terminated){$diag += "; emergency post-kill WaitForExit timed out"}
            $rem=Get-RemainingMilliseconds $emergencyDeadline
            if($outTask){try{$outTask.Wait([Math]::Max(0,$rem))|Out-Null}catch{}}
            $rem=Get-RemainingMilliseconds $emergencyDeadline
            if($errTask){try{$errTask.Wait([Math]::Max(0,$rem))|Out-Null}catch{}}
        }
        [PSCustomObject]@{Succeeded=$false;ExitCode=1;Diagnostic="taskkill exception (PID=$RootPid): $($_.Exception.Message); $diag"}
    }
    finally { if($process){$process.Dispose()} }
}
function Stop-ChildProcessTree { param([Diagnostics.Process]$Process,[long]$DeadlineTimestamp=0); try { if($Process.HasExited){return [PSCustomObject]@{Succeeded=$true;ExitCode=0;Diagnostic="process already exited; PID=$($Process.Id); no taskkill needed"}};$taskkill=Join-Path $env:SystemRoot 'System32\taskkill.exe';if(!(Test-Path -LiteralPath $taskkill -PathType Leaf)){$taskkill=(Get-Command taskkill.exe -ErrorAction Stop).Source};$remaining=if($DeadlineTimestamp){[Math]::Max(1,(Get-RemainingMilliseconds $DeadlineTimestamp))}else{5000};Invoke-BoundedTaskkill $taskkill $Process.Id $remaining -DeadlineTimestamp $DeadlineTimestamp } catch {[PSCustomObject]@{Succeeded=$false;ExitCode=1;Diagnostic="taskkill resolution/invocation failed (PID=$($Process.Id)): $($_.Exception.Message)"}} }
function Invoke-Child {
    param([string]$ScriptPath,[hashtable]$Parameters)
    $psi=New-Object Diagnostics.ProcessStartInfo;$psi.FileName=(Get-Command powershell.exe -ErrorAction Stop).Source
    $parts=@('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',(Quote-WindowsArgument $ScriptPath));foreach($key in $Parameters.Keys){foreach($item in @($Parameters[$key])){$parts+=@('-'+$key,(Quote-WindowsArgument ([string]$item)))}}
    $psi.Arguments=$parts -join ' ';$psi.WorkingDirectory=$repo;$psi.UseShellExecute=$false;$psi.CreateNoWindow=$true;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true
    $process=$null;$outTask=$null;$errTask=$null;$stdout='';$stderr='';$code=124;$timedOut=$false;$treeTerminationFailed=$false;$treeKillExitCode=0;$diagnostic='';$rootPid=0;$operationFailed=$false;$originalException=$null;$terminated=$false;$tasksHealthy=$false
    try {
        $process=[Diagnostics.Process]::Start($psi);$rootPid=$process.Id
        try {
            $outTask=$process.StandardOutput.ReadToEndAsync();$errTask=$process.StandardError.ReadToEndAsync()
            $childDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*($script:ChildTimeoutMilliseconds/1000.0))
            $finished=$process.WaitForExit([Math]::Max(0,(Get-RemainingMilliseconds $childDeadline)));$terminated=$finished
            $cleanupDeadline=$childDeadline
            if(!$finished){$timedOut=$true;$cleanupDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*2);$treeStop=Stop-ChildProcessTree $process $cleanupDeadline;$treeKillExitCode=$treeStop.ExitCode;if(!$treeStop.Succeeded){$treeTerminationFailed=$true;$diagnostic=$treeStop.Diagnostic};$remaining=Get-RemainingMilliseconds $cleanupDeadline;$terminated=$process.WaitForExit([Math]::Max(0,$remaining));if(!$terminated){$treeTerminationFailed=$true;$diagnostic += "; PID=$($process.Id); stage=root-wait; root did not exit before cleanup deadline"}}
            $remaining=Get-RemainingMilliseconds $cleanupDeadline;$outDone=$outTask.Wait([Math]::Max(0,$remaining));$remaining=Get-RemainingMilliseconds $cleanupDeadline;$errDone=$errTask.Wait([Math]::Max(0,$remaining));$tasksHealthy=$outDone -and $errDone -and !$outTask.IsFaulted -and !$errTask.IsFaulted
            if(!$terminated -or !$process.HasExited -or !$tasksHealthy){$treeTerminationFailed=$true;if(!$tasksHealthy){$diagnostic += "; PID=$($process.Id); stage=stream-wait; stdout/stderr tasks did not converge"}}
            if($terminated -and $process.HasExited -and $tasksHealthy -and !$treeTerminationFailed){$code=$process.ExitCode;$stdout=$outTask.GetAwaiter().GetResult();$stderr=$errTask.GetAwaiter().GetResult()}
        } catch {
            $operationFailed=$true;$originalException=$_.Exception
            $diagnostic += "; child operation failed after start (PID=$rootPid): $($originalException.Message)"
            $cleanupDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*2)
            try {
                if($process -and $rootPid){
                    $treeStop=Stop-ChildProcessTree $process $cleanupDeadline;$treeKillExitCode=$treeStop.ExitCode
                    if(!$treeStop.Succeeded){$treeTerminationFailed=$true;$diagnostic += "; TreeTerminationFailed: $($treeStop.Diagnostic)"}
                }
            } catch { $treeTerminationFailed=$true;$diagnostic += "; TreeTerminationFailed: cleanup exception: $($_.Exception.Message)" }
        }
    } catch { $operationFailed=$true;$originalException=$_.Exception;$diagnostic="child start failed: $($originalException.Message)" }
    finally { if($process){$process.Dispose()} }
    if($operationFailed){$code=1}
    if($stdout){Write-Host $stdout.TrimEnd()};if($stderr){Write-Host $stderr.TrimEnd() -ForegroundColor Yellow}
    [PSCustomObject]@{ExitCode=$code;StdOut=$stdout;StdErr=$stderr;TimedOut=$timedOut;TreeTerminationFailed=$treeTerminationFailed;TreeKillExitCode=$treeKillExitCode;Diagnostic=$diagnostic;TreePids=@($rootPid)}
}
function Invoke-Check { param([string]$Name,[string]$ScriptPath,[hashtable]$Parameters);if(!(Test-Path -LiteralPath $ScriptPath -PathType Leaf)){Write-Host "::error::$Name validator missing";return 1};$result=@(Invoke-Child $ScriptPath $Parameters)[-1];if($result.TreeTerminationFailed){Write-Host "::error::$Name TreeTerminationFailed (taskkill_exit=$($result.TreeKillExitCode), process_exit=$($result.ExitCode), TimedOut=$($result.TimedOut), diagnostic=$($result.Diagnostic))";return 1};if($result.TimedOut -or $result.ExitCode -eq 124){Write-Host "::error::$Name validation timed out (timeout=${script:ChildTimeoutMilliseconds}ms)";return 1};if($result.ExitCode -ne 0){Write-Host "::error::$Name validation failed (exit=$($result.ExitCode))"};$result.ExitCode}
function Invoke-FilesystemCleanupChild {
    param([string]$CleanupScriptPath,[string]$CleanupRoot,[string]$CleanupParent,[bool]$RemoveParent)
    $removeParentArgument=if($RemoveParent){' -RemoveParent'}else{''};$psi=New-Object Diagnostics.ProcessStartInfo;$psi.FileName=(Get-Command powershell.exe -ErrorAction Stop).Source;$psi.Arguments='-NoProfile -NonInteractive -ExecutionPolicy Bypass -File '+(Quote-WindowsArgument $CleanupScriptPath)+' -RepoRoot '+(Quote-WindowsArgument $repo)+' -RunRoot '+(Quote-WindowsArgument $CleanupRoot)+$removeParentArgument;$psi.WorkingDirectory=$repo;$psi.UseShellExecute=$false;$psi.CreateNoWindow=$true;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true
    $process=$null;$outTask=$null;$errTask=$null;$output='';$ok=$false;$diagnostic='';$operationDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*3);$emergencyDeadline=0;$rootPid=0
    try {
        $process=[Diagnostics.Process]::Start($psi);$rootPid=$process.Id;$outTask=$process.StandardOutput.ReadToEndAsync();$errTask=$process.StandardError.ReadToEndAsync()
        $remaining=Get-RemainingMilliseconds $operationDeadline;$finished=$process.WaitForExit([Math]::Max(0,$remaining))
        if(!$finished){
            $diagnostic="cleanup child timeout; PID=$rootPid; runRoot=$CleanupRoot; termination stage=emergency-taskkill"
            $emergencyDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*2)
            $stop=Stop-ChildProcessTree $process $emergencyDeadline
            if(!$stop.Succeeded){$diagnostic += "; TreeTerminationFailed: $($stop.Diagnostic)"}
            $remaining=Get-RemainingMilliseconds $emergencyDeadline
            $finished=$process.WaitForExit([Math]::Max(0,$remaining))
            if(!$finished -or !$process.HasExited){$diagnostic += "; PID=$rootPid; runRoot=$CleanupRoot; termination stage=root-wait; helper did not exit"}
            $remaining=Get-RemainingMilliseconds $emergencyDeadline;$outDone=$outTask.Wait([Math]::Max(0,$remaining))
            $remaining=Get-RemainingMilliseconds $emergencyDeadline;$errDone=$errTask.Wait([Math]::Max(0,$remaining))
        } else {
            $outDone=$outTask.Wait([Math]::Max(0,(Get-RemainingMilliseconds $operationDeadline)));$errDone=$errTask.Wait([Math]::Max(0,(Get-RemainingMilliseconds $operationDeadline)))
        }
        if($outDone -and !$outTask.IsFaulted){$output=$outTask.GetAwaiter().GetResult()};if($errDone -and !$errTask.IsFaulted){$output+=$errTask.GetAwaiter().GetResult()}
        if($finished -and $outDone -and $errDone -and !$outTask.IsFaulted -and !$errTask.IsFaulted -and $process.HasExited){$ok=$process.ExitCode -eq 0;if(!$ok){$diagnostic="cleanup child failed exit=$($process.ExitCode); PID=$rootPid; runRoot=$CleanupRoot"}}
    } catch {$savedDiag=$diagnostic;$emergencyDeadline=[Diagnostics.Stopwatch]::GetTimestamp()+[long]([Diagnostics.Stopwatch]::Frequency*2);$treeKilled=$false;try{$hasExited=$process.HasExited}catch{$hasExited=$false};if($process -and !$hasExited){$stop=Stop-ChildProcessTree $process $emergencyDeadline;if(!$stop.Succeeded){$savedDiag += "; TreeTerminationFailed: $($stop.Diagnostic)"};$rem=Get-RemainingMilliseconds $emergencyDeadline;try{$rootExited=$process.WaitForExit([Math]::Max(0,$rem))}catch{$rootExited=$false};if(!$rootExited){$savedDiag += "; PID=$rootPid; stage=root-wait; helper did not exit after emergency tree stop"};$rem=Get-RemainingMilliseconds $emergencyDeadline;if($outTask){try{$outDone=$outTask.Wait([Math]::Max(0,$rem))}catch{$outDone=$false};if(!$outDone){$savedDiag += "; PID=$rootPid; stage=stream-wait; stdout did not converge"}};$rem=Get-RemainingMilliseconds $emergencyDeadline;if($errTask){try{$errDone=$errTask.Wait([Math]::Max(0,$rem))}catch{$errDone=$false};if(!$errDone){$savedDiag += "; PID=$rootPid; stage=stream-wait; stderr did not converge"}};if(!$rootExited -or !$outDone -or !$errDone){try{$process.Kill()}catch{$savedDiag += "; direct Kill failed: $($_.Exception.Message)"};$rem=Get-RemainingMilliseconds $emergencyDeadline;try{$process.WaitForExit([Math]::Max(0,$rem))|Out-Null}catch{}}};$diagnostic="cleanup child supervision failed; PID=$rootPid; runRoot=$CleanupRoot; termination stage=supervision: $($_.Exception.Message); $savedDiag"} finally {if($process){$process.Dispose()}};if($output){Write-Host $output.TrimEnd()};[PSCustomObject]@{Succeeded=$ok;Diagnostic=$diagnostic;Pid=$rootPid;RunRoot=$CleanupRoot}
}
try {
    New-Item -ItemType Directory -Path $runParent -Force|Out-Null;$parentCreated=(!$parentExisted -and (Test-Path -LiteralPath $runParent -PathType Container));New-Item -ItemType Directory -Path $runRoot -Force|Out-Null;[IO.File]::WriteAllText((Join-Path $runRoot '.ci-ownership-marker'), 'CiGovernanceRunRoot');$docs=Join-Path $repo 'scripts\docs';$failed=$false
    $code=Invoke-Check 'Constitution' (Join-Path $docs 'validate-constitution.ps1') @{ConstitutionPath=Join-Path $repo '.specify\memory\constitution.md';Report=Join-Path $runRoot 'constitution.json'};if($code -ne 0){$failed=$true}
    $code=Invoke-Check 'Specs' (Join-Path $docs 'validate-specs.ps1') @{SpecDir=Join-Path $repo 'specs';Report=Join-Path $runRoot 'specs.json'};if($code -ne 0){$failed=$true}
    $approvalFile=Join-Path $runRoot 'approvals.txt';$approval=Invoke-Check 'Approvals' (Join-Path $docs 'validate-approvals.ps1') @{Repo=$repo;BaseRef='HEAD~1';ResultFile=$approvalFile};if($approval -ne 0){$failed=$true};if(!(Test-Path -LiteralPath $approvalFile -PathType Leaf)){Write-Host '::error::Approval ResultFile missing';$failed=$true}else{$rawStatus=Get-Content -LiteralPath $approvalFile -Raw;$status=$rawStatus.Trim();$normalTerminator=($rawStatus -eq $status)-or($rawStatus -eq ($status+"`n"))-or($rawStatus -eq ($status+"`r`n"));$validPass=$normalTerminator -and $status -match '\APASS\tAPPROVAL_GATE_PASSED: .+\z';$validBlock=$normalTerminator -and $status -match '\ABLOCK\tAPPROVAL_GATE_BLOCKED: .+\z';if(!($validPass -or $validBlock)){Write-Host '::error::Approval ResultFile invalid';$failed=$true}elseif($validBlock){Write-Host '::error::Approvals BLOCKED';$failed=$true}else{Write-Host 'Approval validation PASSED'}}
    $code=Invoke-Check 'Links' (Join-Path $docs 'validate-links.ps1') @{Path=$repo;Report=Join-Path $runRoot 'links.json'};if($code -ne 0){$failed=$true};$code=Invoke-Check 'Governance' (Join-Path $docs 'validate-governance.ps1') @{RepoRoot=$repo;Report=Join-Path $runRoot 'governance.json'};if($code -ne 0){$failed=$true};if($failed){Write-Host '::error::Governance CI: FAILED';$scriptExitCode=1}else{Write-Host 'Governance CI: PASSED';$scriptExitCode=0}
} catch {Write-Host "::error::Governance CI exception: $($_.Exception.Message)";$scriptExitCode=1}
finally {
    $cleanupFailed=$false
    try {
        if(!(Test-Path -LiteralPath $cleanupScriptPath -PathType Leaf)){throw "cleanup helper missing: $cleanupScriptPath"};$cleanup=Invoke-FilesystemCleanupChild $cleanupScriptPath $runRoot $runParent $parentCreated;if(!$cleanup.Succeeded){Write-Host "::error::Governance CI cleanup failed: $runRoot; $($cleanup.Diagnostic)";$cleanupFailed=$true}
    } catch { Write-Host "::error::Governance CI cleanup exception: path=$runRoot; parent=$runParent; $($_.Exception.Message)"; $cleanupFailed=$true }
    if($cleanupFailed){ $scriptExitCode=1 }
}
exit $scriptExitCode
