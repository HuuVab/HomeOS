package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

var ErrImportNotEmpty = errors.New("HomeDrive catalog is not empty for this owner")

// CheckDriveImport permits an import only into an empty PostgreSQL catalog.
// The same checks run inside ImportDriveSnapshot's transaction before writing.
func (s *Store) CheckDriveImport(ownerID int64) error {
	if !s.AuthoritativeMetadata() {
		return errors.New("drive import requires PostgreSQL metadata mode")
	}
	if ownerID < 1 {
		return errors.New("invalid owner ID")
	}
	return s.checkDriveImport(s.db, ownerID)
}

type importQuerier interface {
	QueryRow(string, ...any) *sql.Row
}

func (s *Store) checkDriveImport(q importQuerier, ownerID int64) error {
	var exists int
	if err := q.QueryRow(s.query(`SELECT 1 FROM users WHERE id = ?`), ownerID).Scan(&exists); err != nil {
		return fmt.Errorf("owner %d does not exist: %w", ownerID, err)
	}
	for _, table := range []string{"file_entries", "trash_entries", "upload_sessions", "metadata_reconcile"} {
		var count int
		if err := q.QueryRow(s.query(`SELECT COUNT(*) FROM `+table+` WHERE owner_id = ?`), ownerID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrImportNotEmpty
		}
	}
	return nil
}

// ImportDriveSnapshot publishes a selected owner's existing files, Trash, and
// uploads only when the owner has no catalog state. It never deletes records.
func (s *Store) ImportDriveSnapshot(ownerID int64, entries []drive.Entry, trash []drive.TrashEntry, uploads []drive.UploadSession) error {
	if err := s.CheckDriveImport(ownerID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(s.query(`SELECT id FROM users WHERE id = ? FOR UPDATE`), ownerID); err != nil {
		return err
	}
	if err := s.checkDriveImport(tx, ownerID); err != nil {
		return err
	}
	if err := s.reconcileEntriesTx(tx, ownerID, entries, true); err != nil {
		return err
	}
	if err := s.replaceTrashEntriesTx(tx, ownerID, trash); err != nil {
		return err
	}
	if err := s.reconcileUploadSessionsTx(tx, ownerID, uploads, entries); err != nil {
		return err
	}
	return tx.Commit()
}
