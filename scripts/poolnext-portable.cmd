@echo off
setlocal EnableExtensions
for %%I in ("%~dp0.") do set "ROOT=%%~fI"
cd /d "%ROOT%"

set "EXE=%ROOT%\poolnext-server-windows-amd64.exe"
if not exist "%EXE%" set "EXE=%ROOT%\poolnext-server.exe"
if not exist "%EXE%" (
  echo [PoolNeXt] Portable binary not found in "%ROOT%".
  echo [PoolNeXt] Download poolnext-server-windows-amd64.exe into this folder and run this script again.
  pause
  exit /b 1
)

if not defined POOLNEXT_PORT set "POOLNEXT_PORT=17778"
if not defined POOLNEXT_DATA_DIR set "POOLNEXT_DATA_DIR=%ROOT%"

set "SECRET_FILE=%ROOT%\poolnext-secret.txt"
if not exist "%SECRET_FILE%" (
  echo [PoolNeXt] Generating SESSION_SECRET at "%SECRET_FILE%"
  powershell -NoProfile -ExecutionPolicy Bypass -Command "$rng = [Security.Cryptography.RandomNumberGenerator]::Create(); $bytes = New-Object byte[] 32; $rng.GetBytes($bytes); $rng.Dispose(); $hex = [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant(); Set-Content -LiteralPath '%SECRET_FILE%' -Value $hex -NoNewline -Encoding ascii"
  if errorlevel 1 (
    echo [PoolNeXt] Failed to generate SESSION_SECRET.
    pause
    exit /b 1
  )
)

if not exist "%POOLNEXT_DATA_DIR%" mkdir "%POOLNEXT_DATA_DIR%"
set "LOG_DIR=%ROOT%\logs"
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%"

echo [PoolNeXt] Starting portable instance
echo [PoolNeXt] Binary: %EXE%
echo [PoolNeXt] Data:   %POOLNEXT_DATA_DIR%
echo [PoolNeXt] Logs:   %LOG_DIR%
echo [PoolNeXt] Port:   %POOLNEXT_PORT%
echo [PoolNeXt] The one-time root password is printed below on the first start.
echo.

"%EXE%" --data-dir "%POOLNEXT_DATA_DIR%" --session-secret-file "%SECRET_FILE%" --port %POOLNEXT_PORT% --log-dir "%LOG_DIR%"

set "EXIT_CODE=%ERRORLEVEL%"
echo.
echo [PoolNeXt] Server exited with code %EXIT_CODE%.
pause
exit /b %EXIT_CODE%
