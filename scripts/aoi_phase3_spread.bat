@echo off
title Minnsun AOI Phase 3 — Spread (100 bots, spread across maps)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 100 -spread -move -attack=true -duration 2m -report 5s
pause