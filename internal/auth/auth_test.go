package auth

import (
	"testing"
	"time"
)

func TestPasswordAndToken(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "secret123" {
		t.Fatal("must not store the raw password")
	}
	if !CheckPassword(hash, "secret123") {
		t.Fatal("correct password should match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password should not match")
	}

	token, err := IssueToken("test-secret", "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := ParseUserID("test-secret", token)
	if err != nil || id != "user-1" {
		t.Fatalf("parse: %v %q", err, id)
	}
	if _, err := ParseUserID("other-secret", token); err == nil {
		t.Fatal("wrong secret must fail")
	}
}
