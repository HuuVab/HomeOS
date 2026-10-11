package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrAdminExists = errors.New("an administrator already exists")
var ErrAdminNotFound = errors.New("administrator not found")

type Store struct {
	db   *sql.DB
	path string
}

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
}

func Open(path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", absPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure sqlite: %w", err)
		}
	}
	s := &Store{db: db, path: absPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied int
		err := s.db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, entry.Name()).Scan(&applied)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		statement, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(statement)); err == nil {
			_, err = tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, entry.Name(), time.Now().Unix())
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ready(ctx context.Context) error { return s.db.PingContext(ctx) }

// Backup creates a consistent SQLite snapshot while the application is running.
// The destination must be a new file so an existing backup cannot be replaced.
func (s *Store) Backup(destination string) error {
	absPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve backup path: %w", err)
	}
	if strings.EqualFold(absPath, s.path) {
		return errors.New("backup path must differ from the live database")
	}
	if _, err := os.Lstat(absPath); err == nil {
		return errors.New("backup path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check backup path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, absPath); err != nil {
		return fmt.Errorf("create SQLite backup: %w", err)
	}
	backupDB, err := sql.Open("sqlite", absPath)
	if err != nil {
		return fmt.Errorf("open backup for verification: %w", err)
	}
	defer backupDB.Close()
	var result string
	if err := backupDB.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("verify backup: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("backup integrity check failed: %s", result)
	}
	return nil
}

func (s *Store) CreateAdmin(username, passwordHash string) error {
	now := time.Now().Unix()
	result, err := s.db.Exec(`INSERT INTO users (username, display_name, password_hash, role, created_at, updated_at)
        SELECT ?, ?, ?, 'admin', ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, username, username, passwordHash, now, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAdminExists
	}
	return nil
}

func (s *Store) CreateUser(username, passwordHash string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO users (username, display_name, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, 'user', ?, ?)`, username, username, passwordHash, now, now)
	return err
}

func (s *Store) ReplaceFileEntries(ownerID int64, entries []drive.Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM file_entries WHERE owner_id = ?`, ownerID); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := tx.Exec(`INSERT INTO file_entries (owner_id, path, kind, size, modified_at) VALUES (?, ?, ?, ?, ?)`, ownerID, entry.Path, entry.Kind, entry.Size, entry.ModifiedAt.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ResetAdminPassword(username, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ? AND role = 'admin'`, passwordHash, time.Now().Unix(), username)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAdminNotFound
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = ?)`, username); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UserByUsername(username string) (User, error) {
	var user User
	err := s.db.QueryRow(`SELECT id, username, display_name, role, password_hash FROM users WHERE username = ? AND disabled = 0`, username).
		Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.PasswordHash)
	return user, err
}

func (s *Store) CreateSession(userID int64, tokenHash, csrfToken string, expiresAt time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, csrfToken, time.Now().Unix(), expiresAt.Unix())
	return err
}

func (s *Store) Session(tokenHash string) (User, string, error) {
	var user User
	var csrfToken string
	err := s.db.QueryRow(`SELECT u.id, u.username, u.display_name, u.role, s.csrf_token
        FROM sessions s JOIN users u ON u.id = s.user_id
        WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expires_at > ? AND u.disabled = 0`, tokenHash, time.Now().Unix()).
		Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &csrfToken)
	return user, csrfToken, err
}

func (s *Store) RevokeSession(tokenHash string) error {
	_, err := s.db.Exec(`UPDATE sessions SET revoked_at = ? WHERE token_hash = ?`, time.Now().Unix(), tokenHash)
	return err
}
