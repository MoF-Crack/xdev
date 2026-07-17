# scripts/install-git-hooks.ps1
# Configures core.hooksPath to use the versioned hooks in scripts/git-hooks/.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$repoRoot = (& git rev-parse --show-toplevel)
if ($LASTEXITCODE -ne 0 -or -not $repoRoot) { throw 'Not inside a Git repository' }
$repoRoot = $repoRoot.Trim()
& git -C $repoRoot config --local core.hooksPath scripts/git-hooks
if ($LASTEXITCODE -ne 0) { throw 'Failed to configure core.hooksPath' }
$configured = (& git -C $repoRoot config --local --get core.hooksPath).Trim()
if ($configured -ne 'scripts/git-hooks') { throw "Unexpected core.hooksPath: $configured" }
Write-Output 'Configured core.hooksPath=scripts/git-hooks'