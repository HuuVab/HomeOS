package drive

import (
	"archive/zip"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyAndFolderZIP(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.CreateFolder("docs"); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("docs/note.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if err := files.Copy("docs", "archive"); err != nil {
		t.Fatal(err)
	}
	copy, err := files.Open("archive/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(copy)
	copy.Close()
	if err != nil || string(content) != "hello" {
		t.Fatalf("copy: %q %v", content, err)
	}
	var output bytes.Buffer
	if err := files.WriteZip("archive", &output); err != nil {
		t.Fatal(err)
	}
	zipped, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zipped.File) != 2 || zipped.File[1].Name != "archive/note.txt" {
		t.Fatalf("zip entries: %v", zipped.File)
	}
}
