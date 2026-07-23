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

function Invoke-ProcessCapture {
    param([string]$FileName, [string]$Arguments, [int]$TimeoutMs = 60000)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $FileName
    $psi.Arguments = $Arguments
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $proc = $null
    $exitCode = $null
    try {
        try { $proc = [System.Diagnostics.Process]::Start($psi) } catch { throw "process start failed: $_" }
        $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
        $stderrTask = $proc.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $proc.WaitForExit($remaining)) {
            try { $proc.Kill() } catch { }
            $killWait = [Math]::Min(1000, [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds))
            if (-not $proc.WaitForExit($killWait)) { throw "process did not exit after timeout termination" }
            throw "process timed out after $TimeoutMs ms"
        }
        $exitCode = $proc.ExitCode
        $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stdoutTask.Wait($remaining) -or $stdoutTask.Status -ne 'RanToCompletion') { throw 'stdout read failed or did not complete' }
        $remaining = [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)
        if (-not $stderrTask.Wait($remaining) -or $stderrTask.Status -ne 'RanToCompletion') { throw 'stderr read failed or did not complete' }
        try {
            $stdout = $stdoutTask.Result
            $stderr = $stderrTask.Result
        } catch { throw "process output read faulted: $_" }
        return [PSCustomObject]@{ ExitCode = $exitCode; StdOut = $stdout; StdErr = $stderr }
    } finally {
        if ($proc) { $proc.Dispose() }
    }
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
    $quotedCommand = '"' + ($childCommand -replace '"', '\"') + '"'
    $processResult = Invoke-ProcessCapture -FileName ((Get-Command powershell.exe -ErrorAction Stop).Source) `
        -Arguments "-NoProfile -ExecutionPolicy Bypass -Command $quotedCommand" -TimeoutMs 60000
    $exitCode = $processResult.ExitCode
    $resultOutput = (($processResult.StdOut + "`n" + $processResult.StdErr).Trim())
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

function Test-RatificationChain {
    param([string]$ConstitutionVer, [string]$SpecRefVer, [string]$TargetPath)
    $approvalsDir = Join-Path $script:repoRoot '.specify\approvals'
    if (-not (Test-Path -LiteralPath $approvalsDir -PathType Container)) {
        return "no approvals directory; ratification chain required for spec ref '$SpecRefVer' vs constitution '$ConstitutionVer'"
    }
    $approvalFiles = @(Get-ChildItem -LiteralPath $approvalsDir -Filter '*.json' -File -ErrorAction SilentlyContinue)
    if ($approvalFiles.Count -eq 0) {
        return "no approval records; ratification chain required for spec ref '$SpecRefVer' vs constitution '$ConstitutionVer'"
    }
    $records = @()
    $seenIds = @{}
    foreach ($af in $approvalFiles) {
        try {
            $raw = Get-Content -LiteralPath $af.FullName -Raw -Encoding utf8
            # Check for empty content
            if (-not $raw.Trim()) {
                return "approval JSON '$($af.Name)' is empty; malformed"
            }
            $obj = $raw | ConvertFrom-Json
            $id = [string]$obj.approval_id
            if (-not $id) {
                return "approval JSON '$($af.Name)': approval_id field missing or empty"
            }
            $idTrimmed = $id.Trim()
            # Check for duplicate approval_id
            if ($seenIds.ContainsKey($idTrimmed)) {
                return "duplicate approval_id '$idTrimmed' found in '$($af.Name)' (previously in '$($seenIds[$idTrimmed])')"
            }
            $seenIds[$idTrimmed] = $af.Name
            $records += [PSCustomObject]@{
                File = $af.Name
                Id = $idTrimmed
                Type = ([string]$obj.asset_type).Trim()
                Path = ([string]$obj.target_path).Trim()
                Version = ([string]$obj.target_version).Trim()
                Decision = ([string]$obj.decision).Trim()
                ApprovedBy = ([string]$obj.approved_by).Trim()
                Supersedes = @([string[]]$obj.supersedes)
            }
        } catch {
            return "approval JSON '$($af.Name)' is malformed and cannot be parsed: $_"
        }
    }
    # Find ratification record for this constitution version
    $ratifications = @($records | Where-Object {
        $_.Type -eq 'constitution' -and
        $_.Decision -eq 'approve-ratification' -and
        $_.Version -eq $ConstitutionVer -and
        $_.Path -eq $TargetPath
    })
    if ($ratifications.Count -eq 0) {
        return "no ratification record found for constitution '$ConstitutionVer' at '$TargetPath'"
    }
    if ($ratifications.Count -gt 1) {
        return "multiple ratification records for '$ConstitutionVer'; expected 1"
    }
    $rat = $ratifications[0]
    if ($rat.ApprovedBy -ne 'xinyan <zuijiuhjd@foxmail.com>') {
        return "ratification record '$($rat.Id)': approved_by must be 'xinyan <zuijiuhjd@foxmail.com>', got '$($rat.ApprovedBy)'"
    }
    # Check supersedes chain: must have at least one superseded ID
    $supArray = @($rat.Supersedes | Where-Object { $_ -and $_.Trim() })
    if ($supArray.Count -eq 0) {
        return "ratification record '$($rat.Id)': supersedes is empty; must reference rc approval(s)"
    }
    # At least one superseded record must match SpecRefVer with valid fields
    $hasMatch = $false
    $allErrors = @()
    foreach ($supId in $supArray) {
        $supIdClean = ($supId -replace '[`"\[\]]','').Trim()
        if (-not $supIdClean) { continue }
        $rcMatches = @($records | Where-Object { $_.Id -eq $supIdClean })
        if ($rcMatches.Count -eq 0) {
            $allErrors += "superseded record '$supIdClean' not found"
            continue
        }
        if ($rcMatches.Count -gt 1) {
            $allErrors += "superseded record '$supIdClean': duplicate approval_id found"
            continue
        }
        $rc = $rcMatches[0]
        $rcOk = $true
        if ($rc.Type -ne 'constitution') {
            $allErrors += "superseded record '$supIdClean': asset_type must be 'constitution', got '$($rc.Type)'"
            $rcOk = $false
        }
        if ($rc.Decision -ne 'approve-candidate') {
            $allErrors += "superseded record '$supIdClean': decision must be 'approve-candidate', got '$($rc.Decision)'"
            $rcOk = $false
        }
        if ($rc.Path -ne $TargetPath) {
            $allErrors += "superseded record '$supIdClean': target_path '$($rc.Path)' != expected '$TargetPath'"
            $rcOk = $false
        }
        if ($rc.ApprovedBy -ne 'xinyan <zuijiuhjd@foxmail.com>') {
            $allErrors += "superseded record '$supIdClean': approved_by must be 'xinyan <zuijiuhjd@foxmail.com>', got '$($rc.ApprovedBy)'"
            $rcOk = $false
        }
        # Check if this rc record matches SpecRefVer
        if ($rcOk -and $rc.Version -eq $SpecRefVer) {
            $hasMatch = $true
        }
    }
    if (-not $hasMatch) {
        if ($allErrors.Count -gt 0) {
            return "no superseded record with version '$SpecRefVer' and all valid fields found. Errors: $($allErrors -join '; ')"
        } else {
            return "no superseded record with target_version '$SpecRefVer' found; superseded versions: $($supArray -join ', ')"
        }
    }
    return ''  # No issue = chain valid
}

function Invoke-CrossDocChecks {
    Write-SectionHeader 'Cross-Document Consistency'
    $issues = @()

    $constitutionPath = Join-Path $script:repoRoot '.specify\memory\constitution.md'
    $constitutionVer = ''
    if (Test-Path -LiteralPath $constitutionPath) {
        $cc = ''
        try { $cc = Get-Content -LiteralPath $constitutionPath -Raw -Encoding utf8 } catch { $cc = '' }
        # Table format: | Version | `X.Y.Z` | or | \u7248\u672C | `X.Y.Z` |
        if ($cc -match '\|\s*\*{0,2}(?:\u7248\u672C|Version)\*{0,2}\s*\|\s*`?([0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?)`?\s*\|') {
            $constitutionVer = $Matches[1]
        }
        # Bold format: **Version**: `X.Y.Z` or **\u7248\u672C**\uFF1A`X.Y.Z`
        elseif ($cc -match '\*\*(?:\u7248\u672C|Version)\*\*[：:]\s*`?([0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?)`?') {
            $constitutionVer = $Matches[1]
        }
    }
    $isConstitutionFinal = ($constitutionVer -and $constitutionVer -notmatch '-rc\.')
    $constitutionBase = if ($constitutionVer -match '^(\d+\.\d+\.\d+)') { $Matches[1] } else { '' }

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
        if ($sc -match 'Constitution.*?`?([0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[0-9]+)?)') {
            $specRefRc = $Matches[1]
        }
        if ($specRefRc) {
            if (-not $constitutionVer) {
                $dn = Split-Path -Leaf $dir.FullName
                $issues += "Constitution version unparseable; spec '$dn' refs '$specRefRc' cannot be validated"
            } elseif ($specRefRc -eq $constitutionVer) {
                # Exact match: PASS
            } else {
                $specRefBase = if ($specRefRc -match '^(\d+\.\d+\.\d+)') { $Matches[1] } else { '' }
                $specIsRc = ($specRefRc -match '-rc\.')
                if ($specRefBase -ne $constitutionBase) {
                    $dn = Split-Path -Leaf $dir.FullName
                    $issues += "Cross-ref mismatch: spec '$dn' refs Constitution '$specRefRc' but actual is '$constitutionVer' (different base version)"
                } elseif ($isConstitutionFinal -and $specIsRc) {
                    # Constitution is final, spec refs an rc -> needs ratification chain
                    $chainIssue = Test-RatificationChain -ConstitutionVer $constitutionVer `
                        -SpecRefVer $specRefRc -TargetPath '.specify/memory/constitution.md'
                    if ($chainIssue) {
                        $dn = Split-Path -Leaf $dir.FullName
                        $issues += "Ratification chain invalid: spec '$dn' refs rc '$specRefRc' vs constitution '$constitutionVer': $chainIssue"
                    }
                } else {
                    $dn = Split-Path -Leaf $dir.FullName
                    $issues += "Cross-ref mismatch: spec '$dn' refs Constitution '$specRefRc' but actual is '$constitutionVer'"
                }
            }
        } else {
            # No valid semver version found. Check if there's a constitution ref at all.
            if ($sc -match 'Constitution') {
                $dn = Split-Path -Leaf $dir.FullName
                if ($sc -match 'Constitution.*?(?:TBD|TBA|TODO)') {
                    $issues += "Spec '$dn': Constitution ref version is TBD; malformed"
                } else {
                    $issues += "Spec '$dn': Constitution ref version missing or malformed"
                }
            } else {
                $dn = Split-Path -Leaf $dir.FullName
                $issues += "Spec '$dn': missing Constitution reference; cannot validate cross-doc consistency"
            }
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
    $selfRepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
    $govStRoot = Join-Path $selfRepoRoot 'build\test-temp\governance-selftest'
    # Path escape protection. The run root must be exactly one run-<guid> child.
    $normalizedGovParent = [System.IO.Path]::GetFullPath($govStRoot).TrimEnd('\')
    $normalizedSelfRepo = [System.IO.Path]::GetFullPath($selfRepoRoot).TrimEnd('\')
    if ($normalizedGovParent -eq $normalizedSelfRepo -or
        -not $normalizedGovParent.StartsWith($normalizedSelfRepo + '\', [StringComparison]::OrdinalIgnoreCase)) {
        Write-Host "FATAL: Path escape: $normalizedGovParent not under $normalizedSelfRepo" -ForegroundColor Red
        exit 1
    }
    $parentCreated = -not (Test-Path -LiteralPath $normalizedGovParent -PathType Container)
    if ($parentCreated) { New-Item -ItemType Directory -Path $normalizedGovParent -Force | Out-Null }
    $td = Join-Path $normalizedGovParent ('run-' + [guid]::NewGuid().ToString('N'))
    $normalizedTd = [System.IO.Path]::GetFullPath($td).TrimEnd('\')
    if ([System.IO.Path]::GetDirectoryName($normalizedTd).TrimEnd('\') -ne $normalizedGovParent -or
        $normalizedTd -notmatch '\\run-[0-9a-f]+$') {
        throw "Unsafe SelfTest run root: $normalizedTd"
    }
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
        if (Test-Path -LiteralPath $normalizedTd) {
            Remove-Item -Recurse -Force -LiteralPath $normalizedTd -ErrorAction SilentlyContinue
        }
        if ($parentCreated -and (Test-Path -LiteralPath $normalizedGovParent -PathType Container) -and
            @(Get-ChildItem -LiteralPath $normalizedGovParent -Force -ErrorAction SilentlyContinue).Count -eq 0) {
            Remove-Item -Force -LiteralPath $normalizedGovParent -ErrorAction SilentlyContinue
        }
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
        $govRunParent = Join-Path $script:repoRoot 'build\test-temp\governance-run'
        $govRunParentCreated = -not (Test-Path -LiteralPath $govRunParent -PathType Container)
        if ($govRunParentCreated) { New-Item -ItemType Directory -Path $govRunParent -Force | Out-Null }
        $govRunRoot = Join-Path $govRunParent ('run-' + [guid]::NewGuid().ToString('N'))
        # Path escape protection
        $normalizedRunParent = [System.IO.Path]::GetFullPath($govRunParent).TrimEnd('\')
        $normalizedRun = [System.IO.Path]::GetFullPath($govRunRoot).TrimEnd('\')
        $normalizedRepo = [System.IO.Path]::GetFullPath($script:repoRoot)
        if ($normalizedRunParent -eq $normalizedRepo -or
            -not $normalizedRunParent.StartsWith($normalizedRepo.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase) -or
            [System.IO.Path]::GetDirectoryName($normalizedRun).TrimEnd('\') -ne $normalizedRunParent -or
            $normalizedRun -notmatch '\\run-[0-9a-f]+$') {
            Write-Host "  [FAIL] Path escape: $normalizedRun not under $normalizedRepo" -ForegroundColor Red
            $script:reportSections['approvals'] = @{ Status = 'FAIL'; Output = 'Path escape prevented' }
            $script:hasFailures = $true
        } else {
            New-Item -ItemType Directory -Path $normalizedRun -Force | Out-Null
            $tr = Join-Path $govRunRoot 'gov-approval-result.txt'
            try {
                Invoke-ChildValidator -ScriptPath $appScript -Arguments @{ BaseRef = 'HEAD~1'; ResultFile = $tr } -Label 'approvals'
            } finally {
                if (Test-Path -LiteralPath $normalizedRun) {
                    Remove-Item -Recurse -Force -LiteralPath $normalizedRun -ErrorAction SilentlyContinue
                }
                if ($govRunParentCreated -and (Test-Path -LiteralPath $normalizedRunParent -PathType Container) -and
                    @(Get-ChildItem -LiteralPath $normalizedRunParent -Force -ErrorAction SilentlyContinue).Count -eq 0) {
                    Remove-Item -Force -LiteralPath $normalizedRunParent -ErrorAction SilentlyContinue
                }
            }
        }
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
