# XDev Build Script（单二进制模型）
# Usage: .\build.ps1 [-SkipFrontend] [-SkipBuild] [-SkipAudit]
#
# 一个可执行文件（xdev）按调用形态多态执行：无参数 → GUI；tui → TUI；
# 其他参数 → CLI。CLI/GUI 不再分别编译（Spec 030 双二进制已合并）。

param(
    [switch]$SkipFrontend = $false,
    [switch]$SkipBuild = $false,
    [switch]$SkipAudit = $false
)

$ErrorActionPreference = "Stop"

# Read version from Go source
$versionFile = Join-Path $PSScriptRoot "internal\version\version.go"
$versionLine = Get-Content $versionFile | Where-Object { $_ -match 'const Version = ' }
if ($versionLine -match '"([^"]+)"') {
    $version = $Matches[1]
} else {
    Write-Host "ERROR: Cannot parse version from $versionFile" -ForegroundColor Red
    exit 1
}

# F-13: stamp BuildDate in real UTC. The previous `Get-Date -Format
# "yyyy-MM-ddTHH:mm:ssZ"` formatted local time but appended a literal "Z",
# so builds carried a local-time value falsely labelled as UTC. Convert first,
# then format, so the trailing Z is honest.
$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X github.com/xmdemt/xdev/internal/version.BuildDate=$buildDate"

# Security (C-5): Inject the Ed25519 public key for manifest signature
# verification. When the public key is embedded, the downloader refuses
# to accept unsigned/all-zero-hash manifests, protecting against MITM.
$pubKeyFile = Join-Path $PSScriptRoot "keys\pub.key"
if (Test-Path $pubKeyFile) {
    $pubKey = (Get-Content $pubKeyFile -Raw).Trim()
    $ldflags = "$ldflags -X github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=$pubKey"
    Write-Host "[security] Embedding Ed25519 public key for manifest signature verification" -ForegroundColor DarkGray
} else {
    Write-Host "[security] WARNING: keys/pub.key not found — manifest signature verification will be DISABLED in this build" -ForegroundColor Yellow
}

$binDir = Join-Path $PSScriptRoot "build\bin"
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  XDev v$version Build (single binary)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Audit download sources (release gate)
if (-not $SkipAudit) {
    Write-Host ""
    Write-Host "[audit] Download source gate (probe) ..." -ForegroundColor Yellow
    & "$PSScriptRoot\scripts\audit-links.ps1"
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: download source audit failed (exit $LASTEXITCODE). Pass -SkipAudit to bypass." -ForegroundColor Red
        exit 1
    }
    Write-Host "[audit] gate passed" -ForegroundColor Green
} else {
    Write-Host "[audit] Skipping download source audit (-SkipAudit)" -ForegroundColor Gray
}

# Build single binary (GUI+TUI+CLI in one; wails embeds the frontend)
if (-not $SkipBuild) {
    $output = Join-Path $binDir "xdev-$version.exe"
    Write-Host "[1/1] Building xdev-$version.exe (GUI+TUI+CLI)... " -ForegroundColor Yellow

    wails build -ldflags $ldflags 2>&1 | ForEach-Object { Write-Host "  $_" }
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: build failed" -ForegroundColor Red
        exit 1
    }

    # Copy and rename wails output to include version
    $plain = Join-Path $binDir "xdev.exe"
    if (Test-Path $plain) {
        Copy-Item $plain $output -Force
        Write-Host "  -> $output" -ForegroundColor Green
        Write-Host "  -> $plain (copy)" -ForegroundColor Green
    }
} else {
    Write-Host "[1/1] Skipping build (-SkipBuild)" -ForegroundColor Gray
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Build complete! v$version" -ForegroundColor Green
Write-Host "  Output: $binDir" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# List output files
Get-ChildItem $binDir -Filter "xdev*" | ForEach-Object {
    $size = [math]::Round($_.Length / 1MB, 2)
    Write-Host ("  {0,-40} {1,8} MB" -f $_.Name, $size) -ForegroundColor White
}