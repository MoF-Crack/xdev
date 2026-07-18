# scripts/docs/tests/validate-links.tests.ps1
#
# Self-verification tests for validate-links.ps1.
# Drives the validator against fixture directories and checks exit codes
# and error counts.
#
# Usage:
#   powershell -File scripts/docs/tests/validate-links.tests.ps1
#
# Exit codes: 0 = all tests pass, 1 = test failure.

$ErrorActionPreference = 'Stop'
$script:failures = 0
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$validator = Resolve-Path (Join-Path $scriptDir '..\validate-links.ps1')

function Assert-ExitCode {
    param([int]$Actual, [int]$Expected, [string]$Case)
    if ($Actual -ne $Expected) {
        Write-Host "  [FAIL] $Case : exit=$Actual expected=$Expected" -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host "  [OK]   $Case : exit=$Actual" -ForegroundColor Green
    }
}

function Assert-MinErrors {
    param([int]$Actual, [int]$MinExpected, [string]$Case)
    if ($Actual -lt $MinExpected) {
        Write-Host "  [FAIL] $Case : errors=$Actual expected>=$MinExpected" -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host "  [OK]   $Case : errors=$Actual" -ForegroundColor Green
    }
}

# --- Test Cases ---

Write-Host ''
Write-Host '=== validate-links.ps1 Self-Verification Tests ===' -ForegroundColor Cyan

# Case 1: valid/ dir - all links valid
Write-Host ''
Write-Host 'Case 1: valid/ fixture (should pass)' -ForegroundColor Yellow
$validDir = Join-Path $scriptDir 'fixtures\links\valid'
$result = & powershell -NoProfile -File $validator -Path $validDir 2>&1
$exit1 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit1 0 'valid/ fixture'

# Case 2: invalid/ dir - should find errors
Write-Host ''
Write-Host 'Case 2: invalid/ fixture (should fail)' -ForegroundColor Yellow
$invalidDir = Join-Path $scriptDir 'fixtures\links\invalid'
$result = & powershell -NoProfile -File $validator -Path $invalidDir 2>&1
$exit2 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit2 1 'invalid/ fixture'

# Case 3: -Report JSON output
Write-Host ''
Write-Host 'Case 3: -Report JSON output' -ForegroundColor Yellow
$reportPath = Join-Path $env:TEMP 'xdev-link-validation-test-report.json'
$result = & powershell -NoProfile -File $validator -Path $invalidDir -Report $reportPath 2>&1
Write-Host $result

if (-not (Test-Path -LiteralPath $reportPath)) {
    Write-Host "  [FAIL] Report not generated: $reportPath" -ForegroundColor Red
    $script:failures++
} else {
    $reportContent = Get-Content -LiteralPath $reportPath -Raw -Encoding utf8
    try {
        $reportObj = $reportContent | ConvertFrom-Json
        if ($reportObj -is [array] -and $reportObj.Count -gt 0) {
            Write-Host "  [OK]   JSON report generated with $($reportObj.Count) entries" -ForegroundColor Green
        } else {
            Write-Host "  [FAIL] JSON report is empty or not an array" -ForegroundColor Red
            $script:failures++
        }
    } catch {
        Write-Host "  [FAIL] JSON parse error: $_" -ForegroundColor Red
        $script:failures++
    }
}
Remove-Item -LiteralPath $reportPath -Force -ErrorAction SilentlyContinue

# Case 4: -SelfTest internal validation
Write-Host ''
Write-Host 'Case 4: -SelfTest internal validation' -ForegroundColor Yellow
$result = & powershell -NoProfile -File $validator -SelfTest 2>&1
$exit4 = $LASTEXITCODE
Write-Host $result
Assert-ExitCode $exit4 0 'SelfTest'

# --- Summary ---
Write-Host ''
if ($script:failures -gt 0) {
    Write-Host "Tests FAILED: $script:failures error(s)" -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed (4/4)" -ForegroundColor Green
exit 0
