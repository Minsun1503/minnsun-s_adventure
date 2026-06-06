@echo off
title Minnsun Stress Test (100 bots)
cd /d "%~dp0\..\server\cmd\netstress"
go run main.go -bots 100 -clump -move -attack
pause
