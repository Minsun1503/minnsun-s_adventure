@echo off
title Minnsun AOI Phase 1 — Verify (10 bots, spread + move)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 10 -spread -move -attack=false -duration 30s -report 5s
pause