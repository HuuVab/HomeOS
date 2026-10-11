package store

import (
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

func (s *Store) TrashEntries(ownerID int64) ([]drive.TrashEntry, error) {
	rows, err := s.db.Query(s.query(`SELECT id, path, deleted_at FROM trash_entries WHERE owner_id = ? ORDER BY deleted_at DESC, id`), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]drive.TrashEntry, 0)
	for rows.Next() {
		var entry drive.TrashEntry
		var deletedAt int64
		if err := rows.Scan(&entry.ID, &entry.Path, &deletedAt); err != nil {
			return nil, err
		}
		entry.DeletedAt = time.Unix(deletedAt, 0).UTC()
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) TrashEntry(ownerID int64, id string) (drive.TrashEntry, error) {
	var entry drive.TrashEntry
	var deletedAt int64
	err := s.db.QueryRow(s.query(`SELECT id, path, deleted_at FROM trash_entries WHERE owner_id = ? AND id = ?`), ownerID, id).
		Scan(&entry.ID, &entry.Path, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return drive.TrashEntry{}, os.ErrNotExist
	}
	if err != nil {
		return drive.TrashEntry{}, err
	}
	entry.DeletedAt = time.Unix(deletedAt, 0).UTC()
	return entry, nil
}

func (s *Store) TrashOwners() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT owner_id FROM trash_entries ORDER BY owner_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := make([]int64, 0)
	for rows.Next() {
		var ownerID int64
		if err := rows.Scan(&ownerID); err != nil {
			return nil, err
		}
		owners = append(owners, ownerID)
	}
	return owners, rows.Err()
}

func (s *Store) TrashMatchesSnapshot(ownerID int64, snapshot []drive.TrashEntry) (bool, error) {
	stored, err := s.TrashEntries(ownerID)
	if err != nil {
		return false, err
	}
	if len(stored) != len(snapshot) {
		return false, nil
	}
	byID := make(map[string]drive.TrashEntry, len(stored))
	for _, entry := range stored {
		byID[entry.ID] = entry
	}
	for _, entry := range snapshot {
		old, ok := byID[entry.ID]
		if !ok || old.Path != entry.Path || old.DeletedAt.Unix() != entry.DeletedAt.Unix() {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) replaceTrashEntriesTx(tx *sql.Tx, ownerID int64, entries []drive.TrashEntry) error {
	if _, err := tx.Exec(s.query(`DELETE FROM trash_entries WHERE owner_id = ?`), ownerID); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := tx.Exec(s.query(`INSERT INTO trash_entries (id, owner_id, path, deleted_at) VALUES (?, ?, ?, ?)`), entry.ID, ownerID, entry.Path, entry.DeletedAt.Unix()); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileDriveMutation publishes active files and Trash in one metadata transaction.
func (s *Store) ReconcileDriveMutation(ownerID int64, entries []drive.Entry, trash []drive.TrashEntry, uploads []drive.UploadSession) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.reconcileEntriesTx(tx, ownerID, entries, true); err != nil {
		return err
	}
	if err := s.replaceTrashEntriesTx(tx, ownerID, trash); err != nil {
		return err
	}
	if err := s.reconcileUploadSessionsTx(tx, ownerID, uploads, entries); err != nil {
		return err
	}
	if _, err := tx.Exec(s.query(`DELETE FROM metadata_reconcile WHERE owner_id = ?`), ownerID); err != nil {
		return err
	}
	return tx.Commit()
}
