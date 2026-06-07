@echo off
title AOI Test 3 — Single Cell Hell (1000 bots clumped)
cd /d "%~dp0\..\server\cmd\netstress"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   Test 3: Single Cell Hell — Worst-Case Stacking            ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   Description: 1000 bots all clumped near (50,50) with movement.
echo   This is the worst case for AOI — all 1000 bots are within
echo   BroadcastAOIRadius of each other. Without MaxAOIWatchers=50,
echo   this would generate 1000*1000=1,000,000 broadcast packets per
echo   movement tick, which would crash the server.
echo.
echo   Expected result: With MaxAOIWatchers=50 culling, each bot only
echo   sees ~50 neighbors. Broadcast/s should be ~50,000 pkt/s instead
echo   of 1,000,000 pkt/s. The server should stay stable.
echo.
echo   Note: If server CPU spikes above 100% or broadcasts exceed
echo   200,000 pkt/s, AOI culling may not be working correctly.
echo.
go run main.go -bots 1000 -clump -move -attack=false -duration 1m -report 5s
pause