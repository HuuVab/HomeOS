package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusicFolderAndPlayback(t *testing.T) {
	handler, _ := testHandler(t)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "song.mp3"), []byte("sample audio bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/music?path=" + url.QueryEscape(folder)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous listing = %d", response.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"test_admin","password":"a-long-test-password"}`))
	login.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("login = %d", response.Code)
	}
	cookie := response.Result().Cookies()[0]
	call := func(target, rangeHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(cookie)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	response = call(path, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "song.mp3") || strings.Contains(response.Body.String(), "notes.txt") {
		t.Fatalf("listing = %d: %s", response.Code, response.Body.String())
	}
	child := filepath.Join(folder, "Albums")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	response = call("/api/v1/music/folders?path="+url.QueryEscape(folder), "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Albums") || strings.Contains(response.Body.String(), "notes.txt") {
		t.Fatalf("folder browser = %d: %s", response.Code, response.Body.String())
	}
	response = call("/api/v1/music/audio?path="+url.QueryEscape(folder)+"&name=song.mp3", "bytes=0-5")
	if response.Code != http.StatusPartialContent || response.Body.String() != "sample" {
		t.Fatalf("range = %d: %s", response.Code, response.Body.String())
	}
	response = call("/api/v1/music/audio?path="+url.QueryEscape(folder)+"&name=notes.txt", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("non-audio = %d", response.Code)
	}
}
