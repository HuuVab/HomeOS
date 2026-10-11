# Docker deployment and database backup

HomeOS v0.1 is intended for local evaluation. The Compose service publishes port 8081 only on the host's loopback address. It uses a separate, persistent Docker volume for SQLite; it does **not** share the native Windows server's database.

## Start a fresh Docker instance

Install Docker Desktop with Linux containers enabled. On Windows, the default Docker Desktop backend requires WSL 2 to be installed and enabled. Then run from the repository root:

```powershell
docker compose build
docker compose run --rm homeos create-admin <username>
docker compose up -d
```

The administrator command prompts for a password in the terminal. Open `http://127.0.0.1:8081` and sign in. `docker compose ps` shows health status; `docker compose logs homeos` shows startup errors. The `homeos-data` named volume persists across `docker compose down` and `docker compose up`.

Do not run `docker compose down -v` unless you intend to remove the database volume. Do not mount the Docker socket into this container. On Linux container deployments, Windows host metrics are marked unavailable; they are not replaced with misleading container readings.

## Back up the native Windows database

The following command makes a consistent snapshot while the native app is running. Choose a new path outside the Git repository for each backup:

```powershell
cd backend
go run ./cmd/homeos backup 'D:\Backups\homeos-2026-10-10.sqlite'
```

The command refuses to overwrite an existing file and runs SQLite's integrity check on the backup. Keep a copy on another device, and protect it like the live database: it contains account and session data. The command backs up SQLite only. HomeDrive files in `backend/data/files` or the Docker `/data/files` volume need a separate filesystem backup; see [HomeDrive v0.2](v0.2-homedrive.md).

## Back up the Docker database

From the repository root, create a snapshot inside the persistent volume, then copy it out:

```powershell
docker compose exec homeos /app/homeos backup /data/homeos-backup.sqlite
docker compose cp homeos:/data/homeos-backup.sqlite 'D:\Backups\homeos-docker.sqlite'
```

Use a unique backup filename each time. After verifying the copy, remove the temporary backup from the volume using a one-off container or other controlled maintenance procedure; avoid deleting the live database or its WAL files.

## Restore a backup to a native instance

1. Stop HomeOS. Do not copy over an open SQLite database.
2. Move the existing `backend/data/homeos.sqlite` and any matching `-wal` and `-shm` files to a separate recovery directory. Keep them until the restore is verified.
3. Copy the verified backup to `backend/data/homeos.sqlite`.
4. Start HomeOS and sign in. Confirm the expected administrator and status page work.

For Docker, stop the Compose service and restore into the named volume using a temporary container; keep the previous database and WAL files until you have verified the restored instance. Never point both the native app and the container at the same live SQLite files.
