@echo off
REM Build script for xdev - Development Environment Manager（单二进制模型）
REM Usage: build.bat
REM 产出 build\bin\xdev.exe：无参数 → GUI；xdev tui → TUI；其他参数 → CLI。

setlocal

echo === xdev Build Script (single binary: GUI+TUI+CLI) ===
echo.

echo --- Building xdev (wails embeds frontend) ---
wails build -trimpath
if %errorlevel% neq 0 (
    echo [ERROR] Build failed!
    exit /b 1
)
echo [OK] Binary: build\bin\xdev.exe

echo.
echo === Build complete ===