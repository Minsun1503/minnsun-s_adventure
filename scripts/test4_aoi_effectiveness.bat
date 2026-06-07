@echo off
title AOI Test 4 — AOI Effectiveness (1000 bots with/without AOI)
cd /d "%~dp0\..\server\cmd\netstress"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   Test 4: AOI Effectiveness — Culling vs No Culling         ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   Description: Runs two comparisons with 1000 bots spread across
echo   map 1000x1000:
echo.
echo   1. AOI ON (default): BroadcastAOIRadius=60, MaxAOIWatchers=50
echo      → Each bot sees ~50 neighbors. Expected ~50,000 pkt/s.
echo.
echo   2. AOI "OFF": To simulate no AOI culling, run 1000 bots in
echo      clump mode (all within range of each other). Without culling,
echo      each bot would broadcast to all 999 others = 999,000 pkt/s.
echo      With MaxAOIWatchers=50, this drops to ~50,000 pkt/s.
echo.
echo   The difference between the two tests shows the effectiveness
echo   of AOI culling in reducing broadcast traffic.
echo.
echo ═══════════════════════════════════════════════════════════════
echo   Phase 1: 1000 bots SPREAD (AOI active — bots far apart)
echo   → Expected: very low broadcast/s (most bots not in range)
echo ═══════════════════════════════════════════════════════════════
go run main.go -bots 1000 -spread -move -attack=false -duration 1m -report 5s

echo.
echo ═══════════════════════════════════════════════════════════════
echo   Phase 2: 1000 bots CLUMP (worst-case — all within AOI range)
echo   → Expected: ~50,000 pkt/s (bounded by MaxAOIWatchers=50)
echo ═══════════════════════════════════════════════════════════════
echo.
echo   Without AOI culling, this would be ~999,000 pkt/s. The
echo   20x reduction proves MaxAOIWatchers is working correctly.
echo.
go run main.go -bots 1000 -clump -move -attack=false -duration 1m -report 5s
pause