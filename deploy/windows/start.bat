@echo off
setlocal
cd /d "%~dp0"

if not exist "config.yaml" (
  if exist "config.example.yaml" (
    copy /Y "config.example.yaml" "config.yaml" >nul
    echo Created config.yaml from example. Edit admin password and printer.name, then restart.
  ) else (
    echo ERROR: config.yaml not found.
    pause
    exit /b 1
  )
)

if not exist "data" mkdir data
if not exist "logs" mkdir logs

echo Starting print-kiosk...
echo Browser will open automatically when the server is ready.
print-kiosk.exe -config config.yaml
pause
