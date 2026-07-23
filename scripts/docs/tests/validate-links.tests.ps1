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
$script:executed = 0
$script:passed = 0
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$validator = Resolve-Path (Join-Path $scriptDir '..\validate-links.ps1')
$repoRoot = (Resolve-Path (Join-Path $scriptDir '..\..\..')).Path
$linksParent = Join-Path $repoRoot 'build\test-temp\links'
$linksParentExisted = Test-Path -LiteralPath $linksParent -PathType Container
$linksTempRoot = Join-Path $linksParent ('run-' + [guid]::NewGuid().ToString('N'))
# Path escape protection
$normalizedLinksParent = [System.IO.Path]::GetFullPath($linksParent).TrimEnd('\')
$normalizedLinksRoot = [System.IO.Path]::GetFullPath($linksTempRoot).TrimEnd('\')
$normalizedRepoRoot = [System.IO.Path]::GetFullPath($repoRoot).TrimEnd('\')
if ($normalizedLinksParent -eq $normalizedRepoRoot -or
    -not $normalizedLinksParent.StartsWith($normalizedRepoRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
    [System.IO.Path]::GetDirectoryName($normalizedLinksRoot).TrimEnd('\') -ne $normalizedLinksParent -or
    $normalizedLinksRoot -notmatch '\\run-[0-9a-f]+$' -or
    (Test-Path -LiteralPath $normalizedLinksRoot)) {
    Write-Host "FATAL: Path escape detected: $normalizedLinksRoot not under $normalizedRepoRoot" -ForegroundColor Red
    exit 1
}
$linksParentCreated = -not (Test-Path -LiteralPath $normalizedLinksParent -PathType Container)
if ($linksParentCreated) { New-Item -ItemType Directory -Path $normalizedLinksParent -Force | Out-Null }
# Safe cleanup and creation
New-Item -ItemType Directory -Path $normalizedLinksRoot -Force | Out-Null

function Invoke-BoundedProcess {
    param([string]$FileName, [string]$Arguments, [int]$TimeoutMs = 60000)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $FileName; $psi.Arguments = $Arguments
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $proc = $null; $exitCode = $null
    try {
        try { $proc = [System.Diagnostics.Process]::Start($psi) } catch { throw "process start failed: $_" }
        $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
        $stderrTask = $proc.StandardError.ReadToEndAsync()
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        if (-not $proc.WaitForExit([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds))) {
            try { $proc.Kill() } catch { }
            $killWait = [Math]::Min(1000, [Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds))
            if (-not $proc.WaitForExit($killWait)) { throw 'process did not exit after timeout termination' }
            throw "process timed out after $TimeoutMs ms"
        }
        $exitCode = $proc.ExitCode
        if (-not $stdoutTask.Wait([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)) -or $stdoutTask.Status -ne 'RanToCompletion') { throw 'stdout read failed or did not complete' }
        if (-not $stderrTask.Wait([Math]::Max(1, $TimeoutMs - [int]$clock.ElapsedMilliseconds)) -or $stderrTask.Status -ne 'RanToCompletion') { throw 'stderr read failed or did not complete' }
        try { $stdout = $stdoutTask.Result; $stderr = $stderrTask.Result } catch { throw "process output read faulted: $_" }
        return [PSCustomObject]@{ ExitCode = $exitCode; Output = (($stdout + "`n" + $stderr).Trim()) }
    } finally { if ($proc) { $proc.Dispose() } }
}
function Invoke-LinkValidator {
    param([string[]]$Arguments)
    $quote = { param([string]$Value) '"' + $Value.Replace('"', '\"') + '"' }
    $argText = '-NoProfile -ExecutionPolicy Bypass -File ' + (& $quote ([string]$validator)) + ' ' + (($Arguments | ForEach-Object { & $quote ([string]$_) }) -join ' ')
    Invoke-BoundedProcess -FileName ((Get-Command powershell.exe -ErrorAction Stop).Source) -Arguments $argText
}

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
function Start-Case { $script:executed++ ; return $script:failures }
function End-Case { param([int]$Before, [string]$Name)
    if ($script:failures -eq $Before) { $script:passed++ }
}

# --- Test Cases ---

Write-Host ''
Write-Host '=== validate-links.ps1 Self-Verification Tests ===' -ForegroundColor Cyan

try {

# Case 1: valid/ dir - all links valid
Write-Host ''
Write-Host 'Case 1: valid/ fixture (should pass)' -ForegroundColor Yellow
$caseStart = Start-Case
$validDir = Join-Path $scriptDir 'fixtures\links\valid'
$validMarkdown = @(Get-ChildItem -LiteralPath $validDir -Filter '*.md' -File)
if ($validMarkdown.Count -eq 0) { throw "Case 1 valid fixture has no Markdown files: $validDir" }
$result = Invoke-LinkValidator @('-Path', $validDir, '-Exclude', '__NO_SUCH_PATTERN__')
$exit1 = $result.ExitCode
Write-Host $result.Output
Assert-ExitCode $exit1 0 'valid/ fixture'
End-Case $caseStart 'Case 1'

# Case 2: invalid/ dir - should find errors
Write-Host ''
Write-Host 'Case 2: invalid/ fixture (should fail)' -ForegroundColor Yellow
$caseStart = Start-Case
$invalidDir = Join-Path $scriptDir 'fixtures\links\invalid'
$result = Invoke-LinkValidator @('-Path', $invalidDir, '-Exclude', '__NO_SUCH_PATTERN__')
$exit2 = $result.ExitCode
Write-Host $result.Output
Assert-ExitCode $exit2 1 'invalid/ fixture'
End-Case $caseStart 'Case 2'

# Case 3: -Report JSON output
Write-Host ''
Write-Host 'Case 3: -Report JSON output' -ForegroundColor Yellow
$caseStart = Start-Case
$reportPath = Join-Path $linksTempRoot 'xdev-link-validation-test-report.json'
$result = Invoke-LinkValidator @('-Path', $invalidDir, '-Report', $reportPath, '-Exclude', '__NO_SUCH_PATTERN__')
$exit3 = $result.ExitCode
Write-Host $result.Output
Assert-ExitCode $exit3 1 'JSON report validator exit code'

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
End-Case $caseStart 'Case 3'

# Case 4: -SelfTest internal validation
Write-Host ''
Write-Host 'Case 4: -SelfTest internal validation' -ForegroundColor Yellow
$caseStart = Start-Case
$result = Invoke-LinkValidator @('-SelfTest')
$exit4 = $result.ExitCode
Write-Host $result.Output
Assert-ExitCode $exit4 0 'SelfTest'
End-Case $caseStart 'Case 4'

# Case 5: CJK anchor support (should pass - Unicode anchors preserved)
Write-Host ''
Write-Host 'Case 5: CJK anchor support' -ForegroundColor Yellow
$caseStart = Start-Case
$cjkDir = Join-Path $linksTempRoot 'cjk-case'
New-Item -ItemType Directory -Path $cjkDir -Force | Out-Null
$cjkAnchorFixture = Join-Path $scriptDir 'fixtures\links\valid\cjk-anchors.md'
$cjkTargetFixture = Join-Path $scriptDir 'fixtures\links\valid\cjk-target.md'
if (-not (Test-Path -LiteralPath $cjkAnchorFixture -PathType Leaf) -or -not (Test-Path -LiteralPath $cjkTargetFixture -PathType Leaf)) {
    throw 'Case 5 CJK fixtures are missing'
}
Copy-Item -LiteralPath $cjkAnchorFixture -Destination $cjkDir
Copy-Item -LiteralPath $cjkTargetFixture -Destination $cjkDir
$result = Invoke-LinkValidator @('-Path', $cjkDir, '-Exclude', '__NO_SUCH_PATTERN__')
$exit5 = $result.ExitCode
Write-Host $result.Output
Assert-ExitCode $exit5 0 'CJK anchor support'
End-Case $caseStart 'Case 5'

# Case 6: inline code exclusion (isolated fixture — fake links in backticks must be skipped)
Write-Host ''
Write-Host 'Case 6: inline code exclusion' -ForegroundColor Yellow
$caseStart = Start-Case
$inlineCodeDir = Join-Path $scriptDir 'fixtures\links\valid\inline-code'
if (-not (Test-Path -LiteralPath $inlineCodeDir -PathType Container)) {
    throw "Case 6 fixture directory missing: $inlineCodeDir"
}
$inlineCodeMarkdown = @(Get-ChildItem -LiteralPath $inlineCodeDir -Filter '*.md' -File)
if ($inlineCodeMarkdown.Count -lt 2) {
    throw "Case 6 fixture must contain at least 2 markdown files: $inlineCodeDir"
}
$result6 = Invoke-LinkValidator @('-Path', $inlineCodeDir, '-Exclude', '__NO_SUCH_PATTERN__')
$exit6 = $result6.ExitCode
Write-Host $result6.Output
Assert-ExitCode $exit6 0 'inline-code fixture (ExitCode)'
$fakePaths6 = @('./path/to/file.md', './img.png', './other.md', './fake.md', './should-be-skipped.md', './also-fake.md')
$inlineCodeLeaks = $false
foreach ($fp in $fakePaths6) {
    if ($result6.Output -match [regex]::Escape($fp)) {
        Write-Host "  [FAIL] inline code fake link leaked: $fp" -ForegroundColor Red
        $inlineCodeLeaks = $true
    }
}
if ($inlineCodeLeaks) {
    $script:failures++
} else {
    Write-Host "  [OK]   inline code fake links excluded" -ForegroundColor Green
}
End-Case $caseStart 'Case 6'

# Case 7: fixture exclusion and vendor exclusion (invalid fixtures + vendor paths must not appear in repo scan)
Write-Host ''
Write-Host 'Case 7: fixture + vendor exclusion from repo scan' -ForegroundColor Yellow
$caseStart = Start-Case
$repoRoot = Resolve-Path (Join-Path $scriptDir '..\..\..')
$result7 = Invoke-LinkValidator @('-Path', $repoRoot)
$exit7 = $result7.ExitCode
Assert-ExitCode $exit7 0 'repo scan exit code'
$anyLeak = $false
if ($result7.Output -match 'fixtures\\links\\invalid\\') {
    Write-Host "  [FAIL] invalid fixture path leaked into repo scan" -ForegroundColor Red
    $anyLeak = $true
}
if ($result7.Output -match 'vendor\\') {
    Write-Host "  [FAIL] vendor path leaked into repo scan" -ForegroundColor Red
    $anyLeak = $true
}
if ($anyLeak) {
    $script:failures++
} else {
    Write-Host "  [OK]   invalid fixtures + vendor excluded from repo scan" -ForegroundColor Green
}
End-Case $caseStart 'Case 7'

# Case 8: dedup (same line duplicate links reported once)
Write-Host ''
Write-Host 'Case 8: duplicate link dedup' -ForegroundColor Yellow
$caseStart = Start-Case
$dedupDir = Join-Path $linksTempRoot 'xdev-validate-links-dedup-test'
New-Item -ItemType Directory -Path $dedupDir -Force | Out-Null
Set-Content -LiteralPath (Join-Path $dedupDir 'dedup-test.md') -Value @'
# Dedup Test

Same target twice on one line: [a](./nonexistent.md) and [b](./nonexistent.md).

Single link: [c](./also-missing.md).

Same target on different lines:
[d](./nonexistent.md)
[e](./nonexistent.md)
'@ -Encoding utf8
$result8 = Invoke-LinkValidator @('-Path', $dedupDir)
$exit8 = $result8.ExitCode
Assert-ExitCode $exit8 1 'dedup validator exit code'
$errorOutput = $result8.Output
# Count occurrences of './nonexistent.md' in error output
$nonexistentMatches = ([regex]::Matches($errorOutput, [regex]::Escape('./nonexistent.md'))).Count
# Dedup by File+Line+Link means: same-line duplicate -> 1, 2 different lines -> 2 more, total == 3
if ($nonexistentMatches -eq 3) {
    Write-Host "  [OK]   dedup: ./nonexistent.md reported $nonexistentMatches time(s) (expected == 3; 1 same-line + 2 diff-lines)" -ForegroundColor Green
} else {
    Write-Host "  [FAIL] dedup: ./nonexistent.md reported $nonexistentMatches times (expected == 3)" -ForegroundColor Red
    $script:failures++
}
End-Case $caseStart 'Case 8'

} finally {
    $cleanRoot = [System.IO.Path]::GetFullPath($linksTempRoot).TrimEnd('\')
    $cleanParent = [System.IO.Path]::GetDirectoryName($cleanRoot).TrimEnd('\')
    if ($cleanParent -eq $normalizedLinksParent -and $cleanRoot -match '\\run-[0-9a-f]+$' -and
        $cleanRoot -ne $normalizedLinksParent -and (Test-Path -LiteralPath $cleanRoot)) {
        Remove-Item -Recurse -Force -LiteralPath $cleanRoot -ErrorAction SilentlyContinue
    }
    if ($linksParentCreated -and (Test-Path -LiteralPath $normalizedLinksParent -PathType Container)) {
        $children = @(Get-ChildItem -LiteralPath $normalizedLinksParent -Force -ErrorAction SilentlyContinue)
        if ($children.Count -eq 0) { Remove-Item -Force -LiteralPath $normalizedLinksParent -ErrorAction SilentlyContinue }
    }
}

# --- Summary ---
Write-Host ''
if ($script:executed -ne 8 -or $script:passed -ne 8) {
    Write-Host "Tests FAILED: executed=$($script:executed), passed=$($script:passed), failures=$($script:failures)" -ForegroundColor Red
    exit 1
}
if ($script:failures -gt 0) {
    Write-Host "Tests FAILED: $script:failures error(s)" -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed (8/8)" -ForegroundColor Green
exit 0
