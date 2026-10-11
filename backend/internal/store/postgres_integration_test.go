package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

// Set HOMEOS_TEST_POSTGRES_URL to run this against a disposable PostgreSQL
// database. The test creates and drops only its own randomly named schema.
func TestPostgresMetadataIntegration(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_test_" + hex.EncodeToString(random[:])
	admin, err := sql.Open("pgx", connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Errorf("remove isolated test schema: %v", err)
		}
	}()
	parsed, err := url.Parse(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = parameters.Encode()
	db, err := OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	if !db.AuthoritativeMetadata() {
		t.Fatal("PostgreSQL metadata mode disabled")
	}
	if err := db.CreateAdmin("test_admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("second_user", "hash"); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(100, 0)
	entries := []drive.Entry{{Name: "docs", Path: "docs", Kind: "folder", ModifiedAt: stamp}, {Name: "note.txt", Path: "docs/note.txt", Kind: "file", Size: 4, ModifiedAt: stamp}}
	if err := db.BeginFileMutation(1); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileFileMutation(1, entries); err != nil {
		t.Fatal(err)
	}
	children, err := db.FileEntries(1, "docs")
	if err != nil || len(children) != 1 || children[0].Path != "docs/note.txt" {
		t.Fatalf("children = %+v, %v", children, err)
	}
	other, err := db.FileEntries(2, "")
	if err != nil || len(other) != 0 {
		t.Fatalf("other owner's entries = %+v, %v", other, err)
	}
	events, err := db.ChangesAfter(1, 0, 100)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	trash := drive.TrashEntry{ID: "00000000000000000000000000000001", Path: "old.txt", DeletedAt: stamp}
	upload := drive.UploadSession{ID: "00000000000000000000000000000002", Path: "later.txt", Size: 4, Fingerprint: strings.Repeat("a", 64), Offset: 2, Staged: true}
	if err := db.BeginFileMutation(1); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileDriveMutation(1, entries, []drive.TrashEntry{trash}, []drive.UploadSession{upload}); err != nil {
		t.Fatal(err)
	}
	storedTrash, err := db.TrashEntries(1)
	if err != nil || len(storedTrash) != 1 || storedTrash[0].ID != trash.ID {
		t.Fatalf("trash = %+v, %v", storedTrash, err)
	}
	storedUpload, err := db.PendingUploadSession(1, upload.ID)
	if err != nil || storedUpload.Offset != 2 {
		t.Fatalf("upload = %+v, %v", storedUpload, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	children, err = db.FileEntries(1, "docs")
	if err != nil || len(children) != 1 {
		t.Fatalf("entries after restart = %+v, %v", children, err)
	}
	if _, err := db.TrashEntry(1, trash.ID); err != nil {
		t.Fatalf("Trash after restart: %v", err)
	}
	if _, err := db.PendingUploadSession(1, upload.ID); err != nil {
		t.Fatalf("upload after restart: %v", err)
	}
}

func TestPostgresSQLiteMigrationIntegration(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_migrate_" + hex.EncodeToString(random[:])
	admin, err := sql.Open("pgx", connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Errorf("remove isolated test schema: %v", err)
		}
	}()
	parsed, err := url.Parse(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = parameters.Encode()
	target, err := OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	source, err := Open(filepath.Join(t.TempDir(), "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := source.CreateAdmin("migrated_admin", "preserved-hash"); err != nil {
		t.Fatal(err)
	}
	if err := source.CreateUser("migrated_user", "second-hash"); err != nil {
		t.Fatal(err)
	}
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Upload("note.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	first, err := files.AllEntries()
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ReplaceFileEntries(1, first); err != nil {
		t.Fatal(err)
	}
	secondFiles, err := files.ForUser(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondFiles.CreateFolder("docs"); err != nil {
		t.Fatal(err)
	}
	second, err := secondFiles.AllEntries()
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ReplaceFileEntries(2, second); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSQLiteToPostgres(source, target, files); err != nil {
		t.Fatal(err)
	}
	user, err := target.UserByUsername("migrated_admin")
	if err != nil || user.ID != 1 || user.PasswordHash != "preserved-hash" {
		t.Fatalf("migrated admin = %+v, %v", user, err)
	}
	entries, err := target.FileEntries(1, "")
	if err != nil || len(entries) != 1 || entries[0].Path != "note.txt" || entries[0].Version != 1 {
		t.Fatalf("owner 1 files = %+v, %v", entries, err)
	}
	entries, err = target.FileEntries(2, "")
	if err != nil || len(entries) != 1 || entries[0].Path != "docs" {
		t.Fatalf("owner 2 files = %+v, %v", entries, err)
	}
	if err := MigrateSQLiteToPostgres(source, target, files); err == nil {
		t.Fatal("second migration unexpectedly accepted a populated target")
	}
}
