@echo off
title Minnsun AOI Phase 2 — Clump (100 bots, clumped + move)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 100 -clump -move -attack=true -duration 2m -report 5s
pause