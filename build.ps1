# XDev single binary build (Wails 3). Requires pinned wails3 from go.mod.
# Usage: .\build.ps1 [-SkipBuild] [-SkipAudit]
param(
    [switch]$SkipBuild,
    [switch]$SkipAudit
)
$ErrorActionPreference = 'Stop'
$argsList = @((Join-Path $PSScriptRoot 'scripts\build.mjs'))
if ($SkipBuild) { $argsList += '--skip-build' }
if ($SkipAudit) { $argsList += '--skip-audit' }
& node @argsList
exit $LASTEXITCODE
