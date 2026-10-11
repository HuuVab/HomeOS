CREATE TABLE trash_entries (
    id TEXT PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    deleted_at BIGINT NOT NULL
);
CREATE INDEX trash_entries_owner_deleted_idx ON trash_entries(owner_id, deleted_at);
