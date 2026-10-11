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

func TestSearchAcrossFolders(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.CreateFolder("docs"); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("docs/report.pdf", strings.NewReader("pdf")); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("notes.txt", strings.NewReader("text")); err != nil {
		t.Fatal(err)
	}
	results, err := files.Search("REPORT")
	if err != nil || len(results) != 1 || results[0].Path != "docs/report.pdf" {
		t.Fatalf("search: %v %v", results, err)
	}
	preview, kind, err := files.OpenPreview("docs/report.pdf")
	if err != nil || kind != "application/pdf" {
		t.Fatalf("preview: %s %v", kind, err)
	}
	preview.Close()
	if _, _, err := files.OpenPreview("notes.txt"); err == nil {
		t.Fatal("unexpected text preview")
	}
}
