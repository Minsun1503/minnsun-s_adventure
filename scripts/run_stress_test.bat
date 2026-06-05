@echo off
title Minnsun Stress Test (3000 bots)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 3000 -clump -move -attack
pause
