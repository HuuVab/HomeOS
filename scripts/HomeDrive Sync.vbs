Option Explicit
Dim shell, files, folder, script, command
Set shell = CreateObject("WScript.Shell")
Set files = CreateObject("Scripting.FileSystemObject")
folder = files.GetParentFolderName(WScript.ScriptFullName)
script = files.BuildPath(folder, "homeos-sync-desktop.ps1")
If Not files.FileExists(script) Or Not files.FileExists(files.BuildPath(folder, "homeos-sync.exe")) Then
    MsgBox "The HomeDrive Sync package is incomplete.", vbExclamation, "HomeDrive Sync"
    WScript.Quit 1
End If
command = "powershell.exe -NoProfile -STA -ExecutionPolicy Bypass -WindowStyle Hidden -File " & Chr(34) & script & Chr(34)
shell.Run command, 0, False
