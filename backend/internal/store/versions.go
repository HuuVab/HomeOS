package store

import (
	"database/sql"
	"errors"
)

type FileVersion struct {
	Version   int64  `json:"version"`
	Size      int64  `json:"size"`
	Checksum  string `json:"checksum"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) FileVersions(ownerID int64, path string) ([]FileVersion, error) {
	rows, err := s.db.Query(s.query(`SELECT version, size, COALESCE(checksum, ''), created_at FROM file_versions
		WHERE owner_id = ? AND path = ? ORDER BY version DESC`), ownerID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]FileVersion, 0)
	for rows.Next() {
		var item FileVersion
		if err := rows.Scan(&item.Version, &item.Size, &item.Checksum, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) FileVersion(ownerID int64, path string, version int64) (FileVersion, error) {
	var item FileVersion
	err := s.db.QueryRow(s.query(`SELECT version, size, COALESCE(checksum, ''), created_at FROM file_versions
		WHERE owner_id = ? AND path = ? AND version = ?`), ownerID, path, version).
		Scan(&item.Version, &item.Size, &item.Checksum, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FileVersion{}, sql.ErrNoRows
	}
	return item, err
}
