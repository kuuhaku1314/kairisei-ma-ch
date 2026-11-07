@echo off
setlocal
chcp 65001 >nul
pushd "%~dp0" || exit /b 1
set "sync_flags="
if "%~1"=="--dry-run" set "sync_flags=-cdn-sync-dry-run"
if not "%~1"=="" if not "%~1"=="--dry-run" (
  echo Usage: Sync-CDN.cmd [--dry-run]
  popd
  exit /b 2
)
if not "%~2"=="" (
  echo Usage: Sync-CDN.cmd [--dry-run]
  popd
  exit /b 2
)
"%~dp0kairi-server.exe" -sync-cdn cdn-sync.json %sync_flags% -platform-content-config "%~dp0deployment.json"
set "sync_exit=%ERRORLEVEL%"
popd
if "%sync_exit%"=="0" echo CDN command completed.
if not "%sync_exit%"=="0" echo CDN command failed. See the message above; fix it and run again.
if "%~1"=="" pause
exit /b %sync_exit%
