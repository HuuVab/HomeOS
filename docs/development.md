# Local development

This is the first v0.1 code slice: a loopback-only Go API and a React dashboard with application-scoped status samples. Authentication and SQLite are not implemented yet. Do not expose this development build on a LAN or the Internet.

## Prerequisites

- Go 1.27 or newer
- Node.js 22.12 or newer and npm

## Run

In one terminal:

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

Open the local URL printed by Vite. Vite proxies `/api` to the Go server on `127.0.0.1:8080`.

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

The status endpoint reports only application scope metrics: process uptime and Go heap allocation. It does not claim to measure total host or container resource use.
