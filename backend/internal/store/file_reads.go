package store

import (
	"database/sql"
	"errors"
	"os"
	"path"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

// AuthoritativeMetadata is enabled only for server deployments backed by PostgreSQL.
func (s *Store) AuthoritativeMetadata() bool { return s.dialect == "postgres" }

func (s *Store) FileEntry(ownerID int64, name string) (drive.Entry, error) {
	var entry drive.Entry
	var modified int64
	err := s.db.QueryRow(s.query(`SELECT path, kind, size, modified_at FROM file_entries WHERE owner_id = ? AND path = ?`), ownerID, name).
		Scan(&entry.Path, &entry.Kind, &entry.Size, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return drive.Entry{}, os.ErrNotExist
	}
	if err != nil {
		return drive.Entry{}, err
	}
	entry.Name = path.Base(entry.Path)
	entry.ModifiedAt = time.Unix(modified, 0).UTC()
	return entry, nil
}

// FileEntries lists direct children from committed metadata. The empty path is root.
func (s *Store) FileEntries(ownerID int64, folder string) ([]drive.Entry, error) {
	if folder != "" {
		parent, err := s.FileEntry(ownerID, folder)
		if err != nil {
			return nil, err
		}
		if parent.Kind != "folder" {
			return nil, drive.ErrInvalidPath
		}
	}
	rows, err := s.db.Query(s.query(`SELECT path, kind, size, modified_at FROM file_entries WHERE owner_id = ? ORDER BY path`), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]drive.Entry, 0)
	for rows.Next() {
		var entry drive.Entry
		var modified int64
		if err := rows.Scan(&entry.Path, &entry.Kind, &entry.Size, &modified); err != nil {
			return nil, err
		}
		if path.Dir(entry.Path) != folder && !(folder == "" && !strings.Contains(entry.Path, "/")) {
			continue
		}
		entry.Name = path.Base(entry.Path)
		entry.ModifiedAt = time.Unix(modified, 0).UTC()
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) SearchFileEntries(ownerID int64, query string) ([]drive.Entry, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, drive.ErrInvalidPath
	}
	rows, err := s.db.Query(s.query(`SELECT path, kind, size, modified_at FROM file_entries WHERE owner_id = ? ORDER BY path`), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]drive.Entry, 0)
	for rows.Next() {
		var entry drive.Entry
		var modified int64
		if err := rows.Scan(&entry.Path, &entry.Kind, &entry.Size, &modified); err != nil {
			return nil, err
		}
		entry.Name = path.Base(entry.Path)
		if !strings.Contains(strings.ToLower(entry.Name), strings.ToLower(query)) {
			continue
		}
		entry.ModifiedAt = time.Unix(modified, 0).UTC()
		entries = append(entries, entry)
		if len(entries) >= 500 {
			break
		}
	}
	return entries, rows.Err()
}
