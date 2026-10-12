package store

import (
	"errors"
	"fmt"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

type RepairReport struct {
	Entries          int `json:"entries"`
	MissingOnDisk    int `json:"missingOnDisk"`
	MissingInCatalog int `json:"missingInCatalog"`
	Mismatched       int `json:"mismatched"`
	Unverified       int `json:"unverified"`
	MissingChecksums int `json:"missingChecksums"`
}

// RepairDriveCatalog can add missing catalog rows and correct metadata only when
// existing file bytes match their committed checksum. It never deletes rows or
// accepts an unverifiable replacement of committed bytes.
func (s *Store) RepairDriveCatalog(ownerID int64, entries []drive.Entry, apply bool) (RepairReport, error) {
	var report RepairReport
	if !s.AuthoritativeMetadata() || ownerID < 1 {
		return report, errors.New("catalog repair requires PostgreSQL and a valid owner")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	var exists int64
	if err := tx.QueryRow(s.query(`SELECT id FROM users WHERE id = ? FOR UPDATE`), ownerID).Scan(&exists); err != nil {
		return report, fmt.Errorf("owner unavailable: %w", err)
	}
	rows, err := tx.Query(s.query(`SELECT path, kind, size, modified_at, COALESCE(checksum, '') FROM file_entries WHERE owner_id = ?`), ownerID)
	if err != nil {
		return report, err
	}
	type catalogEntry struct {
		kind, checksum string
		size, modified int64
	}
	catalog := make(map[string]catalogEntry)
	for rows.Next() {
		var name string
		var entry catalogEntry
		if err := rows.Scan(&name, &entry.kind, &entry.size, &entry.modified, &entry.checksum); err != nil {
			rows.Close()
			return report, err
		}
		catalog[name] = entry
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	rows.Close()
	report.Entries = len(entries)
	for _, entry := range entries {
		old, ok := catalog[entry.Path]
		delete(catalog, entry.Path)
		if !ok {
			report.MissingInCatalog++
			continue
		}
		if old.kind != entry.Kind || old.size != entry.Size || old.modified != entry.ModifiedAt.Unix() ||
			(old.checksum != "" && old.checksum != entry.Checksum) {
			report.Mismatched++
			if old.kind != entry.Kind || (entry.Kind == "file" && (old.checksum == "" || old.checksum != entry.Checksum)) {
				report.Unverified++
			}
			continue
		}
		if entry.Kind == "file" && old.checksum == "" {
			report.MissingChecksums++
		}
	}
	report.MissingOnDisk = len(catalog)
	if !apply {
		return report, nil
	}
	if report.MissingOnDisk != 0 || report.Unverified != 0 {
		return report, errors.New("missing file bytes or unverified content drift; refusing automatic repair")
	}
	if err := s.reconcileEntriesTx(tx, ownerID, entries, false); err != nil {
		return report, err
	}
	return report, tx.Commit()
}

