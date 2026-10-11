package drive

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
