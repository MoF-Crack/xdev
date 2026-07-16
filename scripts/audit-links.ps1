# scripts/audit-links.ps1
#
# CI release gate for download sources. Runs linkaudit in probe mode across
# the declared platforms, applies the policy gate from
# docs/download-source-policy.md, and blocks (exit 1) when any official
# source is dead or transient. Mirror failures and known implementation gaps
# (IMPLEMENTATION_MISSING, MISSING_CHECKSUM) only warn.
#
# The authoritative gate decision is made by `go run ./cmd/linkaudit -ci-gate`
# (internal/linkaudit/ci_gate.go, covered by ci_gate_test.go). This script
# additionally parses the JSON reports to print a combined cross-platform
# summary and list the blocking entries.
#
# Usage:
#   ./scripts/audit-links.ps1                       # audit all default platforms
#   ./scripts/audit-links.ps1 -SelfTest            # run script-logic tests, no network
#   ./scripts/audit-links.ps1 -Platforms "windows/amd64"
#   ./scripts/audit-links.ps1 -SkipLinux -SkipDarwin
#
# Exit codes: 0 = gate passed, 1 = gate blocked or audit error, 2 = bad args.

[CmdletBinding()]
param(
    [string]$Manifests = "./manifests",
    [string]$OutDir = "./build",
    [int]$TimeoutSec = 30,
    [int]$Concurrency = 4,
    [string[]]$Platforms,
    [switch]$SkipWindows,
    [switch]$SkipLinux,
    [switch]$SkipDarwin,
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"

# Mirror of internal/linkaudit.GateDecisionFor. Keep in sync with ci_gate.go.
function ConvertTo-GateDecision {
    param($Row)
    switch ($Row.failureReason) {
        "PASS"                  { return "pass" }
        "IMPLEMENTATION_MISSING" { return "warn" }
        "MISSING_CHECKSUM"      { return "warn" }
    }
    if ($Row.role -eq "mirror") { return "warn" }
    return "block"
}

function Get-GateSummary {
    param([object[]]$Rows, [string]$Platform)

    $blocking = @()
    $warningRows = @()
    $passed = 0; $failed = 0; $transient = 0; $warnings = 0

    foreach ($r in $Rows) {
        switch (ConvertTo-GateDecision $r) {
            "pass"  { $passed++ }
            "warn"  { $warnings++; $warningRows += $r }
            "block" {
                if ($r.failureReason -eq "TRANSIENT_ERROR") { $transient++ } else { $failed++ }
                $blocking += $r
            }
        }
    }

    $exit = if ($blocking.Count -gt 0) { 1 } else { 0 }

    [PSCustomObject]@{
        Platform    = $Platform
        Total       = $Rows.Count
        Passed      = $passed
        Failed      = $failed
        Transient   = $transient
        Warnings    = $warnings
        ExitCode    = $exit
        Blocking    = $blocking
        WarningRows = $warningRows
    }
}

function Get-DefaultTargets {
    if ($Platforms) {
        $i = 0
        return @($Platforms | ForEach-Object {
            $suffix = switch -Wildcard ($_) {
                "windows/*" { "" }
                "linux/*"   { "-linux" }
                "darwin/*"  { "-darwin" }
                default     { "-p$i" }
            }
            [PSCustomObject]@{ platform = $_; report = "download-audit-ci$suffix.json" }
        })
    }
    $list = @()
    if (-not $SkipWindows) { $list += [PSCustomObject]@{ platform = "windows/amd64"; report = "download-audit-ci.json" } }
    if (-not $SkipLinux)   { $list += [PSCustomObject]@{ platform = "linux/x86_64";  report = "download-audit-ci-linux.json" } }
    if (-not $SkipDarwin)  { $list += [PSCustomObject]@{ platform = "darwin/arm64";  report = "download-audit-ci-darwin.json" } }
    return $list
}

function Invoke-SelfTest {
    $dir = Join-Path $env:TEMP "xdev-audit-links-selftest"
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $dir -Force | Out-Null

    $passJson = @'
[
  {"tool":"a","version":"1","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://ok.invalid/a.zip","failureReason":"PASS"},
  {"tool":"a","version":"1","os":"windows","arch":"amd64","role":"mirror","sourceUrl":"https://ok.invalid/m.zip","failureReason":"PASS"}
]
'@
    $blockJson = @'
[
  {"tool":"b","version":"1","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://ok.invalid/b.zip","failureReason":"PASS"},
  {"tool":"b","version":"2","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://dead.invalid/b2.zip","failureReason":"URL_DEAD"}
]
'@
    $warnJson = @'
[
  {"tool":"c","version":"1","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/base/","failureReason":"IMPLEMENTATION_MISSING"},
  {"tool":"c","version":"1","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/c.zip","failureReason":"MIRROR_DEAD"},
  {"tool":"c","version":"2","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/c2.zip","failureReason":"TRANSIENT_ERROR"}
]
'@
    Set-Content -LiteralPath (Join-Path $dir "pass.json") -Value $passJson -Encoding utf8
    Set-Content -LiteralPath (Join-Path $dir "block.json") -Value $blockJson -Encoding utf8
    Set-Content -LiteralPath (Join-Path $dir "warn.json") -Value $warnJson -Encoding utf8

    $cases = @(
        @{ File = "pass.json";  Platform = "windows/amd64"; WantExit = 0; WantFail = 0; WantWarn = 0; WantTransient = 0 },
        @{ File = "block.json"; Platform = "windows/amd64"; WantExit = 1; WantFail = 1; WantWarn = 0; WantTransient = 0 },
        @{ File = "warn.json";  Platform = "linux/x86_64";  WantExit = 0; WantFail = 0; WantWarn = 3; WantTransient = 0 }
    )

    $failures = 0
    foreach ($c in $cases) {
        $rows = Get-Content (Join-Path $dir $c.File) -Raw -Encoding utf8 | ConvertFrom-Json
        $o = Get-GateSummary $rows $c.Platform
        $ok = $true
        if ($o.ExitCode -ne $c.WantExit) { Write-Host "  [FAIL] $($c.File): exit $($o.ExitCode) != $($c.WantExit)" -ForegroundColor Red; $ok = $false }
        if ($o.Failed -ne $c.WantFail)   { Write-Host "  [FAIL] $($c.File): failed $($o.Failed) != $($c.WantFail)" -ForegroundColor Red; $ok = $false }
        if ($o.Warnings -ne $c.WantWarn) { Write-Host "  [FAIL] $($c.File): warn $($o.Warnings) != $($c.WantWarn)" -ForegroundColor Red; $ok = $false }
        if ($o.Transient -ne $c.WantTransient) { Write-Host "  [FAIL] $($c.File): transient $($o.Transient) != $($c.WantTransient)" -ForegroundColor Red; $ok = $false }
        if ($ok) { Write-Host "  [OK]   $($c.File): total=$($o.Total) pass=$($o.Passed) fail=$($o.Failed) transient=$($o.Transient) warn=$($o.Warnings) exit=$($o.ExitCode)" -ForegroundColor Green }
        else { $script:failures++ }
    }

    # Verify the mirror-with-transient case warns (mirror transient must NOT block).
    $mtJson = @'
[
  {"tool":"m","version":"1","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/m.zip","failureReason":"TRANSIENT_ERROR"}
]
'@
    $mtRows = $mtJson | ConvertFrom-Json
    $mt = Get-GateSummary $mtRows "linux/x86_64"
    if ($mt.ExitCode -ne 0 -or $mt.Warnings -ne 1) {
        Write-Host "  [FAIL] mirror transient should warn, got exit=$($mt.ExitCode) warn=$($mt.Warnings)" -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host "  [OK]   mirror-transient warns (does not block)" -ForegroundColor Green
    }

    if ($script:failures -gt 0) {
        Write-Host ""
        Write-Host "SelfTest FAILED with $script:failures error(s)" -ForegroundColor Red
        return 1
    }
    Write-Host ""
    Write-Host "SelfTest passed (4/4)" -ForegroundColor Green
    return 0
}

# --- main ---

if ($SelfTest) {
    $script:failures = 0
    $code = Invoke-SelfTest
    exit $code
}

if (-not (Test-Path $Manifests -PathType Container)) {
    Write-Error "manifest directory not found: $Manifests"
    exit 2
}
if (-not (Test-Path $OutDir -PathType Container)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}

$targets = Get-DefaultTargets
if ($targets.Count -eq 0) {
    Write-Error "no platforms selected"
    exit 2
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Download source audit (probe gate)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  manifests : $Manifests"
Write-Host "  out dir   : $OutDir"
Write-Host "  platforms : $($targets.platform -join ', ')"
Write-Host ""

$combined = @{ Total = 0; Passed = 0; Failed = 0; Transient = 0; Warnings = 0; Blocking = @() }
$scriptFailed = $false

foreach ($t in $targets) {
    $reportPath = Join-Path $OutDir $t.report
    Write-Host "==> probe $($t.platform)" -ForegroundColor Yellow
    Write-Host "    report -> $reportPath"

    $args = @("run", "./cmd/linkaudit",
        "-manifests", $Manifests,
        "-mode", "probe",
        "-platform", $t.platform,
        "-timeout", "${TimeoutSec}s",
        "-concurrency", $Concurrency,
        "-report", $reportPath,
        "-ci-gate")

    # Native commands that write to stderr (the gate summary and BLOCK/WARN
    # rows) throw under Stop; relax to Continue and merge so we can relay the
    # gate output to stderr (warnings must go to stderr per policy).
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $lines = & go @args 2>&1
    $ErrorActionPreference = $prevEAP
    $runExit = $LASTEXITCODE
    foreach ($line in $lines) { [Console]::Error.WriteLine("    $line") }

    if ($runExit -ne 0 -and $runExit -ne 1) {
        Write-Host "    [ERROR] linkaudit exited with code $runExit for $($t.platform)" -ForegroundColor Red
        $scriptFailed = $true
        continue
    }

    if (-not (Test-Path $reportPath)) {
        Write-Host "    [ERROR] report not produced: $reportPath" -ForegroundColor Red
        $scriptFailed = $true
        continue
    }

    $rows = Get-Content $reportPath -Raw -Encoding utf8 | ConvertFrom-Json
    $o = Get-GateSummary $rows $t.platform
    Write-Host ("    {0,-14} total={1} pass={2} fail={3} transient={4} warn={5} exit={6}" -f `
        $t.platform, $o.Total, $o.Passed, $o.Failed, $o.Transient, $o.Warnings, $o.ExitCode) -ForegroundColor White

    $combined.Total += $o.Total
    $combined.Passed += $o.Passed
    $combined.Failed += $o.Failed
    $combined.Transient += $o.Transient
    $combined.Warnings += $o.Warnings
    $combined.Blocking += $o.Blocking

    # The CLI already prints per-run BLOCK/WARN rows to stderr (relayed
    # above), so we only aggregate here; the combined blocking list is
    # printed once at the end.
    if ($o.ExitCode -ne 0) {
        $scriptFailed = $true
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Combined: total=$($combined.Total) pass=$($combined.Passed) fail=$($combined.Failed) transient=$($combined.Transient) warn=$($combined.Warnings)" -ForegroundColor Cyan
if ($combined.Blocking.Count -gt 0) {
    [Console]::Error.WriteLine("  Blocking entries ($($combined.Blocking.Count)):")
    foreach ($r in $combined.Blocking) {
        [Console]::Error.WriteLine("    BLOCK  $($r.tool) $($r.version) $($r.os)/$($r.arch) $($r.role) -> $($r.failureReason)  ($($r.sourceUrl))")
    }
}
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

if ($scriptFailed) {
    Write-Host "Audit gate BLOCKED the build." -ForegroundColor Red
    exit 1
}
Write-Host "Audit gate passed." -ForegroundColor Green
exit 0
