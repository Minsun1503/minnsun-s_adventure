@echo off
setlocal enabledelayedexpansion

set PORT=1503
echo Finding process using port %PORT%...

set PID=
for /f "tokens=5" %%a in ('netstat -aon ^| findstr :%PORT% ^| findstr LISTENING') do (
    set PID=%%a
)

if "%PID%"=="" (
    echo No process is listening on port %PORT%.
) else (
    echo Found process with PID %PID% on port %PORT%. Killing it...
    taskkill /f /pid %PID%
)
