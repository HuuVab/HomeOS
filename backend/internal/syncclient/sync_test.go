package syncclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSyncPullsUploadsAndPreservesEditConflict(t *testing.T) {
	var mu sync.Mutex
	entries := map[string]RemoteEntry{
		"docs":            {Path: "docs", Kind: "folder", Version: 1},
		"docs/remote.txt": {Path: "docs/remote.txt", Kind: "file", Size: 6, Version: 1},
	}
	contents := map[string]string{"docs/remote.txt": "remote"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "homeos_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"user":{"id":1},"csrfToken":"token"}`))
		case "/api/v1/drive":
			folder := r.URL.Query().Get("path")
			page := make([]RemoteEntry, 0)
			for _, item := range entries {
				if path.Dir(item.Path) == folder || folder == "" && !strings.Contains(item.Path, "/") {
					page = append(page, item)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": page})
		case "/api/v1/drive/files":
			name := r.URL.Query().Get("path")
			if r.Method == http.MethodGet {
				if _, exists := entries[name]; !exists {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(contents[name]))
				return
			}
			if r.Header.Get("X-CSRF-Token") != "token" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if _, exists := entries[name]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			body, _ := io.ReadAll(r.Body)
			contents[name] = string(body)
			entries[name] = RemoteEntry{Path: name, Kind: "file", Version: 1, Size: int64(len(body))}
			w.WriteHeader(http.StatusCreated)
		case "/api/v1/drive/changes":
			_, _ = w.Write([]byte(`{"events":[],"nextCursor":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	state, err := OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	remote, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Login(context.Background(), "test", "password"); err != nil {
		t.Fatal(err)
	}
	report, err := Sync(context.Background(), root, state, remote)
	if err != nil || report.Downloaded != 1 || report.Folders != 1 {
		t.Fatalf("initial sync=%+v err=%v", report, err)
	}
	remoteFile := filepath.Join(root, "docs", "remote.txt")
	if data, err := os.ReadFile(remoteFile); err != nil || string(data) != "remote" {
		t.Fatalf("download=%q err=%v", data, err)
	}
	localFile := filepath.Join(root, "docs", "local.txt")
	if err := os.WriteFile(localFile, []byte("offline addition"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err = Sync(context.Background(), root, state, remote)
	if err != nil || report.Uploaded != 1 {
		pending, _ := state.Pending()
		t.Fatalf("upload sync=%+v pending=%+v err=%v", report, pending, err)
	}
	mu.Lock()
	uploaded := contents["docs/local.txt"]
	mu.Unlock()
	if uploaded != "offline addition" {
		t.Fatalf("uploaded content=%q", uploaded)
	}
	if err := os.WriteFile(remoteFile, []byte("local edit"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err = Sync(context.Background(), root, state, remote)
	if err != nil || report.Conflicts != 1 {
		t.Fatalf("edit conflict sync=%+v err=%v", report, err)
	}
	conflicts, err := state.Conflicts()
	if err != nil || len(conflicts) != 1 || conflicts[0].Path != "docs/remote.txt" {
		t.Fatalf("conflicts=%+v err=%v", conflicts, err)
	}
	mu.Lock()
	unchanged := contents["docs/remote.txt"]
	mu.Unlock()
	if unchanged != "remote" {
		t.Fatalf("remote file was overwritten: %q", unchanged)
	}
}
