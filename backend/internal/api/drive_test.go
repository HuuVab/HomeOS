package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomeDriveAccessAndRoundTrip(t *testing.T) {
	handler, _ := testHandler(t)
	call := func(method, path, body, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		if method == http.MethodPost {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if got := call("GET", "/api/v1/drive", "", "", nil).Code; got != 401 {
		t.Fatalf("anonymous list = %d", got)
	}
	login := call("POST", "/api/v1/auth/login", `{"username":"test_admin","password":"a-long-test-password"}`, "", nil)
	if login.Code != 200 {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if got := call("POST", "/api/v1/drive/folders", `{"path":"docs"}`, "", cookie).Code; got != 403 {
		t.Fatalf("missing CSRF = %d", got)
	}
	if got := call("POST", "/api/v1/drive/folders", `{"path":"docs"}`, session.CSRFToken, cookie).Code; got != 201 {
		t.Fatalf("create folder = %d", got)
	}
	if response := call("PUT", "/api/v1/drive/files?path=docs%2Fnote.txt", "hello", session.CSRFToken, cookie); response.Code != 201 {
		t.Fatalf("upload = %d: %s", response.Code, response.Body.String())
	}
	if got := call("PUT", "/api/v1/drive/files?path=docs%2Fnote.txt", "replace", session.CSRFToken, cookie).Code; got != 409 {
		t.Fatalf("overwrite = %d", got)
	}
	if got := call("PUT", "/api/v1/drive/files?path=..%2Fescape.txt", "bad", session.CSRFToken, cookie).Code; got != 400 {
		t.Fatalf("traversal = %d", got)
	}
	listing := call("GET", "/api/v1/drive?path=docs", "", "", cookie)
	if listing.Code != 200 || !strings.Contains(listing.Body.String(), `"name":"note.txt"`) {
		t.Fatalf("list = %d: %s", listing.Code, listing.Body.String())
	}
	download := call("GET", "/api/v1/drive/files?path=docs%2Fnote.txt", "", "", cookie)
	if download.Code != 200 || download.Body.String() != "hello" {
		t.Fatalf("download = %d: %q", download.Code, download.Body.String())
	}
}
