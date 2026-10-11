CREATE TABLE metadata_reconcile (
    owner_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    started_at INTEGER NOT NULL
);
