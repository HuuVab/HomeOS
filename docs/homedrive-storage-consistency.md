# HomeDrive storage consistency redesign

Status: implemented for PostgreSQL owners whose `owner_storage.format` is
`blobs`. The local owner was migrated on 2026-10-12. Pre-migration owners and
SQLite standalone deployments retain the legacy filesystem-first path.

## Authority and visibility

PostgreSQL owns the live namespace, revisions, Trash state, upload sessions,
and the references to immutable content blobs. The filesystem owns only blob
bytes and temporary upload bytes. A blob's SHA-256 is its name. User-visible
reads resolve a committed PostgreSQL revision to a blob; they never read an
uncommitted mutable path. A database transaction publishes a namespace change
only after every referenced blob has been written, synced, and verified.

There is no cross-resource transaction. A crash before PostgreSQL commit can
leave an unreferenced blob, but no visible partial file. A crash after commit
must find every referenced blob, or fail closed and report corruption rather
than silently adopting different bytes. Garbage collection removes only blobs
that a PostgreSQL snapshot shows to be unreferenced, with concurrent commits
excluded while the reference set is obtained and deletion completes.

## Mutation protocol

1. Authorize against the committed catalog and check the source revision.
2. Stream new bytes into a private temporary blob while computing SHA-256.
   Sync the file, then atomically rename it to its digest name. Never modify
   an existing digest-named blob in place.
3. In one PostgreSQL transaction, check the revision again and update active
   entries, file versions, Trash or upload-session rows, and change events.
   The transaction's commit is the visibility point.
4. Discard staging bytes after a rejected transaction. Sweep abandoned staging
   bytes after restart; do not infer committed metadata from their presence.

Folder move/copy/Trash operations update affected catalog rows in one
transaction. Version history remains path-based: moving a file starts history
at its new path. A restore publishes a new revision pointing at an
already committed blob. Permanent deletion removes catalog references first;
retention-aware garbage collection reclaims bytes later.

## Recovery and audit

Startup replays no filesystem-to-database reconciliation. It checks incomplete
upload transactions against PostgreSQL, discards orphan staging files, and
verifies referenced blob existence and size. The offline migration verifies
full SHA-256 once; normal reconciliation does not hash every file. Missing
or wrong-size committed blobs make affected content unavailable with a storage
error and are never replaced by an uncommitted on-disk file. Same-size external
bit corruption is not detected by the startup check and requires a separate
full checksum audit. The filesystem and PostgreSQL do not share a literal
transaction; durability still depends on the filesystem honoring file sync
and rename semantics after a sudden power loss.

## Migration and rollback

Before switching the live server, stop writes and make a paired `pg_dump` and
filesystem backup. Run `homeos migrate-blobs <owner-id>` for a dry run, then
`homeos migrate-blobs <owner-id> --apply`. It verifies existing active and
trashed content against committed metadata, creates immutable blobs, and
atomically activates the owner format. The local backup is under
`backend/data/backups/blob-cutover-20261012-183014`. Rollback requires the
paired backup; do not mix a new catalog with an old byte tree.

The existing 2 GiB upload path remains filesystem-backed for bytes;
PostgreSQL `bytea` is not the primary file store. Blob-mode uploads use
PostgreSQL session rows and disposable staging bytes, without JSON sidecars.
Legacy files and sidecars are retained only for pre-migration and SQLite mode.
