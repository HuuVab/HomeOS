package store

import (
	"database/sql"
	"errors"
)

// PruneFileVersions removes only old history. Active files always retain their
// newest keepLatest versions; deleted paths retain versions only until cutoff.
func (s *Store) PruneFileVersions(ownerID, cutoff int64, keepLatest int) (int, error) {
	if !s.AuthoritativeMetadata() || ownerID < 1 || keepLatest < 1 {
		return 0, errors.New("version pruning requires PostgreSQL, an owner, and a positive keep count")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int64
	if err := tx.QueryRow(s.query(`SELECT id FROM users WHERE id = ? FOR UPDATE`), ownerID).Scan(&exists); err != nil {
		return 0, err
	}
	rows, err := tx.Query(s.query(`SELECT v.id, v.path, v.created_at, e.path IS NOT NULL
		FROM file_versions v LEFT JOIN file_entries e ON e.owner_id = v.owner_id AND e.path = v.path AND e.kind = 'file'
		WHERE v.owner_id = ? ORDER BY v.path, v.version DESC`), ownerID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	lastPath := ""
	rank := 0
	for rows.Next() {
		var id, createdAt int64
		var path string
		var active bool
		if err := rows.Scan(&id, &path, &createdAt, &active); err != nil {
			rows.Close()
			return 0, err
		}
		if path != lastPath {
			lastPath, rank = path, 0
		}
		rank++
		if createdAt < cutoff && (!active || rank > keepLatest) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, id := range ids {
		if _, err := tx.Exec(s.query(`DELETE FROM file_versions WHERE id = ? AND owner_id = ?`), id, ownerID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// ReferencedVersionChecksums includes current file checksums as an additional
// guard against removing bytes still needed by the live Drive.
func (s *Store) ReferencedVersionChecksums(ownerID int64) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, query := range []string{
		`SELECT checksum FROM file_versions WHERE owner_id = ? AND checksum IS NOT NULL`,
		`SELECT checksum FROM file_entries WHERE owner_id = ? AND checksum IS NOT NULL`,
		`SELECT checksum FROM trash_blobs WHERE owner_id = ? AND checksum IS NOT NULL`,
	} {
		rows, err := s.db.Query(s.query(query), ownerID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var checksum sql.NullString
			if err := rows.Scan(&checksum); err != nil {
				rows.Close()
				return nil, err
			}
			if checksum.Valid {
				result[checksum.String] = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}
