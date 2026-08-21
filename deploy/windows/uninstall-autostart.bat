@echo off
setlocal
set "KIOSK_LINK=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\Print Kiosk.lnk"

if exist "%KIOSK_LINK%" del /F /Q "%KIOSK_LINK%"
echo Print Kiosk automatic startup has been disabled for this Windows account.
timeout /t 3 /nobreak >nul
