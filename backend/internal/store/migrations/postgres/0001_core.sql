CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    revoked_at BIGINT
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);

CREATE TABLE file_entries (
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('file', 'folder')),
    size BIGINT NOT NULL,
    modified_at BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (owner_id, path)
);

CREATE TABLE file_versions (
    id BIGSERIAL PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    version BIGINT NOT NULL,
    size BIGINT NOT NULL,
    checksum TEXT,
    storage_path TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    UNIQUE (owner_id, path, version)
);

CREATE TABLE shares (
    id TEXT PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write')),
    token_hash TEXT UNIQUE,
    expires_at BIGINT,
    created_at BIGINT NOT NULL
);

CREATE TABLE upload_sessions (
    id TEXT PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    expected_size BIGINT NOT NULL,
    received_size BIGINT NOT NULL DEFAULT 0,
    fingerprint TEXT NOT NULL,
    checksum TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'complete', 'failed')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL
);

CREATE TABLE change_events (
    id BIGSERIAL PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'move', 'delete')),
    version BIGINT,
    occurred_at BIGINT NOT NULL
);
CREATE INDEX change_events_owner_id_id_idx ON change_events(owner_id, id);
CREATE INDEX upload_sessions_owner_id_status_idx ON upload_sessions(owner_id, status);
