# Local development

This v0.1 development build includes a loopback-only Go API, SQLite-backed administrator authentication, and a React dashboard with application-scoped status samples. Do not expose this development build on a LAN or the Internet.

## Prerequisites

- Go 1.27 or newer
- Node.js 22.12 or newer and npm

## Run

First, create the administrator in a terminal. The command prompts for the password without echoing it:

```powershell
cd backend
go run ./cmd/homeos create-admin <username>
```

The SQLite database is created at `backend/data/homeos.sqlite` by default. This directory is ignored by Git. Do not commit database files or backups.

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

On native Windows, the status endpoint reports host CPU usage, physical memory usage, and usage of the disk containing the HomeOS data directory. It also reports application uptime and Go heap allocation. The dashboard labels every sample by scope. On other platforms, host metrics are marked unavailable until a platform-specific collector is implemented; the app does not present container metrics as host metrics.

Sessions expire after 24 hours. Login attempts are limited to 10 per source IP per minute. Passwords use Argon2id with 19 MiB of memory, two iterations, one thread, and a unique random salt. Session cookies are `HttpOnly` and `SameSite=Strict`; they are marked `Secure` when the Go server handles HTTPS directly. The database file and its `-wal`/`-shm` companions must be backed up together using a SQLite-aware backup procedure while the app is running, or after stopping the app.
