package syncclient

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const stateDir = ".homeos-sync"

type State struct {
	db *sql.DB
}

type Baseline struct {
	Path          string
	Kind          string
	RemoteVersion int64
	LocalHash     string
}

type Pending struct {
	Path        string
	Operation   string
	BaseVersion int64
	LocalHash   string
	Attempts    int
	NextAttempt int64
	LastError   string
}

type Conflict struct {
	Path   string
	Reason string
}

func OpenState(root string) (*State, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sync root must be an existing directory: %s", absolute)
	}
	directory := filepath.Join(absolute, stateDir)
	if info, err := os.Lstat(directory); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("sync state path must be a directory inside the root: %s", directory)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, "state.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, kind TEXT NOT NULL, remote_version INTEGER NOT NULL, local_hash TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS pending (path TEXT PRIMARY KEY, operation TEXT NOT NULL, base_version INTEGER NOT NULL, local_hash TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS conflicts (path TEXT PRIMARY KEY, reason TEXT NOT NULL, detected_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS remote_entries (path TEXT PRIMARY KEY, kind TEXT NOT NULL, size INTEGER NOT NULL, version INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS local_scan (path TEXT PRIMARY KEY, size INTEGER NOT NULL, modified_ns INTEGER NOT NULL, hash TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &State{db: db}, nil
}

type ScanCacheEntry struct {
	Size       int64
	ModifiedNS int64
	Hash       string
}

func (s *State) ScanCache() (map[string]ScanCacheEntry, error) {
	rows, err := s.db.Query(`SELECT path,size,modified_ns,hash FROM local_scan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ScanCacheEntry)
	for rows.Next() {
		var name string
		var item ScanCacheEntry
		if err := rows.Scan(&name, &item.Size, &item.ModifiedNS, &item.Hash); err != nil {
			return nil, err
		}
		result[name] = item
	}
	return result, rows.Err()
}

func (s *State) SaveScanCache(entries map[string]ScanCacheEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM local_scan`); err != nil {
		return err
	}
	for name, item := range entries {
		if _, err := tx.Exec(`INSERT INTO local_scan(path,size,modified_ns,hash) VALUES(?,?,?,?)`, name, item.Size, item.ModifiedNS, item.Hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *State) RemoteEntries() (map[string]RemoteEntry, error) {
	rows, err := s.db.Query(`SELECT path,kind,size,version FROM remote_entries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make(map[string]RemoteEntry)
	for rows.Next() {
		var entry RemoteEntry
		if err := rows.Scan(&entry.Path, &entry.Kind, &entry.Size, &entry.Version); err != nil {
			return nil, err
		}
		entries[entry.Path] = entry
	}
	return entries, rows.Err()
}

func (s *State) SaveRemoteEntries(entries map[string]RemoteEntry, cursor int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM remote_entries`); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := tx.Exec(`INSERT INTO remote_entries(path,kind,size,version) VALUES(?,?,?,?)`, entry.Path, entry.Kind, entry.Size, entry.Version); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES('cursor',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(cursor)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *State) Close() error { return s.db.Close() }

func (s *State) Baselines() (map[string]Baseline, error) {
	rows, err := s.db.Query(`SELECT path, kind, remote_version, local_hash FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]Baseline)
	for rows.Next() {
		var item Baseline
		if err := rows.Scan(&item.Path, &item.Kind, &item.RemoteVersion, &item.LocalHash); err != nil {
			return nil, err
		}
		result[item.Path] = item
	}
	return result, rows.Err()
}

func (s *State) SaveBaseline(item Baseline) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO files(path,kind,remote_version,local_hash) VALUES(?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET kind=excluded.kind, remote_version=excluded.remote_version, local_hash=excluded.local_hash`,
		item.Path, item.Kind, item.RemoteVersion, item.LocalHash); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pending WHERE path=?`, item.Path); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM conflicts WHERE path=?`, item.Path); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *State) RemoveBaseline(path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"files", "pending", "conflicts"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE path=?`, path); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveTree clears sync bookkeeping for one path and its descendants. The
// caller must finish the chosen remote/local conflict operation first.
func (s *State) RemoveTree(name string) error {
	if !validPath(name) {
		return errors.New("invalid sync path")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"files", "pending", "conflicts", "local_scan"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE path=? OR substr(path,1,?)=?`, name, len(name)+1, name+"/"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *State) Queue(item Pending) error {
	if item.Path == "" || (item.Operation != "create" && item.Operation != "update" && item.Operation != "delete") {
		return errors.New("invalid pending operation")
	}
	_, err := s.db.Exec(`INSERT INTO pending(path,operation,base_version,local_hash) VALUES(?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET operation=excluded.operation, base_version=excluded.base_version,
		local_hash=excluded.local_hash, attempts=CASE WHEN pending.local_hash=excluded.local_hash AND pending.operation=excluded.operation THEN pending.attempts ELSE 0 END,
		next_attempt=CASE WHEN pending.local_hash=excluded.local_hash AND pending.operation=excluded.operation THEN pending.next_attempt ELSE 0 END,
		last_error=CASE WHEN pending.local_hash=excluded.local_hash AND pending.operation=excluded.operation THEN pending.last_error ELSE '' END`,
		item.Path, item.Operation, item.BaseVersion, item.LocalHash)
	return err
}

func (s *State) Pending() ([]Pending, error) {
	rows, err := s.db.Query(`SELECT path,operation,base_version,local_hash,attempts,next_attempt,last_error FROM pending ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Pending
	for rows.Next() {
		var item Pending
		if err := rows.Scan(&item.Path, &item.Operation, &item.BaseVersion, &item.LocalHash, &item.Attempts, &item.NextAttempt, &item.LastError); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *State) ClearPending(path string) error {
	_, err := s.db.Exec(`DELETE FROM pending WHERE path=?`, path)
	return err
}

func (s *State) Retry(path string, failure error) error {
	_, err := s.db.Exec(`UPDATE pending SET attempts=attempts+1,
		next_attempt=? + MIN(3600, 1 << MIN(attempts, 11)), last_error=? WHERE path=?`,
		time.Now().Unix(), failure.Error(), path)
	return err
}

func (s *State) MarkConflict(path, reason string) error {
	_, err := s.db.Exec(`INSERT INTO conflicts(path,reason,detected_at) VALUES(?,?,?)
		ON CONFLICT(path) DO UPDATE SET reason=excluded.reason, detected_at=excluded.detected_at`, path, reason, time.Now().Unix())
	return err
}

func (s *State) ClearConflict(path string) error {
	_, err := s.db.Exec(`DELETE FROM conflicts WHERE path=?`, path)
	return err
}

func (s *State) Conflicts() ([]Conflict, error) {
	rows, err := s.db.Query(`SELECT path,reason FROM conflicts ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Conflict
	for rows.Next() {
		var item Conflict
		if err := rows.Scan(&item.Path, &item.Reason); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *State) Meta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *State) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
