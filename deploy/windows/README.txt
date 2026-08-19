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

2) Copy this folder to the PC, e.g. C:\print-kiosk\

3) Edit config.yaml:
   - admin.password
   - printer.name  (from Settings → Bluetooth & devices → Printers)
   - paths.libreoffice if soffice is not in PATH:
     C:\Program Files\LibreOffice\program\soffice.exe

4) Run start.bat (or print-kiosk.exe -config config.yaml)
   The start page opens in the browser automatically.

5) Kiosk: http://127.0.0.1:8080/
   Admin: http://127.0.0.1:8080/admin/

CI: each push builds artifact "print-kiosk-windows-amd64"
(Actions → latest run → Artifacts).

Build on Mac:
  ./scripts/build-windows.sh
  then send dist/windows/ to the PC.
