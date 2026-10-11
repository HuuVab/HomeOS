# Local development

This development build includes a loopback-only Go API, PostgreSQL-capable metadata storage, and a React dashboard with application-scoped status samples. Do not expose this development build on a LAN or the Internet.

## Prerequisites

- Go 1.27 or newer
- Node.js 22.12 or newer and npm
- PostgreSQL 18 when testing the server deployment path

## Run

First, create the administrator in a terminal. The command prompts for the password without echoing it:

```powershell
cd backend
go run ./cmd/homeos create-admin <username>
```

With no database environment variables, the app uses standalone SQLite at `backend/data/homeos.sqlite`. This mode exists for local evaluation and future offline-client work; deployed servers should set PostgreSQL as the sole metadata authority:

```powershell
$env:HOMEOS_DATABASE_URL = 'postgres://homeos:your-password@127.0.0.1:5432/homeos?sslmode=disable'
```

Alternatively set `HOMEOS_DATABASE_PASSWORD_FILE` and omit the password from the URL. The file is read at startup and its trimmed contents are injected into the connection URL. Database migrations run automatically. Do not commit database files, credentials, or backups.

If administrator access is lost, run `go run ./cmd/homeos reset-admin-password <username>` locally. It prompts for a new password and revokes existing sessions.

Then start the API in one terminal:

```powershell
cd backend
go run ./cmd/homeos
```

In another terminal:

```powershell
cd frontend
npm ci
npm run dev
```

Open the local URL printed by Vite and sign in with the administrator credentials. Vite proxies `/api` to the Go server on `127.0.0.1:8080`.

To build the static frontend and serve it from Go:

```powershell
cd frontend
npm run build
cd ..\backend
$env:HOMEOS_WEB_DIR = '..\frontend\dist'
go run ./cmd/homeos
```

Open `http://127.0.0.1:8080`.

## Verify

```powershell
cd backend
go test ./...
cd ..\frontend
npm run build
```

To exercise the PostgreSQL metadata path, set `HOMEOS_TEST_POSTGRES_URL` to a disposable PostgreSQL database URL and run `go test ./internal/store -run TestPostgresMetadataIntegration -v` from `backend`. The test creates and drops an isolated random schema; it skips when the variable is unset. PostgreSQL-backed HomeDrive serves metadata reads from the database, while standalone SQLite keeps filesystem-backed reads.

Do not point a fresh PostgreSQL database at an existing SQLite-mode HomeDrive file directory expecting automatic import. PostgreSQL mode treats its catalog as authoritative for listings and rejects filesystem/catalog drift on writes. To import one existing owner's files into an **empty** PostgreSQL catalog, stop the HomeOS server, back up the old database and file directory, set `HOMEOS_DATABASE_URL` and the same `HOMEOS_DATA_DIR` used by the files, then run `go run ./cmd/homeos drive-import <owner-id>` to inspect the dry-run counts. Re-run with `--apply` only after checking them. The command refuses owners that already have file, Trash, upload, or pending reconciliation records; it does not repair or overwrite a populated catalog. Start the server after import. PostgreSQL itself can run locally or remotely; Docker is optional.

Resumable upload PATCH requests must send `X-Chunk-SHA256` as the lowercase or uppercase hexadecimal SHA-256 of the exact chunk body. PostgreSQL-backed move, copy, and Trash requests must send `X-Source-Version` from the listed entry; stale versions return HTTP 412. SQLite standalone mode does not require this header.

On native Windows, the status endpoint reports host CPU usage, physical memory usage, and usage of the disk containing the HomeOS data directory. It also reports application uptime and Go heap allocation. The dashboard labels every sample by scope. On other platforms, host metrics are marked unavailable until a platform-specific collector is implemented; the app does not present container metrics as host metrics.

Sessions expire after 24 hours. Login attempts are limited to 10 per source IP per minute. Passwords use Argon2id with 19 MiB of memory, two iterations, one thread, and a unique random salt. Session cookies are `HttpOnly` and `SameSite=Strict`; they are marked `Secure` when the Go server handles HTTPS directly. Use `pg_dump` for PostgreSQL. In standalone SQLite mode, back up the database and its `-wal`/`-shm` companions with a SQLite-aware procedure while the app is running, or after stopping the app.
