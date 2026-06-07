@echo off
title Minnsun AOI Phase 8 — Boss Event (1000 bots, boss mode)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 1000 -boss -move -attack=true -duration 2m -report 5s
pause