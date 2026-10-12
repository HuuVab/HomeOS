package store

import (
	"database/sql"
	"errors"
	"os"
	"path"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

var ErrCatalogRevision = errors.New("HomeDrive source revision changed")

func (s *Store) catalogTx(ownerID int64) (*sql.Tx, error) {
	if s.dialect != "postgres" || ownerID < 1 {
		return nil, drive.ErrInvalidPath
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, ownerID); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (s *Store) catalogParentTx(tx *sql.Tx, ownerID int64, name string) error {
	if !drive.ValidCatalogPath(name) {
		return drive.ErrInvalidPath
	}
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	var kind string
	err := tx.QueryRow(`SELECT kind FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, parent).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return os.ErrNotExist
	}
	if err != nil {
		return err
	}
	if kind != "folder" {
		return drive.ErrInvalidPath
	}
	return nil
}

func (s *Store) CatalogCreateFolder(ownerID int64, name string) error {
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.catalogParentTx(tx, ownerID, name); err != nil {
		return err
	}
	now := time.Now().Unix()
	result, err := tx.Exec(`INSERT INTO file_entries(owner_id,path,kind,size,modified_at,version)
		VALUES($1,$2,'folder',0,$3,1) ON CONFLICT(owner_id,path) DO NOTHING`, ownerID, name, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return os.ErrExist
	}
	if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,version,occurred_at) VALUES($1,$2,'create',1,$3)`, ownerID, name, now); err != nil {
		return err
	}
	return tx.Commit()
}

// CatalogCommitFile publishes a durable immutable blob in one PostgreSQL
// transaction. expectedVersion=0 means create; a positive value means replace.
func (s *Store) CatalogCommitFile(ownerID int64, name, checksum string, size, expectedVersion int64, uploadID ...string) (int64, error) {
	if !drive.ValidCatalogPath(name) || !validCatalogChecksum(checksum) || size < 0 || expectedVersion < 0 {
		return 0, drive.ErrInvalidPath
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := s.catalogParentTx(tx, ownerID, name); err != nil {
		return 0, err
	}
	var kind string
	var currentVersion int64
	err = tx.QueryRow(`SELECT kind,version FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, name).Scan(&kind, &currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	exists := err == nil
	if expectedVersion == 0 && exists {
		return 0, os.ErrExist
	}
	if expectedVersion > 0 && !exists {
		return 0, os.ErrNotExist
	}
	if exists && (kind != "file" || currentVersion != expectedVersion) {
		return 0, ErrCatalogRevision
	}
	version := int64(1)
	operation := "create"
	if exists {
		version, operation = currentVersion+1, "update"
	} else {
		if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM file_versions WHERE owner_id=$1 AND path=$2`, ownerID, name).Scan(&version); err != nil {
			return 0, err
		}
	}
	now := time.Now().Unix()
	_, err = tx.Exec(`INSERT INTO file_entries(owner_id,path,kind,size,modified_at,version,checksum)
		VALUES($1,$2,'file',$3,$4,$5,$6)
		ON CONFLICT(owner_id,path) DO UPDATE SET size=EXCLUDED.size,modified_at=EXCLUDED.modified_at,version=EXCLUDED.version,checksum=EXCLUDED.checksum`, ownerID, name, size, now, version, checksum)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO file_versions(owner_id,path,version,size,checksum,storage_path,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, ownerID, name, version, size, checksum, ".homeos-versions/"+checksum, now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,version,occurred_at)
		VALUES($1,$2,$3,$4,$5)`, ownerID, name, operation, version, now); err != nil {
		return 0, err
	}
	if len(uploadID) > 1 {
		return 0, drive.ErrInvalidPath
	}
	if len(uploadID) == 1 {
		result, err := tx.Exec(`UPDATE upload_sessions SET status='complete',received_size=$1,checksum=$2,updated_at=$3
			WHERE owner_id=$4 AND id=$5 AND path=$6 AND status='pending' AND expected_size=$1
			AND received_size=$1 AND fingerprint=$2`, size, checksum, now, ownerID, uploadID[0], name)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if count != 1 {
			return 0, ErrCatalogRevision
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) CatalogRestoreVersion(ownerID int64, name string, version, expectedVersion int64) error {
	if !drive.ValidCatalogPath(name) || version < 1 || expectedVersion < 1 {
		return drive.ErrInvalidPath
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	var current int64
	if err := tx.QueryRow(`SELECT kind,version FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, name).Scan(&kind, &current); err != nil {
		return err
	}
	if kind != "file" || current != expectedVersion {
		return ErrCatalogRevision
	}
	var checksum string
	var size int64
	if err := tx.QueryRow(`SELECT checksum,size FROM file_versions WHERE owner_id=$1 AND path=$2 AND version=$3`, ownerID, name, version).Scan(&checksum, &size); err != nil {
		return err
	}
	now := time.Now().Unix()
	if _, err := tx.Exec(`UPDATE file_entries SET checksum=$1,size=$2,version=$3,modified_at=$4 WHERE owner_id=$5 AND path=$6`, checksum, size, current+1, now, ownerID, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO file_versions(owner_id,path,version,size,checksum,storage_path,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, ownerID, name, current+1, size, checksum, ".homeos-versions/"+checksum, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,version,occurred_at) VALUES($1,$2,'update',$3,$4)`, ownerID, name, current+1, now); err != nil {
		return err
	}
	return tx.Commit()
}

func validCatalogChecksum(checksum string) bool {
	if len(checksum) != 64 {
		return false
	}
	for _, char := range checksum {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
