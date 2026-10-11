package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

func TestMigrationFailureIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (wrong_column TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(path)
	if err == nil {
		opened.Close()
		t.Fatal("expected migration failure for incompatible schema")
	}
}

func TestAdminPersistsAndResetRevokesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homeos.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := auth.HashPassword("first-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("admin_test", firstHash); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("another_admin", firstHash); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("duplicate bootstrap = %v, want ErrAdminExists", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.UserByUsername("admin_test")
	if err != nil || !auth.VerifyPassword(user.PasswordHash, "first-test-password") {
		t.Fatalf("administrator did not persist: %v", err)
	}
	if err := db.CreateSession(user.ID, "test-token-hash", "test-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	newHash, err := auth.HashPassword("second-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResetAdminPassword("admin_test", newHash); err != nil {
		t.Fatal(err)
	}
	user, err = db.UserByUsername("admin_test")
	if err != nil || !auth.VerifyPassword(user.PasswordHash, "second-test-password") || auth.VerifyPassword(user.PasswordHash, "first-test-password") {
		t.Fatalf("administrator password reset failed: %v", err)
	}
	if _, _, err := db.Session("test-token-hash"); err == nil {
		t.Fatal("password reset did not revoke existing session")
	}
	backupPath := filepath.Join(t.TempDir(), "backup.sqlite")
	if err := db.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Backup(backupPath); err == nil {
		t.Fatal("backup overwrote an existing file")
	}
	backupBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.sqlite")
	if err := os.WriteFile(restoredPath, backupBytes, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatalf("open restored backup: %v", err)
	}
	defer restored.Close()
	restoredAdmin, err := restored.UserByUsername("admin_test")
	if err != nil || !auth.VerifyPassword(restoredAdmin.PasswordHash, "second-test-password") {
		t.Fatalf("restored administrator missing or invalid: %v", err)
	}
}

func TestMultiuserMigrationPreservesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := migrations.ReadFile("migrations/0001_core.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(string(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO schema_migrations VALUES ('0001_core.sql', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO users (id,username,display_name,password_hash,role,created_at,updated_at) VALUES (1,'old_admin','old_admin','hash','admin',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (token_hash,user_id,csrf_token,created_at,expires_at) VALUES ('token',1,'csrf',1,?)`, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	user, _, err := upgraded.Session("token")
	if err != nil || user.Username != "old_admin" {
		t.Fatalf("session after migration: %+v %v", user, err)
	}
}

func TestFileMutationMarkerSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reconcile.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("admin_test", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginFileMutation(1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owners, err := db.PendingFileMutations()
	if err != nil || len(owners) != 1 || owners[0] != 1 {
		t.Fatalf("pending owners = %v, %v", owners, err)
	}
	if err := db.ReconcileFileMutation(1, []drive.Entry{{Name: "bad", Path: "bad", Kind: "invalid"}}); err == nil {
		t.Fatal("expected invalid metadata to fail reconciliation")
	}
	owners, err = db.PendingFileMutations()
	if err != nil || len(owners) != 1 || owners[0] != 1 {
		t.Fatalf("failed reconciliation lost marker: %v, %v", owners, err)
	}
	if err := db.ReconcileFileMutation(1, nil); err != nil {
		t.Fatal(err)
	}
	owners, err = db.PendingFileMutations()
	if err != nil || len(owners) != 0 {
		t.Fatalf("remaining owners = %v, %v", owners, err)
	}
}
