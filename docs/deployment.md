# Docker deployment and database backup

HomeOS is intended for local evaluation. Compose publishes port 8081 only on the host's loopback address. PostgreSQL is the authoritative metadata store and file bytes remain in a separate HomeOS data volume.

## Start a fresh Docker instance

Install Docker Desktop with Linux containers enabled. On Windows, the default Docker Desktop backend requires WSL 2 to be installed and enabled. Create a strong local database secret, then run from the repository root:

```powershell
New-Item -ItemType Directory -Force secrets
Set-Content -NoNewline secrets\postgres_password.txt '<strong-random-password>'
docker compose build
docker compose run --rm homeos create-admin <username>
docker compose up -d
```

The administrator command prompts for a password in the terminal. Open `http://127.0.0.1:8081` and sign in. `docker compose ps` shows health status; `docker compose logs homeos` shows startup errors. The `homeos-postgres` volume persists metadata and `homeos-data` persists file bytes across restarts.

Do not run `docker compose down -v` unless you intend to remove both metadata and files. Do not mount the Docker socket into this container. On Linux container deployments, Windows host metrics are marked unavailable; they are not replaced with misleading container readings.

## Back up the native Windows database

The following command makes a consistent snapshot while the native app is running. Choose a new path outside the Git repository for each backup:

```powershell
cd backend
go run ./cmd/homeos backup 'D:\Backups\homeos-2026-10-10.sqlite'
```

The command refuses to overwrite an existing file and runs SQLite's integrity check on the backup. Keep a copy on another device, and protect it like the live database: it contains account and session data. The command backs up SQLite only. HomeDrive files in `backend/data/files` or the Docker `/data/files` volume need a separate filesystem backup; see [HomeDrive v0.2](v0.2-homedrive.md).

## Back up the Docker PostgreSQL database

Use `pg_dump` and keep its output together with a matching filesystem backup of the HomeDrive data volume. For example, from the repository root:

```powershell
docker compose exec -T postgres pg_dump -U homeos -d homeos -Fc -f /tmp/homeos.dump
docker compose cp postgres:/tmp/homeos.dump 'D:\Backups\homeos-postgres.dump'
```

Use a unique backup filename each time and test restoration. The metadata dump does not contain file bytes.

## Standalone SQLite backup

The native `homeos backup <new-path>` command applies only when `HOMEOS_DATABASE_URL` is unset. It creates and verifies a consistent SQLite snapshot for standalone development. PostgreSQL deployments must use PostgreSQL tooling.

For any restore, stop application writes, preserve the prior database and file volume, restore both data kinds from a consistent backup set, and verify sign-in plus representative downloads before discarding the prior copies.
