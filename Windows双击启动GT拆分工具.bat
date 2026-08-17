@echo off
setlocal
title GT Split Go - Windows Launcher

echo ========================================
echo  GT Split Go - Windows Launcher
echo ========================================
echo.

cd /d "%~dp0"

set "GO_BIN="
where go >nul 2>nul
if not errorlevel 1 set "GO_BIN=go"
if not defined GO_BIN if exist "%LOCALAPPDATA%\Programs\Go-1.26.5\go\bin\go.exe" set "GO_BIN=%LOCALAPPDATA%\Programs\Go-1.26.5\go\bin\go.exe"

if defined GO_BIN (
    echo Building the latest source code...
    "%GO_BIN%" build -o "gt_split_go.exe" .
    if errorlevel 1 (
        echo.
        echo [ERROR] Go build failed. Keep this window open and check the error above.
        pause
        exit /b 1
    )
)

if not exist "gt_split_go.exe" (
    echo [ERROR] gt_split_go.exe was not found and the Go compiler is unavailable.
    echo Checked system PATH and %%LOCALAPPDATA%%\Programs\Go-1.26.5\go\bin\go.exe
    pause
    exit /b 1
)

echo Starting the local web service...
echo The browser will open automatically. Close this window to stop the service.
echo.

"%~dp0gt_split_go.exe"

echo.
echo The service has stopped.
pause
endlocal
