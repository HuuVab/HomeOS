package store

import "errors"

// CatalogBlobReferences enumerates the committed byte objects needed by
// active files, Trash, and retained versions. It reads only PostgreSQL rows.
func (s *Store) CatalogBlobReferences(ownerID int64) (map[string]int64, error) {
	if !s.AuthoritativeMetadata() || ownerID < 1 {
		return nil, errors.New("blob references require PostgreSQL and an owner")
	}
	rows, err := s.db.Query(`SELECT checksum,size FROM file_entries WHERE owner_id=$1 AND checksum IS NOT NULL
		UNION ALL SELECT checksum,size FROM trash_blobs WHERE owner_id=$1 AND checksum IS NOT NULL
		UNION ALL SELECT checksum,size FROM file_versions WHERE owner_id=$1 AND checksum IS NOT NULL`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var checksum string
		var size int64
		if err := rows.Scan(&checksum, &size); err != nil {
			return nil, err
		}
		if !validCatalogChecksum(checksum) || size < 0 {
			return nil, errors.New("invalid committed blob reference")
		}
		if old, exists := result[checksum]; exists && old != size {
			return nil, errors.New("inconsistent committed blob size")
		}
		result[checksum] = size
	}
	return result, rows.Err()
}
