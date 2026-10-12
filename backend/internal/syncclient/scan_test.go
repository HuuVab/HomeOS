package syncclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanQueuesOfflineCreateUpdateAndDelete(t *testing.T) {
	root := t.TempDir()
	state, err := OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "docs", "note.txt")
	if err := os.WriteFile(filename, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := ScanLocal(root, state)
	if err != nil || len(local) != 2 {
		t.Fatalf("initial scan=%+v err=%v", local, err)
	}
	pending, err := state.Pending()
	if err != nil || len(pending) != 2 || pending[0].Operation != "create" || pending[1].Operation != "create" {
		t.Fatalf("initial queue=%+v err=%v", pending, err)
	}
	if err := state.SaveBaseline(Baseline{Path: "docs", Kind: "folder", RemoteVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveBaseline(Baseline{Path: "docs/note.txt", Kind: "file", RemoteVersion: 2, LocalHash: local["docs/note.txt"].Hash}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanLocal(root, state); err != nil {
		t.Fatal(err)
	}
	pending, err = state.Pending()
	if err != nil || len(pending) != 1 || pending[0].Operation != "update" || pending[0].BaseVersion != 2 {
		t.Fatalf("update queue=%+v err=%v", pending, err)
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanLocal(root, state); err != nil {
		t.Fatal(err)
	}
	pending, err = state.Pending()
	if err != nil || len(pending) != 1 || pending[0].Operation != "delete" || pending[0].BaseVersion != 2 {
		t.Fatalf("delete queue=%+v err=%v", pending, err)
	}
}

func TestScanRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	state, err := OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := os.Symlink(filepath.Join(root, stateDir), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := ScanLocal(root, state); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestScanCacheTracksMetadataAndPrunesDeletes(t *testing.T) {
	root := t.TempDir()
	state, err := OpenState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	name := filepath.Join(root, "note.txt")
	if err := os.WriteFile(name, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ScanLocal(root, state)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := state.ScanCache()
	if err != nil || cache["note.txt"].Hash != first["note.txt"].Hash {
		t.Fatalf("cache %+v: %v", cache, err)
	}
	if err := os.WriteFile(name, []byte("a different length"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := ScanLocal(root, state)
	if err != nil || second["note.txt"].Hash == first["note.txt"].Hash {
		t.Fatalf("changed scan %+v: %v", second, err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanLocal(root, state); err != nil {
		t.Fatal(err)
	}
	cache, err = state.ScanCache()
	if err != nil || len(cache) != 0 {
		t.Fatalf("stale cache %+v: %v", cache, err)
	}
}
