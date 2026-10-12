package redisaccel

import (
	"context"
	"testing"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/alicebob/miniredis/v2"
)

func testLayer(t *testing.T) *Layer {
	t.Helper()
	server := miniredis.RunT(t)
	layer, err := Open(context.Background(), "redis://"+server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = layer.Close() })
	return layer
}

func TestLoginLimitIsPerAddress(t *testing.T) {
	layer := testLayer(t)
	for attempt := 1; attempt <= 11; attempt++ {
		allowed, err := layer.AllowLogin(context.Background(), "127.0.0.1:1234")
		if err != nil || allowed != (attempt <= 10) {
			t.Fatalf("attempt %d: allowed=%t err=%v", attempt, allowed, err)
		}
	}
	allowed, err := layer.AllowLogin(context.Background(), "127.0.0.2:1234")
	if err != nil || !allowed {
		t.Fatalf("separate address: allowed=%t err=%v", allowed, err)
	}
}

func TestListCacheIsOwnerScopedAndInvalidated(t *testing.T) {
	layer := testLayer(t)
	ctx := context.Background()
	entries := []drive.Entry{{Path: "docs", Name: "docs", Kind: "folder", Version: 3}}
	generation, err := layer.Generation(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := layer.StoreFiles(ctx, 1, "", generation, entries); err != nil {
		t.Fatal(err)
	}
	got, hit, _, err := layer.ListFiles(ctx, 1, "")
	if err != nil || !hit || len(got) != 1 || got[0].Version != 3 {
		t.Fatalf("cached entries=%+v hit=%t err=%v", got, hit, err)
	}
	if _, hit, _, err := layer.ListFiles(ctx, 2, ""); err != nil || hit {
		t.Fatalf("other owner cache hit=%t err=%v", hit, err)
	}
	if err := layer.InvalidateOwner(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, hit, _, err := layer.ListFiles(ctx, 1, ""); err != nil || hit {
		t.Fatalf("stale cache hit=%t err=%v", hit, err)
	}
}
