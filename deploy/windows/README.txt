Print Kiosk — Windows deploy
============================

1) On the Windows PC (next to the printer):
   - Install printer drivers and set the Canon (or other) as default, OR
     put the exact queue name into config.yaml → printer.name
   - LibreOffice: if missing, the app installs it on first start (UAC + internet).
     Or install manually beforehand.
   - Recommended: put SumatraPDF.exe next to print-kiosk.exe
     (silent PDF printing without dialogs)
     https://www.sumatrapdfreader.org/download-free-pdf-viewer
   - Scanner/copy: the same MFP must be visible to Windows as a WIA scanner.
     Optional: install NAPS2 (NAPS2.Console.exe) for more reliable scanning.

2) Copy this folder to the PC, e.g. C:\print-kiosk\

3) Edit config.yaml:
   - admin.password
   - printer.name  (from Settings → Bluetooth & devices → Printers)
   - paths.libreoffice if soffice is not in PATH:
     C:\Program Files\LibreOffice\program\soffice.exe

4) Run start.bat (or print-kiosk.exe -config config.yaml)
   The start page opens fullscreen in Edge/Chrome (kiosk, no address bar).
   Exit the browser with Alt+F4.

5) Automatic startup with Windows 11:
   - Run install-autostart.bat once under the Windows account used by the kiosk.
   - It creates a shortcut in that account's Startup folder and starts the kiosk.
   - After the next Windows sign-in, the server and fullscreen browser open automatically.
   - To disable automatic startup, run uninstall-autostart.bat.

6) Kiosk: http://127.0.0.1:8080/
   Admin: http://127.0.0.1:8080/admin/

Touch gestures:
   - The app launches Edge/Chrome with browser navigation swipes, pull-to-refresh,
     elastic overscroll and pinch zoom disabled on public and specialist pages.
   - Vertical scrolling inside the kiosk remains enabled (files, previews, settings).
   - Only an authenticated specialist can use "Свернуть окно" in the cabinet.
     On normal app shutdown the browser window opened by Print Kiosk is also closed.
   - These protections apply to Chromium gestures. They do not change Windows registry.
   - To also block Windows shell gestures (Task View, Start, notifications), configure
     Windows 11 Assigned Access for Microsoft Edge and use a separate specialist account.
     This is an operating-system policy and intentionally is not toggled by the app: a
     crash or power loss must never leave the maintenance desktop permanently locked.

CI: each push builds artifact "print-kiosk-windows-amd64"
(Actions → latest run → Artifacts).

Build on Mac:
  ./scripts/build-windows.sh
  then send dist/windows/ to the PC.
