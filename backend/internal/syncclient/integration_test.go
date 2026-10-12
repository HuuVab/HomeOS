package syncclient_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/api"
	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadata"
	"github.com/HuuVab/HomeOS/backend/internal/monitor"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"github.com/HuuVab/HomeOS/backend/internal/syncclient"
)

type emptyCollector struct{}

func (emptyCollector) Collect(context.Context) []monitor.Metric { return nil }

func TestPostgresDesktopSyncThroughHomeOSAPI(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_sync_" + hex.EncodeToString(random[:])
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
			t.Errorf("drop isolated schema: %v", err)
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
	password := "sync-integration-password"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("sync_admin", hash); err != nil {
		t.Fatal(err)
	}
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	metadataClient, closeMetadata, err := metadata.NewInProcessClient(db, files)
	if err != nil {
		t.Fatal(err)
	}
	defer closeMetadata()
	server := httptest.NewServer(api.NewHandler("", db, emptyCollector{}, files, metadataClient))
	defer server.Close()
	remote, err := syncclient.NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Login(context.Background(), "sync_admin", password); err != nil {
		t.Fatal(err)
	}
	localRoot := t.TempDir()
	state, err := syncclient.OpenState(localRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := os.WriteFile(filepath.Join(localRoot, "local.txt"), []byte("offline file"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := syncclient.Sync(context.Background(), localRoot, state, remote)
	if err != nil || report.Uploaded != 1 {
		t.Fatalf("upload sync=%+v err=%v", report, err)
	}
	entry, err := db.FileEntry(1, "local.txt")
	if err != nil || entry.Version != 1 {
		t.Fatalf("committed upload=%+v err=%v", entry, err)
	}
	if err := remote.UploadNew(context.Background(), "remote.txt", strings.NewReader("server file")); err != nil {
		t.Fatal(err)
	}
	report, err = syncclient.Sync(context.Background(), localRoot, state, remote)
	if err != nil || report.Downloaded != 1 {
		t.Fatalf("download sync=%+v err=%v", report, err)
	}
	data, err := os.ReadFile(filepath.Join(localRoot, "remote.txt"))
	if err != nil || string(data) != "server file" {
		t.Fatalf("downloaded data=%q err=%v", data, err)
	}
}
