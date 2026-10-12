package store

import (
	"database/sql"
	"errors"
	"time"
)

// StorageFormat defaults to legacy for pre-migration users. No request may
// infer blob mode just because a directory or a blob happens to exist.
func (s *Store) StorageFormat(ownerID int64) (string, error) {
	if s.dialect != "postgres" {
		return "legacy", nil
	}
	var format string
	err := s.db.QueryRow(`SELECT format FROM owner_storage WHERE owner_id = $1`, ownerID).Scan(&format)
	if errors.Is(err, sql.ErrNoRows) {
		return "legacy", nil
	}
	return format, err
}

// ActivateBlobStorage is called only by the verified, offline migration.
// An owner cannot be silently changed back to the mutable legacy format.
func (s *Store) ActivateBlobStorage(ownerID int64) error {
	if s.dialect != "postgres" {
		return errors.New("blob storage requires PostgreSQL")
	}
	_, err := s.db.Exec(`INSERT INTO owner_storage(owner_id,format,migrated_at) VALUES($1,'blobs',$2)
		ON CONFLICT(owner_id) DO UPDATE SET format='blobs', migrated_at=EXCLUDED.migrated_at
		WHERE owner_storage.format='legacy'`, ownerID, time.Now().Unix())
	return err
}
