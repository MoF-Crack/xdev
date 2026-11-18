@echo off
REM Delegate to the shared Wails 3 build contract.
node "%~dp0build.mjs" %*
exit /b %errorlevel%
