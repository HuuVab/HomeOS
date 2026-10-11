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

// MetadataMatchesSnapshot detects out-of-band filesystem changes before a
// PostgreSQL-backed mutation can publish them into the authoritative catalog.
func (s *Store) MetadataMatchesSnapshot(ownerID int64, entries []drive.Entry) (bool, error) {
	rows, err := s.db.Query(s.query(`SELECT path, kind, size, modified_at FROM file_entries WHERE owner_id = ?`), ownerID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	type metadata struct {
		kind             string
		size, modifiedAt int64
	}
	stored := make(map[string]metadata)
	for rows.Next() {
		var name string
		var item metadata
		if err := rows.Scan(&name, &item.kind, &item.size, &item.modifiedAt); err != nil {
			return false, err
		}
		stored[name] = item
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(stored) != len(entries) {
		return false, nil
	}
	for _, entry := range entries {
		item, ok := stored[entry.Path]
		if !ok || item.kind != entry.Kind || item.size != entry.Size || item.modifiedAt != entry.ModifiedAt.Unix() {
			return false, nil
		}
	}
	return true, nil
}
