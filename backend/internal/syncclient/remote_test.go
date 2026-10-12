package syncclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

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
