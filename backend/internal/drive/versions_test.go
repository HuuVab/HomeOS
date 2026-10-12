package drive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSnapshotAndRestore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "files")
	files, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Upload("note.txt", strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	entries, err := files.AllEntries()
	if err != nil || len(entries) != 1 || len(entries[0].Checksum) != 64 {
		t.Fatalf("snapshot = %+v, %v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := files.RestoreVersion("note.txt", entries[0].Checksum, entries[0].Size); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(got) != "first" {
		t.Fatalf("restored = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, versionsDir, entries[0].Checksum), []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := files.RestoreVersion("note.txt", entries[0].Checksum, entries[0].Size); err == nil {
		t.Fatal("corrupt snapshot was accepted")
	}
	got, err = os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(got) != "first" {
		t.Fatalf("live file changed after corrupt restore: %q, %v", got, err)
	}
}

func TestPruneVersionBlobsKeepsReferencedBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "files")
	files, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Upload("one.txt", strings.NewReader("one")); err != nil {
		t.Fatal(err)
	}
	one, err := files.SnapshotFile("one.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("two.txt", strings.NewReader("two")); err != nil {
		t.Fatal(err)
	}
	two, err := files.SnapshotFile("two.txt")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := files.PruneVersionBlobs(map[string]bool{one: true})
	if err != nil || removed != 1 {
		t.Fatalf("removed = %d, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, versionsDir, one)); err != nil {
		t.Fatalf("referenced blob disappeared: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, versionsDir, two)); !os.IsNotExist(err) {
		t.Fatalf("unreferenced blob remains: %v", err)
	}
}
