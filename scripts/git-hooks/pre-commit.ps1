# scripts/git-hooks/pre-commit.ps1
# XDev governance approval gate pre-commit hook.
$ErrorActionPreference = 'Stop'
$repoRoot = (& git rev-parse --show-toplevel)
if ($LASTEXITCODE -ne 0 -or -not $repoRoot) {
    [Console]::Error.WriteLine('APPROVAL_GATE_BLOCKED: cannot resolve repository root')
    exit 1
}
$repoRoot = $repoRoot.Trim()
$validator = Join-Path $repoRoot 'scripts\docs\validate-approvals.ps1'
if (-not (Test-Path -LiteralPath $validator -PathType Leaf)) {
    [Console]::Error.WriteLine('APPROVAL_GATE_BLOCKED: validator not found at scripts\docs\validate-approvals.ps1')
    exit 1
}
& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $validator -Repo $repoRoot -Staged
exit $LASTEXITCODE