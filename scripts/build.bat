@echo off
REM Build script for xdev - Development Environment Manager
REM Usage: build.bat [cli|gui|all]

setlocal enabledelayedexpansion

if "%1"=="" set "target=all"
if "%1"=="cli" set "target=cli"
if "%1"=="gui" set "target=gui"
if "%1"=="all" set "target=all"

echo === xdev Build Script ===
echo Target: %target%
echo.

REM Build CLI
if "%target%"=="cli" goto :build_cli
if "%target%"=="all" goto :build_all

:build_all
echo --- Building CLI binary ---
go build -ldflags="-s -w" -o xdev.exe .
if %errorlevel% neq 0 (
    echo [ERROR] CLI build failed!
    exit /b 1
)
echo [OK] CLI binary: xdev.exe
echo.

:build_cli
echo --- Building Wails GUI binary ---
wails build -trimpath
if %errorlevel% neq 0 (
    echo [ERROR] GUI build failed!
    exit /b 1
)
echo [OK] GUI binary: build\bin\xdev.exe
echo.

goto :done

:build_all
echo --- Building CLI binary ---
go build -ldflags="-s -w" -o xdev.exe .
if %errorlevel% neq 0 (
    echo [ERROR] CLI build failed!
    exit /b 1
)
echo [OK] CLI binary: xdev.exe

echo --- Building Wails GUI binary ---
wails build -trimpath
if %errorlevel% neq 0 (
    echo [ERROR] GUI build failed!
    exit /b 1
)
echo [OK] GUI binary: build\bin\xdev.exe

:done
echo.
echo === Build complete ===