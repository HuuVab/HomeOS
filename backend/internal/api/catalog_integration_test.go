package api

import (
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func TestCatalogHTTPRoundTripPostgres(t *testing.T) {
	connectionURL := os.Getenv("HOMEOS_TEST_POSTGRES_URL")
	if connectionURL == "" {
		t.Skip("HOMEOS_TEST_POSTGRES_URL is not configured")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "homedrive_catalog_http_" + hex.EncodeToString(random[:])
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
	parameters := parsed.Query()
	parameters.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = parameters.Encode()
	db, err := store.OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hash, err := auth.HashPassword("catalog-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("catalog_http", hash); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateBlobStorage(1); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "files")
	files, err := drive.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	handler := NewHandler("", db, testCollector{}, files)
	call := func(method, resource, body, csrf string, cookie *http.Cookie, version string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, resource, strings.NewReader(body))
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		if version != "" {
			request.Header.Set("X-Source-Version", version)
		}
		if method == http.MethodPost && !strings.Contains(resource, "/complete") {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	login := call("POST", "/api/v1/auth/login", `{"username":"catalog_http","password":"catalog-test-password"}`, "", nil, "")
	if login.Code != 200 {
		t.Fatalf("login %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if result := call("POST", "/api/v1/drive/folders", `{"path":"docs"}`, session.CSRF, cookie, ""); result.Code != 201 {
		t.Fatalf("folder %d: %s", result.Code, result.Body.String())
	}
	if result := call("PUT", "/api/v1/drive/files?path=docs%2Fnote.txt", "original", session.CSRF, cookie, ""); result.Code != 201 {
		t.Fatalf("upload %d: %s", result.Code, result.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("mutable live file exists: %v", err)
	}
	if result := call("GET", "/api/v1/drive/files?path=docs%2Fnote.txt", "", "", cookie, ""); result.Code != 200 || result.Body.String() != "original" {
		t.Fatalf("download %d: %q", result.Code, result.Body.String())
	}
	if result := call("PUT", "/api/v1/drive/files?path=docs%2Fnote.txt", "updated", session.CSRF, cookie, "1"); result.Code != 204 {
		t.Fatalf("replace %d: %s", result.Code, result.Body.String())
	}
	if result := call("PUT", "/api/v1/drive/files?path=docs%2Fnote.txt", "stale", session.CSRF, cookie, "1"); result.Code != 412 {
		t.Fatalf("stale replace %d: %s", result.Code, result.Body.String())
	}
	if result := call("POST", "/api/v1/drive/trash", `{"path":"docs/note.txt"}`, session.CSRF, cookie, "2"); result.Code != 204 {
		t.Fatalf("trash %d: %s", result.Code, result.Body.String())
	}
	trash, err := db.TrashEntries(1)
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash entries %+v: %v", trash, err)
	}
	if result := call("POST", "/api/v1/drive/trash/"+trash[0].ID+"/restore", "", session.CSRF, cookie, ""); result.Code != 204 {
		t.Fatalf("restore %d: %s", result.Code, result.Body.String())
	}
	if result := call("GET", "/api/v1/drive/files?path=docs%2Fnote.txt", "", "", cookie, ""); result.Code != 200 || result.Body.String() != "updated" {
		t.Fatalf("restored download %d: %q", result.Code, result.Body.String())
	}
	chunk := []byte("resumable")
	digest := sha256.Sum256(chunk)
	fingerprint := hex.EncodeToString(digest[:])
	started := call("POST", "/api/v1/drive/uploads", `{"path":"docs/chunk.txt","size":9,"fingerprint":"`+fingerprint+`"}`, session.CSRF, cookie, "")
	if started.Code != 200 {
		t.Fatalf("start upload %d: %s", started.Code, started.Body.String())
	}
	var upload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &upload); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/drive/uploads/"+upload.ID, strings.NewReader(string(chunk)))
	request.Header.Set("X-CSRF-Token", session.CSRF)
	request.Header.Set("X-Upload-Offset", "0")
	request.Header.Set("X-Chunk-SHA256", fingerprint)
	request.AddCookie(cookie)
	patched := httptest.NewRecorder()
	handler.ServeHTTP(patched, request)
	if patched.Code != 200 {
		t.Fatalf("append upload %d: %s", patched.Code, patched.Body.String())
	}
	if completed := call("POST", "/api/v1/drive/uploads/"+upload.ID+"/complete", "", session.CSRF, cookie, ""); completed.Code != 204 {
		t.Fatalf("complete upload %d: %s", completed.Code, completed.Body.String())
	}
	if result := call("GET", "/api/v1/drive/files?path=docs%2Fchunk.txt", "", "", cookie, ""); result.Code != 200 || result.Body.String() != string(chunk) {
		t.Fatalf("resumable download %d: %q", result.Code, result.Body.String())
	}
	committed, err := db.FileEntry(1, "docs/chunk.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".homeos-versions", committed.Checksum)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "chunk.txt"), []byte("uncommitted"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := call("GET", "/api/v1/drive/files?path=docs%2Fchunk.txt", "", "", cookie, ""); result.Code != 500 || strings.Contains(result.Body.String(), "uncommitted") {
		t.Fatalf("missing blob read did not fail closed: %d %q", result.Code, result.Body.String())
	}
}
