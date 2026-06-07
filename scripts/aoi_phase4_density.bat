@echo off
title Minnsun AOI Phase 4 — Density Scaling (auto: 100,200,500,1000,2000)
cd /d "%~dp0\..\server\cmd\netstress"

echo ======================================================
echo   AOI Phase 4 — Density Scaling Test
echo   Running: 100, 200, 500, 1000, 2000 bots (clump mode)
echo   Logs will be saved to scripts/density_results.md
echo ======================================================

set RESULTS_FILE="%~dp0density_results.md"
echo # AOI Density Scaling Results > %RESULTS_FILE%
echo. >> %RESULTS_FILE%
echo | Test Date | Bots | TickP99 | Broadcast/s | AOIEnter/s | AOILeave/s | VisibleAvg | VisibleMax | >> %RESULTS_FILE%
echo |---|---|---|---|---|---|---|---| >> %RESULTS_FILE%

for %%B in (100 200 500 1000 2000) do (
    echo.
    echo [PHASE 4] Running with %%B bots (clump mode, 1 minute each)...
    echo.
    echo ### Bot Count: %%B >> %RESULTS_FILE%
    go run main.go -bots %%B -clump -move -attack=true -duration 1m -report 10s >> %RESULTS_FILE% 2>&1
    echo. >> %RESULTS_FILE%
    echo [DONE] %%B bots completed.
    timeout /t 5 /nobreak >nul
)

echo.
echo ======================================================
echo   Density scaling test complete!
echo   Results saved to scripts/density_results.md
echo ======================================================
pause