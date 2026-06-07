@echo off
title AOI Test 5 — Long Run Leak Detection (100 bots, 1 hour)
cd /d "%~dp0\..\server\cmd\netstress"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   Test 5: Long Run — Memory Leak Detection (1 hour)         ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   Description: 100 bots running in spread mode with movement
echo   for a full hour on map 1000x1000. This test is designed to
echo   detect memory leaks, goroutine leaks, and packet loss over
echo   extended runtime.
echo.
echo   What to monitor:
echo   - Memory usage (should stabilize, not grow linearly)
echo   - Bot disconnect count (should be 0 if connections stable)
echo   - Packet rates (should remain consistent throughout)
echo   - Goroutine count (should not leak)
echo.
echo   Expected result: Server should maintain stable memory usage
echo   and packet rates for the entire hour. No bots should disconnect.
echo.
go run main.go -bots 100 -spread -move -attack=true -duration 1h -report 30s
pause