@echo off
REM Test script for xdev - Development Environment Manager
echo === Running all tests ===
echo.

go test ./... -count=1 -v 2>&1

if %errorlevel% equ 0 (
    echo.
    echo === All tests passed! ===
) else (
    echo.
    echo [ERROR] Some tests failed!
    exit /b 1
)