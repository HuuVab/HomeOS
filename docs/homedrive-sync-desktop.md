# HomeDrive Sync for Windows

The package contains the sync engine and a Windows desktop interface. Extract
the entire ZIP, then double-click **HomeDrive Sync.vbs**. Windows PowerShell and
its built-in Windows Forms are required; no Docker or separate GUI runtime is
needed. Keep all package files together.

Choose an existing local folder, enter the HomeOS server address and account,
and press **Sync now**. The password is passed to the engine through standard
input, not a command-line argument, and the password box clears when sync
starts. Click **Refresh conflicts** to see unresolved paths. Select one, mark
it as a folder when the choice should include descendants, then choose **Keep
local** or **Keep remote** and confirm. Displaced local content is archived
under `.homeos-sync/removed/`; displaced remote content goes to HomeDrive
Trash. Resolve one conflict at a time and sync again afterward.

The GUI is a single-run client, not an always-on background service. The
command-line `homeos-sync.exe watch` mode remains available for continuous
polling. Sync state is stored inside the selected local folder in
`.homeos-sync/state.sqlite`; protect that folder and keep backups. The first
scan reads all local files. Later scans traverse directory metadata but reuse
saved SHA-256 values for files whose size and modification timestamp have not
changed. If another program changes bytes while preserving both fields, run a
fresh scan after changing the timestamp; this metadata-based cache cannot
detect such a change on its own.

For developers, run `scripts/package-homeos-sync.ps1` to build
`backend/bin/HomeDrive-Sync-Windows.zip`. The package does not install HomeOS
or PostgreSQL; it connects to an existing HomeOS server.
