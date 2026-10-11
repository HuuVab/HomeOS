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

This is still not an atomic filesystem-and-database transaction or complete sidecar removal: successful byte operations happen before the metadata commit, and crash recovery still rebuilds state from the filesystem. True restorable content versions, full-file checksums, and repair of an existing PostgreSQL catalog remain open work. Revision checks currently cover move/copy/Trash sources, not every possible concurrent operation. The integration test covers metadata, Trash, and upload records and now runs successfully against a local PostgreSQL 17 test database. The gRPC metadata service, Redis, desktop sync client, event messaging, and observability remain later addendum phases.
