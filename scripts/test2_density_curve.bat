@echo off
title AOI Test 2 — Density Curve (100/500/1000/2000 bots)
cd /d "%~dp0\..\server\cmd\netstress"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   Test 2: Density Curve — AOI Scalability Profile           ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   Description: A loop that runs 4 sequential tests with increasing
echo   bot counts (100, 500, 1000, 2000) in spread mode on map 1000x1000.
echo   Each run lasts 1 minute. The goal is to observe how broadcast/s,
echo   CPU, and packet loss scale with concurrent players.
echo.
echo   Expected result: Broadcast/s should scale roughly linearly with
echo   bot count (or better) since AOI culling keeps visible neighbors
echo   bounded to ~50 per entity.
echo.

for %%B in (100 500 1000 2000) do (
    echo.
    echo ═══════════════════════════════════════════════════════════════
    echo   Running test with %%B bots...
    echo ═══════════════════════════════════════════════════════════════
    go run main.go -bots %%B -spread -move -attack=false -duration 1m -report 5s
    echo.
    echo   Test with %%B bots completed. Waiting 5s before next test...
    timeout /t 5 /nobreak >nul
)
pause