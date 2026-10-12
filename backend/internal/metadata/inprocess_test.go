package metadata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPostgresMetadataServiceIntegration(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_grpc_" + hex.EncodeToString(random[:])
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
			t.Errorf("remove isolated schema: %v", err)
		}
	}()
	parsed, err := url.Parse(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = parameters.Encode()
	db, err := store.OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAdmin("grpc_admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("grpc_second", "hash"); err != nil {
		t.Fatal(err)
	}
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	client, close, err := NewInProcessClient(db, files)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	created, err := client.CreateFolder(context.Background(), &metadatapb.CreateFolderRequest{OwnerId: 1, Path: "docs"})
	if err != nil || created.GetPath() != "docs" || created.GetVersion() != 1 {
		t.Fatalf("create folder = %+v, %v", created, err)
	}
	listed, err := client.ListFiles(context.Background(), &metadatapb.ListFilesRequest{OwnerId: 1})
	if err != nil || len(listed.GetEntries()) != 1 || listed.GetEntries()[0].GetPath() != "docs" {
		t.Fatalf("list files = %+v, %v", listed, err)
	}
	if _, err := client.GetFile(context.Background(), &metadatapb.GetFileRequest{OwnerId: 2, Path: "docs"}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-owner file lookup = %v", err)
	}
	events, err := db.ChangesAfter(1, 0, 10)
	if err != nil || len(events) != 1 || events[0].Operation != "create" {
		t.Fatalf("change feed = %+v, %v", events, err)
	}
}

func TestInProcessServiceRejectsStandaloneMode(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	client, close, err := NewInProcessClient(db, files)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	_, err = client.ListFiles(context.Background(), &metadatapb.ListFilesRequest{OwnerId: 1})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("standalone-mode RPC = %v, want FailedPrecondition", err)
	}
}
