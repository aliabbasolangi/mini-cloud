package auth

import "testing"

func TestHashCodeBindsEmailAndPurpose(t *testing.T) {
	a := HashCode("secret", "Ali@example.com", "register", "123456")
	b := HashCode("secret", "ali@example.com", "register", "123456")
	if a != b {
		t.Fatal("same email should hash the same after lowercasing")
	}
	if CheckCode("secret", "ali@example.com", "reset", "123456", a) {
		t.Fatal("a register hash must not work for reset")
	}
	if !CheckCode("secret", "ali@example.com", "register", "123456", a) {
		t.Fatal("expected a match")
	}
	if CheckCode("secret", "ali@example.com", "register", "000000", a) {
		t.Fatal("wrong code should fail")
	}
}

func TestRandomCodeIsSixDigits(t *testing.T) {
	code, err := RandomCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("len %d", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit %q", code)
		}
	}
}
