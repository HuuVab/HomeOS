# HomeDrive — Architecture Addendum

> **Scope:** New ideas to add to the existing HomeDrive roadmap. Keep all current features and phases unchanged; this is an additive update, not a redesign.

## New technology decisions

- **PostgreSQL — authoritative server metadata:** Store users, file/folder records, ownership, permissions, versions, shares, upload sessions, and change events. Keep file bytes in the filesystem.
- **SQLite — local/offline state:** Embed SQLite in a future desktop sync client for indexed local files, remote revision IDs, pending changes, retry queues, and sync conflict tracking. Optionally use SQLite for standalone deployments, but do not duplicate PostgreSQL as a second server source of truth.
- **Redis — ephemeral acceleration:** Cache frequently accessed metadata, enforce rate limits, track temporary upload progress, and deliver short-lived notifications. PostgreSQL remains authoritative; cached permissions require careful invalidation.
- **gRPC + Protocol Buffers — internal APIs:** Define typed contracts between the Go gateway and internal metadata/sync services. Keep browser-facing APIs as HTTPS/REST and use HTTP streaming for large file transfers initially.

## Additional implementation ideas

- Add a **metadata gRPC service** backed by PostgreSQL, with methods such as `GetFile`, `ListFiles`, and `CreateFolder`.
- Add **resumable upload sessions** with durable metadata, chunk checksums, and crash recovery.
- Add a **server change feed** so future desktop clients can synchronize incrementally.
- Add a **desktop sync prototype** using SQLite to persist offline edits, pending operations, and last-synced versions.
- Add **Redis cache-aside** for metadata and **rate limiting**, with explicit invalidation on changes.
- Add **optimistic concurrency/version checks** to detect simultaneous edits.
- Consider **NATS JetStream + transactional outbox** later for reliable thumbnail, indexing, and notification jobs.
- Add **OpenTelemetry, Prometheus, and Grafana** later for tracing and monitoring across services.

## Suggested incremental order

1. Keep the existing HomeDrive MVP and add PostgreSQL metadata models.
2. Extract a small Go metadata service using gRPC/Protobuf.
3. Add Redis for rate limiting and selected metadata caching.
4. Add durable upload sessions, revisions, and a change feed.
5. Prototype a Windows sync client with embedded SQLite.
6. Introduce event messaging and observability only when needed.

**Design rule:** One source of truth per kind of data; introduce each technology for a clear responsibility rather than complexity alone.

## Current implementation status

HomeDrive now writes a durable per-user reconciliation marker before a file-mutating HTTP request. It buffers the response until the filesystem snapshot and metadata rows commit together in the database; if that commit fails, the API returns 503 and retains the marker. On restart, the server replays pending markers before accepting traffic. This prevents a successful response from hiding a metadata-sync failure and makes an interrupted update recoverable.

The next increment preserves a per-path metadata revision during reconciliation and writes create/update/delete events in the same database transaction. Authenticated clients can page through their own events with `GET /api/v1/drive/changes?after=<cursor>` (up to 100 events per page). Unchanged snapshots emit no events. Moves currently appear as a delete plus a create; these revision numbers are metadata counters, not restorable file versions or content checksums.

PostgreSQL deployments now serve file listings, search results, previews, and downloads from committed metadata records. Normal startup no longer rescans the filesystem into PostgreSQL; only an interrupted mutation's recovery marker triggers reconciliation. SQLite standalone mode keeps its existing filesystem-backed reads. The isolated PostgreSQL integration test passed against local PostgreSQL 17 on 2026-10-11. Docker is optional.

PostgreSQL-backed mutations now check that the filesystem matches committed metadata before proceeding, and reject catalog drift with `409 metadata_drift` instead of silently importing out-of-band files. Mutation sources and destinations are also checked against the catalog. An interrupted mutation can still be reconciled on restart or on the next request.

PostgreSQL-backed Trash now has owner-scoped database records; listings and restore/permanent-delete authorization use those records. Active resumable-upload sessions now have database ownership, offset, and status records. File and Trash metadata reconcile together, and upload status is reconciled from staged bytes at startup. Retention cleanup updates Trash metadata for every affected user. Disk sidecars remain as recovery artifacts and in standalone SQLite mode.

An explicit `drive-import <owner-id> [--apply]` command now provides a dry-run-first import of files, Trash, and active uploads into an empty PostgreSQL catalog for one owner. It refuses a populated catalog and is intended to run while the server is stopped; it is not a general repair tool. Resumable upload PATCH requests now verify a SHA-256 checksum for each chunk before advancing the offset. PostgreSQL file listings expose per-path revisions, and move/copy/Trash requests require the source revision, rejecting stale requests with HTTP 412.

The Go gateway now calls a typed, PostgreSQL-backed metadata gRPC service for `GetFile`, `ListFiles`, and `CreateFolder`. The service runs over an in-process transport rather than a separately deployed network service; the browser API remains REST. PostgreSQL integration tests cover the service and the HTTP gateway path. This completes the first small gRPC increment, not a full service extraction or sync API.

Optional Redis acceleration is now available when `HOMEOS_REDIS_URL` is set. It provides an atomic 10-attempt/60-second login limit per source IP and a short-lived, owner-scoped cache for metadata folder listings. Successful HomeDrive mutations advance an owner generation to invalidate cached listings; PostgreSQL remains authoritative. If Redis becomes unavailable after startup, metadata reads fall back to PostgreSQL and login limiting falls back to the existing per-process limiter. Cached listings expire after 10 seconds, bounding stale data if invalidation fails. Redis is not installed or started by the Windows PostgreSQL setup. The local Windows launcher now enables acceleration when the user's Redis container is reachable on `127.0.0.1:6379`, and otherwise starts HomeOS without Redis.

A manual Windows desktop sync prototype now uses a separate local SQLite database for file baselines, queued offline changes, retry timing, conflict records, account pinning, and a change-feed cursor. `homeos-sync.exe` can pull new remote files and upload new local files/folders through the authenticated REST API. It preserves existing bytes when edits, deletions, remote updates, or collisions need resolution; it does not yet run continuously, overwrite or delete remotely, or transfer files above 32 MiB. The Windows setup builds the CLI but does not launch it automatically. See `docs/homedrive-sync-prototype.md`.

HomeDrive now computes full-file SHA-256 checksums during reconciliation and keeps immutable content-addressed snapshots under each owner's hidden `.homeos-versions` directory. PostgreSQL records the checksum on the current file and links each committed file revision to a snapshot. Authenticated clients can list a file's history at `GET /api/v1/drive/versions?path=...` and restore a selected version with `POST /api/v1/drive/versions/restore`, supplying `{ "path": "...", "version": N }`, the CSRF token, and the current `X-Source-Version`. Restoration checks the archived byte count and SHA-256 before replacing the live file; reconciliation then records the restored bytes as a new revision. Existing files without checksums can be backfilled with `homeos drive-repair <owner-id> [--apply]`: the default is a dry-run report. Apply can add catalog rows missing for on-disk files and correct metadata whose bytes still match the committed checksum, but refuses missing disk bytes, changes of file kind, or unverified content drift. Stop the server and back up PostgreSQL and the file tree before applying. It does not reconstruct missing file bytes or corrupted snapshots.

The Drive details panel now lists saved versions and offers a confirmed restore action with the current file revision as a precondition. On the local installation, four pre-existing file checksums were backfilled after a paired PostgreSQL/file backup; a follow-up audit reported zero missing checksums or catalog mismatches.

This is still not an atomic filesystem-and-database transaction or complete sidecar removal: successful byte operations happen before the metadata commit, and crash recovery still rebuilds state from the filesystem. Snapshot storage consumes additional disk space and currently has no retention/pruning policy; reconciliation hashes and copies every active file, so large drives incur significant I/O. Version history follows paths rather than file identities, and moving a file starts history at its new path. Revision checks cover move/copy/Trash sources and version restore, not every possible concurrent operation. A full bidirectional desktop client, event messaging, and observability remain later addendum phases.

The local HomeDrive installation was migrated from standalone SQLite to PostgreSQL 17 with `migrate-postgres --apply` after backing up the SQLite database and file directory. One account and six file/folder entries were verified in PostgreSQL. `scripts/start-homeos-postgres.ps1` now selects PostgreSQL and the existing file directory for local startup; the old SQLite database remains available for rollback.

A native Windows first-install setup script now detects PostgreSQL 17, launches its WinGet installer if missing, provisions an empty HomeOS database/role without overwriting existing state, builds the app, and registers a HomeOS scheduled task at user sign-in. PostgreSQL's own service is set to automatic startup. Installation and initial account creation still require interactive password entry; HomeOS does not run before user login.
