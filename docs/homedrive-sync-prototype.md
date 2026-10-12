# HomeDrive desktop sync prototype

`homeos-sync.exe` is an explicit, Windows-friendly command-line prototype. It is built at `backend/bin/homeos-sync.exe` by `scripts/setup-windows-postgres.ps1`, or with `go build -o bin/homeos-sync.exe ./cmd/homeos-sync` from `backend`. It is not started by the HomeOS server task.

Choose a dedicated, existing local directory. Do not point it at `backend/data/files`, the PostgreSQL data directory, or your only copy of important files. A fresh empty directory is the safest first test.

```powershell
New-Item -ItemType Directory -Path 'D:\HomeDrive-Sync-Test' -Force
& 'D:\HomeOS\backend\bin\homeos-sync.exe' init 'D:\HomeDrive-Sync-Test'
& 'D:\HomeOS\backend\bin\homeos-sync.exe' sync 'D:\HomeDrive-Sync-Test' --user YOUR_USERNAME
& 'D:\HomeOS\backend\bin\homeos-sync.exe' status 'D:\HomeDrive-Sync-Test'
& 'D:\HomeOS\backend\bin\homeos-sync.exe' watch 'D:\HomeDrive-Sync-Test' --user YOUR_USERNAME --interval 30s
& 'D:\HomeOS\backend\bin\homeos-sync.exe' resolve 'D:\HomeDrive-Sync-Test' --user YOUR_USERNAME --path 'docs/example.txt' --choice keep-remote
```

`scan` records offline local creates, edits, and deletions in `.homeos-sync/state.sqlite` without contacting the server. `status` displays pending operations and conflicts. `sync` prompts for the password interactively, uses the HomeOS REST API, and pins that directory to the first server URL and account ID. `watch` does the same, then polls every 30 seconds until Ctrl+C; `--interval` can change the period but must be at least five seconds. It retries transient sync failures and stops when the session expires, requiring a fresh login. Use `--server https://HOST` for a remote server; plaintext HTTP is accepted only for loopback addresses. The password and session cookie are not saved to SQLite.

On a sync run, the client downloads remote files that are not already present locally, creates matching folders, uploads newly created local files and folders, and records the server change cursor. New uploads that fail are retained in a SQLite retry queue with backoff. A local edit uploads only when the remote revision still matches its baseline; an untouched local file accepts a newer remote version. A local deletion moves the matching remote item to HomeDrive Trash only if its revision is unchanged, and folder deletion refuses newly added remote children. A remote deletion moves the clean local copy under `.homeos-sync/removed/` so its bytes remain recoverable. Concurrent edits and name collisions remain visible conflicts. For a recorded **file** conflict, `resolve --path FILE --choice keep-local` uploads the local bytes against the current remote revision (or sends a local deletion to Trash); `keep-remote` archives the local bytes before downloading the remote file. Both are explicit choices, not automatic merges. Folder conflicts still require manual resolution.

The Windows ZIP package now includes a graphical shell for single-run sync and explicit file/folder conflict choices; see `homedrive-sync-desktop.md`. The CLI `watch` command still requires an open terminal. New uploads above 32 MiB use resumable 4 MiB chunks, bound to the full-file SHA-256 hash; the client handles files up to 2 GiB by default, subject to the server's configured limits. Existing-file updates stream through the revision-checked replacement endpoint and retry from the beginning after a transfer failure. The change feed limits remote relisting after the initial crawl. Local scans traverse directory metadata but reuse SHA-256 hashes for unchanged files, with a full audit at least every 24 hours. Symlinks and unsafe paths are rejected. Keep independent backups of both server and local files, including the `.homeos-sync/removed/` recovery area.
