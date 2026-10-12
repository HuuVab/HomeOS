package store

import (
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

func (s *Store) UpsertUploadSession(ownerID int64, session drive.UploadSession) error {
	return s.upsertUploadSession(s.db, ownerID, session)
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *Store) upsertUploadSession(executor sqlExecutor, ownerID int64, session drive.UploadSession) error {
	now := time.Now().Unix()
	result, err := executor.Exec(s.query(`INSERT INTO upload_sessions
		(id, owner_id, path, expected_size, received_size, fingerprint, status, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET received_size = EXCLUDED.received_size, updated_at = EXCLUDED.updated_at,
		expires_at = EXCLUDED.expires_at
		WHERE upload_sessions.owner_id = EXCLUDED.owner_id AND upload_sessions.path = EXCLUDED.path
		AND upload_sessions.expected_size = EXCLUDED.expected_size AND upload_sessions.fingerprint = EXCLUDED.fingerprint
		AND upload_sessions.status = 'pending'`), session.ID, ownerID, session.Path, session.Size, session.Offset, session.Fingerprint, now, now, now+7*24*3600)
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
	return nil
}

func (s *Store) PendingUploadSession(ownerID int64, id string) (drive.UploadSession, error) {
	var session drive.UploadSession
	err := s.db.QueryRow(s.query(`SELECT id, path, expected_size, fingerprint, received_size FROM upload_sessions
		WHERE owner_id = ? AND id = ? AND status = 'pending'`), ownerID, id).
		Scan(&session.ID, &session.Path, &session.Size, &session.Fingerprint, &session.Offset)
	if errors.Is(err, sql.ErrNoRows) {
		return drive.UploadSession{}, os.ErrNotExist
	}
	return session, err
}

func (s *Store) SetUploadOffset(ownerID int64, id string, offset int64) error {
	result, err := s.db.Exec(s.query(`UPDATE upload_sessions SET received_size = ?, updated_at = ?
		WHERE owner_id = ? AND id = ? AND status = 'pending' AND received_size <= ?`), offset, time.Now().Unix(), ownerID, id, offset)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *Store) reconcileUploadSessionsTx(tx *sql.Tx, ownerID int64, active []drive.UploadSession, entries []drive.Entry) error {
	activeIDs := make(map[string]struct{}, len(active))
	for _, session := range active {
		if err := s.upsertUploadSession(tx, ownerID, session); err != nil {
			return err
		}
		activeIDs[session.ID] = struct{}{}
	}
	completed := make(map[string]int64)
	for _, entry := range entries {
		if entry.Kind == "file" {
			completed[entry.Path] = entry.Size
		}
	}
	rows, err := tx.Query(s.query(`SELECT id, path, expected_size, received_size FROM upload_sessions WHERE owner_id = ? AND status = 'pending'`), ownerID)
	if err != nil {
		return err
	}
	type candidate struct {
		id, path     string
		size, offset int64
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.path, &item.size, &item.offset); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range candidates {
		if _, ok := activeIDs[item.id]; ok {
			continue
		}
		status := "failed"
		offset := item.offset
		if size, ok := completed[item.path]; ok && size == item.size {
			status = "complete"
			offset = item.size
		}
		if _, err := tx.Exec(s.query(`UPDATE upload_sessions SET status = ?, received_size = ?, updated_at = ? WHERE owner_id = ? AND id = ?`), status, offset, time.Now().Unix(), ownerID, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RecoverUploadSessions(ownerID int64, active []drive.UploadSession) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(s.query(`SELECT path, size FROM file_entries WHERE owner_id = ? AND kind = 'file'`), ownerID)
	if err != nil {
		return err
	}
	entries := make([]drive.Entry, 0)
	for rows.Next() {
		var entry drive.Entry
		entry.Kind = "file"
		if err := rows.Scan(&entry.Path, &entry.Size); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if err := s.reconcileUploadSessionsTx(tx, ownerID, active, entries); err != nil {
		return err
	}
	return tx.Commit()
}
