@echo off
title OmniTrace Smoke Test — Phase 5 Wire Validation
cd /d "%~dp0"
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║   OmniTrace Smoke Test — Phase 5 Wire Validation           ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.
echo   This test validates the end-to-end trace pipeline:
echo     1. Start server.exe in dev mode
echo     2. Send LOGIN + ATTACK packet with trace_id="aaaa1234"
echo     3. Call MCP blackbox_filter_trace("aaaa1234")
echo     4. Assert response has ^>=3 entries: NET_RX + ATTACK + DB_QUEUE
echo.

set SERVER_DIR=%~dp0..\server
set LOG_DIR=%~dp0..\server\data
set SERVER_EXE=%SERVER_DIR%\server.exe
set CHECK_EXE=%~dp0omnitrace_check.exe

rem ─── Step 1: Clean up any previous server ────────────────────────────
echo  [STEP 1] Killing any existing server on port 1503...
call kill_port_1503.bat 2>nul
timeout /t 2 /nobreak >nul

rem ─── Step 2: Clean old trace files ───────────────────────────────────
echo  [STEP 2] Cleaning old trace files...
if exist "%LOG_DIR%" (
    del "%LOG_DIR%\trace-*.jsonl" 2>nul
)
echo  [STEP 2] Done.

rem ─── Step 3: Start server in dev mode (from server/ directory for config) ─
echo  [STEP 3] Starting server.exe in dev mode...
cd /d "%SERVER_DIR%"
start "OmniTraceServer" /B "%SERVER_EXE%" -dev
cd /d "%~dp0"
if errorlevel 1 (
    echo  [FAIL] Could not start server.exe
    pause
    exit /b 1
)

rem ─── Step 4: Wait for server to be ready ────────────────────────────
echo  [STEP 4] Waiting for server to be ready...
set WAIT_MAX=30
set WAIT_COUNT=0
:WAIT_LOOP
set /a WAIT_COUNT+=1
if %WAIT_COUNT% gtr %WAIT_MAX% (
    echo  [FAIL] Server did not start within %WAIT_MAX% seconds.
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)
timeout /t 1 /nobreak >nul
curl -s http://localhost:8080/mcp >nul 2>&1
if errorlevel 1 goto WAIT_LOOP
echo  [STEP 4] Server ready on localhost:8080/mcp

rem ─── Step 5: Give server a moment to finish booting ─────────────────
timeout /t 2 /nobreak >nul

rem ─── Step 6: Run the trace check helper ─────────────────────────────
echo  [STEP 5] Running omnitrace_check.exe...
"%CHECK_EXE%"
if errorlevel 1 (
    echo  [FAIL] omnitrace_check.exe failed
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)
echo  [STEP 5] Done.

rem ─── Step 7: Allow trace entries to be flushed to disk ──────────────
timeout /t 2 /nobreak >nul

rem ─── Step 8: Query MCP blackbox_filter_trace ────────────────────────
echo  [STEP 6] Calling MCP blackbox_filter_trace("aaaa1234")...
set MCP_RESPONSE_FILE=%TEMP%\omnitrace_response.json
curl -s -X POST http://localhost:8080/mcp ^
    -H "Content-Type: application/json" ^
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"blackbox_filter_trace\",\"params\":{\"trace_id\":\"aaaa1234\",\"limit\":100}}" ^
    > "%MCP_RESPONSE_FILE%" 2>&1

echo  Response saved to %MCP_RESPONSE_FILE%

rem ─── Step 9: Count entries ──────────────────────────────────────────
echo.
echo  [STEP 7] Analyzing response...

rem Find total count
findstr /c:"\"total\"" "%MCP_RESPONSE_FILE%" >nul
if errorlevel 1 (
    echo  [FAIL] No 'total' field found in response.
    echo  Raw response:
    type "%MCP_RESPONSE_FILE%"
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)

rem Extract total value
for /f "tokens=2 delims=: " %%a in ('findstr /c:"\"total\"" "%MCP_RESPONSE_FILE%"') do set TOTAL_ENTRIES=%%a
set TOTAL_ENTRIES=%TOTAL_ENTRIES:,=%
echo  Total trace entries found: %TOTAL_ENTRIES%

rem Check if we have the required 3 types of entries
findstr /c:"NET_RX" "%MCP_RESPONSE_FILE%" >nul
if errorlevel 1 (
    echo  [FAIL] Missing NET_RX trace entry
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)
echo  [OK] Found NET_RX entry

findstr /c:"ATTACK" "%MCP_RESPONSE_FILE%" >nul
if errorlevel 1 (
    echo  [FAIL] Missing ATTACK trace entry
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)
echo  [OK] Found ATTACK entry

findstr /c:"DB_QUEUE" "%MCP_RESPONSE_FILE%" >nul
if errorlevel 1 (
    echo  [FAIL] Missing DB_QUEUE trace entry
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)
echo  [OK] Found DB_QUEUE entry

rem ─── Step 10: Verify total >= 3 ─────────────────────────────────────
if %TOTAL_ENTRIES% LSS 3 (
    echo  [FAIL] Expected ^>=3 trace entries, got %TOTAL_ENTRIES%
    taskkill /f /im server.exe 2>nul
    pause
    exit /b 1
)

echo.
echo ╔═══════════════════════════════════════════════════════════════╗
echo ║                    OMNITRACE TEST PASSED!                   ║
echo ║                                                             ║
echo ║   All 3 trace entries found:                                ║
echo ║     ✓ NET_RX   (network layer packet received)              ║
echo ║     ✓ ATTACK   (handler layer attack processed)             ║
echo ║     ✓ DB_QUEUE (DB layer save queued with same trace_id)    ║
echo ╚═══════════════════════════════════════════════════════════════╝
echo.

rem ─── Cleanup ──────────────────────────────────────────────────────────
echo  Cleaning up...
taskkill /f /im server.exe 2>nul
echo  Done.

pause
exit /b 0