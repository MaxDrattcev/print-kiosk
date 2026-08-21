Option Explicit

Dim shell, files, base, command
Set shell = CreateObject("WScript.Shell")
Set files = CreateObject("Scripting.FileSystemObject")

base = files.GetParentFolderName(WScript.ScriptFullName)
shell.CurrentDirectory = base
command = Chr(34) & base & "\print-kiosk.exe" & Chr(34) & _
          " -config " & Chr(34) & base & "\config.yaml" & Chr(34)

' Window style 0 keeps the server console hidden; the app opens Edge itself.
shell.Run command, 0, False
