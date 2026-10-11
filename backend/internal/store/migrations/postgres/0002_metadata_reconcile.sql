CREATE TABLE metadata_reconcile (
    owner_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    started_at BIGINT NOT NULL
);
