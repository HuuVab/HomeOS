package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

func testHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "homeos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hash, err := auth.HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("test_admin", hash); err != nil {
		t.Fatal(err)
	}
	return NewHandler("", db), db
}

func TestAuthenticationAndStatus(t *testing.T) {
	handler, _ := testHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"test_admin","password":"a-long-test-password"}`))
	login.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected login cookie: %+v", cookies)
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.CSRFToken == "" {
		t.Fatal("missing CSRF token")
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d", response.Code)
	}
	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "healthy" || len(payload.Metrics) != 2 {
		t.Fatalf("unexpected status response: %+v", payload)
	}
	for _, sample := range payload.Metrics {
		if sample.Scope != "application" || sample.State != "available" || sample.SampledAt.IsZero() {
			t.Errorf("invalid metric: %+v", sample)
		}
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, logout)
	if response.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF = %d, want 403", response.Code)
	}
	logout.Header.Set("X-CSRF-Token", session.CSRFToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, logout)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d, want 401", response.Code)
	}
}

func TestHealthReadinessFollowsDatabase(t *testing.T) {
	handler, db := testHandler(t)
	for _, path := range []string{"/api/v1/health/live", "/api/v1/health/ready"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("%s returned %d", path, response.Code)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness with closed database = %d, want 503", response.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	handler, _ := testHandler(t)
	for attempt := 0; attempt < 11; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"test_admin","password":"wrong-password"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if attempt == 10 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, want)
		}
	}
}
