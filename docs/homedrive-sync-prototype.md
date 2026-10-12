# HomeDrive desktop sync prototype

`homeos-sync.exe` is an explicit, Windows-friendly command-line prototype. It is built at `backend/bin/homeos-sync.exe` by `scripts/setup-windows-postgres.ps1`, or with `go build -o bin/homeos-sync.exe ./cmd/homeos-sync` from `backend`. It is not started by the HomeOS server task.

Choose a dedicated, existing local directory. Do not point it at `backend/data/files`, the PostgreSQL data directory, or your only copy of important files. A fresh empty directory is the safest first test.

```powershell
New-Item -ItemType Directory -Path 'D:\HomeDrive-Sync-Test' -Force
& 'D:\HomeOS\backend\bin\homeos-sync.exe' init 'D:\HomeDrive-Sync-Test'
& 'D:\HomeOS\backend\bin\homeos-sync.exe' sync 'D:\HomeDrive-Sync-Test' --user YOUR_USERNAME
& 'D:\HomeOS\backend\bin\homeos-sync.exe' status 'D:\HomeDrive-Sync-Test'
& 'D:\HomeOS\backend\bin\homeos-sync.exe' watch 'D:\HomeDrive-Sync-Test' --user YOUR_USERNAME --interval 30s
```

`scan` records offline local creates, edits, and deletions in `.homeos-sync/state.sqlite` without contacting the server. `status` displays pending operations and conflicts. `sync` prompts for the password interactively, uses the HomeOS REST API, and pins that directory to the first server URL and account ID. `watch` does the same, then polls every 30 seconds until Ctrl+C; `--interval` can change the period but must be at least five seconds. It retries transient sync failures and stops when the session expires, requiring a fresh login. Use `--server https://HOST` for a remote server; plaintext HTTP is accepted only for loopback addresses. The password and session cookie are not saved to SQLite.

On a sync run, the client downloads remote files that are not already present locally, creates matching folders, uploads newly created local files and folders, and records the server change cursor. New uploads that fail are retained in a SQLite retry queue with backoff. Existing-file edits, local deletions, remote updates to files already present locally, and name collisions become visible conflicts; they are **not** silently applied. Move a conflicting local copy outside the sync folder and use a fresh directory for a clean pull while conflict-resolution controls are still being developed.

This is not yet full bidirectional sync: `watch` requires an open terminal, and there is no automatic remote overwrite or deletion, conflict-resolution UI, or large-file transfer. Files above 32 MiB are not transferred by this prototype. It re-lists the remote tree each run and uses the change feed as a durable cursor; the cursor is not yet an incremental transfer plan. Symlinks and unsafe paths are rejected. Keep independent backups of both server and local files.
