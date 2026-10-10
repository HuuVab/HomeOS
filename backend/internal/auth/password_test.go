package auth

import "testing"

func TestPasswordHashAndVerification(t *testing.T) {
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "a-long-test-password" || !VerifyPassword(hash, "a-long-test-password") {
		t.Fatal("password did not verify securely")
	}
	if VerifyPassword(hash, "incorrect-password") || VerifyPassword("invalid", "a-long-test-password") {
		t.Fatal("invalid password was accepted")
	}
}
