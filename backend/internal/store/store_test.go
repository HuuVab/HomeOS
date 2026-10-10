package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/auth"
)

func TestAdminPersistsAndResetRevokesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homeos.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := auth.HashPassword("first-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("admin_test", firstHash); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin("another_admin", firstHash); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("duplicate bootstrap = %v, want ErrAdminExists", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.UserByUsername("admin_test")
	if err != nil || !auth.VerifyPassword(user.PasswordHash, "first-test-password") {
		t.Fatalf("administrator did not persist: %v", err)
	}
	if err := db.CreateSession(user.ID, "test-token-hash", "test-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	newHash, err := auth.HashPassword("second-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResetAdminPassword("admin_test", newHash); err != nil {
		t.Fatal(err)
	}
	user, err = db.UserByUsername("admin_test")
	if err != nil || !auth.VerifyPassword(user.PasswordHash, "second-test-password") || auth.VerifyPassword(user.PasswordHash, "first-test-password") {
		t.Fatalf("administrator password reset failed: %v", err)
	}
	if _, _, err := db.Session("test-token-hash"); err == nil {
		t.Fatal("password reset did not revoke existing session")
	}
}
