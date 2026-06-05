@echo off
title Minnsun Stress Test (1000 bots)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 1000 -clump -move -attack
pause
