package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

// MigrateSQLiteToPostgres copies standalone metadata into an empty PostgreSQL
// catalog. Sessions are intentionally not copied, so users sign in again.
// The caller must stop the server and back up SQLite and the file directory.
func MigrateSQLiteToPostgres(source, target *Store, files *drive.Store) error {
	if source.dialect != "sqlite" || target.dialect != "postgres" {
		return errors.New("migration requires SQLite source and PostgreSQL target")
	}
	owners, err := source.UserIDs()
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return errors.New("SQLite source has no users")
	}
	pending, err := source.PendingFileMutations()
	if err != nil {
		return err
	}
	if len(pending) != 0 {
		return errors.New("SQLite source has pending file mutations; start and stop the old server to recover them")
	}
	type snapshot struct {
		ownerID int64
		trash   []drive.TrashEntry
		uploads []drive.UploadSession
	}
	snapshots := make([]snapshot, 0, len(owners))
	for _, ownerID := range owners {
		userFiles, err := files.ForUser(ownerID)
		if err != nil {
			return err
		}
		entries, err := userFiles.AllEntries()
		if err != nil {
			return fmt.Errorf("scan owner %d: %w", ownerID, err)
		}
		matches, err := source.MetadataMatchesSnapshot(ownerID, entries)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("owner %d file catalog differs from disk; repair SQLite before migration", ownerID)
		}
		trash, err := userFiles.ListTrash()
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot{ownerID, trash, userFiles.PendingUploads()})
	}
	tx, err := target.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"users", "sessions", "file_entries", "file_versions", "shares", "upload_sessions", "change_events", "trash_entries", "metadata_reconcile"} {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("PostgreSQL target is not empty: %s", table)
		}
	}
	for _, table := range []string{"users", "file_entries", "file_versions", "shares", "change_events"} {
		if err := copyMigrationTable(source.db, tx, table); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	for _, state := range snapshots {
		if err := target.replaceTrashEntriesTx(tx, state.ownerID, state.trash); err != nil {
			return err
		}
		for _, upload := range state.uploads {
			if err := target.upsertUploadSession(tx, state.ownerID, upload); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"users", "file_versions", "change_events"} {
		statement := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE((SELECT MAX(id) FROM %s), 1), EXISTS(SELECT 1 FROM %s))`, table, table, table)
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Table names come exclusively from the fixed list above, never user input.
func copyMigrationTable(source *sql.DB, target *sql.Tx, table string) error {
	rows, err := source.Query(`SELECT * FROM ` + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	statement := `INSERT INTO ` + table + ` (` + strings.Join(quoted, ", ") + `) VALUES (` + strings.Join(placeholders, ", ") + `)`
	for rows.Next() {
		values := make([]any, len(columns))
		scan := make([]any, len(columns))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return err
		}
		if _, err := target.Exec(statement, values...); err != nil {
			return err
		}
	}
	return rows.Err()
}
