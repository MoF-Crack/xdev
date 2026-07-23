# Bounded filesystem cleanup helper for governance simulation. Windows PowerShell 5.1.
# Validates that RunRoot is a regular directory (not a reparse point/junction/symlink),
# verifies an ownership marker, and re-verifies the path before deletion.
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$RepoRoot,
    [Parameter(Mandatory=$true)][string]$RunRoot,
    [switch]$RemoveParent
)
$ErrorActionPreference = 'Stop'

function Normalize-ExistingPath {
    param([string]$Path,[string]$Name)
    $resolved = Resolve-Path -LiteralPath $Path -ErrorAction Stop
    $full = [IO.Path]::GetFullPath($resolved.Path)
    $root = [IO.Path]::GetPathRoot($full)
    if ($full.Length -gt $root.Length) { $full = $full.TrimEnd('\') }
    return $full
}

function Test-IsRegularDirectory {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) { return $false }
    try {
        $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
        # Reject reparse points (junctions, symlinks, mount points)
        if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { return $false }
        return $true
    } catch { return $false }
}

$repo = Normalize-ExistingPath $RepoRoot 'RepoRoot'
$run = Normalize-ExistingPath $RunRoot 'RunRoot'
$ci = [IO.Path]::Combine($repo, 'build', 'test-temp', 'ci')
$expectedPattern = '^' + [regex]::Escape($ci.TrimEnd('\')) + '\\run-[0-9a-f]{32}$'
if ($run -notmatch $expectedPattern) { throw "unsafe RunRoot rejected: $run" }

$parent = Split-Path -Parent $run
if ($parent -ne $ci.TrimEnd('\')) { throw "unsafe RunRoot parent rejected: $parent" }

# Validate RepoRoot is a regular directory
if (-not (Test-IsRegularDirectory $repo)) { throw "RepoRoot is not a regular directory: $repo" }

# Validate ci parent is a regular directory (if it exists)
if (Test-Path -LiteralPath $parent -PathType Container) {
    if (-not (Test-IsRegularDirectory $parent)) { throw "ci parent is not a regular directory: $parent" }
}

# Validate RunRoot is a regular directory and not a reparse point
if (-not (Test-IsRegularDirectory $run)) { throw "RunRoot is not a regular directory or is a reparse point: $run" }

# Verify ownership marker file exists inside the run root with exact content
# The marker is created by the simulator when creating the run root
$markerPath = [IO.Path]::Combine($run, '.ci-ownership-marker')
if (-not (Test-Path -LiteralPath $markerPath -PathType Leaf)) { throw "RunRoot ownership marker missing: $run" }
$markerContent = Get-Content -LiteralPath $markerPath -Raw -ErrorAction Stop
# Must be exactly "CiGovernanceRunRoot" — no extra whitespace, no multiline, no BOM
$expectedMarker = 'CiGovernanceRunRoot'
if ($markerContent -ne $expectedMarker -and $markerContent -ne ($expectedMarker + "`r`n") -and $markerContent -ne ($expectedMarker + "`n")) {
    throw "RunRoot ownership marker content invalid: '$([string]::Join("`n", @($markerContent.Split("`n") | Where-Object { $_ -ne '' })))' (expected '$expectedMarker')"
}

# Re-verify the path is still a regular directory before deletion (TOCTOU mitigation)
# Re-resolve the path to detect any reparse point substitution
$reResolved = Normalize-ExistingPath $RunRoot 'RunRoot'
if ($reResolved -ne $run) { throw "RunRoot path changed between validation and deletion: was '$run', now '$reResolved'" }
if (-not (Test-IsRegularDirectory $reResolved)) { throw "RunRoot is no longer a regular directory: $reResolved" }
if ($reResolved -notmatch $expectedPattern) { throw "RunRoot no longer matches expected pattern: $reResolved" }

Remove-Item -LiteralPath $reResolved -Recurse -Force -ErrorAction Stop
if ($RemoveParent -and (Test-Path -LiteralPath $parent -PathType Container)) {
    if (-not (Test-IsRegularDirectory $parent)) { throw "ci parent is not a regular directory before removal check: $parent" }
    if (@(Get-ChildItem -LiteralPath $parent -Force -ErrorAction Stop).Count -eq 0) {
        Remove-Item -LiteralPath $parent -Force -ErrorAction Stop
    }
}