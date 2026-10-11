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
