@echo off
title Minnsun Server
cd /d "%~dp0\..\server"
go run server.go
pause
