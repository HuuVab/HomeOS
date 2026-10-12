# HomeDrive storage consistency redesign

Status: design and durability prerequisite only. The current filesystem-first
mutation path remains active; this document does not claim it is atomic.

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

Folder move/copy/Trash operations must update all affected catalog rows in one
transaction. Moving a file preserves an identity separate from its path so
version history follows it. A restore publishes a new revision pointing at an
already verified blob. Permanent deletion removes catalog references first;
retention-aware garbage collection reclaims bytes later.

## Recovery and audit

Startup replays no filesystem-to-database reconciliation. It checks incomplete
upload transactions against PostgreSQL, discards orphan staging files, and
verifies referenced blob existence and size. A full checksum audit remains an
explicit maintenance operation, not a per-request or startup prerequisite.
Any missing or corrupted committed blob makes affected content unavailable
with a diagnostic; it must not be replaced by an uncommitted on-disk file.

## Migration and rollback

Before switching the live server, stop writes and make a paired `pg_dump` and
filesystem backup. Stream each existing active and trashed file to immutable
blobs, verify size and SHA-256, and populate a new catalog schema without
changing the old tables. Compare counts and byte totals, then switch reads
and mutations behind an explicit storage-format version. Keep the old tree and
schema until the new server passes restart, restore, upload, Trash, retention,
and crash-injection tests. Rollback requires the paired backup; do not mix a
new catalog with an old byte tree.

The migration must preserve the existing 2 GiB upload path. PostgreSQL `bytea`
is not the primary file store. The older `.homeos-upload-*.json` markers and
mutable live/Trash paths are removed only after equivalent catalog-backed
flows are implemented and the old-mode migration is verified.
