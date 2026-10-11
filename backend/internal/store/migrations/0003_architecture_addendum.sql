ALTER TABLE file_entries ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE file_versions (
    id INTEGER PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    version INTEGER NOT NULL,
    size INTEGER NOT NULL,
    checksum TEXT,
    storage_path TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (owner_id, path, version)
);

CREATE TABLE shares (
    id TEXT PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write')),
    token_hash TEXT UNIQUE,
    expires_at INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE upload_sessions (
    id TEXT PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    expected_size INTEGER NOT NULL,
    received_size INTEGER NOT NULL DEFAULT 0,
    fingerprint TEXT NOT NULL,
    checksum TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'complete', 'failed')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE change_events (
    id INTEGER PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'move', 'delete')),
    version INTEGER,
    occurred_at INTEGER NOT NULL
);

CREATE INDEX change_events_owner_id_id_idx ON change_events(owner_id, id);
CREATE INDEX upload_sessions_owner_id_status_idx ON upload_sessions(owner_id, status);
