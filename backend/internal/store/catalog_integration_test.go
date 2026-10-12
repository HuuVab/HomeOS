package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

func TestCatalogTransactionsPostgres(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_catalog_" + hex.EncodeToString(token[:])
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
			t.Errorf("drop schema: %v", err)
		}
	}()
	parsed, err := url.Parse(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAdmin("catalog_admin", "hash"); err != nil {
		t.Fatal(err)
	}
	format, err := db.StorageFormat(1)
	if err != nil || format != "legacy" {
		t.Fatalf("initial format %q: %v", format, err)
	}
	if err := db.CatalogCreateFolder(1, "docs"); err != nil {
		t.Fatal(err)
	}
	if err := db.CatalogCreateFolder(1, "docs"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate folder: %v", err)
	}
	checksum := strings.Repeat("a", 64)
	version, err := db.CatalogCommitFile(1, "docs/a.txt", checksum, 4, 0)
	if err != nil || version != 1 {
		t.Fatalf("create version=%d err=%v", version, err)
	}
	if _, err := db.CatalogCommitFile(1, "docs/a.txt", checksum, 4, 0); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate file: %v", err)
	}
	if _, err := db.CatalogCommitFile(1, "docs/a.txt", checksum, 4, 2); !errors.Is(err, ErrCatalogRevision) {
		t.Fatalf("stale replace: %v", err)
	}
	version, err = db.CatalogCommitFile(1, "docs/a.txt", strings.Repeat("b", 64), 5, 1)
	if err != nil || version != 2 {
		t.Fatalf("replace version=%d err=%v", version, err)
	}
	if err := db.CatalogMoveOrCopy(1, "docs/a.txt", "docs/b.txt", true, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.CatalogMoveOrCopy(1, "docs/a.txt", "docs/c.txt", false, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileEntry(1, "docs/a.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moved source: %v", err)
	}
	if err := db.CatalogTrash(1, "docs", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileEntry(1, "docs/c.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trashed child: %v", err)
	}
	trash, err := db.TrashEntries(1)
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash=%+v err=%v", trash, err)
	}
	items, err := db.CatalogTrashFiles(1, trash[0].ID)
	if err != nil || len(items) != 3 {
		t.Fatalf("manifest=%+v err=%v", items, err)
	}
	refs, err := db.ReferencedVersionChecksums(1)
	if err != nil || !refs[strings.Repeat("b", 64)] {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
	if err := db.CatalogRestoreTrash(1, trash[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FileEntry(1, "docs/c.txt"); err != nil {
		t.Fatalf("restored child: %v", err)
	}
	if err := db.ActivateBlobStorage(1); err != nil {
		t.Fatal(err)
	}
	format, err = db.StorageFormat(1)
	if err != nil || format != "blobs" {
		t.Fatalf("activated format %q: %v", format, err)
	}
	if err := db.CreateUser("legacy_owner", "hash"); err != nil {
		t.Fatal(err)
	}
	files, err := drive.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	legacy, err := files.ForUser(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Upload("keep.txt", strings.NewReader("kept")); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Upload("gone.txt", strings.NewReader("trashed")); err != nil {
		t.Fatal(err)
	}
	entries, err := legacy.AllEntries()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginFileMutation(2); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileDriveMutation(2, entries, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Trash("gone.txt"); err != nil {
		t.Fatal(err)
	}
	entries, err = legacy.AllEntries()
	if err != nil {
		t.Fatal(err)
	}
	legacyTrash, err := legacy.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginFileMutation(2); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileDriveMutation(2, entries, legacyTrash, nil); err != nil {
		t.Fatal(err)
	}
	report, err := db.MigrateOwnerToBlobs(2, legacy, false)
	if err != nil || report.ActiveEntries != 1 || report.TrashEntries != 1 || report.TrashNodes != 1 {
		t.Fatalf("dry migration %+v: %v", report, err)
	}
	format, err = db.StorageFormat(2)
	if err != nil || format != "legacy" {
		t.Fatalf("dry migration format %q: %v", format, err)
	}
	report, err = db.MigrateOwnerToBlobs(2, legacy, true)
	if err != nil || report.VerifiedBlobs != 2 {
		t.Fatalf("migration %+v: %v", report, err)
	}
	format, err = db.StorageFormat(2)
	if err != nil || format != "blobs" {
		t.Fatalf("migrated format %q: %v", format, err)
	}
	manifest, err := db.CatalogTrashFiles(2, legacyTrash[0].ID)
	if err != nil || len(manifest) != 1 || manifest[0].Checksum == "" {
		t.Fatalf("migrated Trash %+v: %v", manifest, err)
	}
}
