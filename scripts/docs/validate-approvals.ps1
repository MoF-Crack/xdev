# scripts/docs/validate-approvals.ps1
# XDev governance approval gate.
[CmdletBinding(DefaultParameterSetName='Staged')]
param(
    [string]$Repo = (Get-Location).Path,
    [Parameter(ParameterSetName='Staged')] [switch]$Staged,
    [Parameter(Mandatory=$true, ParameterSetName='BaseRef')] [string]$BaseRef,
    [string]$ResultFile = '',
    [string]$GitExe = '',
    [string[]]$GitPrefixArgs = @(),
    [int]$GitTimeoutMs = 60000
)

$ErrorActionPreference = 'Stop'
if ($GitTimeoutMs -le 0) { throw '-GitTimeoutMs must be greater than zero' }
if ($GitExe) { $script:git = $GitExe } else { $script:git = (Get-Command git -ErrorAction Stop).Source }
if ([System.IO.Path]::GetExtension($script:git) -ne '.exe') {
    throw "-GitExe must refer to an executable .exe file"
}
if ($GitPrefixArgs.Count -eq 1 -and $GitPrefixArgs[0] -match '\|') {
    $GitPrefixArgs = @($GitPrefixArgs[0] -split '\|')
}
if ($GitPrefixArgs.Count -eq 0 -and $env:XDEV_APPROVAL_GIT_PREFIX_ARGS) {
    $GitPrefixArgs = @($env:XDEV_APPROVAL_GIT_PREFIX_ARGS -split '\|')
}
$script:ProtectedPaths = @{}
$script:ApprovalAdds = @()
$script:ApprovalRecords = @()
$script:BlockReason = ''

# ---- helpers ----
function Git { param([string[]]$A)
    # Use .NET Process to safely capture stdout/stderr without ErrorActionPreference interference.
    # Native 2>&1 with $ErrorActionPreference=Stop can treat stderr warnings as errors.
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $script:git
    $argList = @($GitPrefixArgs) + @('-C', $Repo) + $A
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
    $stdoutTask = $null
    $stderrTask = $null
    $timedOut = $false
    try {
        $proc = [System.Diagnostics.Process]::Start($psi)
        # Start both .NET Framework async reads before waiting for the child so
        # neither redirected pipe can fill and block the process.
        $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
        $stderrTask = $proc.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $proc.WaitForExit($GitTimeoutMs)) {
            $timedOut = $true
            try { $proc.Kill() } catch { }
            $remaining = [Math]::Max(1, $GitTimeoutMs - [int]$clock.ElapsedMilliseconds)
            if (-not $proc.WaitForExit($remaining)) { throw "git $A did not exit after timeout termination" }
        }
        $remaining = [Math]::Max(1, $GitTimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stdoutTask.Wait($remaining)) { throw "git $A stdout read did not complete within timeout budget" }
        $remaining = [Math]::Max(1, $GitTimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stderrTask.Wait($remaining)) { throw "git $A stderr read did not complete within timeout budget" }
        if ($stdoutTask.Status -ne 'RanToCompletion') { throw "git $A stdout read failed" }
        if ($stderrTask.Status -ne 'RanToCompletion') { throw "git $A stderr read failed" }
        $stdout = $stdoutTask.Result
        $stderr = $stderrTask.Result
        $exitCode = if ($timedOut) { -1 } else { $proc.ExitCode }
    } finally {
        if ($proc) { $proc.Dispose() }
    }

    if ($timedOut) {
        throw "git $A timed out after $GitTimeoutMs ms"
    }

    # Judge by exit code, not stderr presence
    if ($exitCode -ne 0) {
        $errMsg = if ($stderr.Trim()) { $stderr.Trim() } else { '(no stderr)' }
        throw "git $A failed: exit=$exitCode, stderr: $errMsg"
    }
    # On success (exit 0), stderr is diagnostics/warnings — surface but don't treat as diff data
    if ($stderr.Trim()) {
        Write-Warning "git $A stderr (exit 0): $($stderr.Trim())"
    }
    if ([string]::IsNullOrEmpty($stdout)) { return @() }
    return @($stdout -split "`r?`n")
}
function GitShow { param([string]$P)
    # For staged files, read directly from disk to avoid PowerShell 5.1
    # pipeline encoding issues (GBK codepage mangling UTF-8 git output).
    # Staged files always exist on disk, so this is safe for -Staged mode.
    $diskPath = $P -replace '/', ([System.IO.Path]::DirectorySeparatorChar)
    if (Test-Path -LiteralPath $diskPath -PathType Leaf) {
        return [System.IO.File]::ReadAllText((Resolve-Path -LiteralPath $diskPath).Path, [System.Text.Encoding]::UTF8)
    }
    # Fallback: read from HEAD via temp file (for non-staged validation)
    $tempFile = [System.IO.Path]::GetTempFileName()
    try {
        Start-Process -FilePath "git" -ArgumentList "show","HEAD:$P" -NoNewWindow -Wait -RedirectStandardOutput $tempFile -PassThru | Out-Null
        return [System.IO.File]::ReadAllText($tempFile, [System.Text.Encoding]::UTF8)
    } finally {
        Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
    }
}
function Fail { param([string]$R)
    $script:BlockReason = $R
    throw $R
}

# ---- classification ----
function AssetOf { param([string]$P)
    if ($P -eq '.specify/memory/constitution.md') { return 'constitution' }
    if ($P -match '^docs/adr/[^/]+\.md$' -and $P -notmatch '/README\.md$') { return 'adr' }
    if ($P -match '^docs/security/exceptions/[^/]+\.md$' -and $P -notmatch '/README\.md$') { return 'security-exception' }
    if ($P -match '^specs/[0-9][^/]*/[^/]*spec\.md$') { return 'spec' }
    if ($P -match '^specs/[0-9][^/]*/[^/]*plan\.md$') { return 'plan' }
    if ($P -match '^specs/[0-9][^/]*/(acceptance|review-record)\.md$') { return 'acceptance' }
    if ($P -match '^specs/archive/.+/(archive-metadata|spec)\.md$') { return 'archive' }
    return ''
}

# Words that flag a protected governance line
$script:FlagWords = @(
    'Proposed', 'Accepted', 'Rejected', 'Superseded', 'Draft', 'Active',
    'Extended', 'Closed', 'Approved', 'Ready', 'Verified', 'Archived',
    'Pending', 'Candidate', 'Planned', 'Ratified'
)
function ProtectedLine { param([string]$L)
    $t = $L.TrimStart('+-').Trim()
    if ($t -match '^\*\*Version\*\*:') { return $true }
    if ($t -match '^\|\s*\*{0,2}Version\*{0,2}\s*\|') { return $true }
    if ($t -match '\bApproval History\b') { return $true }
    if ($t -match 'Approved Candidate') { return $true }
    # Detect Chinese governance labels: \u7248\u672C = version, \u72B6\u6001 = state
    if ($t -match '\u7248\u672C') { return $true }
    if ($t -match '\u72B6\u6001') { return $true }
    foreach ($w in $script:FlagWords) {
        if ($t -match "\b$([regex]::Escape($w))\b") { return $true }
    }
    return $false
}

# ---- diff scan ----
function ScanDiff {
    if ($Staged) { $lines = Git @('diff', '--cached', '--unified=0', '--no-ext-diff') }
    else         { $lines = Git @('diff', $BaseRef, '--unified=0', '--no-ext-diff') }

    $cp = ''
    foreach ($l in $lines) {
        if ($l -match '^diff --git a/(.+) b/') { $cp = $Matches[1]; continue }
        if ($l -match '^\+\+\+ b/(.+)$')       { $cp = $Matches[1]; continue }
        if ($l -match '^[-+]' -and $l -notmatch '^[-+]{3} ') {
            $at = AssetOf $cp
            if ($at -and (ProtectedLine $l) -and -not $script:ProtectedPaths.ContainsKey($cp)) {
                $script:ProtectedPaths[$cp] = $at
            }
        }
    }

    # Find newly-added approval JSONs
    if ($Staged) { $afiles = Git @('diff', '--cached', '--diff-filter=A', '--name-only', '--', '.specify/approvals/') }
    else         { $afiles = Git @('diff', $BaseRef, '--diff-filter=A', '--name-only', '--', '.specify/approvals/') }
    foreach ($f in $afiles) {
        if ($f -match '^\.specify/approvals/.+\.json$') { $script:ApprovalAdds += $f }
    }
}

# ---- parse approvals ----
function LoadApprovals {
    foreach ($f in $script:ApprovalAdds) {
        $raw = GitShow $f
        try   { $obj = $raw | ConvertFrom-Json }
        catch { Fail "approval '$f': not valid JSON" }

        # Validate required fields
        $req = @('schema_version','approval_id','asset_type','target_path','decision',
                 'target_version','target_state','scope','approved_by','approved_at',
                 'authorization_text','recorded_at','recorded_by')
        foreach ($k in $req) {
            $v = $obj.$k
            if ($null -eq $v -or ($v -is [string] -and $v.Trim() -eq '')) {
                Fail "approval '$f': missing/empty field '$k'"
            }
        }
        if ($obj.approved_by.Trim() -ne 'xinyan <zuijiuhjd@foxmail.com>') {
            Fail "approval '$f': approved_by must be 'xinyan <zuijiuhjd@foxmail.com>'"
        }
        $eid = [System.IO.Path]::GetFileNameWithoutExtension($f)
        if ($obj.approval_id.Trim() -ne $eid) {
            Fail "approval '$f': approval_id mismatch with filename"
        }

        $script:ApprovalRecords += [PSCustomObject]@{
            File=$f; Id=$obj.approval_id.Trim(); Type=$obj.asset_type.Trim()
            Path=$obj.target_path.Trim(); Version=$obj.target_version.Trim()
            State=$obj.target_state.Trim(); Decision=$obj.decision.Trim()
            ApprovedBy=$obj.approved_by.Trim(); AuthText=$obj.authorization_text.Trim()
        }
    }
}

# ---- doc meta extraction ----
function DocVersion { param([string]$C)
    # Only search the first 10 lines (metadata block) so table cells that
    # mention 版本 (version) later in the spec cannot steal the document version.
    $header = (($C -split "`n")[0..9]) -join "`n"
    if ($header -match '^\*\*Version\*\*:\s*`([^`]+)`') { return $Matches[1].Trim() }
    if ($header -match '\*\*version\*\*:\s*`?([0-9][0-9rc.-]+)`?') { return $Matches[1].Trim() }
    if ($header -match '\| [^|]*\u7248\u672C[^|]*\| `([^`]+)`') { return $Matches[1].Trim() }
    if ($header -match '\*\*\u7248\u672C\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') { return $Matches[1].Trim() }
    return ''
}
function DocState { param([string]$C)
    # Only search the first 10 lines (metadata block) to avoid greedy matching
    # into Approval History tables deeper in the document.
    $header = (($C -split "`n")[0..9]) -join "`n"
    # Prefer explicit metadata labels in the header block
    if ($header -match '\*\*\u72B6\u6001\*\*[：:]\s*([^\n`*]+)') { return ($Matches[1].Trim() -replace '[`*]','') }
    if ($header -match '(?m)^>\s*\*\*\u72B6\u6001\*\*[：:]\s*([^\n`*]+)') { return ($Matches[1].Trim() -replace '[`*]','') }
    if ($header -match '\*\*Status\*\*:\s*(.+)') { return ($Matches[1].Trim() -replace '[`*]','') }
    # Table row only (for specs without blockquote metadata): | 状态 | value |
    if ($header -match '\|\s*\*{0,2}\u72B6\u6001\*{0,2}\s*\|\s*([^|\n]+)\s*\|') { return ($Matches[1].Trim() -replace '[`*]','') }
    return ''
}

# ---- validation loop ----
function ValidateAll {
    $seenIds = @{}
    foreach ($r in $script:ApprovalRecords) {
        if ($seenIds.ContainsKey($r.Id)) { Fail "duplicate approval_id '$($r.Id)'" }
        $seenIds[$r.Id] = $r.File
    }

    foreach ($path in $script:ProtectedPaths.Keys) {
        $atype = $script:ProtectedPaths[$path]
        $matches = @($script:ApprovalRecords | Where-Object { $_.Path -eq $path -and $_.Type -eq $atype })
        if ($matches.Count -eq 0) { Fail "protected change '$path' ($atype): no matching new approval record" }
        if ($matches.Count -gt 1) { Fail "protected change '$path' ($atype): $($matches.Count) approvals, need exactly 1" }
        $rec = $matches[0]

        $content = GitShow $path
        $dver = DocVersion $content
        $dstate = DocState $content

        if ($dver -and $dver -ne $rec.Version) {
            Fail "protected change '$path': doc version '$dver' != approval target_version '$($rec.Version)'"
        }
        if ($atype -ne 'constitution' -and $dstate -and $dstate -ne $rec.State) {
            Fail "protected change '$path': doc state '$dstate' != approval target_state '$($rec.State)'"
        }
        if ($atype -eq 'constitution') {
            if ($rec.Decision -eq 'approve-ratification') {
                # Ratification approval explicitly allows changing Ratified from Pending
                if ($content -notmatch '\*{0,2}Ratified\*{0,2}\s*:\s*[0-9]{4}-[0-9]{2}-[0-9]{2}') {
                    Fail "Constitution ratification requires Ratified date; approval '$($rec.Id)'"
                }
            } elseif ($content -notmatch '\*{0,2}Ratified\*{0,2}\s*:\s*Pending') {
                Fail "Constitution Ratified field must stay Pending; approval '$($rec.Id)' does not ratify 1.0.0"
            }
        }
        if ($content -notmatch [regex]::Escape($rec.Id)) {
            Fail "protected change '$path': doc Approval History missing approval_id '$($rec.Id)'"
        }
    }
}

# === DRIVER ===
try {
    ScanDiff
    if ($script:ProtectedPaths.Count -eq 0) {
        $msg = 'APPROVAL_GATE_PASSED: 0 protected change(s) validated'
        if ($ResultFile) { "PASS`t$msg" | Out-File $ResultFile -Encoding UTF8 } else { Write-Output $msg }
        exit 0
    }
    LoadApprovals
    ValidateAll
    $msg = "APPROVAL_GATE_PASSED: $($script:ProtectedPaths.Count) protected change(s) validated"
    if ($ResultFile) { "PASS`t$msg" | Out-File $ResultFile -Encoding UTF8 } else { Write-Output $msg }
    exit 0
} catch {
    $reason = if ($script:BlockReason) { $script:BlockReason } else { $_.Exception.Message }
    if ($ResultFile) {
        "BLOCK`tAPPROVAL_GATE_BLOCKED: $reason" | Out-File $ResultFile -Encoding UTF8
        exit 0
    } else {
        [Console]::Error.WriteLine("APPROVAL_GATE_BLOCKED: $reason")
        exit 1
    }
}
