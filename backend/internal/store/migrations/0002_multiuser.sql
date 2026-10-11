CREATE TABLE sessions_saved AS SELECT * FROM sessions;
DROP TABLE sessions;
CREATE TABLE users_new (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
INSERT INTO users_new SELECT * FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
);
INSERT INTO sessions SELECT * FROM sessions_saved;
DROP TABLE sessions_saved;
CREATE INDEX sessions_user_id_idx ON sessions(user_id);

CREATE TABLE file_entries (
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('file', 'folder')),
    size INTEGER NOT NULL,
    modified_at INTEGER NOT NULL,
    PRIMARY KEY (owner_id, path)
);
