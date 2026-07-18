# scripts/docs/validate-approvals.ps1
# XDev governance approval gate.
[CmdletBinding(DefaultParameterSetName='Staged')]
param(
    [string]$Repo = (Get-Location).Path,
    [Parameter(ParameterSetName='Staged')] [switch]$Staged,
    [Parameter(Mandatory=$true, ParameterSetName='BaseRef')] [string]$BaseRef,
    [string]$ResultFile = ''
)

$ErrorActionPreference = 'Stop'
$script:git = (Get-Command git -ErrorAction Stop).Source
$script:ProtectedPaths = @{}
$script:ApprovalAdds = @()
$script:ApprovalRecords = @()
$script:BlockReason = ''

# ---- helpers ----
function Git { param([string[]]$A)
    [array]$out = & $script:git -C $Repo @A 2>&1
    if ($LASTEXITCODE -ne 0) { throw "git $A failed: exit=$LASTEXITCODE" }
    return $out
}
function GitShow { param([string]$P)
    $out = Git @('show', ":$P")
    return ($out -join "`n")
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
    if ($C -match '^\*\*Version\*\*:\s*`([^`]+)`') { return $Matches[1].Trim() }
    if ($C -match '\*\*version\*\*:\s*`?([0-9][0-9rc.-]+)`?') { return $Matches[1].Trim() }
    if ($C -match '\| [^|]*\u7248\u672C[^|]*\| `([^`]+)`') { return $Matches[1].Trim() }
    if ($C -match '\*\*\u7248\u672C\*\*[：:]\s*`?([0-9][^`\s\n]*)`?') { return $Matches[1].Trim() }
    return ''
}
function DocState { param([string]$C)
    if ($C -match '\u72B6\u6001[^|]*\| ([^|]+) \|') { return ($Matches[1].Trim() -replace '[`*]','') }
    if ($C -match '\*\*\u72B6\u6001\*\*[：:]\s*([^\n`*]+)') { return ($Matches[1].Trim() -replace '[`*]','') }
    if ($C -match '\*\*Status\*\*:\s*(.+)') { return ($Matches[1].Trim() -replace '[`*]','') }
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
