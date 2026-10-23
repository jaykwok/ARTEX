@echo off
rem Keep this file ASCII-only: CMD can misparse UTF-8 comments across code pages.
rem Use UTF-8 for the backend's console output.
chcp 65001 >nul 2>&1
rem ARTEX launcher for Windows.
rem
rem Usage:
rem   start.bat                 Run in the foreground; Ctrl-C stops it.
rem   start.bat -addr :9000     Pass extra arguments to artex.exe.
rem
rem Restart artex.exe according to its exit code:
rem
rem   0      Normal shutdown: stop.
rem   75     Update or rollback: restart immediately.
rem   Other  Crash: retry after 1, 2, 4 seconds, up to 60 seconds.
rem
rem The backend handles downloads, SHA256 checks and replacement on startup.
rem See selfupdate and start.sh for details.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] Executable not found: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] Exited normally.
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem The backend completes the update or rollback on its next startup.
	echo [artex] Restart requested; applying update.
	set /a delay=1
	goto loop
)

echo [artex] Unexpected exit ^(code=!code!^); restarting in !delay!s. 1>&2
rem Use ping because timeout fails with redirected input; N seconds = N+1 pings.
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
