package drive

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestCatalogStageReplaysUncommittedChunk(t *testing.T) {
	files, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	id := strings.Repeat("a", 32)
	if err := files.CreateCatalogStage(id); err != nil {
		t.Fatal(err)
	}
	checksum := func(value string) string {
		digest := sha256.Sum256([]byte(value))
		return hex.EncodeToString(digest[:])
	}
	if offset, err := files.AppendCatalogStage(id, 0, 4, strings.NewReader("old!"), checksum("old!")); err != nil || offset != 4 {
		t.Fatalf("first chunk %d: %v", offset, err)
	}
	// The first file Sync succeeded, but the hypothetical PostgreSQL offset
	// update did not. Replaying offset zero must discard those staged bytes.
	if offset, err := files.AppendCatalogStage(id, 0, 4, strings.NewReader("new!"), checksum("new!")); err != nil || offset != 4 {
		t.Fatalf("replay %d: %v", offset, err)
	}
	got, err := files.FinishCatalogStage(id, 4, checksum("new!"))
	if err != nil || got != checksum("new!") {
		t.Fatalf("finish %q: %v", got, err)
	}
	blob, err := files.OpenBlob(got, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Close()
	bytes, err := io.ReadAll(blob)
	if err != nil || string(bytes) != "new!" {
		t.Fatalf("blob %q: %v", bytes, err)
	}
}
