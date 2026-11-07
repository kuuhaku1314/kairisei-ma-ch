@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-Server.ps1" -ValidationOnly
set "server_exit=%ERRORLEVEL%"
echo.
if not "%server_exit%"=="0" echo Server startup failed. See the error above.
if "%server_exit%"=="0" echo Server is ready. Admin URL is shown above.
pause
exit /b %server_exit%
