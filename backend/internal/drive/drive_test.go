package drive

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestResumableUploadSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "files")
	files, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("a", 64)
	session, err := files.StartUpload("large.txt", 11, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if offset, err := files.AppendChunk(session.ID, 0, strings.NewReader("hello ")); err != nil || offset != 6 {
		t.Fatalf("first chunk: %d, %v", offset, err)
	}
	if entries, err := files.List(""); err != nil || len(entries) != 0 {
		t.Fatalf("unfinished file listed: %v, %v", entries, err)
	}
	if _, err := files.Open("large.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unfinished file opened: %v", err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if pending := files.PendingUploads(); len(pending) != 1 || pending[0].Offset != 6 {
		t.Fatalf("restored pending offset: %+v", pending)
	}
	resumed, err := files.StartUpload("large.txt", 11, fingerprint)
	if err != nil || resumed.ID != session.ID || resumed.Offset != 6 {
		t.Fatalf("resume: %+v, %v", resumed, err)
	}
	if _, err := files.StartUpload("large.txt", 11, strings.Repeat("b", 64)); !errors.Is(err, os.ErrExist) {
		t.Fatalf("different file resumed: %v", err)
	}
	if _, err := files.AppendChunk(session.ID, 0, strings.NewReader("bad")); !errors.Is(err, ErrOffset) {
		t.Fatalf("wrong offset: %v", err)
	}
	if err := files.FinishUpload(session.ID); !errors.Is(err, ErrOffset) {
		t.Fatalf("early finish: %v", err)
	}
	if offset, err := files.AppendChunk(session.ID, 6, strings.NewReader("world")); err != nil || offset != 11 {
		t.Fatalf("second chunk: %d, %v", offset, err)
	}
	if err := files.FinishUpload(session.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := files.List("")
	if err != nil || len(entries) != 1 || entries[0].Name != "large.txt" {
		t.Fatalf("completed file not listed: %v, %v", entries, err)
	}
	file, err := files.Open("large.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil || string(content) != "hello world" {
		t.Fatalf("content: %q, %v", content, err)
	}
}

func TestChunkFailureKeepsOffset(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	session, err := files.StartUpload("sample", 10, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.AppendChunk(session.ID, 0, io.MultiReader(strings.NewReader("partial"), failingReader{})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("failed chunk: %v", err)
	}
	resumed, err := files.StartUpload("sample", 10, strings.Repeat("c", 64))
	if err != nil || resumed.Offset != 0 {
		t.Fatalf("offset after failure: %+v, %v", resumed, err)
	}
	if _, err := files.AppendChunk(session.ID, 0, strings.NewReader("12345678901")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized chunk: %v", err)
	}
	resumed, err = files.StartUpload("sample", 10, strings.Repeat("c", 64))
	if err != nil || resumed.Offset != 0 {
		t.Fatalf("offset after oversized chunk: %+v, %v", resumed, err)
	}
}

func TestVerifiedChunkRejectsMismatchWithoutAdvancing(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	session, err := files.StartUpload("verified.txt", 5, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.AppendChunkVerified(session.ID, 0, strings.NewReader("hello"), strings.Repeat("0", 64)); !errors.Is(err, ErrChecksum) {
		t.Fatalf("mismatched checksum: %v", err)
	}
	if pending := files.PendingUploads(); len(pending) != 1 || pending[0].Offset != 0 {
		t.Fatalf("offset after mismatch: %+v", pending)
	}
	if offset, err := files.AppendChunkVerified(session.ID, 0, strings.NewReader("hello"), "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"); err != nil || offset != 5 {
		t.Fatalf("verified chunk: %d, %v", offset, err)
	}
}

func TestMoveTrashRestore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "files")
	files, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.CreateFolder("docs"); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("docs/note.txt", strings.NewReader("important")); err != nil {
		t.Fatal(err)
	}
	if err := files.Move("docs/note.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := files.Move("docs", "docs/child"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("move folder into itself: %v", err)
	}
	if err := files.Trash("renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Open("renamed.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trashed file visible: %v", err)
	}
	trash, err := files.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash: %v, %v", trash, err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Restore(trash[0].ID); err != nil {
		t.Fatal(err)
	}
	f, err := files.Open("renamed.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || string(content) != "important" {
		t.Fatalf("restored content: %q, %v", content, err)
	}
}

func TestTrashRetentionAndPermanentDeletion(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.CreateFolder("album"); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("album/photo.txt", strings.NewReader("content")); err != nil {
		t.Fatal(err)
	}
	if err := files.Trash("album"); err != nil {
		t.Fatal(err)
	}
	entries, err := files.ListTrash()
	if err != nil || len(entries) != 1 {
		t.Fatalf("trash: %v %v", entries, err)
	}
	if count, err := files.PurgeExpired(30*24*time.Hour, entries[0].DeletedAt.Add(29*24*time.Hour)); err != nil || count != 0 {
		t.Fatalf("early purge: %d %v", count, err)
	}
	if count, err := files.PurgeExpired(30*24*time.Hour, entries[0].DeletedAt.Add(31*24*time.Hour)); err != nil || count != 1 {
		t.Fatalf("expired purge: %d %v", count, err)
	}
	if err := files.Restore(entries[0].ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restored deleted item: %v", err)
	}
	if err := files.Upload("other.txt", strings.NewReader("content")); err != nil {
		t.Fatal(err)
	}
	if err := files.Trash("other.txt"); err != nil {
		t.Fatal(err)
	}
	entries, err = files.ListTrash()
	if err != nil || len(entries) != 1 {
		t.Fatalf("trash: %v %v", entries, err)
	}
	if err := files.DeleteForever(entries[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestStagedUploadAndQuota(t *testing.T) {
	root := filepath.Join(t.TempDir(), "files")
	files, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetLimits(10, 20); err != nil {
		t.Fatal(err)
	}
	if err := files.Upload("oversized.txt", strings.NewReader("12345678901")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("direct limit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "oversized.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized file visible: %v", err)
	}
	if _, err := files.StartUpload("too-big.txt", 11, strings.Repeat("a", 64)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("file limit: %v", err)
	}
	session, err := files.StartUpload("one.txt", 10, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.StartUpload("two.txt", 10, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := files.StartUpload("three.txt", 1, strings.Repeat("c", 64)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("reserved quota: %v", err)
	}
	if _, err := files.AppendChunk(session.ID, 0, strings.NewReader("1234567890")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "one.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unfinished file visible on disk: %v", err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.SetLimits(10, 20); err != nil {
		t.Fatal(err)
	}
	resumed, err := files.StartUpload("one.txt", 10, strings.Repeat("a", 64))
	if err != nil || resumed.Offset != 10 {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	if err := files.FinishUpload(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "one.txt")); err != nil {
		t.Fatalf("committed file missing: %v", err)
	}
	if err := files.Copy("one.txt", "copy.txt"); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("copy quota: %v", err)
	}
}
