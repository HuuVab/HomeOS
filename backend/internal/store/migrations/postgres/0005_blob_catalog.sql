CREATE TABLE owner_storage (
    owner_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    format TEXT NOT NULL CHECK (format IN ('legacy', 'blobs')),
    migrated_at BIGINT
);

CREATE TABLE trash_blobs (
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    trash_id TEXT NOT NULL REFERENCES trash_entries(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('file', 'folder')),
    size BIGINT NOT NULL,
    modified_at BIGINT NOT NULL,
    version BIGINT NOT NULL,
    checksum TEXT,
    PRIMARY KEY (owner_id, trash_id, path),
    CHECK ((kind = 'folder' AND checksum IS NULL) OR
           (kind = 'file' AND checksum ~ '^[0-9a-f]{64}$'))
);

CREATE INDEX trash_blobs_owner_checksum_idx ON trash_blobs(owner_id, checksum);
