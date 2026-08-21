@echo off
setlocal
cd /d "%~dp0"

if not exist "print-kiosk.exe" (
  echo ERROR: print-kiosk.exe was not found in %~dp0
  pause
  exit /b 1
)

if not exist "config.yaml" (
  if exist "config.example.yaml" (
    copy /Y "config.example.yaml" "config.yaml" >nul
    echo Created config.yaml. Check its settings before using the kiosk.
  ) else (
    echo ERROR: config.yaml and config.example.yaml were not found.
    pause
    exit /b 1
  )
)

set "KIOSK_VBS=%~dp0run-kiosk.vbs"
set "KIOSK_BASE=%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$w=New-Object -ComObject WScript.Shell; $s=$w.CreateShortcut([IO.Path]::Combine($env:APPDATA,'Microsoft\Windows\Start Menu\Programs\Startup\Print Kiosk.lnk')); $s.TargetPath=$env:KIOSK_VBS; $s.WorkingDirectory=$env:KIOSK_BASE; $s.Description='Print Kiosk automatic startup'; $s.Save()"

if errorlevel 1 (
  echo ERROR: Failed to create the Startup shortcut.
  pause
  exit /b 1
)

echo Print Kiosk will now start automatically after Windows sign-in.
start "" wscript.exe "%KIOSK_VBS%"
timeout /t 3 /nobreak >nul
