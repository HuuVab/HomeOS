package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/syncclient"
)

func TestInitScanAndStatusWithoutServer(t *testing.T) {
	root := t.TempDir()
	if err := run([]string{"init", root}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "offline.txt"), []byte("offline"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"scan", root}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"status", root}); err != nil {
		t.Fatal(err)
	}
	state, err := syncclient.OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	pending, err := state.Pending()
	if err != nil || len(pending) != 1 || pending[0].Path != "offline.txt" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestWatchRejectsFastPolling(t *testing.T) {
	if err := run([]string{"watch", t.TempDir(), "--interval", "1s"}); err == nil {
		t.Fatal("watch accepted a one-second polling interval")
	}
}
