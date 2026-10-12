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
