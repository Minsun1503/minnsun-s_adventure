@echo off
title Minnsun AOI Phase 6 — Teleport (100 bots, teleport mode)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 100 -teleport -move -attack=true -duration 2m -report 5s
pause