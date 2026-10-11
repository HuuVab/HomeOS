package drive

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestLargeUploadAndFailedUploadCleanup(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	const size = 101 << 20
	if err := files.Upload("large.bin", io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatal(err)
	}
	file, err := files.Open("large.bin")
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("uploaded size = %d, want %d", info.Size(), size)
	}
	if err := files.Upload("partial.bin", io.MultiReader(strings.NewReader("partial"), failingReader{})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("failed upload = %v", err)
	}
	if _, err := files.Open("partial.bin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file remains: %v", err)
	}
}

func TestPathsStayInsideRoot(t *testing.T) {
	parent := t.TempDir()
	files, err := New(filepath.Join(parent, "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	for _, path := range []string{"../escape", "/absolute", `C:\escape`, "nested/../escape", "CON.txt", "file.", "nested//file"} {
		if err := files.Upload(path, strings.NewReader("bad")); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("upload %q: got %v, want invalid path", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape file created: %v", err)
	}
	if err := files.CreateFolder("safe"); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("safe/hello.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("safe/hello.txt", strings.NewReader("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite: %v", err)
	}
	got, err := files.Open("safe/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	got.Close()
}
