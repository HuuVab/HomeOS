package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadata"
	"github.com/HuuVab/HomeOS/backend/internal/redisaccel"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"github.com/alicebob/miniredis/v2"
)

func TestPostgresGatewayUsesMetadataRPC(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_gateway_" + hex.EncodeToString(random[:])
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
	hash, err := auth.HashPassword("gateway-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("gateway_admin", hash); err != nil {
		t.Fatal(err)
	}
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	redisServer := miniredis.RunT(t)
	cache, err := redisaccel.Open(context.Background(), "redis://"+redisServer.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	client, close, err := metadata.NewInProcessClient(db, files, cache)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	handler := NewHandlerWithRedis("", db, testCollector{}, files, cache, client)
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"gateway_admin","password":"gateway-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, request)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/drive/folders", strings.NewReader(`{"path":"grpc-folder"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(cookie)
	handler.ServeHTTP(create, request)
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body.String())
	}
	listed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/drive", nil)
	request.AddCookie(cookie)
	handler.ServeHTTP(listed, request)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"path":"grpc-folder"`) {
		t.Fatalf("list = %d: %s", listed.Code, listed.Body.String())
	}
	listed = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/drive?path=grpc-folder", nil)
	request.AddCookie(cookie)
	handler.ServeHTTP(listed, request)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"entries":[]`) {
		t.Fatalf("empty folder list = %d: %s", listed.Code, listed.Body.String())
	}
	upload := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/api/v1/drive/files?path=grpc-folder%2Fnote.txt", strings.NewReader("cached listing must refresh"))
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(cookie)
	handler.ServeHTTP(upload, request)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload = %d: %s", upload.Code, upload.Body.String())
	}
	listed = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/drive?path=grpc-folder", nil)
	request.AddCookie(cookie)
	handler.ServeHTTP(listed, request)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"path":"grpc-folder/note.txt"`) {
		t.Fatalf("list after upload = %d: %s", listed.Code, listed.Body.String())
	}
}
