package syncclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

func TestRemoteCatalogUsesChangeFeedAfterBootstrap(t *testing.T) {
	entries := map[string]RemoteEntry{"docs": {Path: "docs", Kind: "folder", Version: 1}, "docs/a.txt": {Path: "docs/a.txt", Kind: "file", Version: 1}}
	requests := 0
	events := []Change{{ID: 1, Path: "docs", Operation: "create"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/drive":
			requests++
			folder := r.URL.Query().Get("path")
			var children []RemoteEntry
			for _, entry := range entries {
				if path.Dir(entry.Path) == folder || folder == "" && !strings.Contains(entry.Path, "/") {
					children = append(children, entry)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": children})
		case "/api/v1/drive/changes":
			if r.URL.Query().Get("after") == "0" {
				_ = json.NewEncoder(w).Encode(map[string]any{"events": events, "nextCursor": 1})
			} else {
				_, _ = w.Write([]byte(`{"events":[],"nextCursor":1}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	state, err := OpenState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	remote, err := NewRemote(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote.owner = 1
	first, cursor, err := remoteCatalog(context.Background(), state, remote)
	if err != nil || cursor != 1 || len(first) != 2 {
		t.Fatalf("bootstrap: %v %d %v", err, cursor, first)
	}
	requests = 0
	second, cursor, err := remoteCatalog(context.Background(), state, remote)
	if err != nil || cursor != 1 || len(second) != 2 || requests != 0 {
		t.Fatalf("incremental no-op: %v %d %v requests=%d", err, cursor, second, requests)
	}
}
