package store

import (
	"database/sql"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

type ChangeEvent struct {
	ID         int64  `json:"id"`
	Path       string `json:"path"`
	Operation  string `json:"operation"`
	Version    *int64 `json:"version"`
	OccurredAt int64  `json:"occurredAt"`
}

// ChangesAfter returns the next page of committed changes for one owner.
func (s *Store) ChangesAfter(ownerID, afterID int64, limit int) ([]ChangeEvent, error) {
	rows, err := s.db.Query(s.query(`SELECT id, path, operation, version, occurred_at FROM change_events
		WHERE owner_id = ? AND id > ? ORDER BY id LIMIT ?`), ownerID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]ChangeEvent, 0)
	for rows.Next() {
		var event ChangeEvent
		var version sql.NullInt64
		if err := rows.Scan(&event.ID, &event.Path, &event.Operation, &version, &event.OccurredAt); err != nil {
			return nil, err
		}
		if version.Valid {
			event.Version = &version.Int64
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) reconcileEntriesTx(tx *sql.Tx, ownerID int64, entries []drive.Entry, recordEvents bool) error {
	type previousEntry struct {
		kind                      string
		size, modifiedAt, version int64
	}
	previous := make(map[string]previousEntry)
	rows, err := tx.Query(s.query(`SELECT path, kind, size, modified_at, version FROM file_entries WHERE owner_id = ?`), ownerID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var path string
		var old previousEntry
		if err := rows.Scan(&path, &old.kind, &old.size, &old.modifiedAt, &old.version); err != nil {
			rows.Close()
			return err
		}
		previous[path] = old
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	now := time.Now().Unix()
	for _, entry := range entries {
		old, exists := previous[entry.Path]
		delete(previous, entry.Path)
		modifiedAt := entry.ModifiedAt.Unix()
		if exists && old.kind == entry.Kind && old.size == entry.Size && old.modifiedAt == modifiedAt {
			continue
		}
		version := int64(1)
		operation := "create"
		if exists {
			version = old.version + 1
			operation = "update"
		}
		if _, err := tx.Exec(s.query(`INSERT INTO file_entries (owner_id, path, kind, size, modified_at, version) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (owner_id, path) DO UPDATE SET kind = EXCLUDED.kind, size = EXCLUDED.size, modified_at = EXCLUDED.modified_at, version = EXCLUDED.version`),
			ownerID, entry.Path, entry.Kind, entry.Size, modifiedAt, version); err != nil {
			return err
		}
		if recordEvents {
			if _, err := tx.Exec(s.query(`INSERT INTO change_events (owner_id, path, operation, version, occurred_at) VALUES (?, ?, ?, ?, ?)`), ownerID, entry.Path, operation, version, now); err != nil {
				return err
			}
		}
	}
	for path := range previous {
		if _, err := tx.Exec(s.query(`DELETE FROM file_entries WHERE owner_id = ? AND path = ?`), ownerID, path); err != nil {
			return err
		}
		if recordEvents {
			if _, err := tx.Exec(s.query(`INSERT INTO change_events (owner_id, path, operation, occurred_at) VALUES (?, ?, 'delete', ?)`), ownerID, path, now); err != nil {
				return err
			}
		}
	}
	return nil
}
