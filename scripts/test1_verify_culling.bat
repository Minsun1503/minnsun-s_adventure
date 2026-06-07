@echo off
title AOI Test 1 — Verify Culling (100 bots spread, 1 min)
cd /d "%~dp0\..\server\cmd\netstress"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   Test 1: Verify AOI Culling (100 bots, spread, 1 min)      ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   Description: 100 bots spread across map 1000x1000 with random
echo   movement. Each bot spans at (X,Z) in [10,900]. With AOI culling
echo   (MaxAOIWatchers=50, BroadcastAOIRadius=60), each bot should only
echo   see ~50 neighbors. Expected broadcast/s should be low (~5,000 pkt/s).
echo.
echo   Expected result: Broadcast rate should be well under 200,000 pkt/s
echo   since bots are spread across a large map.
echo.
go run main.go -bots 100 -spread -move -attack=false -duration 1m -report 5s
pause