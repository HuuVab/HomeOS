package store

import (
	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

// CatalogUsage counts each immutable blob once, including retained history
// and Trash. Pending uploads reserve their full expected size because staged
// bytes are not yet committed catalog objects.
func (s *Store) CatalogUsage(ownerID, maxFileBytes, quotaBytes int64) (drive.StorageUsage, error) {
	usage := drive.StorageUsage{QuotaBytes: quotaBytes, MaxFileBytes: maxFileBytes}
	if !s.AuthoritativeMetadata() || ownerID < 1 {
		return usage, drive.ErrInvalidPath
	}
	rows, err := s.db.Query(`SELECT checksum,size FROM file_entries WHERE owner_id=$1 AND checksum IS NOT NULL
		UNION ALL SELECT checksum,size FROM file_versions WHERE owner_id=$1 AND checksum IS NOT NULL
		UNION ALL SELECT checksum,size FROM trash_blobs WHERE owner_id=$1 AND checksum IS NOT NULL`, ownerID)
	if err != nil {
		return usage, err
	}
	sizes := make(map[string]int64)
	for rows.Next() {
		var checksum string
		var size int64
		if err := rows.Scan(&checksum, &size); err != nil {
			rows.Close()
			return usage, err
		}
		if old, exists := sizes[checksum]; exists && old != size {
			rows.Close()
			return usage, drive.ErrInvalidPath
		}
		sizes[checksum] = size
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return usage, err
	}
	rows.Close()
	for _, size := range sizes {
		usage.UsedBytes += size
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(expected_size),0) FROM upload_sessions
		WHERE owner_id=$1 AND status='pending'`, ownerID).Scan(&usage.ReservedBytes); err != nil {
		return usage, err
	}
	return usage, nil
}
