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
	if pending, err := db.HasPendingFileMutation(1); err != nil || !pending {
		t.Fatalf("marker not visible: %v, %v", pending, err)
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
	if pending, err := db.HasPendingFileMutation(1); err != nil || pending {
		t.Fatalf("marker not cleared: %v, %v", pending, err)
	}
	owners, err = db.PendingFileMutations()
	if err != nil || len(owners) != 0 {
		t.Fatalf("remaining owners = %v, %v", owners, err)
	}
}

func TestReconciliationProducesIncrementalChanges(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "changes.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAdmin("admin_test", "hash"); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(100, 0)
	entry := drive.Entry{Name: "note.txt", Path: "note.txt", Kind: "file", Size: 1, ModifiedAt: stamp}
	for _, snapshot := range [][]drive.Entry{{entry}, {entry}, {{Name: "note.txt", Path: "note.txt", Kind: "file", Size: 2, ModifiedAt: stamp.Add(time.Second)}}, {}} {
		if err := db.BeginFileMutation(1); err != nil {
			t.Fatal(err)
		}
		if err := db.ReconcileFileMutation(1, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.ChangesAfter(1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Operation != "create" || *events[0].Version != 1 || events[1].Operation != "update" || *events[1].Version != 2 || events[2].Operation != "delete" || events[2].Version != nil {
		t.Fatalf("unexpected change feed: %+v", events)
	}
	page, err := db.ChangesAfter(1, events[0].ID, 1)
	if err != nil || len(page) != 1 || page[0].ID != events[1].ID {
		t.Fatalf("cursor page = %+v, %v", page, err)
	}
}

func TestFileMetadataReadsUseCommittedRows(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAdmin("admin_test", "hash"); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(100, 0)
	entries := []drive.Entry{
		{Name: "docs", Path: "docs", Kind: "folder", ModifiedAt: stamp},
		{Name: "note.txt", Path: "docs/note.txt", Kind: "file", Size: 4, ModifiedAt: stamp},
		{Name: "root.txt", Path: "root.txt", Kind: "file", Size: 2, ModifiedAt: stamp},
	}
	if err := db.ReplaceFileEntries(1, entries); err != nil {
		t.Fatal(err)
	}
	if matches, err := db.MetadataMatchesSnapshot(1, entries); err != nil || !matches {
		t.Fatalf("expected matching snapshot: %v, %v", matches, err)
	}
	if matches, err := db.MetadataMatchesSnapshot(1, entries[:2]); err != nil || matches {
		t.Fatalf("missing file not detected: %v, %v", matches, err)
	}
	root, err := db.FileEntries(1, "")
	if err != nil || len(root) != 2 {
		t.Fatalf("root = %+v, %v", root, err)
	}
	children, err := db.FileEntries(1, "docs")
	if err != nil || len(children) != 1 || children[0].Path != "docs/note.txt" {
		t.Fatalf("children = %+v, %v", children, err)
	}
	if _, err := db.FileEntries(1, "root.txt"); !errors.Is(err, drive.ErrInvalidPath) {
		t.Fatalf("file as folder = %v", err)
	}
	if _, err := db.FileEntries(1, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing folder = %v", err)
	}
	results, err := db.SearchFileEntries(1, "NOTE")
	if err != nil || len(results) != 1 || results[0].Path != "docs/note.txt" {
		t.Fatalf("search = %+v, %v", results, err)
	}
	other, err := db.FileEntries(2, "")
	if err != nil || len(other) != 0 {
		t.Fatalf("other owner = %+v, %v", other, err)
	}
}

func TestTrashMetadataReconcilesAtomically(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "trash.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAdmin("admin_test", "hash"); err != nil {
		t.Fatal(err)
	}
	item := drive.TrashEntry{ID: "00000000000000000000000000000001", Path: "deleted.txt", DeletedAt: time.Unix(100, 0)}
	if err := db.BeginFileMutation(1); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileDriveMutation(1, nil, []drive.TrashEntry{item}); err != nil {
		t.Fatal(err)
	}
	got, err := db.TrashEntry(1, item.ID)
	if err != nil || got.Path != item.Path {
		t.Fatalf("trash entry = %+v, %v", got, err)
	}
	other, err := db.TrashEntries(2)
	if err != nil || len(other) != 0 {
		t.Fatalf("other owner's trash = %+v, %v", other, err)
	}
	if err := db.BeginFileMutation(1); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileDriveMutation(1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TrashEntry(1, item.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted Trash record = %v", err)
	}
}
