package metadata

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInProcessServiceRejectsStandaloneMode(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := drive.New(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	client, close, err := NewInProcessClient(db, files)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	_, err = client.ListFiles(context.Background(), &metadatapb.ListFilesRequest{OwnerId: 1})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("standalone-mode RPC = %v, want FailedPrecondition", err)
	}
}
