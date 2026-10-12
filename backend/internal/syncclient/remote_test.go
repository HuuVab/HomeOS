package syncclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestResumableUploadContinuesAtServerOffset(t *testing.T) {
	contents := []byte(strings.Repeat("x", 5<<20))
	filename := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(filename, contents, 0600); err != nil {
		t.Fatal(err)
	}
	staged := append([]byte(nil), contents[:1<<20]...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/drive/uploads":
			if r.Header.Get("X-CSRF-Token") != "csrf" {
				t.Error("missing CSRF token")
			}
			_, _ = w.Write([]byte(`{"id":"session","offset":1048576}`))
		case "/api/v1/drive/uploads/session":
			if r.Header.Get("X-Upload-Offset") != strconv.Itoa(len(staged)) {
				t.Errorf("offset = %q", r.Header.Get("X-Upload-Offset"))
			}
			chunk, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			digest := sha256.Sum256(chunk)
			if r.Header.Get("X-Chunk-SHA256") != hex.EncodeToString(digest[:]) {
				t.Error("chunk digest mismatch")
			}
			staged = append(staged, chunk...)
			_, _ = fmt.Fprintf(w, `{"offset":%d}`, len(staged))
		case "/api/v1/drive/uploads/session/complete":
			if string(staged) != string(contents) {
				t.Error("completed bytes differ")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	remote, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote.csrf = "csrf"
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fullHash := sha256.Sum256(contents)
	if err := remote.UploadResumable(context.Background(), "large.bin", file, int64(len(contents)), hex.EncodeToString(fullHash[:])); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteLoginListingAndChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			if r.Method != http.MethodPost {
				t.Error("login method")
			}
			http.SetCookie(w, &http.Cookie{Name: "homeos_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"user":{"id":7},"csrfToken":"csrf"}`))
		case "/api/v1/drive":
			if _, err := r.Cookie("homeos_session"); err != nil {
				t.Error("missing session cookie")
			}
			if r.URL.Query().Get("path") == "docs" {
				_, _ = w.Write([]byte(`{"entries":[{"path":"docs/note.txt","kind":"file","size":4,"version":2}]}`))
			} else {
				_, _ = w.Write([]byte(`{"entries":[{"path":"docs","kind":"folder","version":1}]}`))
			}
		case "/api/v1/drive/changes":
			_, _ = w.Write([]byte(`{"events":[{"id":9,"path":"docs/note.txt","operation":"update","version":2}],"nextCursor":9}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(context.Background(), "test", "password"); err != nil || client.OwnerID() != 7 {
		t.Fatalf("login owner=%d err=%v", client.OwnerID(), err)
	}
	entries, err := client.ListAll(context.Background())
	if err != nil || len(entries) != 2 || entries["docs/note.txt"].Version != 2 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	changes, cursor, err := client.Changes(context.Background(), 0)
	if err != nil || cursor != 9 || len(changes) != 1 {
		t.Fatalf("changes=%+v cursor=%d err=%v", changes, cursor, err)
	}
}

func TestRemoteRejectsUnsafeLocationsAndPaths(t *testing.T) {
	for _, origin := range []string{"http://example.com", "http://127.0.0.1:8080/private", "http://user:password@127.0.0.1:8080"} {
		if _, err := NewRemote(origin); err == nil {
			t.Errorf("accepted unsafe origin %q", origin)
		}
	}
	for _, name := range []string{"../outside", "docs/../outside", "C:/secret", "docs\\note.txt", ".homeos-sync/state.sqlite"} {
		if validPath(name) {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
	if !validPath("docs/note.txt") || !strings.HasPrefix("docs/note.txt", "docs/") {
		t.Fatal("rejected normal path")
	}
}

func TestRemoteRejectsCaseCollidingFilesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows case-insensitive filesystem safeguard")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			_, _ = w.Write([]byte(`{"user":{"id":1},"csrfToken":"token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"entries":[{"path":"Readme.txt","kind":"file","version":1},{"path":"README.txt","kind":"file","version":1}]}`))
	}))
	defer server.Close()
	remote, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Login(context.Background(), "test", "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.ListAll(context.Background()); err == nil {
		t.Fatal("expected server case collision to be rejected")
	}
}

func TestRemoteDoesNotForwardPasswordAcrossRedirect(t *testing.T) {
	forwarded := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", other.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	remote, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Login(context.Background(), "test", "secret"); err == nil || forwarded {
		t.Fatalf("redirect login error=%v forwarded=%t", err, forwarded)
	}
}
