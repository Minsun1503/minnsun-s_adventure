@echo off
title Minnsun AOI Phase 5 — Cell Border Crossing (50 bots, border mode)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 50 -border -move -attack=true -duration 2m -report 5s
pause