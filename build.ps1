# XDev Build Script
# Usage: .\build.ps1 [-SkipFrontend] [-SkipCLI] [-SkipGUI] [-SkipAudit]

param(
    [switch]$SkipFrontend = $false,
    [switch]$SkipCLI = $false,
    [switch]$SkipGUI = $false,
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

$buildDate = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
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
Write-Host "  XDev v$version Build" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Build CLI
if (-not $SkipCLI) {
    $cliOutput = Join-Path $binDir "xdev-$version.exe"
    Write-Host "[1/2] Building CLI: xdev-$version.exe ..." -ForegroundColor Yellow
    go build -ldflags $ldflags -o $cliOutput .
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: CLI build failed" -ForegroundColor Red
        exit 1
    }
    # Also create a copy without version suffix for convenience
    $cliPlain = Join-Path $binDir "xdev.exe"
    Copy-Item $cliOutput $cliPlain -Force
    Write-Host "  -> $cliOutput" -ForegroundColor Green
    Write-Host "  -> $cliPlain (copy)" -ForegroundColor Green
} else {
    Write-Host "[1/2] Skipping CLI build" -ForegroundColor Gray
}

# Audit download sources (release gate) before GUI build
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

# Build GUI
if (-not $SkipGUI) {
    $guiOutput = Join-Path $binDir "xdev-gui-$version.exe"
    Write-Host "[2/2] Building GUI: xdev-gui-$version.exe ..." -ForegroundColor Yellow

    # Build with wails (outputfilename stays as xdev-gui)
    wails build -tags gui -ldflags $ldflags 2>&1 | ForEach-Object { Write-Host "  $_" }
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: GUI build failed" -ForegroundColor Red
        exit 1
    }

    # Copy and rename wails output to include version
    $guiPlain = Join-Path $binDir "xdev-gui.exe"
    if (Test-Path $guiPlain) {
        Copy-Item $guiPlain $guiOutput -Force
        Write-Host "  -> $guiOutput" -ForegroundColor Green
        Write-Host "  -> $guiPlain (copy)" -ForegroundColor Green
    }
} else {
    Write-Host "[2/2] Skipping GUI build" -ForegroundColor Gray
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
